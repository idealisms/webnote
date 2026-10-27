package main

// Python string and URL functions that the handlers' behavior depends on,
// reproduced exactly. Strings that may hold lone surrogates (from JSON) are
// []rune.

import (
	"fmt"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"
)

// pyIsSpace is Python's str.isspace() for one character.
func pyIsSpace(r rune) bool {
	switch {
	case r >= '\t' && r <= '\r', r >= 0x1c && r <= ' ', r == 0x85, r == 0xa0,
		r == 0x1680, r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029,
		r == 0x202f, r == 0x205f, r == 0x3000:
		return true
	}
	return false
}

// pyStrip is Python's str.strip().
func pyStrip(s []rune) []rune {
	start, end := 0, len(s)
	for start < end && pyIsSpace(s[start]) {
		start++
	}
	for end > start && pyIsSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func pyStripString(s string) string {
	return strings.TrimFunc(s, pyIsSpace)
}

// pyInt is Python's int() of a string: surrounding whitespace, a sign, and
// decimal digits of any script with single underscores between them. ok is
// false where Python raises ValueError.
func pyInt(s string) (n *big.Int, ok bool) {
	s = pyStripString(s)
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	n = new(big.Int)
	ten := big.NewInt(10)
	prevDigit := false
	digits := 0
	for _, r := range s {
		if r == '_' {
			if !prevDigit {
				return nil, false
			}
			prevDigit = false
			continue
		}
		d, isDigit := digitValue(r)
		if !isDigit {
			return nil, false
		}
		n.Mul(n, ten).Add(n, big.NewInt(int64(d)))
		prevDigit = true
		digits++
	}
	if digits == 0 || !prevDigit {
		return nil, false
	}
	if neg {
		n.Neg(n)
	}
	return n, true
}

// digitValue returns the value of a decimal digit in any script. Unicode puts
// each script's digits 0-9 in a contiguous run, and each range in unicode.Nd
// starts at a 0.
func digitValue(r rune) (int, bool) {
	if r >= '0' && r <= '9' {
		return int(r - '0'), true
	}
	if !unicode.Is(unicode.Nd, r) {
		return 0, false
	}
	for _, rng := range unicode.Nd.R16 {
		if r >= rune(rng.Lo) && r <= rune(rng.Hi) {
			return int(r-rune(rng.Lo)) % 10, true
		}
	}
	for _, rng := range unicode.Nd.R32 {
		if r >= rune(rng.Lo) && r <= rune(rng.Hi) {
			return int(r-rune(rng.Lo)) % 10, true
		}
	}
	return 0, false
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

// pyUnquote is urllib.parse.unquote(): it percent-decodes the ASCII runs of s
// (an invalid escape stays as is) and decodes each run's bytes as UTF-8, with
// errors replaced. Other characters pass through unchanged.
func pyUnquote(s []rune) []rune {
	var out []rune
	for i := 0; i < len(s); {
		if s[i] >= 0x80 {
			out = append(out, s[i])
			i++
			continue
		}
		j := i
		var run []byte
		for j < len(s) && s[j] < 0x80 {
			run = append(run, byte(s[j]))
			j++
		}
		out = append(out, pyDecodeUTF8Replace(percentDecode(run))...)
		i = j
	}
	return out
}

// percentDecode is urllib.parse.unquote_to_bytes().
func percentDecode(s []byte) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			out = append(out, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
		} else {
			out = append(out, s[i])
		}
	}
	return out
}

// pyDecodeUTF8Replace decodes UTF-8 like Python's bytes.decode('utf-8',
// 'replace'): each maximal invalid subsequence becomes one U+FFFD. (Go's
// decoder replaces each invalid byte instead.)
func pyDecodeUTF8Replace(b []byte) []rune {
	var out []rune
	for i := 0; i < len(b); {
		if r, size := utf8.DecodeRune(b[i:]); r != utf8.RuneError || size > 1 {
			out = append(out, r)
			i += size
			continue
		}
		// Count the valid prefix of a multi-byte sequence.
		c := b[i]
		n, lo, hi := 0, byte(0x80), byte(0xbf)
		switch {
		case c >= 0xc2 && c <= 0xdf:
			n = 1
		case c == 0xe0:
			n, lo = 2, 0xa0
		case c >= 0xe1 && c <= 0xec, c == 0xee, c == 0xef:
			n = 2
		case c == 0xed:
			n, hi = 2, 0x9f
		case c == 0xf0:
			n, lo = 3, 0x90
		case c >= 0xf1 && c <= 0xf3:
			n = 3
		case c == 0xf4:
			n, hi = 3, 0x8f
		}
		j := i + 1
		for k := 0; k < n && j < len(b); k++ {
			if b[j] < lo || b[j] > hi {
				break
			}
			lo, hi = 0x80, 0xbf
			j++
		}
		out = append(out, utf8.RuneError)
		i = j
	}
	return out
}

// pyQuote is urllib.parse.quote(s) with the default safe='/'.
func pyQuote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.IndexByte("_.-~/", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// queryParams is the parsed query string: '&' separates pairs, '+' is a
// space, and a name without '=' has an empty value. Only '%' and two hex
// digits is an escape; any other '%' stays, so old links with %u escapes
// still work. A name or value that isn't UTF-8 is read as Latin-1: that's
// what the native escape() means by %E9. (webob also split on ';' and read
// "%2" as byte 2.)
type queryParams []struct{ name, value string }

func parseQuery(raw string) queryParams {
	var params queryParams
	raw = strings.ReplaceAll(raw, "+", " ")
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		n := utf8OrLatin1(string(percentDecode([]byte(name))))
		v := utf8OrLatin1(string(percentDecode([]byte(value))))
		params = append(params, struct{ name, value string }{n, v})
	}
	return params
}

// get is webapp2's request.get(): the first value, or "".
func (p queryParams) get(name string) string {
	v, _ := p.lookup(name)
	return v
}

// lookup returns the first value and whether there is one.
func (p queryParams) lookup(name string) (string, bool) {
	for _, kv := range p {
		if kv.name == name {
			return kv.value, true
		}
	}
	return "", false
}

// utf8OrLatin1 returns s if it's valid UTF-8, and otherwise s decoded as
// Latin-1.
func utf8OrLatin1(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return string(latin1Runes(s))
}

// latin1Runes decodes bytes as Latin-1, as WSGI does for headers.
func latin1Runes(s string) []rune {
	out := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = rune(s[i])
	}
	return out
}
