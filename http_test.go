package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/datastore"
)

// httpGolden is testdata/http.json, written by tools/golden_http.py running the
// Python app (git show 85a04b5:tools/golden_http.py). The Python app has been
// removed, so the file is fixed: record intended changes in fixedSincePython.
type httpGolden struct {
	SaveNow string `json:"save_now"`
	Seed    struct {
		Workspaces []struct {
			Key, Name, Time string
			NextNoteNum     int64
		}
		Notes []struct {
			Workspace, Time string
			Blob            []byte
			Meaning22       bool
		}
	}
	Cases []httpCase
}

type httpCase struct {
	Request struct {
		Method, Path string
		Headers      map[string]string
		Body         string
		BodyB64      []byte `json:"body_b64"`
	}
	Response struct {
		Status          int
		ContentType     string `json:"Content-Type"`
		Location, Allow string
		Body            []byte
	}
	Changed map[string]storedWorkspace
}

type storedWorkspace struct {
	Name        string
	NextNoteNum int64
	Time        string
	Revisions   int
	Latest      *struct {
		Time         string
		TimeToDelete *string `json:"time_to_delete"`
		Notes        string
	}
}

func readHTTPGolden(t *testing.T) *httpGolden {
	t.Helper()
	data, err := os.ReadFile("testdata/http.json")
	if err != nil {
		t.Fatal(err)
	}
	var g httpGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return &g
}

