package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/datastore"
)

func date(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

func putWorkspace(t *testing.T, ctx context.Context, client *datastore.Client, name string, at time.Time, nextNoteNum int64) *Workspace {
	t.Helper()
	w := &Workspace{Key: workspaceKey(name), Name: canonicalWsName(name), NextNoteNum: nextNoteNum, Time: at}
	if _, err := client.Put(ctx, w.Key, w); err != nil {
		t.Fatal(err)
	}
	return w
}

func putNotes(t *testing.T, ctx context.Context, client *datastore.Client, w *Workspace, at time.Time, notesJSON string) {
	t.Helper()
	blob, err := zlibCompress([]byte(notesJSON))
	if err != nil {
		t.Fatal(err)
	}
	n := &Notes{WorkspaceKey: w.Key, Time: at, NotesJSONArray: blob, TimeToDelete: at.Add(notesLifetime)}
	if _, err := client.Put(ctx, datastore.IncompleteKey("Notes", nil), n); err != nil {
		t.Fatal(err)
	}
}

func zlibCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	err := zw.Close()
	return buf.Bytes(), err
}

// jsonEqual reports whether two JSON documents have the same value.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatalf("%v: %s", err, a)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return reflect.DeepEqual(va, vb)
}

func notesListOK(t *testing.T, ctx context.Context, client *datastore.Client, w *Workspace, loadTime string) string {
	t.Helper()
	notes, err := w.notesList(ctx, client, loadTime)
	if err != nil {
		t.Fatal(err)
	}
	return string(notes)
}

func TestGetWorkspace(t *testing.T) {
	ctx, client := newTestClient(t)
	putWorkspace(t, ctx, client, "tc%20testing", date(2017, 12, 31, 0, 0, 0), 3)

	w, err := getWorkspace(ctx, client, "tc%20testing")
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "tc%20testing" || w.NextNoteNum != 3 || !w.Time.Equal(date(2017, 12, 31, 0, 0, 0)) {
		t.Errorf("got %+v", w)
	}
	if w.Key.Kind != "Workspace" || w.Key.Name != workspaceKeyID("tc%20testing") {
		t.Errorf("key = %v", w.Key)
	}
	if w, err := getWorkspace(ctx, client, "not found"); w != nil || err != nil {
		t.Errorf("getWorkspace(not found) = %v, %v", w, err)
	}
}

func TestGetWorkspaceIgnoresBrowserEncoding(t *testing.T) {
	ctx, client := newTestClient(t)
	putWorkspace(t, ctx, client, "my-notes", date(2017, 12, 31, 0, 0, 0), 1)
	for _, name := range []string{"my-notes", "my%2Dnotes"} {
		w, err := getWorkspace(ctx, client, name)
		if err != nil || w == nil || w.Name != "my%2Dnotes" {
			t.Errorf("getWorkspace(%q) = %+v, %v", name, w, err)
		}
	}
	// A raw non-ASCII name (e.g. from a URL typed by hand).
	if w, err := getWorkspace(ctx, client, "Bogotá"); w != nil || err != nil {
		t.Errorf("getWorkspace(Bogotá) = %v, %v", w, err)
	}
}

func TestNotesListNoNotes(t *testing.T) {
	ctx, client := newTestClient(t)
	w := putWorkspace(t, ctx, client, "empty", date(2017, 12, 31, 0, 0, 0), 0)
	if got := notesListOK(t, ctx, client, w, ""); got != "[]" {
		t.Errorf("notesList = %s", got)
	}
}

func TestNotesListLatest(t *testing.T) {
	ctx, client := newTestClient(t)
	oldTime, newTime := date(2017, 12, 30, 0, 0, 0), date(2017, 12, 31, 0, 0, 0)
	w := putWorkspace(t, ctx, client, "ws", newTime, 0)
	putNotes(t, ctx, client, w, oldTime, `[{"id":"note0","text":"old"}]`)
	putNotes(t, ctx, client, w, newTime, `[{"id":"note0","text":"new"}]`)
	if got := notesListOK(t, ctx, client, w, ""); got != `[{"id":"note0","text":"new"}]` {
		t.Errorf("notesList = %s", got)
	}
}

