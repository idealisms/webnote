package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"cloud.google.com/go/datastore"
)

// Revisions expire (by a TTL policy on time_to_delete) after 2 years.
const notesLifetime = 365 * 2 * 24 * time.Hour

// Workspace is a Workspace entity. Its key name is workspaceKeyID(Name).
type Workspace struct {
	Key         *datastore.Key `datastore:"-"`
	Name        string         `datastore:"name"`
	NextNoteNum int64          `datastore:"nextNoteNum"`
	Time        time.Time      `datastore:"time"`
}

// Notes is a Notes entity: one saved revision of a workspace.
type Notes struct {
	WorkspaceKey *datastore.Key `datastore:"workspaceKey"`
	Time         time.Time      `datastore:"time"`
	// zlib-compressed JSON. See decodeNotes() and encodeNotes().
	NotesJSONArray []byte    `datastore:"notesJsonArray,noindex"`
	TimeToDelete   time.Time `datastore:"time_to_delete"`
}

func workspaceKey(name string) *datastore.Key {
	return datastore.NameKey("Workspace", workspaceKeyID(name), nil)
}

// ignoreFieldMismatch ignores properties that the structs don't have, which
// ndb ignores too.
func ignoreFieldMismatch(err error) error {
	var mismatch *datastore.ErrFieldMismatch
	if errors.As(err, &mismatch) {
		return nil
	}
	return err
}

// getWorkspace returns the workspace with the given name, or nil if there
// isn't one.
func getWorkspace(ctx context.Context, client *datastore.Client, name string) (*Workspace, error) {
	w := &Workspace{Key: workspaceKey(name)}
	err := ignoreFieldMismatch(client.Get(ctx, w.Key, w))
	if errors.Is(err, datastore.ErrNoSuchEntity) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return w, nil
}

// notesList returns the notes of the revision saved at the workspace's latest
// time, or at loadTime if it's set and valid: exactly that time if it's
// epochMicros(), or within that second if it's US/Eastern (see
// parseLoadTime()). Revisions used to be whole seconds; now they're exact, so
// a US/Eastern time from a page opened before that matches the latest
// revision saved in its second. The notes are a JSON value, normally an
// array of objects; they're [] if there's no such revision.
func (w *Workspace) notesList(ctx context.Context, client *datastore.Client, loadTime string) (json.RawMessage, error) {
	q := datastore.NewQuery("Notes").FilterField("workspaceKey", "=", w.Key).Limit(1)
	if t, ok := parseEpochMicros(loadTime); ok {
		q = q.FilterField("time", "=", t)
	} else if t, ok := parseLoadTime(loadTime); ok {
		q = q.FilterField("time", ">=", t).FilterField("time", "<", t.Add(time.Second)).Order("-time")
	} else {
		q = q.FilterField("time", "=", w.Time)
	}
	var notes []Notes
	if _, err := client.GetAll(ctx, q, &notes); ignoreFieldMismatch(err) != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return json.RawMessage("[]"), nil
	}
	return decodeNotes(notes[0].NotesJSONArray)
}

// notesTimes returns the times of a workspace's revisions, newest first.
func notesTimes(ctx context.Context, client *datastore.Client, key *datastore.Key, offset, limit int) ([]time.Time, error) {
	q := datastore.NewQuery("Notes").
		FilterField("workspaceKey", "=", key).
		Order("-time").
		Project("time").
		Offset(offset).
		Limit(limit)
	var notes []Notes
	if _, err := client.GetAll(ctx, q, &notes); err != nil {
		return nil, err
	}
	times := make([]time.Time, len(notes))
	for i, n := range notes {
		times[i] = n.Time
	}
	return times, nil
}

// errTransactionFailed is when a save's transaction fails after retries, like
// ndb's TransactionFailedError.
var errTransactionFailed = errors.New("transaction failed")

// errConflict is when someone else saved the workspace after the client
// loaded it. saveWorkspace returns it with the stored workspace.
var errConflict = errors.New("saved since loaded")

