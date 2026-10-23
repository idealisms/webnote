package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
)

var unpaddedU = regexp.MustCompile(`%u0([0-9A-Fa-f]{3})`)

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
		hex := fmt.Sprintf("%02X", unit)
		if len(hex) > 2 {
			hex = "u" + hex
		}
		b.WriteString("%" + hex)
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
//   - Native escape() pads %u escapes to 4 hex digits; myescape() doesn't
//     (e.g. '%u05D9' -> '%u5D9').
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
	return unpaddedU.ReplaceAllString(b.String(), "%u$1")
}

// workspaceKeyID returns the key name of a workspace's Workspace entity: the
// SHA-1 of its canonical name.
func workspaceKeyID(name string) string {
	sum := sha1.Sum([]byte(canonicalWsName(name)))
	return hex.EncodeToString(sum[:])
}
