package main

// The RSS feed, as rss_handler.py writes it with PyRSS2Gen.

import (
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

// renderRSS returns the feed for a workspace and its notes (a JSON value).
func renderRSS(hostURL string, w *Workspace, notes pyValue) ([]byte, error) {
	var elements []pyValue
	switch notes.kind {
	case kindList:
		elements = notes.items
	case kindDict:
		// Iterating a dict gives its keys, which are strings.
		for _, k := range notes.keys {
			elements = append(elements, pyValue{kind: kindStr, s: k})
		}
	case kindStr:
		for _, r := range notes.s {
			elements = append(elements, pyValue{kind: kindStr, s: []rune{r}})
		}
	default:
		return nil, errRSSNotes
	}
	link := hostURL + "/webnote/" + pyQuote(pyQuote(w.Name))
	var items []rssItem
	for _, n := range elements {
		if n.kind != kindDict {
			return nil, errRSSNotes
		}
		text := []rune{}
		if t, ok := n.get("text"); ok {
			if t.kind != kindStr {
				return nil, errRSSNotes
			}
			text = t.s
		}
		id := []rune{}
		if v, ok := n.get("id"); ok {
			var err error
			if id, err = pyStr(v); err != nil {
				return nil, err
			}
		}
		description := pyStrip(pyUnquote(text))
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
		items = append(items, rssItem{title, description, append([]rune(link+"#"), id...)})
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

// pyStr is str(v) for the JSON values a note id can be. Lists and dicts
// (Python's repr) aren't supported; no stored note has one.
func pyStr(v pyValue) ([]rune, error) {
	switch v.kind {
	case kindStr:
		return v.s, nil
	case kindNull:
		return []rune("None"), nil
	case kindBool:
		if v.b {
			return []rune("True"), nil
		}
		return []rune("False"), nil
	case kindInt:
		return []rune(v.i.String()), nil
	case kindFloat:
		s := pyFloatRepr(v.f)
		switch s {
		case "Infinity":
			s = "inf"
		case "-Infinity":
			s = "-inf"
		}
		return []rune(s), nil
	}
	return nil, errors.New("note id is a list or dict")
}