func TestNotesListIgnoresOtherWorkspaces(t *testing.T) {
	// Regression test: two workspaces saved in the same second must not see
	// each other's notes.
	ctx, client := newTestClient(t)
	at := date(2017, 12, 31, 0, 0, 0)
	a := putWorkspace(t, ctx, client, "a", at, 0)
	b := putWorkspace(t, ctx, client, "b", at, 0)
	putNotes(t, ctx, client, a, at, `[{"text":"from a"}]`)
	putNotes(t, ctx, client, b, at, `[{"text":"from b"}]`)
	if got := notesListOK(t, ctx, client, a, ""); got != `[{"text":"from a"}]` {
		t.Errorf("notesList(a) = %s", got)
	}
	if got := notesListOK(t, ctx, client, b, ""); got != `[{"text":"from b"}]` {
		t.Errorf("notesList(b) = %s", got)
	}
	c := putWorkspace(t, ctx, client, "c", at, 0)
	if got := notesListOK(t, ctx, client, c, ""); got != "[]" {
		t.Errorf("notesList(c) = %s", got)
	}
}

func TestNotesListAtTime(t *testing.T) {
	// The time parameter is US/Eastern local time.
	ctx, client := newTestClient(t)
	oldTime, newTime := date(2017, 12, 30, 17, 0, 0), date(2017, 12, 31, 0, 0, 0)
	w := putWorkspace(t, ctx, client, "ws", newTime, 0)
	putNotes(t, ctx, client, w, oldTime, `[{"text":"old"}]`)
	putNotes(t, ctx, client, w, newTime, `[{"text":"new"}]`)
	for _, c := range []struct{ loadTime, want string }{
		{"2017-12-30 12:00:00", `[{"text":"old"}]`},
		{"2017-12-30 12:00:01", `[]`},
		// A bad time falls back to the latest revision.
		{"not a time", `[{"text":"new"}]`},
		// So does a time on a day pytz couldn't handle (a 500 in Python).
		{"9999-12-31 00:00:00", `[{"text":"new"}]`},
	} {
		if got := notesListOK(t, ctx, client, w, c.loadTime); got != c.want {
			t.Errorf("notesList(%q) = %s, want %s", c.loadTime, got, c.want)
		}
	}
}

func TestNotesListUnicode(t *testing.T) {
	ctx, client := newTestClient(t)
	at := date(2017, 12, 31, 0, 0, 0)
	w := putWorkspace(t, ctx, client, "ws", at, 0)
	blob, err := encodeNotes([]note{{{"id", "note0"}, {"text", "café ☃ \U0001f600"}}})
	if err != nil {
		t.Fatal(err)
	}
	putRawNotes(t, w, at, blob, false)
	got := notesListOK(t, ctx, client, w, "")
	if !jsonEqual(t, []byte(got), []byte(`[{"id":"note0","text":"café ☃ 😀"}]`)) {
		t.Errorf("notesList = %s", got)
	}
}

func TestNotesListReadsMeaning22(t *testing.T) {
	// Revisions written by ndb are marked with meaning 22; the client must
	// still read them as plain bytes.
	ctx, client := newTestClient(t)
	at := date(2017, 12, 31, 0, 0, 0)
	w := putWorkspace(t, ctx, client, "ws", at, 0)
	blob, err := encodeNotes([]note{{{"id", "note0"}}})
	if err != nil {
		t.Fatal(err)
	}
	putRawNotes(t, w, at, blob, true)
	if got := notesListOK(t, ctx, client, w, ""); got != `[{"id":"note0"}]` {
		t.Errorf("notesList = %s", got)
	}
}

