package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestStaticFilesFromAppYAML(t *testing.T) {
	handlers, err := readStaticHandlers("app.yaml")
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("app"))
	})
	h := withStaticFiles(handlers, next)
	for path, file := range map[string]string{
		"/webnote/":                           "index.html",
		"/webnote/webnote.js":                 "webnote.js",
		"/webnote/FAQ.html":                   "FAQ.html",
		"/robots.txt":                         "robots.txt",
		"/webnote/images/new.gif":             "images/new.gif",
		"/favicon.ico":                        "webnote_favicon.ico",
		"/webnote/icons/favicon.svg":          "icons/favicon.svg",
		"/webnote/icons/manifest.webmanifest": "icons/manifest.webmanifest",
		"/webnote/strings.js":                 "",
		"/webnote/webnote%2Ejs":               "webnote.js",
		"/webnote/webnoteXjs":                 "",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		want := []byte("app")
		if file != "" {
			if want, err = os.ReadFile(file); err != nil {
				t.Fatal(err)
			}
		}
		if rec.Body.String() != string(want) {
			t.Errorf("%s: got %d %.40q, want %s", path, rec.Code, rec.Body.String(), file)
		}
	}
	// Only files under a static_dir.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/webnote/images/../go.mod", nil))
	if rec.Code == http.StatusOK {
		t.Errorf("served a file outside images/: %.40q", rec.Body.String())
	}
}
