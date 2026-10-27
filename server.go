package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"cloud.google.com/go/datastore"
)

//go:embed strings.js.en strings.js.de templates/workspace.gotmpl
var files embed.FS

var workspaceTemplate = template.Must(template.ParseFS(files, "templates/workspace.gotmpl"))

func main() {
	slog.SetDefault(newCloudLogger(os.Stdout))
	ctx := context.Background()
	client, err := datastore.NewClient(ctx, datastore.DetectProjectID)
	if err != nil {
		slog.Error("datastore.NewClient", "error", err)
		os.Exit(1)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	s := &server{client: client, now: time.Now, save: saveWorkspace,
		project: os.Getenv("GOOGLE_CLOUD_PROJECT"), version: os.Getenv("GAE_VERSION")}
	var handler http.Handler = s
	if os.Getenv("WEBNOTE_SERVE_STATIC") == "1" {
		static, err := readStaticHandlers("app.yaml")
		if err != nil {
			slog.Error("readStaticHandlers", "error", err)
			os.Exit(1)
		}
		handler = withStaticFiles(static, s)
	}
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("ListenAndServe", "error", err)
		os.Exit(1)
	}
}

type server struct {
	client  *datastore.Client
	project string // for grouping log lines with requests
	version string // GAE_VERSION
	now     func() time.Time
	save    func(context.Context, *datastore.Client, string, int64, []note, int64, func() time.Time) (*Workspace, error)
}

// webapp2 answers 501 for methods outside this set, before routing.
var knownMethods = map[string]bool{
	"GET": true, "POST": true, "HEAD": true, "OPTIONS": true, "PUT": true,
	"DELETE": true, "TRACE": true,
}

var (
	rssRoute  = regexp.MustCompile(`^/webnote/(.+)\.xml$`)
	loadRoute = regexp.MustCompile(`^/webnote/(.+)$`)
)

type request struct {
	w      http.ResponseWriter
	r      *http.Request
	params queryParams
}

// formatTime formats a time for getrecent.py, getdates.py and save.py: as
// epochMicros() if the client asks with epoch=1, and otherwise in US/Eastern
// for pages loaded before the client asked (see notesList()).
func (req *request) formatTime(t time.Time) string {
	if req.params.get("epoch") == "1" {
		return epochMicros(t)
	}
	return localtimeStr(t)
}

// ServeHTTP routes like main.py's webapp2 routes.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !knownMethods[r.Method] {
		http.Error(w, "501 Not Implemented", http.StatusNotImplemented)
		return
	}
	req := &request{w: w, r: r, params: parseQuery(r.URL.RawQuery)}
	// The path is decoded once. webob failed if that wasn't UTF-8, but the
	// native escape() writes U+0080-U+00FF as Latin-1 bytes (%E9).
	path := utf8OrLatin1(r.URL.Path)
	method := func(allowed string, handle func()) {
		// HEAD is served like GET; net/http drops the body.
		if allowed == "GET" && r.Method == "HEAD" {
			handle()
			return
		}
		if r.Method != allowed {
			if allowed == "GET" {
				allowed = "GET, HEAD"
			}
			w.Header().Set("Allow", allowed)
			http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		handle()
	}
	switch path {
	case "/", "/webnote", "/webnote/index.html":
		method("GET", func() { s.redirect(req, "/webnote/") })
	// The FAQ and hints are sections of the home page now.
	case "/webnote/FAQ.html":
		method("GET", func() { s.redirect(req, "/webnote/#questions") })
	case "/webnote/hints.html":
		method("GET", func() { s.redirect(req, "/webnote/#tips") })
	case "/webnote/strings.js":
		method("GET", func() { s.strings(req) })
	case "/webnote/save.py":
		method("POST", func() { s.saveHandler(req) })
	case "/webnote/getrecent.py":
		method("GET", func() { s.getRecent(req) })
	case "/webnote/getdates.py":
		method("GET", func() { s.getDates(req) })
	case "/webnote/rss.py":
		method("GET", func() { s.rss(req, "", false) })
	case "/webnote/load.py":
		method("GET", func() { s.load(req, "", false) })
	case "/webnote/jserror":
		method("POST", func() { s.jsError(req) })
	default:
		if m := rssRoute.FindStringSubmatch(path); m != nil {
			method("GET", func() { s.rss(req, m[1], true) })
		} else if m := loadRoute.FindStringSubmatch(path); m != nil {
			method("GET", func() { s.load(req, m[1], true) })
		} else {
			http.Error(w, "404 Not Found", http.StatusNotFound)
		}
	}
}

