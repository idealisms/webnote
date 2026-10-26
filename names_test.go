package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Documents how stored workspace names are normalized, like
// CanonicalWsNameTest in models_test.py. Stored names are the myescape() form
// from objects.js.
func TestCanonicalWsNameExamples(t *testing.T) {
	cases := []struct{ name, want string }{
		// Unaffected: letters, digits, '_' and escapes both browsers agree on.
		{"Sample%20Workspace", "Sample%20Workspace"},
		{"abc_XYZ_123", "abc_XYZ_123"},
		{"%25", "%25"},
		{"caf%E9", "caf%E9"},
		{"%u2665", "%u2665"},
		// Firefox (native escape) leaves @*+-./ unescaped.
		{"my-notes", "my%2Dnotes"},
		{"a.b/c@d*e+f", "a%2Eb%2Fc%40d%2Ae%2Bf"},
		// Firefox pads %u escapes for U+0100-U+0FFF to 4 digits.
		{"%u05D9%u05D5", "%u5D9%u5D5"},
		{"%u0100", "%u100"},
		// Raw characters are escaped the way myescape() does.
		{"a b", "a%20b"},
		{"Bogotá", "Bogot%E1"},
		{"י", "%u5D9"},
		{"♥", "%u2665"},
		{"\U0001d400", "%uD835%uDC00"}, // UTF-16 surrogate pair
		// Already canonical.
		{"my%2Dnotes", "my%2Dnotes"},
		{"%u5D9", "%u5D9"},
		{"", ""},
	}
	for _, c := range cases {
		if got := canonicalWsName(c.name); got != c.want {
			t.Errorf("canonicalWsName(%q) = %q, want %q", c.name, got, c.want)
		}
		if got := canonicalWsName(c.want); got != c.want {
			t.Errorf("canonicalWsName(%q) = %q, want it unchanged", c.want, got)
		}
	}
}

func TestWorkspaceKeyID(t *testing.T) {
	sum := sha1.Sum([]byte("tc%20testing"))
	if got, want := workspaceKeyID("tc%20testing"), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("workspaceKeyID = %q, want %q", got, want)
	}
	// Chrome, Safari and Edge saved "my-notes" as "my%2Dnotes"; Firefox saved
	// it as "my-notes". Both refer to the same workspace.
	sum = sha1.Sum([]byte("my%2Dnotes"))
	for _, name := range []string{"my-notes", "my%2Dnotes"} {
		if got, want := workspaceKeyID(name), hex.EncodeToString(sum[:]); got != want {
			t.Errorf("workspaceKeyID(%q) = %q, want %q", name, got, want)
		}
	}
}

// For every UTF-16 code unit (and some astral characters), checks that
// canonicalizing the raw character or Firefox's escape() output gives the same
// result as myescape() in objects.js.
func TestCanonicalWsNameMatchesJavaScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("needs node")
	}
	script := `
		import('./objects.js').then(({myescape}) => {
			const chars = [];
			for (let i = 0; i < 0x10000; i++) chars.push(String.fromCharCode(i));
			chars.push('\u{1D400}', '\u{1F600}', 'my-notes.txt', 'Bogotá');
			console.log(JSON.stringify(chars.map(
				c => [c, escape(c), myescape(c)])));
		});
	`
	out, err := exec.Command("node", "-e", script).Output()
	if err != nil {
		t.Fatal(err)
	}
	var cases [][3]string
	if err := json.Unmarshal(out, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 0x10004 {
		t.Fatalf("got %d cases from node", len(cases))
	}
	for i, c := range cases {
		raw, native, want := c[0], c[1], c[2]
		// A raw '%' is ambiguous: canonicalWsName() treats '%' as the start of
		// an escape, which is right for real (escaped) names. A lone surrogate
		// can't be a Go string; names reach the server escaped anyway.
		if raw != "%" && !(i >= 0xD800 && i <= 0xDFFF) {
			if got := canonicalWsName(raw); got != want {
				t.Errorf("canonicalWsName(%q) = %q, want %q", raw, got, want)
			}
		}
		if got := canonicalWsName(native); got != want {
			t.Errorf("canonicalWsName(%q) = %q, want %q (for %q)", native, got, want, raw)
		}
	}
}

