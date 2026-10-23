package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"testing"
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
		const vm = require('vm'), fs = require('fs');
		const ctx = {};
		vm.createContext(ctx);
		vm.runInContext(fs.readFileSync('objects.js', 'utf8'), ctx);
		const chars = [];
		for (let i = 0; i < 0x10000; i++) chars.push(String.fromCharCode(i));
		chars.push('\u{1D400}', '\u{1F600}', 'my-notes.txt', 'Bogotá');
		console.log(JSON.stringify(chars.map(
			c => [c, escape(c), ctx.myescape(c)])));
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

// Checks every stored workspace name, like names_datatest.py. Needs a dump of
// the Workspace table (user data, not in the repo); see names_datatest.py.
// Failures report line numbers, not names.
func TestStoredNames(t *testing.T) {
	f, err := os.Open(".data/workspaces.jsonl")
	if os.IsNotExist(err) {
		t.Skip("needs .data/workspaces.jsonl")
	} else if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	escaped := regexp.MustCompile(`^([A-Za-z0-9_]|%[0-9A-F]{2}|%u[0-9A-F]{3,4})*$`)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 1<<20)
	n := 0
	for line := 1; scanner.Scan(); line++ {
		var w struct{ Key, Name string }
		if err := json.Unmarshal(scanner.Bytes(), &w); err != nil {
			t.Fatalf("line %d: %v", line, err)
		}
		if canonicalWsName(w.Name) != w.Name {
			t.Errorf("line %d: name isn't canonical", line)
		}
		if workspaceKeyID(w.Name) != w.Key {
			t.Errorf("line %d: key isn't the SHA-1 of the name", line)
		}
		if !escaped.MatchString(w.Name) {
			t.Errorf("line %d: name has unescaped characters", line)
		}
		n++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("checked %d names", n)
}
