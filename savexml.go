package main

// Parsing of save.py's XML body the way save_handler.py does it with
// xml.dom.minidom (expat, with namespaces).

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Client attribute names for each note key (DBKEYS and DB2JS in models.py).
var noteAttrKeys = map[string]string{
	"noteid":    "id",
	"xposition": "xPos",
	"yposition": "yPos",
	"height":    "height",
	"width":     "width",
	"bgcolor":   "bgcolor",
	"zindex":    "zIndex",
	"text":      "text",
}

type xmlNodeKind int

const (
	xmlElement xmlNodeKind = iota
	xmlText                // text or a CDATA section
	xmlComment
	xmlProcInst
)

type xmlNode struct {
	kind     xmlNodeKind
	name     string // qualified name, as minidom's tagName
	attrs    []xml.Attr
	data     string // minidom's nodeValue, for non-elements
	children []*xmlNode
}

func (n *xmlNode) attr(name string) string {
	for _, a := range n.attrs {
		if qualifiedName(a.Name) == name {
			return a.Value
		}
	}
	return ""
}

// elementsByTagName is getElementsByTagName(): descendants in document order.
func (n *xmlNode) elementsByTagName(name string, out []*xmlNode) []*xmlNode {
	for _, c := range n.children {
		if c.kind == xmlElement {
			if c.name == name {
				out = append(out, c)
			}
			out = c.elementsByTagName(name, out)
		}
	}
	return out
}

func qualifiedName(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

var errSaveXML = errors.New("bad save XML")

type saveRequest struct {
	name        string
	nextNoteNum string
	notes       []note
}

// parseSaveBody does what SaveHandler.post() does before saving. An error is
// where Python raises (a 500).
func parseSaveBody(body []byte) (*saveRequest, error) {
	doc, err := parseXMLDocument(body)
	if err != nil {
		return nil, err
	}
	workspaces := doc.elementsByTagName("workspace", nil)
	if len(workspaces) == 0 {
		return nil, errSaveXML
	}
	ws := workspaces[0]
	req := &saveRequest{name: ws.attr("name"), nextNoteNum: ws.attr("nextNoteNum")}
	for _, el := range ws.elementsByTagName("note", nil) {
		n := note{}
		for _, a := range el.attrs {
			if key, ok := noteAttrKeys[qualifiedName(a.Name)]; ok {
				n = append(n, noteProperty{key, a.Value})
			}
		}
		text := ""
		if len(el.children) > 0 {
			first := el.children[0]
			if first.kind == xmlElement {
				// An element's nodeValue is None.
				return nil, errSaveXML
			}
			text = first.data
		}
		text = string(pyStrip([]rune(text)))
		replaced := false
		for i := range n {
			if n[i].Key == "text" {
				n[i].Value = text
				replaced = true
			}
		}
		if !replaced {
			n = append(n, noteProperty{"text", text})
		}
		req.notes = append(req.notes, n)
	}
	return req, nil
}

var (
	xmlDeclEncoding = regexp.MustCompile(`^<\?xml[^>]*?encoding\s*=\s*["']([A-Za-z0-9._-]+)["']`)
	entityDecl      = regexp.MustCompile(`<!ENTITY\s+([^\s%]+)\s+(?:"([^"]*)"|'([^']*)')\s*>`)
	charRef         = regexp.MustCompile(`&#(x[0-9A-Fa-f]+|[0-9]+);`)
)

// parseXMLDocument parses a document like minidom.parseString(), returning
// the document node.
func parseXMLDocument(body []byte) (*xmlNode, error) {
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	if m := xmlDeclEncoding.FindSubmatch(body); m != nil {
		switch strings.ToLower(string(m[1])) {
		case "utf-8", "utf8":
		case "iso-8859-1", "latin-1", "latin1":
			// Converted here, so the decoder's offsets match the input.
			var b strings.Builder
			for _, c := range body {
				b.WriteRune(rune(c))
			}
			body = []byte(b.String())
		case "us-ascii", "ascii":
			for _, c := range body {
				if c >= 0x80 {
					return nil, errSaveXML
				}
			}
		default:
			return nil, errSaveXML
		}
	}

	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = true
	d.Entity = map[string]string{}
	// Any encoding was handled above.
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }

	doc := &xmlNode{}
	stack := []*xmlNode{doc}
	namespaces := []map[string]bool{{"xml": true, "xmlns": true}}
	seenRoot, first := false, true
	for {
		start := d.InputOffset()
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		parent := stack[len(stack)-1]
		atTop := len(stack) == 1
		switch t := tok.(type) {
		case xml.StartElement:
			if atTop && seenRoot {
				return nil, errSaveXML // junk after the document element
			}
			seenRoot = true
			attrs, scope, err := checkAttrs(t, body[start:d.InputOffset()], d.Entity, namespaces[len(namespaces)-1])
			if err != nil {
				return nil, err
			}
			if !scope[t.Name.Space] && t.Name.Space != "" {
				return nil, errSaveXML // unbound prefix
			}
			el := &xmlNode{kind: xmlElement, name: qualifiedName(t.Name), attrs: attrs}
			parent.children = append(parent.children, el)
			stack = append(stack, el)
			namespaces = append(namespaces, scope)
		case xml.EndElement:
			if atTop || qualifiedName(t.Name) != parent.name {
				return nil, errSaveXML
			}
			stack = stack[:len(stack)-1]
			namespaces = namespaces[:len(namespaces)-1]
		case xml.CharData:
			if atTop {
				if len(bytes.Trim(t, " \t\r\n")) > 0 {
					return nil, errSaveXML
				}
				break
			}
			parent.children = append(parent.children, &xmlNode{kind: xmlText, data: string(t)})
		case xml.Comment:
			parent.children = append(parent.children, &xmlNode{kind: xmlComment, data: string(t)})
		case xml.ProcInst:
			if strings.EqualFold(t.Target, "xml") && !first {
				return nil, errSaveXML
			}
			if !strings.EqualFold(t.Target, "xml") {
				parent.children = append(parent.children, &xmlNode{kind: xmlProcInst, data: string(t.Inst)})
			}
		case xml.Directive:
			if !atTop || seenRoot {
				return nil, errSaveXML
			}
			for _, m := range entityDecl.FindAllStringSubmatch(string(t), -1) {
				if _, ok := d.Entity[m[1]]; !ok {
					d.Entity[m[1]] = expandCharRefs(m[2] + m[3])
				}
			}
		}
		first = false
	}
	if len(stack) != 1 || !seenRoot {
		return nil, errSaveXML
	}
	return doc, nil
}