func parseUTC(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(timeLayout, s)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func putSeed(t *testing.T, ctx context.Context, client *datastore.Client, g *httpGolden) {
	t.Helper()
	for _, w := range g.Seed.Workspaces {
		key := datastore.NameKey("Workspace", w.Key, nil)
		ws := &Workspace{Name: w.Name, NextNoteNum: w.NextNoteNum, Time: parseUTC(t, w.Time)}
		if _, err := client.Put(ctx, key, ws); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range g.Seed.Notes {
		ws := &Workspace{Key: datastore.NameKey("Workspace", n.Workspace, nil)}
		putRawNotes(t, ws, parseUTC(t, n.Time), n.Blob, n.Meaning22)
	}
}

// storedState is stored_state() in golden_http.py.
func storedState(t *testing.T, ctx context.Context, client *datastore.Client) map[string]storedWorkspace {
	t.Helper()
	var workspaces []Workspace
	keys, err := client.GetAll(ctx, datastore.NewQuery("Workspace"), &workspaces)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]storedWorkspace{}
	for i, w := range workspaces {
		var notes []Notes
		q := datastore.NewQuery("Notes").FilterField("workspaceKey", "=", keys[i]).Order("-time")
		if _, err := client.GetAll(ctx, q, &notes); err != nil {
			t.Fatal(err)
		}
		s := storedWorkspace{Name: w.Name, NextNoteNum: w.NextNoteNum,
			Time: w.Time.UTC().Format(timeLayout), Revisions: len(notes)}
		if len(notes) > 0 {
			n := notes[0]
			s.Latest = &struct {
				Time         string
				TimeToDelete *string `json:"time_to_delete"`
				Notes        string
			}{Time: n.Time.UTC().Format(timeLayout)}
			if !n.TimeToDelete.IsZero() {
				ttd := n.TimeToDelete.UTC().Format(timeLayout)
				s.Latest.TimeToDelete = &ttd
			}
			if data, err := zlibDecompress(n.NotesJSONArray); err == nil && utf8Valid(data) {
				s.Latest.Notes = string(data)
			} else {
				s.Latest.Notes = "raw:" + base64Std(n.NotesJSONArray)
			}
		}
		state[keys[i].Name] = s
	}
	return state
}

// gaeRejects reports whether App Engine's front end answers a request path
// with a 400 before it reaches the app (checked on 2026-10-22: invalid
// escapes, %00 and raw spaces), so it can't happen here.
func gaeRejects(path string) bool {
	if strings.Contains(path, " ") || strings.Contains(path, "%00") {
		return true
	}
	_, err := url.ParseRequestURI(path)
	return err != nil
}

func (c *httpCase) do(s *server) *httptest.ResponseRecorder {
	body := c.Request.BodyB64
	if body == nil {
		body = []byte(c.Request.Body)
	}
	r := httptest.NewRequest(c.Request.Method, c.Request.Path, bytes.NewReader(body))
	r.Host = "localhost"
	for k, v := range c.Request.Headers {
		if k == "Host" {
			r.Host = v
		} else {
			r.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

func (c *httpCase) String() string {
	s := c.Request.Method + " " + c.Request.Path
	if c.Request.BodyB64 != nil {
		s += " " + strconv.Quote(string(c.Request.BodyB64))
	}
	if len(c.Request.Headers) > 0 {
		s += fmt.Sprint(" ", c.Request.Headers)
	}
	return s
}

func checkResponse(t *testing.T, c *httpCase, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := c.Response
	if rec.Code != want.Status {
		t.Errorf("%s: status %d, want %d (body %q)", c, rec.Code, want.Status, truncate(rec.Body.String()))
		return
	}
	if got := rec.Header().Get("Allow"); got != want.Allow {
		t.Errorf("%s: Allow %q, want %q", c, got, want.Allow)
	}
	if want.Status >= 400 {
		return // Error pages differ.
	}
	if got := rec.Header().Get("Content-Type"); got != want.ContentType {
		t.Errorf("%s: Content-Type %q, want %q", c, got, want.ContentType)
	}
	if got := rec.Header().Get("Location"); got != want.Location {
		t.Errorf("%s: Location %q, want %q", c, got, want.Location)
	}
	got := rec.Body.Bytes()
	if bytes.Contains(want.Body, []byte("function loadinit()")) {
		checkPage(t, c, got, want.Body)
	} else if !bytes.Equal(got, want.Body) {
		t.Errorf("%s: body\n got %q\nwant %q", c, truncate(string(got)), truncate(string(want.Body)))
	}
}

func truncate(s string) string {
	if len(s) > 3000 {
		return s[:3000] + "..."
	}
	return s
}

// The toolbar's icons and the RSS link are inline SVG since Python's GIFs
// (and the RSS link was a literal "%s.xml"; TestPageFeedLink checks it).
// checkPage compares the page around them.
var (
	pageControls = regexp.MustCompile(`(?s)<div class='controls'>.*?\n        </div>`)
	pageRSSLink  = regexp.MustCompile(`<a id='rsslink' href="[^"]*"[^>]*>.*?</a>`)
)

// pageValuePatterns find the values in a page: the name, notes, title,
// loadedTime and nextNoteNum. Python's script used var and set adminEmail
// after loadedTime; the module uses const and calls init().
var pageValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)workspace\.setName\(unescape\('(.*)'\)\);\n  workspace\.loadedTime`),
	regexp.MustCompile(`(?s)(?:var|const) notes = (.*);\n  for \((?:var|const) n in notes\)`),
	regexp.MustCompile(`(?s)<title> (.*)</title>\n</head>`),
	regexp.MustCompile(`workspace\.loadedTime = (.*);\n  (?:adminEmail|init)`),
	regexp.MustCompile(`workspace\.nextNoteNum = (.*);`),
}

// pageScripts is the head's scripts, which are ES modules since Python's
// classic scripts. checkPage compares the values in them, and the page around
// them byte for byte.
var pageScripts = regexp.MustCompile(`(?s)\n    <script .*</script>\n`)

// pageSincePython are changes to the page's markup since Python, applied to
// Python's page: event handlers are added by webnote.js instead of inline
// attributes, and the favicon links.
var pageSincePython = strings.NewReplacer(
	"<body onload='loadinit();' ", "<body ",
	`<link rel="SHORTCUT ICON" href="webnote_favicon.ico" type="image/x-icon">`,
	`<link rel="icon" href="webnote_favicon.ico" sizes="32x32">
    <link rel="icon" href="icons/favicon.svg" type="image/svg+xml">
    <link rel="apple-touch-icon" href="icons/apple-touch-icon.png">
    <link rel="manifest" href="icons/manifest.webmanifest">`,
	` onchange='workspace.filter(this.value)' onkeydown='if(13==event.keyCode){workspace.filter(this.value);}; event.cancelBubble=true;'/>`, " />",
)

// checkPage compares a workspace page with Python's. Python inserted values
// unescaped; this port escapes them for their context, so the values are
// compared after unescaping, and the rest byte for byte, except the
// scripts (see pageScripts). Since the port, loadedTime is epoch microseconds
// instead of a US/Eastern string.
func checkPage(t *testing.T, c *httpCase, got, want []byte) {
	t.Helper()
	gotSkeleton, wantSkeleton := string(got), string(want)
	// Fixed since Python: the template had a stray `"""`.
	wantSkeleton = strings.Replace(wantSkeleton, `type="text/css" />"""`, `type="text/css" />`, 1)
	for _, p := range []*regexp.Regexp{pageControls, pageRSSLink} {
		gotSkeleton = p.ReplaceAllString(gotSkeleton, "@@icons@@")
		wantSkeleton = p.ReplaceAllString(wantSkeleton, "@@icons@@")
	}
	wantSkeleton = pageSincePython.Replace(wantSkeleton)
	var gotValues, wantValues []string
	for _, p := range pageValuePatterns {
		gm, wm := p.FindStringSubmatchIndex(gotSkeleton), p.FindStringSubmatchIndex(wantSkeleton)
		if gm == nil || wm == nil {
			t.Errorf("%s: page value %v missing (got %v, want %v)", c, p, gm != nil, wm != nil)
			return
		}
		gotValues = append(gotValues, gotSkeleton[gm[2]:gm[3]])
		wantValues = append(wantValues, wantSkeleton[wm[2]:wm[3]])
		gotSkeleton = gotSkeleton[:gm[2]] + "@@" + gotSkeleton[gm[3]:]
		wantSkeleton = wantSkeleton[:wm[2]] + "@@" + wantSkeleton[wm[3]:]
	}
	gotSkeleton = pageScripts.ReplaceAllString(gotSkeleton, "\n@@scripts@@\n")
	wantSkeleton = pageScripts.ReplaceAllString(wantSkeleton, "\n@@scripts@@\n")
	if gotSkeleton != wantSkeleton {
		t.Errorf("%s: page differs outside the values\n got %q\nwant %q", c, truncate(gotSkeleton), truncate(wantSkeleton))
	}
	if name, err := jsUnescape(gotValues[0]); err != nil || name != wantValues[0] {
		t.Errorf("%s: page name %q (%v), want %q", c, gotValues[0], err, wantValues[0])
	}
	if strings.ContainsAny(gotValues[1], "<>&  ") {
		t.Errorf("%s: page notes aren't escaped for the script: %s", c, truncate(gotValues[1]))
	}
	if !sameJSONValue(gotValues[1], wantValues[1]) {
		t.Errorf("%s: page notes\n got %s\nwant %s", c, truncate(gotValues[1]), truncate(wantValues[1]))
	}
	if title := html.UnescapeString(gotValues[2]); title != wantValues[2] {
		t.Errorf("%s: page title %q, want %q", c, gotValues[2], wantValues[2])
	}
	loaded := "''"
	if us, err := strconv.ParseInt(gotValues[3], 10, 64); err != nil {
		loaded = "error: " + err.Error()
	} else if us != 0 {
		loaded = "'" + localtimeStr(time.UnixMicro(us)) + "'"
	}
	if loaded != wantValues[3] {
		t.Errorf("%s: page loadedTime %s (%s), want %s", c, gotValues[3], loaded, wantValues[3])
	}
	if gotValues[4] != wantValues[4] {
		t.Errorf("%s: page nextNoteNum %s, want %s", c, gotValues[4], wantValues[4])
	}
}

// sameJSONValue reports whether two JSON texts decode to the same value. The
// page passes stored notes through, so their formatting differs from
// Python's json.dumps(), but the browser sees the same values.
func sameJSONValue(got, want string) bool {
	var g, w any
	if json.Unmarshal([]byte(got), &g) != nil || json.Unmarshal([]byte(want), &w) != nil {
		return got == want
	}
	return reflect.DeepEqual(g, w)
}

// jsUnescape undoes jsStringEscape().
func jsUnescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			return "", errors.New("trailing backslash")
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'u':
			if i+5 > len(s) {
				return "", errors.New("short \\u escape")
			}
			v, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
			if err != nil {
				return "", err
			}
			b.WriteRune(rune(v))
			i += 4
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}

func newTestServer(client *datastore.Client, now time.Time) *server {
	return &server{client: client, now: func() time.Time { return now }, save: saveWorkspace}
}

// fixedResponse is how a golden case's response changed on purpose since the
// port: the response of another case (sameAs), or a status with a body that
// contains a string. Error bodies aren't compared.
type fixedResponse struct {
	sameAs   string
	status   int
	contains string
}

// fixedSincePython lists the golden cases where the Go version fixes a bug
// that the Python version had, keyed by httpCase.String().
var fixedSincePython = map[string]fixedResponse{
	// A path or query value that isn't UTF-8 is read as Latin-1, which is
	// what the native escape() writes for U+0080-U+00FF.
	"GET /webnote/caf%E9":                           {sameAs: "GET /webnote/caf%C3%A9"},
	"GET /webnote/Bogot%E1":                         {sameAs: "GET /webnote/Bogot%C3%A1"},
	"GET /webnote/%ED%A0%80":                        {status: 200, contains: "setName(unescape('í \u0080'))"},
	"GET /webnote/load.py?name=a%E9":                {status: 200, contains: "setName(unescape('aé'))"},
	"GET /webnote/load.py?x=%E9&name=a":             {status: 200, contains: "setName(unescape('a'))"},
	"GET /webnote/load.py?name=a&x=%E9":             {status: 200, contains: "setName(unescape('a'))"},
	"GET /webnote/load.py?name=%ED%A0%80":           {status: 200, contains: "setName(unescape('í \u0080'))"},
	"GET /webnote/load.py?name=%F4%90%80%80":        {status: 200, contains: "setName(unescape('ô\u0090\u0080\u0080'))"},
	"GET /webnote/load.py?name=%C0%80":              {status: 200, contains: "setName(unescape('À\u0080'))"},
	"GET /webnote/Sample%2520Workspace?x=%E9":       {sameAs: "GET /webnote/Sample%2520Workspace"},
	"GET /webnote/getrecent.py?name=caf%E9":         {sameAs: "GET /webnote/getrecent.py?name=caf%C3%A9"},
	"GET /webnote/getdates.py?name=caf%E9":          {sameAs: "GET /webnote/getdates.py?name=caf%C3%A9"},
	"GET /webnote/getrecent.py?name=Bogot%E1":       {status: 200, contains: "2017-12-30 19:00:00"},
	"GET /webnote/getdates.py?name=Bogot%E1":        {status: 200},
	"GET /webnote/getdates.py?name=many&offset=%E1": {sameAs: "GET /webnote/getdates.py?name=many&offset=abc"},

	// A time on a day pytz couldn't handle falls back to the latest
	// revision, like a bad time.
	"GET /webnote/Sample%2520Workspace?time=9999-12-31%2000:00:00": {sameAs: "GET /webnote/Sample%2520Workspace?time=bad"},

	// RSS writes every non-ASCII character as a character reference. Python
	// failed on U+0080-U+00FF, or showed them as the wrong characters.
	"GET /webnote/blob%2520unicode.xml":          {status: 200, contains: "<title>&#233; &#8232; &#128512; / &lt;/script&gt;</title>"},
	"GET /webnote/rss%2520latin1.xml":            {status: 200, contains: "<title>caf&#233;</title><description>caf&#233;</description>"},
	"GET /webnote/rss%2520utf8%2520escaped.xml":  {status: 200, contains: "<title>caf&#233;</title><description>caf&#233;</description>"},
	"GET /webnote/rss%2520valid%2520utf8.xml":    {status: 200, contains: "<title>&#195;&#169;</title><description>&#195;&#169;</description>"},
	"GET /webnote/rss%2520split.xml":             {status: 200, contains: "<title>&#169;</title><description>&#169;</description><guid isPermaLink=\"true\">http://localhost/webnote/rss%252520split#n1</guid></item><item><title>&#195;</title>"},
	"GET /webnote/caf%2525C3%2525A9%2520rss.xml": {status: 200, contains: "<title>caf&#233; rss</title>"},

	// A note id that isn't a string is its JSON text, not Python's str().
	"GET /webnote/blob%2520nonstring%2520id.xml": {status: 200, contains: "#1.5</guid></item><item><title>t</title><description>t</description><guid isPermaLink=\"true\">http://localhost/webnote/blob%252520nonstring%252520id#true</guid></item><item><title>t</title><description>t</description><guid isPermaLink=\"true\">http://localhost/webnote/blob%252520nonstring%252520id#null</guid>"},

	// The page passes the stored notes JSON through instead of Python's
	// json.dumps(). The browser reads 1e400 as Infinity either way.
	"GET /webnote/blob%2520overflow": {status: 200, contains: "const notes = [1e400, -1e400, 1e-400];"},

	// Requests that can't be served as sent are 400s, not 500s. (So are
	// saves Python answered with a 500; see badSaveSincePython.)
	"GET /webnote/load.py":                                           {status: 400},
	"GET /webnote/load.py?name":                                      {status: 400},
	"GET /webnote/load.py?name=":                                     {status: 400},
	"GET /webnote/load.py?nam=x":                                     {status: 400},
	"GET /webnote/load.py?NAME=a":                                    {status: 400},
	"GET /webnote/rss.py":                                            {status: 400},
	"GET /webnote/rss.py?name=":                                      {status: 400},
	"GET /webnote/getdates.py?name=many&offset=-1":                   {status: 400},
	"GET /webnote/getdates.py?name=many&offset=-14":                  {status: 400},
	"GET /webnote/getdates.py?name=many&offset=99999999999999999999": {status: 400},
	"GET /webnote/getdates.py?name=many&offset=2147483648":           {status: 400},
}

// stringsSincePython are changes to strings.js since the port, applied to
// Python's responses: times are shown in the browser's time zone, the
// history's error and the save conflict warning are new, and the collision
// warning is plain text.
var stringsSincePython = strings.NewReplacer(
	"all times are US Eastern Time (-0500)", "times are in your time zone",
	"alle Zeiten sind in US Eastern Time (-0500)", "alle Zeiten in Ihrer Zeitzone",
	"saved to this workspace.',\n", "saved to this workspace.',\n"+
		`  LOADVERSIONS_ERROR: 'Couldn\'t retrieve the version history.',
  LOADVERSIONS_RETRY: 'try again',
`,
	"Arbeitsplatz gespeichert.',\n", "Arbeitsplatz gespeichert.',\n"+
		`  LOADVERSIONS_ERROR: 'Die Versionen konnten nicht abgerufen werden.',
  LOADVERSIONS_RETRY: 'erneut versuchen',
`,
	// The collision warning is a banner, and its link a separate string.
	"  COLLISION_WARNING: \"This workspace has been changed by someone else while you were working on it.  If you want to see what the workspace looks like now, you can <a href='$1' target='_new'>open it in a new window</a>.\",\n",
	`  COLLISION_WARNING: "Someone else changed this workspace while you were working on it.",
  COLLISION_OPEN: 'Open it in a new tab',
  CONFLICT_WARNING: 'Someone else saved this workspace after you loaded it, so your notes were not saved.',
  CONFLICT_OPEN_NEWER: 'Open their version in a new tab',
  CONFLICT_SAVE_ANYWAY: 'Save mine anyway',
  CONFLICT_RELOAD: 'Load their version (discard my changes)',
  CONFLICT_KEEP_EDITING: 'Keep editing',
  DIALOG_CLOSE: 'Close',
`,
	"  COLLISION_WARNING: \"Dieser Arbeitsplatz wurde von jemand anderem ver\\u00E4ndert w\\u00E4hrend Du daran gearbeitet hast. Wenn Du sehen willst wie der Arbeitsplatz jetzt aussieht, kannst Du den<a href='$1' target='_new'>Arbeitsplatz in einem neuen Fenster \\u00F6ffnen</a>.\",\n",
	`  COLLISION_WARNING: "Dieser Arbeitsplatz wurde von jemand anderem ver\u00E4ndert, w\u00E4hrend Du daran gearbeitet hast.",
  COLLISION_OPEN: 'In einem neuen Tab \u00F6ffnen',
  CONFLICT_WARNING: 'Jemand anderes hat diesen Arbeitsplatz gespeichert, nachdem Du ihn geladen hast. Deine Notizen wurden deshalb nicht gespeichert.',
  CONFLICT_OPEN_NEWER: 'Die neuere Version in einem neuen Tab \u00F6ffnen',
  CONFLICT_SAVE_ANYWAY: 'Meine trotzdem speichern',
  CONFLICT_RELOAD: 'Die neuere Version laden (meine \u00C4nderungen verwerfen)',
  CONFLICT_KEEP_EDITING: 'Weiter bearbeiten',
  DIALOG_CLOSE: 'Schlie\u00DFen',
`,
)

// badSaveSincePython reports whether a golden case is a save that Python
// answered with a 500. Those were all bad XML or an out-of-range
// nextNoteNum, which are now 400s that still don't write.
func badSaveSincePython(c *httpCase) bool {
	return c.Request.Method == "POST" && strings.HasPrefix(c.Request.Path, "/webnote/save.py") &&
		c.Response.Status == 500
}

// checkFixed checks a response against its entry in fixedSincePython.
func checkFixed(t *testing.T, c *httpCase, f fixedResponse, cases map[string]*httpCase, rec *httptest.ResponseRecorder) {
	t.Helper()
	if f.sameAs != "" {
		other, ok := cases[f.sameAs]
		if !ok {
			t.Fatalf("%s: no case %q", c, f.sameAs)
		}
		fixed := *c
		fixed.Response = other.Response
		checkResponse(t, &fixed, rec)
		return
	}
	if rec.Code != f.status {
		t.Errorf("%s: status %d, want %d (body %q)", c, rec.Code, f.status, truncate(rec.Body.String()))
	} else if !strings.Contains(rec.Body.String(), f.contains) {
		t.Errorf("%s: body doesn't contain %q\n got %q", c, f.contains, truncate(rec.Body.String()))
	}
}

func TestHTTPMatchesPython(t *testing.T) {
	g := readHTTPGolden(t)
	cases := map[string]*httpCase{}
	for i := range g.Cases {
		c := &g.Cases[i]
		cases[c.String()] = c
		if strings.HasPrefix(c.Request.Path, "/webnote/strings.js") {
			c.Response.Body = []byte(stringsSincePython.Replace(string(c.Response.Body)))
		}
	}
	unused := map[string]bool{}
	for k := range fixedSincePython {
		unused[k] = true
	}
	check := func(c *httpCase, rec *httptest.ResponseRecorder) {
		t.Helper()
		if f, ok := fixedSincePython[c.String()]; ok {
			delete(unused, c.String())
			checkFixed(t, c, f, cases, rec)
		} else if badSaveSincePython(c) {
			checkFixed(t, c, fixedResponse{status: 400}, cases, rec)
		} else {
			checkResponse(t, c, rec)
		}
	}
	defer func() {
		for k := range unused {
			t.Errorf("fixedSincePython has %q, which isn't a golden case", k)
		}
	}()
	ctx, client := newTestClient(t)
	putSeed(t, ctx, client, g)
	before := storedState(t, ctx, client)
	s := newTestServer(client, parseUTC(t, g.SaveNow))

	var saves []*httpCase
	n := 0
	for i := range g.Cases {
		c := &g.Cases[i]
		if c.Changed != nil || c.Request.Path == "/webnote/save.py" && c.Request.Method == "POST" {
			saves = append(saves, c)
			continue
		}
		if gaeRejects(c.Request.Path) {
			continue
		}
		check(c, c.do(s))
		n++
	}
	if after := storedState(t, ctx, client); !reflect.DeepEqual(after, before) {
		t.Errorf("a read-only request wrote")
	}

	for _, c := range saves {
		emulatorReset(t)
		putSeed(t, ctx, client, g)
		check(c, c.do(s))
		changed := map[string]storedWorkspace{}
		for k, v := range storedState(t, ctx, client) {
			if !reflect.DeepEqual(before[k], v) {
				changed[k] = v
			}
		}
		if c.Changed == nil {
			c.Changed = map[string]storedWorkspace{}
		}
		if !reflect.DeepEqual(changed, c.Changed) {
			gotJSON, _ := json.Marshal(changed)
			wantJSON, _ := json.Marshal(c.Changed)
			t.Errorf("%s: stored\n got %s\nwant %s", c, gotJSON, wantJSON)
		}
		n++
	}
	t.Logf("checked %d requests (%d saves)", n, len(saves))
}

func TestSaveTransactionFailure(t *testing.T) {
	ctx, client := newTestClient(t)
	s := newTestServer(client, time.Now())
	s.save = func(context.Context, *datastore.Client, string, int64, []note, int64, func() time.Time) (*Workspace, error) {
		return nil, fmt.Errorf("%w: test", errTransactionFailed)
	}
	r := httptest.NewRequest("POST", "/webnote/save.py", strings.NewReader("<workspace name='ws' nextNoteNum='1'></workspace>"))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != 500 || rec.Body.String() != `<return><status value="error"/></return>` {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
	if ws, err := getWorkspace(ctx, client, "ws"); ws != nil || err != nil {
		t.Errorf("workspace saved: %v, %v", ws, err)
	}
}

func TestHostURLBehindAppEngine(t *testing.T) {
	// App Engine's front end sets X-Forwarded-Proto; gunicorn turns it into
	// the scheme, so production redirects to https.
	_, client := newTestClient(t)
	s := newTestServer(client, time.Now())
	for _, c := range []struct{ host, proto, want string }{
		{"test.aypwip.org", "https", "https://test.aypwip.org/webnote/"},
		{"test.aypwip.org:443", "https", "https://test.aypwip.org/webnote/"},
		{"localhost:80", "", "http://localhost/webnote/"},
		{"localhost:8080", "", "http://localhost:8080/webnote/"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = c.host
		if c.proto != "" {
			r.Header.Set("X-Forwarded-Proto", c.proto)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if got := rec.Header().Get("Location"); got != c.want {
			t.Errorf("Location for %s %s = %q, want %q", c.proto, c.host, got, c.want)
		}
	}
}

func TestSaveUpdateMatchesGetRecent(t *testing.T) {
	// The client compares the save's update time against getrecent.py to
	// detect saves from other clients, so the two must match exactly.
	_, client := newTestClient(t)
	s := newTestServer(client, time.Now())
	s.now = time.Now
	r := httptest.NewRequest("POST", "/webnote/save.py", strings.NewReader("<workspace name='ws' nextNoteNum='1'><note noteid='n'>x</note></workspace>"))
	saved := httptest.NewRecorder()
	s.ServeHTTP(saved, r)
	recent := httptest.NewRecorder()
	s.ServeHTTP(recent, httptest.NewRequest("GET", "/webnote/getrecent.py?name=ws", nil))
	if want := `<return><status value="ok" update="` + recent.Body.String() + `"/></return>`; saved.Body.String() != want || recent.Body.Len() != 19 {
		t.Errorf("save = %q, getrecent = %q", saved.Body.String(), recent.Body.String())
	}
}

func TestSaveConflict(t *testing.T) {
	ctx, client := newTestClient(t)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	s := &server{client: client, now: func() time.Time { return now }, save: saveWorkspace}
	save := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/webnote/save.py?epoch=1"+query, strings.NewReader(
			"<workspace name='ws' nextNoteNum='1'><note noteid='n'>x</note></workspace>"))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}
	var latest time.Time // the stored workspace's time
	for _, c := range []struct {
		query  string
		status int // 200 saves, 409 reports latest
	}{
		// A new workspace saves from 0, the page's loadedTime without one.
		{"&loaded=0", 200},
		// A page loaded before that save conflicts and writes nothing.
		{"&loaded=0", 409},
		{"&loaded=OLDER", 409},
		// Saving from the stored time, as "save anyway" does, works.
		{"&loaded=LATEST", 200},
		// Clients that don't send loaded replace whatever is there.
		{"", 200},
		{"&loaded=x", 400},
		{"&loaded=-1", 400},
	} {
		now = now.Add(time.Microsecond)
		query := strings.NewReplacer("OLDER", epochMicros(latest.Add(-time.Microsecond)),
			"LATEST", epochMicros(latest)).Replace(c.query)
		rec := save(query)
		want := ""
		switch c.status {
		case 200:
			latest = now
			want = `<return><status value="ok" update="` + epochMicros(latest) + `"/></return>`
		case 409:
			want = `<return><status value="conflict" update="` + epochMicros(latest) + `"/></return>`
		case 400:
			want = "400 Bad Request\n"
		}
		if rec.Code != c.status || rec.Body.String() != want {
			t.Errorf("save %q: got %d %q, want %d %q", query, rec.Code, rec.Body.String(), c.status, want)
		}
		ws, err := getWorkspace(ctx, client, "ws")
		if err != nil || !ws.Time.Equal(latest) {
			t.Errorf("save %q: stored workspace %+v, %v; want time %v", query, ws, err, latest)
		}
	}
	// Only the three successful saves wrote revisions.
	ws, _ := getWorkspace(ctx, client, "ws")
	if times, err := notesTimes(ctx, client, ws.Key, 0, 11); err != nil || len(times) != 3 {
		t.Errorf("notesTimes = %v, %v", times, err)
	}
}

func TestRSSCaching(t *testing.T) {
	_, client := newTestClient(t)
	saved := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	s := newTestServer(client, saved)
	r := httptest.NewRequest("POST", "/webnote/save.py", strings.NewReader("<workspace name='ws' nextNoteNum='1'><note noteid='n'>x</note></workspace>"))
	s.ServeHTTP(httptest.NewRecorder(), r)
	get := func(ifModifiedSince string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/webnote/ws.xml", nil)
		if ifModifiedSince != "" {
			r.Header.Set("If-Modified-Since", ifModifiedSince)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}
	const lastModified = "Fri, 23 Oct 2026 12:00:00 GMT"
	for _, c := range []struct {
		ifModifiedSince string
		status          int
	}{
		{"", 200},
		{lastModified, 304},
		{"Fri, 23 Oct 2026 12:00:01 GMT", 304},
		{"Fri, 23 Oct 2026 11:59:59 GMT", 200},
		{"not a date", 200},
	} {
		rec := get(c.ifModifiedSince)
		if rec.Code != c.status {
			t.Errorf("If-Modified-Since %q: status %d, want %d", c.ifModifiedSince, rec.Code, c.status)
		}
		if got := rec.Header().Get("Last-Modified"); got != lastModified {
			t.Errorf("If-Modified-Since %q: Last-Modified %q", c.ifModifiedSince, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "max-age=60" {
			t.Errorf("If-Modified-Since %q: Cache-Control %q", c.ifModifiedSince, got)
		}
		if c.status == 304 && rec.Body.Len() != 0 {
			t.Errorf("If-Modified-Since %q: 304 has a body", c.ifModifiedSince)
		}
	}
	// A missing feed isn't cached.
	r = httptest.NewRequest("GET", "/webnote/missing.xml", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != 404 || rec.Header().Get("Cache-Control") != "" {
		t.Errorf("missing feed: %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestEpochTimes(t *testing.T) {
	_, client := newTestClient(t)
	s := newTestServer(client, time.Time{})
	s.version = "v1"
	do := func(method, path, body string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s %s: status %d", method, path, rec.Code)
		}
		return rec.Body.String()
	}
	// Two saves in the same second (17:30:15 US/Eastern) are told apart.
	first := time.Date(2026, 10, 23, 21, 30, 15, 250_000_123, time.UTC)
	second := first.Add(500 * time.Millisecond)
	for i, now := range []time.Time{first, second} {
		s.now = func() time.Time { return now }
		got := do("POST", "/webnote/save.py?epoch=1", fmt.Sprintf("<workspace name='ws' nextNoteNum='1'><note noteid='n'>save%d</note></workspace>", i))
		if want := fmt.Sprintf(`<return><status value="ok" update="%d"/></return>`, now.UnixMicro()); got != want {
			t.Errorf("save %d = %q, want %q", i, got, want)
		}
	}
	firstUs, secondUs := fmt.Sprint(first.UnixMicro()), fmt.Sprint(second.UnixMicro())
	if got := do("GET", "/webnote/getrecent.py?name=ws&epoch=1", ""); got != secondUs {
		t.Errorf("getrecent = %q, want %q", got, secondUs)
	}
	if got, want := do("GET", "/webnote/getdates.py?name=ws&epoch=1", ""), secondUs+"|"+firstUs; got != want {
		t.Errorf("getdates = %q, want %q", got, want)
	}
	// Without epoch=1 (pages loaded before), times are US/Eastern as before.
	if got := do("GET", "/webnote/getrecent.py?name=ws", ""); got != "2026-10-23 17:30:15" {
		t.Errorf("getrecent without epoch = %q", got)
	}
	for _, c := range []struct{ time, want string }{
		{firstUs, "save0"},
		{secondUs, "save1"},
		// A US/Eastern time gets the latest revision in its second.
		{"2026-10-23%2017:30:15", "save1"},
		// A time with no revision gets none, as before.
		{fmt.Sprint(first.UnixMicro() + 1), "[]"},
		{"2026-10-23%2017:30:14", "[]"},
		// Times out of range fall back to the latest revision.
		{"999999999999999999", "save1"},
		{"99999999999999999999", "save1"},
	} {
		page := do("GET", "/webnote/ws?time="+c.time, "")
		if c.want == "[]" && !strings.Contains(page, "const notes = [];") || c.want != "[]" && !strings.Contains(page, `"text":"`+c.want+`"`) {
			t.Errorf("time=%s: page doesn't have %s", c.time, c.want)
		}
	}
	page := do("GET", "/webnote/ws", "")
	for _, want := range []string{
		"workspace.loadedTime = " + secondUs + ";",
		`src="strings.js?v=v1"`, `"./objects.js": "./objects.js?v=v1"`, `"./webnote.js": "./webnote.js?v=v1"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page doesn't contain %q", want)
		}
	}
	if page := do("GET", "/webnote/new", ""); !strings.Contains(page, "workspace.loadedTime = 0;") {
		t.Errorf("new workspace's page doesn't have loadedTime 0")
	}
}

