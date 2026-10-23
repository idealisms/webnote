package main

// Notes JSON as Python sees it: json.loads() into dicts (which keep key order
// and the last value of a duplicate key), ints and floats, and str (which may
// hold lone surrogates), and json.dumps() back out for the workspace page.

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type pyKind int

const (
	kindNull pyKind = iota
	kindBool
	kindInt
	kindFloat
	kindStr
	kindList
	kindDict
)

type pyValue struct {
	kind  pyKind
	b     bool
	i     *big.Int
	f     float64
	s     []rune
	items []pyValue // list items, or dict values
	keys  [][]rune  // dict keys, in insertion order
}

// get is dict.get(key): the value and whether the key is there.
func (v pyValue) get(key string) (pyValue, bool) {
	for i, k := range v.keys {
		if string(k) == key {
			return v.items[i], true
		}
	}
	return pyValue{}, false
}

var errBadJSON = errors.New("bad JSON")

// parsePyJSON parses JSON that json.Valid() accepted.
func parsePyJSON(data []byte) (pyValue, error) {
	p := &jsonParser{data: data}
	p.skipSpace()
	v, err := p.value()
	if err != nil {
		return pyValue{}, err
	}
	p.skipSpace()
	if p.pos != len(p.data) {
		return pyValue{}, errBadJSON
	}
	return v, nil
}

type jsonParser struct {
	data []byte
	pos  int
}

func (p *jsonParser) skipSpace() {
	for p.pos < len(p.data) && strings.IndexByte(" \t\n\r", p.data[p.pos]) >= 0 {
		p.pos++
	}
}

func (p *jsonParser) consume(lit string) bool {
	// Not string(p.data[p.pos:]): that copies the rest of the input on every
	// call, which made parsing quadratic.
	if len(p.data)-p.pos >= len(lit) && string(p.data[p.pos:p.pos+len(lit)]) == lit {
		p.pos += len(lit)
		return true
	}
	return false
}

func (p *jsonParser) value() (pyValue, error) {
	if p.pos >= len(p.data) {
		return pyValue{}, errBadJSON
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		p.pos++
		v := pyValue{kind: kindDict}
		p.skipSpace()
		if p.consume("}") {
			return v, nil
		}
		for {
			p.skipSpace()
			key, err := p.str()
			if err != nil {
				return pyValue{}, err
			}
			p.skipSpace()
			if !p.consume(":") {
				return pyValue{}, errBadJSON
			}
			p.skipSpace()
			item, err := p.value()
			if err != nil {
				return pyValue{}, err
			}
			replaced := false
			for i, k := range v.keys {
				if string(k) == string(key) {
					v.items[i] = item
					replaced = true
				}
			}
			if !replaced {
				v.keys = append(v.keys, key)
				v.items = append(v.items, item)
			}
			p.skipSpace()
			if p.consume("}") {
				return v, nil
			}
			if !p.consume(",") {
				return pyValue{}, errBadJSON
			}
		}
	case c == '[':
		p.pos++
		v := pyValue{kind: kindList}
		p.skipSpace()
		if p.consume("]") {
			return v, nil
		}
		for {
			p.skipSpace()
			item, err := p.value()
			if err != nil {
				return pyValue{}, err
			}
			v.items = append(v.items, item)
			p.skipSpace()
			if p.consume("]") {
				return v, nil
			}
			if !p.consume(",") {
				return pyValue{}, errBadJSON
			}
		}
	case c == '"':
		s, err := p.str()
		return pyValue{kind: kindStr, s: s}, err
	case p.consume("null"):
		return pyValue{kind: kindNull}, nil
	case p.consume("true"):
		return pyValue{kind: kindBool, b: true}, nil
	case p.consume("false"):
		return pyValue{kind: kindBool}, nil
	default:
		return p.number()
	}
}

// str parses a JSON string. Like Python, \u escapes of a surrogate pair make
// one character, and a lone surrogate stays.
func (p *jsonParser) str() ([]rune, error) {
	if !p.consume(`"`) {
		return nil, errBadJSON
	}
	var out []rune
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return out, nil
		case c == '\\':
			if p.pos+1 >= len(p.data) {
				return nil, errBadJSON
			}
			e := p.data[p.pos+1]
			p.pos += 2
			switch e {
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				r, ok := p.hex4()
				if !ok {
					return nil, errBadJSON
				}
				if utf16.IsSurrogate(r) && r < 0xdc00 && p.consume(`\u`) {
					start := p.pos - 2
					if r2, ok := p.hex4(); ok && r2 >= 0xdc00 && r2 <= 0xdfff {
						r = utf16.DecodeRune(r, r2)
					} else {
						p.pos = start
					}
				}
				out = append(out, r)
			default:
				out = append(out, rune(e))
			}
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			out = append(out, r)
			p.pos += size
		}
	}
	return nil, errBadJSON
}

func (p *jsonParser) hex4() (rune, bool) {
	if p.pos+4 > len(p.data) {
		return 0, false
	}
	v, err := strconv.ParseUint(string(p.data[p.pos:p.pos+4]), 16, 32)
	if err != nil {
		return 0, false
	}
	p.pos += 4
	return rune(v), true
}

func (p *jsonParser) number() (pyValue, error) {
	start := p.pos
	for p.pos < len(p.data) && strings.IndexByte("+-0123456789.eE", p.data[p.pos]) >= 0 {
		p.pos++
	}
	lit := string(p.data[start:p.pos])
	if lit == "" {
		return pyValue{}, errBadJSON
	}
	if !strings.ContainsAny(lit, ".eE") {
		n, ok := new(big.Int).SetString(lit, 10)
		if !ok {
			return pyValue{}, errBadJSON
		}
		return pyValue{kind: kindInt, i: n}, nil
	}
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return pyValue{}, errBadJSON
	}
	return pyValue{kind: kindFloat, f: f}, nil
}

// pyDumps is json.dumps(v): ', ' and ': ' separators and ASCII-only output.
func pyDumps(v pyValue) string {
	var b strings.Builder
	writePyDumps(&b, v)
	return b.String()
}

func writePyDumps(b *strings.Builder, v pyValue) {
	switch v.kind {
	case kindNull:
		b.WriteString("null")
	case kindBool:
		if v.b {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case kindInt:
		b.WriteString(v.i.String())
	case kindFloat:
		b.WriteString(pyFloatRepr(v.f))
	case kindStr:
		writePyJSONString(b, v.s)
	case kindList:
		b.WriteByte('[')
		for i, item := range v.items {
			if i > 0 {
				b.WriteString(", ")
			}
			writePyDumps(b, item)
		}
		b.WriteByte(']')
	case kindDict:
		b.WriteByte('{')
		for i, item := range v.items {
			if i > 0 {
				b.WriteString(", ")
			}
			writePyJSONString(b, v.keys[i])
			b.WriteString(": ")
			writePyDumps(b, item)
		}
		b.WriteByte('}')
	}
}

// pyFloatRepr is repr(float) as json.dumps() writes it.
func pyFloatRepr(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64)
	exp, _ := strconv.Atoi(e[strings.IndexByte(e, 'e')+1:])
	if exp < -4 || exp >= 16 {
		return e
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
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