func TestSaveWorkspace(t *testing.T) {
	ctx, client := newTestClient(t)
	before := time.Now().UTC().Truncate(time.Second)
	notes := []note{{{"id", "note0"}, {"text", "hello"}}}
	w, err := saveWorkspace(ctx, client, "my-notes", 2, notes, -1, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	// New workspaces get the canonical name; times are exact to Datastore's
	// microseconds.
	if w.Name != "my%2Dnotes" || w.NextNoteNum != 2 || w.Time.Before(before) ||
		w.Time.Nanosecond()%1000 != 0 || w.Key.Name != workspaceKeyID("my-notes") {
		t.Errorf("saveWorkspace = %+v", w)
	}
	stored, err := getWorkspace(ctx, client, "my%2Dnotes")
	if err != nil || !reflect.DeepEqual(stored, w) {
		t.Errorf("stored workspace = %+v, %v; want %+v", stored, err, w)
	}
	if got := notesListOK(t, ctx, client, w, ""); got != `[{"id":"note0","text":"hello"}]` {
		t.Errorf("notesList = %s", got)
	}
	// Loading by the time the page shows gets this revision.
	if got := notesListOK(t, ctx, client, w, localtimeStr(w.Time)); got != `[{"id":"note0","text":"hello"}]` {
		t.Errorf("notesList(%s) = %s", localtimeStr(w.Time), got)
	}

	// Saving again updates the workspace and keeps its stored name.
	if _, err := client.Put(ctx, w.Key, &Workspace{Name: "stored name", NextNoteNum: 2, Time: w.Time}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(w.Time.Add(time.Second)))
	w2, err := saveWorkspace(ctx, client, "my-notes", 5, nil, -1, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if w2.Name != "stored name" || w2.NextNoteNum != 5 || !w2.Time.After(w.Time) {
		t.Errorf("second save = %+v", w2)
	}
	if got := notesListOK(t, ctx, client, w2, ""); got != `[]` {
		t.Errorf("notesList = %s", got)
	}
	times, err := notesTimes(ctx, client, w.Key, 0, 11)
	if err != nil || len(times) != 2 || !times[0].Equal(w2.Time) || !times[1].Equal(w.Time) {
		t.Errorf("notesTimes = %v, %v", times, err)
	}
}

// Documents the raw Datastore format of a saved Notes entity, which Python
// must still read after a rollback (see testNotesStorageFormat in
// models_test.py).
func TestNotesStorageFormat(t *testing.T) {
	ctx, client := newTestClient(t)
	w, err := saveWorkspace(ctx, client, "ws", 1, []note{{{"id", "note0"}, {"xPos", "10"}, {"text", "café"}}}, -1, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Batch struct {
			EntityResults []struct {
				Entity struct {
					Key        json.RawMessage
					Properties map[string]map[string]any
				}
			}
		}
	}
	emulatorAPI(t, "runQuery", map[string]any{
		"partitionId": map[string]any{"projectId": testProject},
		"query":       map[string]any{"kind": []any{map[string]any{"name": "Notes"}}},
	}, &resp)
	if len(resp.Batch.EntityResults) != 1 {
		t.Fatalf("got %d Notes", len(resp.Batch.EntityResults))
	}
	props := resp.Batch.EntityResults[0].Entity.Properties
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	if want := []string{"notesJsonArray", "time", "time_to_delete", "workspaceKey"}; !sameStrings(keys, want) {
		t.Errorf("properties = %v, want %v", keys, want)
	}
	// The emulator writes all six fraction digits (".339050Z").
	stamp := func(name string) time.Time {
		ts, err := time.Parse(time.RFC3339Nano, fmt.Sprint(props[name]["timestampValue"]))
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
		return ts
	}
	if got := stamp("time"); !got.Equal(w.Time) {
		t.Errorf("time = %v, want %v", got, w.Time)
	}
	if got, want := stamp("time_to_delete"), w.Time.AddDate(0, 0, 730); !got.Equal(want) {
		t.Errorf("time_to_delete = %v, want %v", got, want)
	}
	path := props["workspaceKey"]["keyValue"].(map[string]any)["path"].([]any)[0].(map[string]any)
	if path["kind"] != "Workspace" || path["name"] != w.Key.Name {
		t.Errorf("workspaceKey = %v", path)
	}
	// notesJsonArray is an unindexed blob of zlib-compressed JSON, without
	// a meaning: Python reads that the way it reads pre-2025 revisions.
	blob := props["notesJsonArray"]
	if blob["excludeFromIndexes"] != true || blob["meaning"] != nil {
		t.Errorf("notesJsonArray = %v", blob)
	}
	raw, err := base64.StdEncoding.DecodeString(blob["blobValue"].(string))
	if err != nil {
		t.Fatal(err)
	}
	data, err := zlibDecompress(raw)
	if want := "[{\"id\":\"note0\",\"xPos\":\"10\",\"text\":\"caf\\u00e9\"}]"; err != nil || string(data) != want {
		t.Errorf("notesJsonArray = %s, %v; want %s", data, err, want)
	}
}

func sameStrings(a, b []string) bool {
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return len(a) == len(b)
}

func TestNotesJSONMatchesPython(t *testing.T) {
	data, err := os.ReadFile("testdata/notes_encoding.json")
	if err != nil {
		t.Fatal(err)
	}
	// Each case is [notes as [[key, value], ...] lists, ndb's JSON].
	var cases [][2]json.RawMessage
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		var input [][][2]string
		var want string
		if err := json.Unmarshal(c[0], &input); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(c[1], &want); err != nil {
			t.Fatal(err)
		}
		var notes []note
		for _, n := range input {
			properties := note{}
			for _, p := range n {
				properties = append(properties, noteProperty{p[0], p[1]})
			}
			notes = append(notes, properties)
		}
		if got := string(notesJSON(notes)); got != want {
			t.Errorf("notesJSON(%q)\n got %s\nwant %s", input, got, want)
		}
	}
}

// countErrors counts error logs while f runs.
func countErrors(f func()) int {
	h := &errorCounter{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	defer slog.SetDefault(old)
	f()
	return h.count
}

type errorCounter struct {
	slog.Handler
	count int
}

func (h *errorCounter) Enabled(context.Context, slog.Level) bool { return true }
func (h *errorCounter) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		h.count++
	}
	return nil
}