func TestRSSNotesJSON(t *testing.T) {
	w := &Workspace{Name: "ws", Time: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)}
	for _, c := range []struct {
		notes string
		want  string // in the feed, or "" for errRSSNotes
	}{
		{`[]`, "</docs></channel>"},
		{`[{"id":"a","text":"x"},{"id":"b","text":"y"}]`, "#b</guid></item><item><title>x</title>"},
		{`[{"text":"no id"}]`, "<guid isPermaLink=\"true\">http://h/webnote/ws#</guid>"},
		{`[{"id":"no text"}]`, "<title></title><description></description>"},
		// Keys match exactly, as in Python: "Text" isn't "text".
		{`[{"id":"a","Text":"x","text":"y"}]`, "<title>y</title>"},
		{`[{"id":"a","text":"x","text":"y"}]`, "<title>y</title>"},
		{`[{"id":[1, {"a":2}],"text":"t"}]`, `#[1, {"a":2}]</guid>`},
		// encoding/json reads a lone surrogate as U+FFFD (Python kept it).
		{`[{"id":"a","text":"\ud800"}]`, "<title>&#65533;</title>"},
		{`[{"id":"a","text":"caf%C3%A9\n2"}]`, "<title>caf&#233;</title><description>caf&#233;\n2</description>"},
		{`{}`, ""},
		{`""`, ""},
		{`null`, ""},
		{`{"id":"a","text":"x"}`, ""},
		{`[null]`, ""},
		{`["a"]`, ""},
		{`[{"id":"a","text":null}]`, ""},
		{`[{"id":"a","text":5}]`, ""},
	} {
		body, err := renderRSS("http://h", w, []byte(c.notes))
		if c.want == "" {
			if err != errRSSNotes {
				t.Errorf("%s: got %q, %v; want errRSSNotes", c.notes, body, err)
			}
		} else if err != nil || !strings.Contains(string(body), c.want) {
			t.Errorf("%s: got %q, %v; want it to contain %q", c.notes, body, err, c.want)
		}
	}
}