// badRequest answers a request that can't be served as sent. Python
// answered these with a 500.
func (s *server) badRequest(req *request, err error) {
	slog.Warn("bad request", "path", req.r.URL.Path, "error", err,
		traceAttr(s.project, req.r))
	http.Error(req.w, "400 Bad Request", http.StatusBadRequest)
}

func (s *server) serverError(req *request, err error) {
	slog.Error("request failed", "path", req.r.URL.Path, "error", err,
		traceAttr(s.project, req.r))
	http.Error(req.w, "500 Internal Server Error", http.StatusInternalServerError)
}

// hostURL is webob's request.host_url, with the scheme gunicorn sees behind
// App Engine's front end.
func hostURL(r *http.Request) string {
	scheme := "http"
	if r.Header.Get("X-Forwarded-Proto") == "https" ||
		r.Header.Get("X-Forwarded-Protocol") == "ssl" ||
		r.Header.Get("X-Forwarded-Ssl") == "on" {
		scheme = "https"
	}
	host, port, hasPort := strings.Cut(r.Host, ":")
	url := scheme + "://" + host
	if hasPort && port != "" && !(scheme == "http" && port == "80") && !(scheme == "https" && port == "443") {
		url += ":" + port
	}
	return url
}

func writeBody(w http.ResponseWriter, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	// Set even past net/http's 2 KB buffer, so HEAD has it too.
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
}

func (s *server) redirect(req *request, path string) {
	req.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	req.w.Header().Set("Location", hostURL(req.r)+path)
	req.w.WriteHeader(http.StatusMovedPermanently)
}

// language picks strings.js's language from Accept-Language, like
// StringHandler.getLanguage().
func language(acceptLanguage []string) string {
	header := "en"
	if len(acceptLanguage) > 0 {
		header = strings.Join(acceptLanguage, ",")
	}
	for _, token := range strings.Split(header, ",") {
		lang, _, _ := strings.Cut(token, ";")
		lang = strings.ToLower(string(pyStrip(latin1Runes(lang))))
		if strings.HasPrefix(lang, "de") {
			return "de"
		} else if strings.HasPrefix(lang, "en") {
			return "en"
		}
	}
	return "en"
}

func (s *server) strings(req *request) {
	body, err := files.ReadFile("strings.js." + language(req.r.Header.Values("Accept-Language")))
	if err != nil {
		s.serverError(req, err)
		return
	}
	writeBody(req.w, "text/javascript; charset=utf-8", body)
}

func (s *server) getRecent(req *request) {
	req.w.Header().Set("Content-Type", "text/plain")
	name := req.params.get("name")
	if name == "" {
		return
	}
	ws, err := getWorkspace(req.r.Context(), s.client, name)
	if err != nil {
		s.serverError(req, err)
		return
	} else if ws == nil {
		return
	}
	writeBody(req.w, "text/plain; charset=utf-8", []byte(req.formatTime(ws.Time)))
}

func (s *server) getDates(req *request) {
	req.w.Header().Set("Content-Type", "text/plain")
	name := req.params.get("name")
	if name == "" {
		return
	}
	ws, err := getWorkspace(req.r.Context(), s.client, name)
	if err != nil {
		s.serverError(req, err)
		return
	} else if ws == nil {
		return
	}
	offset := int64(0)
	if v, ok := req.params.lookup("offset"); !ok {
		offset = 0
	} else if n, err := parseInt(v); err == nil || errors.Is(err, strconv.ErrRange) {
		// ndb rejects offsets outside a non-negative int32. Anything else
		// that isn't a number is 0, like before.
		if err != nil || n < 0 || n > math.MaxInt32 {
			s.badRequest(req, fmt.Errorf("offset %q out of range", v))
			return
		}
		offset = n
	}
	times, err := notesTimes(req.r.Context(), s.client, ws.Key, int(offset), 11)
	if err != nil {
		s.serverError(req, err)
		return
	}
	dates := make([]string, len(times))
	for i, t := range times {
		dates[i] = req.formatTime(t)
	}
	// Writing, even nothing, adds the charset.
	writeBody(req.w, "text/plain; charset=utf-8", []byte(strings.Join(dates, "|")))
}

// workspaceName is LoadHandler.get_workspace_name(): the name from the path
// (decoded once more), or the name parameter. An error is a bad request.
func workspaceName(req *request, pathName string, fromPath bool) (string, error) {
	name := ""
	if fromPath {
		name = string(pyUnquote([]rune(pathName)))
	} else {
		name = req.params.get("name")
	}
	if name == "" {
		return "", errors.New("no workspace name")
	}
	return name, nil
}

