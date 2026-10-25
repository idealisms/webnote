package main

// Serving static files when running locally (WEBNOTE_SERVE_STATIC=1, see
// run_local.sh). On App Engine, app.yaml's handlers serve them before
// requests reach the app; this reads the same handlers.

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type staticHandler struct {
	pattern *regexp.Regexp // the handler's url, matched in full
	file    string         // static_files
	dir     string         // static_dir: url is a prefix
}

var yamlField = regexp.MustCompile(`^-?\s*(url|static_files|static_dir):\s*(\S+)\s*$`)

// readStaticHandlers reads the static handlers from an app.yaml file.
func readStaticHandlers(path string) ([]staticHandler, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var handlers []staticHandler
	var url string
	for _, line := range strings.Split(string(data), "\n") {
		m := yamlField.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		switch m[1] {
		case "url":
			url = m[2]
		case "static_files":
			handlers = append(handlers, staticHandler{
				pattern: regexp.MustCompile("^(?:" + url + ")$"), file: m[2]})
		case "static_dir":
			handlers = append(handlers, staticHandler{
				pattern: regexp.MustCompile("^" + regexp.QuoteMeta(url) + "/(.+)$"), dir: m[2]})
		}
	}
	return handlers, nil
}

// withStaticFiles serves the static handlers' files and passes other
// requests to next.
func withStaticFiles(handlers []staticHandler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range handlers {
			m := h.pattern.FindStringSubmatch(r.URL.Path)
			if m == nil {
				continue
			}
			file := h.file
			if h.dir != "" {
				file = filepath.Join(h.dir, filepath.Clean("/"+m[1]))
			}
			http.ServeFile(w, r, file)
			return
		}
		next.ServeHTTP(w, r)
	})
}
