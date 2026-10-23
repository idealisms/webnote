package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/datastore"
)

// httpGolden is testdata/http.json, written by tools/golden_http.py.
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

var pageValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)workspace\.setName\(unescape\('(.*)'\)\);\n  workspace\.loadedTime`),
	regexp.MustCompile(`(?s)var notes = (.*);\n  for \(var n in notes\)`),
	regexp.MustCompile(`(?s)<title> (.*)</title>\n</head>`),
}

// checkPage compares a workspace page with Python's. Python inserted values
// unescaped; this port escapes them for their context, so the values are
// compared after unescaping, and the rest byte for byte.
func checkPage(t *testing.T, c *httpCase, got, want []byte) {
	t.Helper()
	gotSkeleton, wantSkeleton := string(got), string(want)
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
	if gotSkeleton != wantSkeleton {
		t.Errorf("%s: page differs outside the values\n got %q\nwant %q", c, truncate(gotSkeleton), truncate(wantSkeleton))
	}
	if name, err := jsUnescape(gotValues[0]); err != nil || name != wantValues[0] {
		t.Errorf("%s: page name %q (%v), want %q", c, gotValues[0], err, wantValues[0])
	}
	if jsSafeJSON(wantValues[1]) != gotValues[1] {
		t.Errorf("%s: page notes\n got %s\nwant %s", c, gotValues[1], wantValues[1])
	}
	if title := html.UnescapeString(gotValues[2]); title != wantValues[2] {
		t.Errorf("%s: page title %q, want %q", c, gotValues[2], wantValues[2])
	}
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
		cases[g.Cases[i].String()] = &g.Cases[i]
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
	s.save = func(context.Context, *datastore.Client, string, int64, []note, func() time.Time) (*Workspace, error) {
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
