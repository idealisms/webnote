package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"regexp"
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
		project: os.Getenv("GOOGLE_CLOUD_PROJECT")}
	var handler http.Handler = s
	if os.Getenv("WEBNOTE_SERVE_STATIC") == "1" {
		static, err := readStaticHandlers("app-go.yaml")
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
	now     func() time.Time
	save    func(context.Context, *datastore.Client, string, int64, []note, func() time.Time) (*Workspace, error)
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
	// Route patterns end with Python's $, which also matches before a final
	// newline.
	path = strings.TrimSuffix(path, "\n")
	method := func(allowed string, handle func()) {
		if r.Method != allowed {
			w.Header().Set("Allow", allowed)
			http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		handle()
	}
	switch path {
	case "/", "/webnote", "/webnote/index.html":
		method("GET", func() { s.redirect(req) })
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
	w.Write(body)
}

func (s *server) redirect(req *request) {
	req.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	req.w.Header().Set("Location", hostURL(req.r)+"/webnote/")
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
	writeBody(req.w, "text/plain; charset=utf-8", []byte(localtimeStr(ws.Time)))
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
	} else if n, ok := pyInt(v); ok {
		// ndb rejects offsets outside a non-negative int32.
		if !n.IsInt64() || n.Int64() < 0 || n.Int64() > math.MaxInt32 {
			s.badRequest(req, fmt.Errorf("offset %v out of range", n))
			return
		}
		offset = n.Int64()
	}
	times, err := notesTimes(req.r.Context(), s.client, ws.Key, int(offset), 11)
	if err != nil {
		s.serverError(req, err)
		return
	}
	dates := make([]string, len(times))
	for i, t := range times {
		dates[i] = localtimeStr(t)
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

// notesValue loads the notes of the revision the request asks for (by its
// time parameter) as a JSON value.
func (s *server) notesValue(req *request, ws *Workspace) (pyValue, error) {
	raw, err := ws.notesList(req.r.Context(), s.client, req.params.get("time"))
	if err != nil {
		return pyValue{}, err
	}
	return parsePyJSON(raw)
}

type pageValues struct {
	NameJS, NameHTML, LastTime, NotesJS string
	NextNoteNum                         int64
}

func (s *server) load(req *request, pathName string, fromPath bool) {
	name, err := workspaceName(req, pathName, fromPath)
	if err != nil {
		s.badRequest(req, err)
		return
	}
	values := pageValues{
		NameJS:   jsStringEscape(name),
		NameHTML: html.EscapeString(name),
		NotesJS:  "[]",
	}
	ws, err := getWorkspace(req.r.Context(), s.client, name)
	if err != nil {
		s.serverError(req, err)
		return
	}
	if ws != nil {
		values.NextNoteNum = ws.NextNoteNum
		values.LastTime = localtimeStr(ws.Time)
		notes, err := s.notesValue(req, ws)
		if err != nil {
			s.serverError(req, err)
			return
		}
		values.NotesJS = jsSafeJSON(pyDumps(notes))
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

// jsSafeJSON makes JSON safe in an inline script: '<', '>' and '&' only
// occur inside strings, where these escapes mean the same.
var jsSafeJSON = strings.NewReplacer("<", `<`, ">", `>`, "&", `&`).Replace

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
	notes, err := s.notesValue(req, ws)
	if err != nil {
		s.serverError(req, err)
		return
	}
	body, err := renderRSS(hostURL(req.r), ws, notes)
	if err != nil {
		s.serverError(req, err)
		return
	}
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
	nextNoteNum := int64(0)
	if n, ok := pyInt(save.nextNoteNum); ok {
		// Datastore integers are 64-bit; ndb fails the save otherwise.
		if !n.IsInt64() {
			s.badRequest(req, fmt.Errorf("nextNoteNum %v out of range", n))
			return
		}
		nextNoteNum = n.Int64()
	}
	ws, err := s.save(req.r.Context(), s.client, save.name, nextNoteNum, save.notes, s.now)
	if errors.Is(err, errTransactionFailed) {
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
		[]byte(`<return><status value="ok" update="`+localtimeStr(ws.Time)+`"/></return>`))
}
