package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCloudLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := newCloudLogger(&buf)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Cloud-Trace-Context", "105445aa7843bc8bf206b120001000/1;o=1")
	logger.Error("request failed", "path", "/x", traceAttr("webnote-184120", r))
	logger.Warn("warn")
	logger.Info("info", traceAttr("", r))

	var lines []map[string]any
	dec := json.NewDecoder(&buf)
	for dec.More() {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	want := []map[string]any{
		{"severity": "ERROR", "message": "request failed", "path": "/x",
			"logging.googleapis.com/trace": "projects/webnote-184120/traces/105445aa7843bc8bf206b120001000"},
		{"severity": "WARNING", "message": "warn"},
		{"severity": "INFO", "message": "info"},
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("got %v\nwant %v", lines, want)
	}
}
