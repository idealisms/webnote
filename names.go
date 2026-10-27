package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
)

// uEscape matches a %u escape with 3 or 4 hex digits. A fourth hex digit is
// always read as part of the escape, as unescape() does.
var uEscape = regexp.MustCompile(`%u[0-9A-Fa-f]{3,4}`)

// padUEscapes pads %u escapes with 3 hex digits to 4 (%u5D9 -> %u05D9), so
// that unescape() decodes them. Before 2026-10-26, myescape() in objects.js
// didn't pad them, so old tabs and links still send them.
func padUEscapes(name string) string {
	return uEscape.ReplaceAllStringFunc(name, func(m string) string {
		if len(m) == 5 {
			return "%u0" + m[2:]
		}
		return m
	})
}

// isMyescapeSafe reports whether myescape() in objects.js leaves r unescaped.
func isMyescapeSafe(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		r == '_'
}

// myescapeRune escapes one character the way myescape() in objects.js does.
//
// Like JavaScript, this works on UTF-16 code units, so characters outside the
// BMP become two escapes (a surrogate pair).
func myescapeRune(r rune) string {
	var b strings.Builder
	for _, unit := range utf16.Encode([]rune{r}) {
		if unit > 0xFF {
			fmt.Fprintf(&b, "%%u%04X", unit)
		} else {
			fmt.Fprintf(&b, "%%%02X", unit)
		}
	}
	return b.String()
}

// canonicalWsName returns the stored form of a workspace name, like
// canonical_ws_name() in models.py.
//
// The client escapes workspace names itself, and the stored name is the escaped
// form. Browsers used to escape differently: Chrome, Safari and Edge used
// myescape() from objects.js, while Firefox used the native escape(). This maps
// any of those forms (or an unescaped name) to the myescape() form:
//   - Native escape() leaves @*+-./ unescaped; myescape() escapes them
//     (e.g. '-' -> '%2D').
//   - myescape() didn't pad %u escapes to 4 hex digits before 2026-10-26;
//     they're padded now, like native escape() ('%u5D9' -> '%u05D9'). No
//     stored name had one. An unpadded escape followed by a hex digit
//     ('%u5D9A' for U+05D9 'A') can't be told from a 4-digit one; it's read
//     as U+5D9A.
//   - Any other character myescape() would escape (e.g. a raw 'á' from a URL
//     typed by hand) is escaped ('á' -> '%E1').
//
// '%' always starts an escape sequence, since both functions escape a literal
// '%' as '%25'.
func canonicalWsName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if isMyescapeSafe(r) || r == '%' {
			b.WriteRune(r)
		} else {
			b.WriteString(myescapeRune(r))
		}
	}
	return padUEscapes(b.String())
}

// workspaceKeyID returns the key name of a workspace's Workspace entity: the
// SHA-1 of its canonical name.
func workspaceKeyID(name string) string {
	sum := sha1.Sum([]byte(canonicalWsName(name)))
	return hex.EncodeToString(sum[:])
}