func TestDecodeNotesMatchesPython(t *testing.T) {
	data, err := os.ReadFile("testdata/notes_blobs.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string
		Blob      []byte
		Meaning22 bool
		Result    struct {
			Value  json.RawMessage
			Logged bool
			Error  string
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		name := c.Name
		want := c.Result
		if strings.HasPrefix(want.Error, "not representable") {
			continue // Python accepts NaN; JSON doesn't. No stored data has it.
		}
		if c.Meaning22 {
			// The marker isn't visible to this client. It only matters for
			// blobs that ndb itself never writes, where Python fails.
			if want.Error != "" {
				continue
			}
			name += " (meaning 22)"
		}
		var got json.RawMessage
		var gotErr error
		logged := countErrors(func() { got, gotErr = decodeNotes(c.Blob) })
		switch {
		case want.Error != "":
			if gotErr == nil {
				t.Errorf("%s: got %s, want an error (%s)", name, got, want.Error)
			}
		case gotErr != nil:
			t.Errorf("%s: got error %v, want %s", name, gotErr, want.Value)
		case !jsonEqual(t, got, want.Value):
			t.Errorf("%s: got %s, want %s", name, got, want.Value)
		case !c.Meaning22 && (logged > 0) != want.Logged:
			t.Errorf("%s: logged %d errors, want logged = %v", name, logged, want.Logged)
		}
	}
}

// skipIfShort skips a data test that takes more than a few seconds. deploy.sh
// runs the tests with -short.
func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("slow data test; run without -short")
	}
}

// sampleRevision is a Notes entity from .data/notes_sample.jsonl, written by
// tools/sample_notes.py.
type sampleRevision struct {
	line            int
	Type            string
	Indexed         bool
	Meaning         *int
	OtherProperties []string `json:"other_properties"`
	ValueB64        string   `json:"value_b64"`
	blob, json      []byte   // the stored blob, and its decompressed JSON
}

// loadNotesSample reads a sample of real revisions (user data, not in the
// repo), downloaded with:
//
//	TOKEN=$(gcloud auth print-access-token) python tools/dump_workspaces.py .data/workspaces.jsonl
//	TOKEN=$(gcloud auth print-access-token) python tools/sample_notes.py \
//	    .data/workspaces.jsonl .data/notes_sample.jsonl
//
// Set NOTES_SAMPLE_LIMIT to read only the first N revisions. Failures report
// the record number, not note contents.
func loadNotesSample(t *testing.T) []sampleRevision {
	t.Helper()
	f, err := os.Open(".data/notes_sample.jsonl")
	if os.IsNotExist(err) {
		t.Skip("needs .data/notes_sample.jsonl")
	} else if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	limit, _ := strconv.Atoi(os.Getenv("NOTES_SAMPLE_LIMIT"))
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 4<<20)
	var records []sampleRevision
	for line := 1; scanner.Scan() && (limit == 0 || len(records) < limit); line++ {
		r := sampleRevision{line: line}
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatalf("record %d: %v", line, err)
		}
		if r.blob, err = base64.StdEncoding.DecodeString(r.ValueB64); err != nil {
			t.Fatalf("record %d: %v", line, err)
		}
		if r.json, err = zlibDecompress(r.blob); err != nil {
			t.Fatalf("record %d: %v", line, err)
		}
		records = append(records, r)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