func expandCharRefs(s string) string {
	return charRef.ReplaceAllStringFunc(s, func(ref string) string {
		r, ok := parseCharRef(ref[2 : len(ref)-1])
		if !ok {
			return ref
		}
		return string(r)
	})
}

func parseCharRef(s string) (rune, bool) {
	var v uint64
	var err error
	if strings.HasPrefix(s, "x") {
		v, err = strconv.ParseUint(s[1:], 16, 32)
	} else {
		v, err = strconv.ParseUint(s, 10, 32)
	}
	return rune(v), err == nil
}

// checkAttrs checks a start tag's attributes like expat (no duplicates, no
// unbound prefixes) and returns them with their values normalized as expat
// does (a literal tab, newline or CR becomes a space; character references
// don't), plus the namespace prefixes in scope for the element.
func checkAttrs(t xml.StartElement, raw []byte, entities map[string]string, outer map[string]bool) ([]xml.Attr, map[string]bool, error) {
	scope, copied := outer, false
	for _, a := range t.Attr {
		if a.Name.Space == "xmlns" {
			if !copied {
				scope, copied = copyScope(outer), true
			}
			scope[a.Name.Local] = true
		}
	}
	values := rawAttrValues(raw)
	seen := map[string]bool{}
	attrs := make([]xml.Attr, len(t.Attr))
	for i, a := range t.Attr {
		q := qualifiedName(a.Name)
		if seen[q] {
			return nil, nil, errSaveXML
		}
		seen[q] = true
		if a.Name.Space != "" && !scope[a.Name.Space] {
			return nil, nil, errSaveXML
		}
		attrs[i] = a
		if len(values) == len(t.Attr) {
			attrs[i].Value = normalizeAttr(values[i], entities)
		}
	}
	return attrs, scope, nil
}

func copyScope(m map[string]bool) map[string]bool {
	c := make(map[string]bool, len(m)+1)
	for k, v := range m {
		c[k] = v
	}
	return c
}

var rawAttr = regexp.MustCompile(`\s[^\s=]+\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// rawAttrValues returns the literal attribute values in a start tag.
func rawAttrValues(tag []byte) []string {
	var values []string
	for _, m := range rawAttr.FindAllSubmatch(tag, -1) {
		values = append(values, string(m[1])+string(m[2]))
	}
	return values
}

func normalizeAttr(raw string, entities map[string]string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch c {
		case '\t', '\n', '\r':
			b.WriteByte(' ')
		case '&':
			end := strings.IndexByte(raw[i:], ';')
			if end < 0 {
				b.WriteByte(c)
				continue
			}
			ref := raw[i+1 : i+end]
			switch {
			case strings.HasPrefix(ref, "#"):
				if r, ok := parseCharRef(ref[1:]); ok {
					b.WriteRune(r)
				}
			case ref == "lt":
				b.WriteByte('<')
			case ref == "gt":
				b.WriteByte('>')
			case ref == "amp":
				b.WriteByte('&')
			case ref == "apos":
				b.WriteByte('\'')
			case ref == "quot":
				b.WriteByte('"')
			default:
				b.WriteString(entities[ref])
			}
			i += end
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