// notesJSON loads the notes of the revision the request asks for (by its
// time parameter), as stored: valid JSON, though not necessarily a list.
func (s *server) notesJSON(req *request, ws *Workspace) (json.RawMessage, error) {
	return ws.notesList(req.r.Context(), s.client, req.params.get("time"))
}

type pageValues struct {
	NameJS, NameHTML, NotesJS string
	// FeedURL is the workspace's RSS feed, escaped twice like the links
	// the client builds (escape(escape(myescape(name)))).
	FeedURL string
	// LoadedTime is the workspace's time as epochMicros(), or 0.
	LoadedTime  string
	NextNoteNum int64
	// Version is the deployed version, so a new page doesn't run scripts
	// cached from an older one.
	Version string
}

func (s *server) load(req *request, pathName string, fromPath bool) {
	name, err := workspaceName(req, pathName, fromPath)
	if err != nil {
		s.badRequest(req, err)
		return
	}
	values := pageValues{
		// Old links can have unpadded %u escapes, which unescape() can't
		// decode.
		NameJS:     jsStringEscape(padUEscapes(name)),
		NameHTML:   html.EscapeString(name),
		FeedURL:    html.EscapeString("/webnote/" + pyQuote(pyQuote(canonicalWsName(name))) + ".xml"),
		NotesJS:    "[]",
		LoadedTime: "0",
		Version:    url.QueryEscape(s.version),
	}
	ws, err := getWorkspace(req.r.Context(), s.client, name)
	if err != nil {
		s.serverError(req, err)
		return
	}
	if ws != nil {
		values.NextNoteNum = ws.NextNoteNum
		values.LoadedTime = epochMicros(ws.Time)
		notes, err := s.notesJSON(req, ws)
		if err != nil {
			s.serverError(req, err)
			return
		}
		values.NotesJS = jsSafeJSON(string(notes))
	}
	var b bytes.Buffer
	if err := workspaceTemplate.Execute(&b, values); err != nil {
		s.serverError(req, err)
		return
	}
	writeBody(req.w, "text/html; charset=utf-8", b.Bytes())
}

// jsStringEscape escapes a string for a single-quoted JavaScript string in
// an inline script. Names made of the characters myescape() outputs are
// unchanged, as in the Python page, which didn't escape them.
func jsStringEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '<', '>', '&', 0x2028, 0x2029:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// jsSafeJSON makes JSON safe in an inline script, so that a note can't end
// the script with "</script>" or start a comment with "<!--". '<', '>', '&',
// U+2028 and U+2029 only occur inside strings, where these escapes mean the
// same. (Scripts allow raw U+2028 and U+2029 only since ES2019.)
var jsSafeJSON = strings.NewReplacer(
	"<", "\\u003c", ">", "\\u003e", "&", "\\u0026",
	"\u2028", "\\u2028", "\u2029", "\\u2029",
).Replace

func (s *server) rss(req *request, pathName string, fromPath bool) {
	name, err := workspaceName(req, pathName, fromPath)
	if err != nil {
		s.badRequest(req, err)
		return
	}
	ws, err := getWorkspace(req.r.Context(), s.client, name)
	if err != nil {
		s.serverError(req, err)
		return
	} else if ws == nil {
		req.w.Header().Set("Content-Type", "text/html; charset=utf-8")
		req.w.WriteHeader(http.StatusNotFound)
		return
	}
	// A feed only changes when the workspace is saved. Feed readers poll
	// (one polls every 8 seconds), so let them keep it for a minute, and
	// answer their If-Modified-Since without reading the notes.
	lastModified := ws.Time.UTC().Truncate(time.Second)
	cacheHeaders := func() {
		req.w.Header().Set("Cache-Control", "max-age=60")
		req.w.Header().Set("Last-Modified", lastModified.Format(http.TimeFormat))
	}
	if since, err := http.ParseTime(req.r.Header.Get("If-Modified-Since")); err == nil && !lastModified.After(since) {
		cacheHeaders()
		req.w.WriteHeader(http.StatusNotModified)
		return
	}
	notes, err := s.notesJSON(req, ws)
	if err != nil {
		s.serverError(req, err)
		return
	}
	body, err := renderRSS(hostURL(req.r), ws, notes)
	if err != nil {
		s.serverError(req, err)
		return
	}
	cacheHeaders()
	writeBody(req.w, "text/xml; charset=utf-8", body)
}