// Every sampled revision is zlib-compressed JSON in an unindexed blob, and
// decodes the same. Revisions written by ndb have meaning 22; revisions
// saved before 2025-08-13 lost it when the dataflow job that added
// time_to_delete rewrote them (they expire by ~2027-08).
func TestNotesSample(t *testing.T) {
	records := loadNotesSample(t)
	reencoded := 0
	for _, r := range records {
		if r.Type != "bytes" || r.Indexed || r.Meaning != nil && *r.Meaning != 22 || len(r.OtherProperties) > 0 {
			t.Errorf("record %d: stored as %s, indexed %v, meaning %v, other properties %v",
				r.line, r.Type, r.Indexed, r.Meaning, r.OtherProperties)
		}
		var got json.RawMessage
		var err error
		logged := countErrors(func() { got, err = decodeNotes(r.blob) })
		if err != nil || logged > 0 || !bytes.Equal(got, r.json) {
			t.Errorf("record %d: decodeNotes differs (error %v, %d logged)", r.line, err, logged)
		}
		// Revisions written by ndb: Go writes the same JSON for the same
		// notes. (Older revisions came from older code.)
		if r.Meaning != nil && *r.Meaning == 22 {
			notes, err := orderedNotes(r.json)
			if err != nil {
				t.Errorf("record %d: %v", r.line, err)
			} else if !bytes.Equal(notesJSON(notes), r.json) {
				t.Errorf("record %d: notesJSON differs from ndb's", r.line)
			}
			reencoded++
		}
	}
	t.Logf("checked %d revisions, re-encoded %d", len(records), reencoded)
}

// Every sampled note is an object of strings with the client's keys and an
// id. Attribute values go into the save XML unescaped, so none may contain
// quotes or markup.
func TestNotesSampleShape(t *testing.T) {
	for _, r := range loadNotesSample(t) {
		var notes []map[string]any
		if err := json.Unmarshal(r.json, &notes); err != nil || notes == nil {
			t.Errorf("record %d: not a list of objects (%v)", r.line, err)
			continue
		}
		for i, n := range notes {
			if _, ok := n["id"]; !ok {
				t.Errorf("record %d, note %d: no id", r.line, i)
			}
			for k, v := range n {
				s, ok := v.(string)
				if _, known := noteAttrXML[k]; !ok || !known && k != "text" {
					t.Errorf("record %d, note %d: unexpected key or non-string value (%q)", r.line, i, k)
				} else if k != "text" && strings.ContainsAny(s, `'"<>&`) {
					t.Errorf("record %d, note %d: %s has quotes or markup", r.line, i, k)
				}
			}
		}
	}
}

// noteAttrXML maps note keys to the save XML's attribute names.
var noteAttrXML = func() map[string]string {
	m := map[string]string{}
	for xmlName, key := range noteAttrKeys {
		m[key] = xmlName
	}
	return m
}()

// jsResaveTexts returns escape(unescape(text)) for each text, as the client
// computes it: the page shows unescape(text), and the next save sends
// escape() of the shown text.
func jsResaveTexts(t *testing.T, texts []string) []string {
	t.Helper()
	script := `
		const fs = require('fs');
		const texts = JSON.parse(fs.readFileSync(0, 'utf8'));
		console.log(JSON.stringify(texts.map(t => escape(unescape(t)))));
	`
	var out []string
	runNode(t, script, texts, &out)
	if len(out) != len(texts) {
		t.Fatalf("node returned %d texts for %d", len(out), len(texts))
	}
	return out
}

// runNode runs a node script with in as JSON on stdin, and decodes its
// stdout as JSON into out.
func runNode(t *testing.T, script string, in, out any) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("needs node")
	}
	input, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "-e", script)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output, out); err != nil {
		t.Fatal(err)
	}
}