// storedNames reads a dump of the Workspace table (user data, not in the
// repo), downloaded with:
//
//	TOKEN=$(gcloud auth print-access-token) python tools/dump_workspaces.py .data/workspaces.jsonl
//
// Failures report line numbers, not names.
func storedNames(t *testing.T) []struct{ Key, Name string } {
	t.Helper()
	f, err := os.Open(".data/workspaces.jsonl")
	if os.IsNotExist(err) {
		t.Skip("needs .data/workspaces.jsonl")
	} else if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 1<<20)
	var names []struct{ Key, Name string }
	for line := 1; scanner.Scan(); line++ {
		var w struct{ Key, Name string }
		if err := json.Unmarshal(scanner.Bytes(), &w); err != nil {
			t.Fatalf("line %d: %v", line, err)
		}
		names = append(names, w)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

// Every stored name is canonical, keyed by its SHA-1, and made of
// myescape() output: letters, digits, '_' and escapes.
func TestStoredNames(t *testing.T) {
	escaped := regexp.MustCompile(`^([A-Za-z0-9_]|%[0-9A-F]{2}|%u[0-9A-F]{3,4})*$`)
	names := storedNames(t)
	for i, w := range names {
		if canonicalWsName(w.Name) != w.Name {
			t.Errorf("line %d: name isn't canonical", i+1)
		}
		if workspaceKeyID(w.Name) != w.Key {
			t.Errorf("line %d: key isn't the SHA-1 of the name", i+1)
		}
		if !escaped.MatchString(w.Name) {
			t.Errorf("line %d: name has unescaped characters", i+1)
		}
	}
	t.Logf("checked %d names", len(names))
}

// storedUnpaddedU matches myescape()'s unpadded escapes of U+0100-U+0FFF (%u5D9),
// which the native unescape() can't decode, so these names don't survive
// the page's unescape() and the next save's myescape(). Known and
// pre-existing; see canonicalWsName().
var storedUnpaddedU = regexp.MustCompile(`%u[0-9A-F]{3}([^0-9A-F]|$)`)

// The page shows unescape(name), and the next save sends myescape() of the
// shown name. That gives back the stored name, except for the known
// unpadded %u names.
func TestStoredNamesBrowserRoundTrip(t *testing.T) {
	stored := storedNames(t)
	names := make([]string, len(stored))
	for i, w := range stored {
		names[i] = w.Name
	}
	script := `
		const fs = require('fs');
		import('./objects.js').then(({myescape}) => {
			const names = JSON.parse(fs.readFileSync(0, 'utf8'));
			console.log(JSON.stringify(names.map(n => myescape(unescape(n)))));
		});
	`
	var resaved []string
	runNode(t, script, names, &resaved)
	if len(resaved) != len(names) {
		t.Fatalf("node returned %d names for %d", len(resaved), len(names))
	}
	changed := 0
	for i, n := range names {
		if resaved[i] != n {
			changed++
			if !storedUnpaddedU.MatchString(n) {
				t.Errorf("line %d: name changes across a save", i+1)
			}
		}
	}
	t.Logf("%d names round-trip; %d known unpadded %%u names don't", len(names)-changed, changed)
}

// The client builds workspace URLs as escape(escape(storedName)). Stored
// names only contain [A-Za-z0-9_%], so that just escapes '%' twice. The URL
// must reach the stored name's page and feed.
func TestStoredNamesRoute(t *testing.T) {
	skipIfShort(t)
	names := storedNames(t)
	_, client := newTestClient(t)
	s := newTestServer(client, time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	for i, w := range names {
		if w.Name == "" {
			// save.py accepted an empty name (one such workspace exists),
			// but no URL can reach it: /webnote/ is the home page.
			continue
		}
		path := "/webnote/" + strings.ReplaceAll(w.Name, "%", "%2525")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "workspace.setName(unescape('"+w.Name+"'));") {
			t.Errorf("line %d: page is a %d for another name", i+1, rec.Code)
		}
		rec = httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", path+".xml", nil))
		if rec.Code != 404 { // No such workspace in the emulator.
			t.Errorf("line %d: feed is a %d", i+1, rec.Code)
		}
	}
}