func TestPageEscapesNotes(t *testing.T) {
	ctx, client := newTestClient(t)
	at := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	w := putWorkspace(t, ctx, client, "ws", at, 1)
	const notes = `[{"id":"a","text":"</script><!-- & \u2028 \u2029 >"}]`
	putNotes(t, ctx, client, w, at, strings.NewReplacer(`\u2028`, "\u2028", `\u2029`, "\u2029").Replace(notes))
	rec := httptest.NewRecorder()
	newTestServer(client, at).ServeHTTP(rec, httptest.NewRequest("GET", "/webnote/ws", nil))
	want := `const notes = [{"id":"a","text":"\u003c/script\u003e\u003c!-- \u0026 \u2028 \u2029 \u003e"}];`
	if page := rec.Body.String(); rec.Code != 200 || !strings.Contains(page, want) {
		t.Errorf("page %d doesn't contain %s:\n%s", rec.Code, want, truncate(page))
	}
}

// A page takes time linear in its notes: parsing them with a parser that
// copied the rest of the input for each token made large workspaces take
// 20-50 s to load (2026-10-23). Allocated bytes are a deterministic stand-in
// for time.
func TestLargePageAllocatesLinearly(t *testing.T) {
	ctx, client := newTestClient(t)
	at := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	w := putWorkspace(t, ctx, client, "ws", at, 1)
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 5000; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":"note%d","xPos":"%d","text":"caf%%E9 <b>%d</b> &amp; \"quoted\" \\u00e9"}`, i, i, i)
	}
	b.WriteString("]")
	putNotes(t, ctx, client, w, at, b.String())
	s := newTestServer(client, at)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/webnote/ws", nil))
	runtime.ReadMemStats(&after)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	alloc := after.TotalAlloc - before.TotalAlloc
	t.Logf("%d bytes of notes allocated %d bytes (%.1fx)", b.Len(), alloc, float64(alloc)/float64(b.Len()))
	if alloc > 50*uint64(b.Len()) {
		t.Errorf("a page of %d bytes of notes allocated %d bytes", b.Len(), alloc)
	}
}

// The toolbar's RSS link reaches the workspace's feed, for the page's URL
// and load.py, whatever form the name was requested in.
func TestPageFeedLink(t *testing.T) {
	ctx, client := newTestClient(t)
	at := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	s := newTestServer(client, at)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	href := regexp.MustCompile(`<a id='rsslink' href="([^"]*)">`)
	for _, name := range []string{"plain", "my notes", "Bogotá café", "日本 😀", "a%2Db"} {
		stored := canonicalWsName(name)
		putNotes(t, ctx, client, putWorkspace(t, ctx, client, stored, at, 1), at, `[{"id":"n","text":"x"}]`)
		for _, path := range []string{
			"/webnote/" + pyQuote(pyQuote(stored)), // what the client links to
			"/webnote/" + pyQuote(name),            // typed by hand
			"/webnote/load.py?name=" + pyQuote(stored),
		} {
			m := href.FindStringSubmatch(get(path).Body.String())
			if m == nil {
				t.Errorf("%s: no RSS link", path)
				continue
			}
			// Resolve the link against the page, as a browser does.
			ref, err := url.Parse(html.UnescapeString(m[1]))
			if err != nil {
				t.Errorf("%s: RSS link %q: %v", path, m[1], err)
				continue
			}
			link := (&url.URL{Path: "/webnote/"}).ResolveReference(ref).EscapedPath()
			feed := get(link)
			want := "<link>http://example.com" + strings.TrimSuffix(link, ".xml") + "</link>"
			if feed.Code != 200 || !strings.Contains(feed.Body.String(), want) {
				t.Errorf("%s: RSS link %s is a %d without %s", path, link, feed.Code, want)
			}
		}
	}
}

