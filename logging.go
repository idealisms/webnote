package main

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// newCloudLogger returns a logger that writes JSON lines for Cloud Logging,
// which reads "severity" and "message" and timestamps the line itself.
func newCloudLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.LevelKey:
				level, _ := a.Value.Any().(slog.Level)
				severity := "DEBUG"
				switch {
				case level >= slog.LevelError:
					severity = "ERROR"
				case level >= slog.LevelWarn:
					severity = "WARNING"
				case level >= slog.LevelInfo:
					severity = "INFO"
				}
				return slog.String("severity", severity)
			case slog.MessageKey:
				return slog.String("message", a.Value.String())
			case slog.TimeKey:
				return slog.Attr{}
			}
			return a
		},
	}))
}

// traceAttr groups a log line with its request in Cloud Logging, from App
// Engine's X-Cloud-Trace-Context header ("TRACE_ID/SPAN_ID;o=1").
func traceAttr(project string, r *http.Request) slog.Attr {
	trace, _, _ := strings.Cut(r.Header.Get("X-Cloud-Trace-Context"), "/")
	if project == "" || trace == "" {
		return slog.Attr{}
	}
	return slog.String("logging.googleapis.com/trace", "projects/"+project+"/traces/"+trace)
}