// What the user sees (unescape(text)) doesn't change across a save.
func TestNotesSampleResaveKeepsShownText(t *testing.T) {
	skipIfShort(t)
	var texts []string
	for _, r := range loadNotesSample(t) {
		notes, err := orderedNotes(r.json)
		if err != nil {
			t.Fatalf("record %d: %v", r.line, err)
		}
		for _, n := range notes {
			for _, p := range n {
				if p.Key == "text" {
					texts = append(texts, p.Value)
				}
			}
		}
	}
	script := `
		const fs = require('fs');
		const texts = JSON.parse(fs.readFileSync(0, 'utf8'));
		console.log(JSON.stringify(texts.filter(
			t => unescape(escape(unescape(t))) !== unescape(t)).length));
	`
	var changed int
	runNode(t, script, texts, &changed)
	if changed != 0 {
		t.Errorf("%d of %d texts show differently after a save", changed, len(texts))
	}
}

// Each sampled revision, saved the way webnote.js saves it, is stored and
// shown on the page as the client sent it.
func TestNotesSampleSaveRoundTrip(t *testing.T) {
	skipIfShort(t)
	records := loadNotesSample(t)
	var all [][]note
	var texts []string
	for _, r := range records {
		notes, err := orderedNotes(r.json)
		if err != nil {
			t.Fatalf("record %d: %v", r.line, err)
		}
		all = append(all, notes)
		for _, n := range notes {
			texts = append(texts, noteText(n))
		}
	}
	resaved := jsResaveTexts(t, texts)

	ctx, client := newTestClient(t)
	s := newTestServer(client, time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	notesOnPage := regexp.MustCompile(`(?s)const notes = (.*);\n  for \(const n in notes\)`)
	for i, r := range records {
		if i%100 == 0 {
			emulatorReset(t) // The emulator slows down as it fills.
		}
		expected := []map[string]string{}
		var xmlBody strings.Builder
		name := fmt.Sprintf("w%d", i)
		fmt.Fprintf(&xmlBody, "<workspace name='%s' nextNoteNum='1'>\n", name)
		for _, n := range all[i] {
			e := map[string]string{}
			var attrs []string
			for _, p := range n {
				if p.Key == "text" {
					continue
				}
				e[p.Key] = p.Value
				attrs = append(attrs, fmt.Sprintf("%s='%s'", noteAttrXML[p.Key], p.Value))
			}
			text := resaved[0]
			resaved = resaved[1:]
			e["text"] = text
			fmt.Fprintf(&xmlBody, "<note %s>\n%s\n</note>\n", strings.Join(attrs, " "), text)
			expected = append(expected, e)
		}
		xmlBody.WriteString("</workspace>")
		if rec := do("POST", "/webnote/save.py", xmlBody.String()); rec.Code != 200 {
			t.Errorf("record %d: save returned %d", r.line, rec.Code)
			continue
		}

		ws, err := getWorkspace(ctx, client, name)
		if err != nil || ws == nil {
			t.Fatalf("record %d: workspace %v, %v", r.line, ws, err)
		}
		stored, err := ws.notesList(ctx, client, "")
		if err != nil {
			t.Fatalf("record %d: %v", r.line, err)
		}
		var got []map[string]string
		if err := json.Unmarshal(stored, &got); err != nil || !reflect.DeepEqual(got, expected) {
			t.Errorf("record %d: stored notes differ from the save (%v)", r.line, err)
		}

		m := notesOnPage.FindStringSubmatch(do("GET", "/webnote/"+name, "").Body.String())
		got = nil
		if m == nil || json.Unmarshal([]byte(m[1]), &got) != nil || !reflect.DeepEqual(got, expected) {
			t.Errorf("record %d: page notes differ from the save", r.line)
		}
	}
	t.Logf("saved and loaded %d revisions", len(records))
}

// noteText is a note's text, or "" if it has none.
func noteText(n note) string {
	for _, p := range n {
		if p.Key == "text" {
			return p.Value
		}
	}
	return ""
}

// orderedNotes parses a JSON array of objects with string values, keeping
// key order.
func orderedNotes(data []byte) ([]note, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var notes []note
	expect := func(want json.Delim) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if tok != want {
			return errors.New("unexpected JSON token")
		}
		return nil
	}
	if err := expect('['); err != nil {
		return nil, err
	}
	for dec.More() {
		if err := expect('{'); err != nil {
			return nil, err
		}
		var n note
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			value, err := dec.Token()
			if err != nil {
				return nil, err
			}
			k, ok1 := key.(string)
			v, ok2 := value.(string)
			if !ok1 || !ok2 {
				return nil, errors.New("note property isn't a string")
			}
			n = append(n, noteProperty{k, v})
		}
		if err := expect('}'); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, expect(']')
}