func TestJSError(t *testing.T) {
	var buf bytes.Buffer
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(newCloudLogger(&buf))
	s := &server{project: "webnote-184120"}
	post := func(body string) int {
		r := httptest.NewRequest("POST", "/webnote/jserror", strings.NewReader(body))
		r.Header.Set("User-Agent", "test-agent")
		r.Header.Set("X-Cloud-Trace-Context", "abc/1;o=1")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec.Code
	}
	report := func(fields map[string]any) string {
		b, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	v8 := "TypeError: x is undefined\n    at f (https://e.com/webnote/webnote.js?v=1:10:5)"
	firefox := "f@https://e.com/webnote/webnote.js?v=1:10:5\n"
	for _, body := range []string{
		report(map[string]any{"message": "TypeError: x is undefined", "stack": v8, "version": "20261026t1"}),
		report(map[string]any{"message": "TypeError: x is undefined", "stack": firefox, "version": "20261026t1"}),
		report(map[string]any{"message": "oops", "source": "https://e.com/webnote/webnote.js?v=1", "line": 3, "col": 7}),
		report(map[string]any{"message": strings.Repeat("m", 600), "stack": strings.Repeat("s", 5000)}),
		"not json",
		`{"message": ""}`,
		report(map[string]any{"message": "too big", "stack": strings.Repeat("s", 9000)}),
	} {
		if code := post(body); code != 204 {
			t.Errorf("%.40s: got %d, want 204", body, code)
		}
	}

	var lines []map[string]any
	dec := json.NewDecoder(&buf)
	for dec.More() {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 4 {
		t.Fatalf("got %d log lines, want 4 (bad and oversized reports dropped): %v", len(lines), lines)
	}
	want := map[string]any{
		"severity": "ERROR",
		"message":  v8,
		"@type":    errorEventType,
		"serviceContext": map[string]any{
			"service": "webnote-client", "version": "20261026t1"},
		"context": map[string]any{
			"httpRequest": map[string]any{"userAgent": "test-agent"}},
		"logging.googleapis.com/trace": "projects/webnote-184120/traces/abc",
	}
	if !reflect.DeepEqual(lines[0], want) {
		t.Errorf("got %v\nwant %v", lines[0], want)
	}
	for i, message := range []string{
		"TypeError: x is undefined\n" + firefox,
		"oops\n    at https://e.com/webnote/webnote.js?v=1:3:7",
	} {
		if got := lines[i+1]["message"]; got != message {
			t.Errorf("message %d: got %q, want %q", i+1, got, message)
		}
	}
	if got := lines[3]["message"].(string); len(got) != 500+1+4000 {
		t.Errorf("long fields: got a %d-byte message, want 4501", len(got))
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/webnote/jserror", nil))
	if rec.Code != 405 || rec.Header().Get("Allow") != "POST" {
		t.Errorf("GET: got %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}
