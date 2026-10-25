package main

// The RSS feed, as rss_handler.py writes it with PyRSS2Gen.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var errRSSNotes = errors.New("notes aren't a list of dicts with string text")

// rssWriter writes XML the way xml.sax.saxutils.XMLGenerator does, except
// that every non-ASCII character is a character reference. (Python wrote
// U+0080-U+00FF as Latin-1 bytes into a response that decoded them as UTF-8,
// which was a 500 or the wrong characters.) The output is ASCII, so it
// matches both the declared iso-8859-1 and the Content-Type's UTF-8.
type rssWriter struct {
	b strings.Builder
}

func (w *rssWriter) raw(s string) { w.b.WriteString(s) }

func (w *rssWriter) text(s []rune) {
	for _, r := range s {
		switch {
		case r == '&':
			w.b.WriteString("&amp;")
		case r == '<':
			w.b.WriteString("&lt;")
		case r == '>':
			w.b.WriteString("&gt;")
		case r < 0x80:
			w.b.WriteRune(r)
		default:
			fmt.Fprintf(&w.b, "&#%d;", r)
		}
	}
}

func (w *rssWriter) element(name string, text []rune) {
	w.raw("<" + name + ">")
	w.text(text)
	w.raw("</" + name + ">")
}

// rfc822 is PyRSS2Gen's _format_date() of a naive UTC time.
func rfc822(t time.Time) string {
	return t.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

type rssItem struct{ title, description, guid []rune }

// renderRSS returns the feed for a workspace and its notes JSON.
func renderRSS(hostURL string, w *Workspace, notesJSON []byte) ([]byte, error) {
	// Fields are looked up by exact name, as in Python: decoding into a
	// struct would also match "Text" or "ID".
	var notes []map[string]json.RawMessage
	if err := json.Unmarshal(notesJSON, &notes); err != nil || notes == nil {
		return nil, errRSSNotes
	}
	link := hostURL + "/webnote/" + pyQuote(pyQuote(w.Name))
	var items []rssItem
	for _, n := range notes {
		if n == nil {
			return nil, errRSSNotes
		}
		text := ""
		if raw, ok := n["text"]; ok {
			if text, ok = jsonString(raw); !ok {
				return nil, errRSSNotes
			}
		}
		// An id that isn't a string (no stored note has one) is used as
		// its JSON text.
		id := ""
		if raw, ok := n["id"]; ok {
			if id, ok = jsonString(raw); !ok {
				id = string(raw)
			}
		}
		description := pyStrip(pyUnquote([]rune(text)))
		title := description
		for i, r := range title {
			if r == '\n' {
				title = title[:i]
				break
			}
		}
		if len(title) > 60 {
			title = title[:60]
		}
		items = append(items, rssItem{title, description, []rune(link + "#" + id)})
	}

	out := &rssWriter{}
	out.raw(`<?xml version="1.0" encoding="iso-8859-1"?>` + "\n")
	out.raw(`<rss version="2.0"><channel>`)
	out.element("title", pyUnquote([]rune(w.Name)))
	out.element("link", []rune(link))
	out.element("description", []rune("Webnote RSS feed"))
	out.element("lastBuildDate", []rune(rfc822(w.Time)))
	out.element("generator", []rune("PyRSS2Gen-1.1.0"))
	out.element("docs", []rune("http://blogs.law.harvard.edu/tech/rss"))
	// The recent entries first.
	for i := len(items) - 1; i >= 0; i-- {
		out.raw("<item>")
		out.element("title", items[i].title)
		out.element("description", items[i].description)
		out.raw(`<guid isPermaLink="true">`)
		out.text(items[i].guid)
		out.raw("</guid></item>")
	}
	out.raw("</channel></rss>")
	return []byte(out.b.String()), nil
}

// jsonString decodes a JSON string. Unmarshaling null into a string would
// succeed and leave it empty, so other values are checked for first.
func jsonString(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}