func (s *server) saveHandler(req *request) {
	body, err := io.ReadAll(req.r.Body)
	if err != nil {
		s.serverError(req, err)
		return
	}
	save, err := parseSaveBody(body)
	if err != nil {
		s.badRequest(req, err)
		return
	}
	// A nextNoteNum that isn't a number is 0, like before, rather than
	// losing the save.
	nextNoteNum, err := parseInt(save.nextNoteNum)
	if errors.Is(err, strconv.ErrRange) {
		// Datastore integers are 64-bit; ndb fails the save otherwise.
		s.badRequest(req, fmt.Errorf("nextNoteNum %q out of range", save.nextNoteNum))
		return
	} else if err != nil {
		nextNoteNum = 0
	}
	// loaded is the client's workspace.loadedTime. Clients loaded before it
	// was sent don't check for conflicts.
	loaded := int64(-1)
	if v, ok := req.params.lookup("loaded"); ok {
		loaded, err = strconv.ParseInt(v, 10, 64)
		if err != nil || loaded < 0 {
			s.badRequest(req, fmt.Errorf("loaded=%q", v))
			return
		}
	}
	ws, err := s.save(req.r.Context(), s.client, save.name, nextNoteNum, save.notes, loaded, s.now)
	if errors.Is(err, errConflict) {
		// Someone else saved since this client loaded the workspace. The
		// client can save again with this time as loaded.
		slog.Info("save conflict", "name", save.name, traceAttr(s.project, req.r))
		req.w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		req.w.WriteHeader(http.StatusConflict)
		io.WriteString(req.w, `<return><status value="conflict" update="`+req.formatTime(ws.Time)+`"/></return>`)
		return
	} else if errors.Is(err, errTooLarge) {
		// Logged like a failed transaction, so webnote_failed_saves counts it.
		slog.Error("Failed to save workspace", "name", save.name, "error", err,
			traceAttr(s.project, req.r))
		req.w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		req.w.WriteHeader(http.StatusRequestEntityTooLarge)
		io.WriteString(req.w, `<return><status value="toolarge"/></return>`)
		return
	} else if errors.Is(err, errTransactionFailed) {
		slog.Error("Failed to save workspace", "name", save.name, "error", err,
			traceAttr(s.project, req.r))
		req.w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		req.w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(req.w, `<return><status value="error"/></return>`)
		return
	} else if err != nil {
		s.serverError(req, err)
		return
	}
	// The client compares this against getrecent.py to detect saves from
	// other clients, so it must use the same format.
	writeBody(req.w, "text/xml; charset=utf-8",
		[]byte(`<return><status value="ok" update="`+req.formatTime(ws.Time)+`"/></return>`))
}

// jsReport is what the client's error beacon sends (reportErrors() in
// webnote.js).
type jsReport struct {
	Message string `json:"message"`
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Stack   string `json:"stack"`
	Version string `json:"version"`
}

// errorEventType makes Cloud Error Reporting pick up a log line that has no
// server stack trace. It groups the line by its "message".
const errorEventType = "type.googleapis.com/google.devtools.clouderrorreporting.v1beta1.ReportedErrorEvent"

// clip shortens s to at most n bytes, on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

// jsError logs an error from a page's script for Error Reporting. The
// beacon doesn't read the answer, so bad reports are dropped without an
// error.
func (s *server) jsError(req *request) {
	body, err := io.ReadAll(http.MaxBytesReader(req.w, req.r.Body, 8<<10))
	var report jsReport
	if err == nil && json.Unmarshal(body, &report) == nil && report.Message != "" {
		message := clip(report.Message, 500)
		// V8's stack starts with the error ("TypeError: x"); Firefox's
		// and Safari's are only the frames.
		event := clip(report.Stack, 4000)
		if event == "" {
			event = fmt.Sprintf("%s\n    at %s:%d:%d", message, clip(report.Source, 500), report.Line, report.Col)
		} else if !strings.HasPrefix(event, message) {
			event = message + "\n" + event
		}
		slog.Error(event,
			"@type", errorEventType,
			slog.Group("serviceContext",
				"service", "webnote-client",
				"version", clip(report.Version, 100)),
			slog.Group("context",
				slog.Group("httpRequest",
					"userAgent", clip(req.r.UserAgent(), 500))),
			traceAttr(s.project, req.r))
	}
	req.w.WriteHeader(http.StatusNoContent)
}
