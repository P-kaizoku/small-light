package middleware

// HOW TO TEST RequestLogger:
//
// Capture the slog output instead of letting it hit stdout:
//
//	var buf bytes.Buffer
//	logger := slog.New(slog.NewTextHandler(&buf, nil))
//
// then serve a handler through RequestLogger(logger)(next) with httptest and
// assert strings.Contains(buf.String(), "status=200") etc. NOTE: TextHandler
// quotes string values, e.g. `status=200` for ints but `method=GET` is
// `method=GET`. Match on the exact keys you assert.
//
// statusRecorder is a plain struct in this package — you can also unit-test it
// directly by wrapping an httptest.NewRecorder and calling methods on it.

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequestLoggerLogsExplicitStatus asserts the log line reports the status
// a handler set explicitly. Required: the per-request log is the first place
// you look when a request misbehaves, so it must carry method/path/status.
func TestRequestLoggerLogsExplicitStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	RequestLogger(logger)(next).ServeHTTP(rec, req)

	line := buf.String()
	for _, want := range []string{"status=404", "method=GET", "path=/missing"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %q:\n%s", want, line)
		}
	}
}

// TestRequestLoggerImplicit200 asserts a handler that writes a body without
// ever calling WriteHeader is logged as 200 — the case statusRecorder has to
// infer. Required because most 2xx handlers (e.g. the redirect) never call
// WriteHeader explicitly; without this inference they'd be logged as 0.
func TestRequestLoggerImplicit200(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hi"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	RequestLogger(logger)(next).ServeHTTP(rec, req)

	line := buf.String()
	if !strings.Contains(line, "status=200") {
		t.Errorf("log line missing inferred status=200:\n%s", line)
	}
	if !strings.Contains(line, "bytes=2") {
		t.Errorf("log line missing bytes=2:\n%s", line)
	}
}

// TestStatusRecorderDuplicateWriteHeader asserts the recorder ignores a second
// WriteHeader, mirroring net/http. Required: a handler that double-writes
// must not clobber the status we log.
func TestStatusRecorderDuplicateWriteHeader(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner}

	rec.WriteHeader(http.StatusOK)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.status != http.StatusOK {
		t.Errorf("status = %d, want %d (first WriteHeader must win)", rec.status, http.StatusOK)
	}
}

// TestStatusRecorderUnwrap asserts Unwrap() returns the underlying writer —
// what http.ResponseController needs to reach Flush/Hijack through the
// wrapper. Required so the middleware stack does not silently break
// streaming responses.
func TestStatusRecorderUnwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner}

	if got := rec.Unwrap(); got != inner {
		t.Error("Unwrap() did not return the underlying writer")
	}
}