// saveWorkspace saves a revision of a workspace, creating the workspace if
// needed, and returns the workspace. Times are exact (to Datastore's
// microseconds), so the time in the response is the stored one.
//
// loaded is the workspace time the client's notes are based on, in
// microseconds since the epoch (0 for a workspace that didn't exist). If the
// stored workspace is newer, nothing is written and saveWorkspace returns the
// stored workspace and errConflict. The check is inside the transaction, so a
// concurrent save forces a retry that sees its time. A negative loaded skips
// the check, for clients that don't send it.
func saveWorkspace(ctx context.Context, client *datastore.Client, name string, nextNoteNum int64, notes []note, loaded int64, now func() time.Time) (*Workspace, error) {
	blob, err := encodeNotes(notes)
	if err != nil {
		return nil, err
	}
	var w *Workspace
	// ndb.transaction(retries=5) makes up to 6 attempts.
	_, err = client.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		now := now().UTC().Truncate(time.Microsecond)
		w = &Workspace{Key: workspaceKey(name)}
		err := ignoreFieldMismatch(tx.Get(w.Key, w))
		if errors.Is(err, datastore.ErrNoSuchEntity) {
			w.Name = canonicalWsName(name)
		} else if err != nil {
			return err
		} else if loaded >= 0 && w.Time.UnixMicro() > loaded {
			return errConflict
		}
		w.Time = now
		w.NextNoteNum = nextNoteNum
		if _, err := tx.Put(w.Key, w); err != nil {
			return err
		}
		_, err = tx.Put(datastore.IncompleteKey("Notes", nil), &Notes{
			WorkspaceKey:   w.Key,
			Time:           now,
			NotesJSONArray: blob,
			TimeToDelete:   now.Add(notesLifetime),
		})
		return err
	}, datastore.MaxAttempts(6))
	if errors.Is(err, datastore.ErrConcurrentTransaction) {
		return nil, fmt.Errorf("%w: %v", errTransactionFailed, err)
	}
	return w, err
}

// A note is a note's properties in order, as parsed from save.py's XML.
type note []noteProperty

type noteProperty struct{ Key, Value string }

// encodeNotes returns the notesJsonArray blob for a list of notes: zlib
// compression of the same JSON that ndb's JsonProperty writes, which is
// json.dumps(separators=(',', ':')), with non-ASCII characters escaped.
func encodeNotes(notes []note) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(notesJSON(notes)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func notesJSON(notes []note) []byte {
	var b strings.Builder
	b.WriteByte('[')
	for i, n := range notes {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('{')
		for j, p := range n {
			if j > 0 {
				b.WriteByte(',')
			}
			writePyJSONString(&b, []rune(p.Key))
			b.WriteByte(':')
			writePyJSONString(&b, []rune(p.Value))
		}
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return []byte(b.String())
}

// writePyJSONString writes a JSON string the way Python's json.dumps() does
// with ensure_ascii: everything outside ' '-'~' is escaped, as \uXXXX UTF-16
// code units in lowercase hex unless it has a short escape.
func writePyJSONString(b *strings.Builder, s []rune) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r >= ' ' && r <= '~' {
				b.WriteRune(r)
				continue
			}
			units := []uint16{uint16(r)}
			if r > 0xffff {
				r1, r2 := utf16.EncodeRune(r)
				units = []uint16{uint16(r1), uint16(r2)}
			}
			for _, unit := range units {
				b.WriteString(`\u`)
				h := strconv.FormatUint(uint64(unit), 16)
				b.WriteString(strings.Repeat("0", 4-len(h)) + h)
			}
		}
	}
	b.WriteByte('"')
}

var errNotesNotJSON = errors.New("notesJsonArray isn't JSON")

// decodeNotes decodes a notesJsonArray blob like get_notes_list() in
// models.py, returning a JSON value.
//
// Revisions are zlib-compressed JSON. ndb marks the ones it writes with
// meaning 22 (a compressed JsonProperty); older ones lost the marker. This
// client can't see the marker, so it follows Python's path for unmarked
// blobs, which gives the same result for every blob ndb writes.
func decodeNotes(blob []byte) (json.RawMessage, error) {
	if data, err := zlibDecompress(blob); err == nil && utf8.Valid(data) && json.Valid(data) {
		return data, nil
	}
	slog.Error("Failed to decode legacy notesJsonArray")
	// Then ndb's JsonProperty reads the blob itself, with json.loads() on
	// the raw bytes, which skips a UTF-8 BOM.
	data := bytes.TrimPrefix(blob, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		slog.Error("Failed to decode notesJsonArray")
		return json.RawMessage("[]"), nil
	}
	if !json.Valid(data) {
		return nil, errNotesNotJSON
	}
	return data, nil
}

func zlibDecompress(blob []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
