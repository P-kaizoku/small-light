package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder wraps an http.ResponseWriter so RequestLogger can report the
// status code and bytes written after the handler has run. It tracks the two
// facts net/http does not expose: whether WriteHeader was called, and how many
// bytes were written.
//
// It is also what Recover depends on: Recover is mounted INSIDE RequestLogger,
// so the writer it receives is this recorder, and rec.status tells it whether
// the response headers were already committed before a panic (in which case a
// 500 can no longer be sent without corrupting an in-flight body).
//
// The zero value is not usable — always construct it as
// &statusRecorder{ResponseWriter: w}.
type statusRecorder struct {
	http.ResponseWriter
	status int // 0 = no explicit status yet; once set, the response is committed
	bytes  int
}

// WriteHeader records the status and forwards it. net/http permits only one
// explicit status per response; mirroring that, any later call is ignored so
// a duplicate call cannot clobber the status we log.
func (r *statusRecorder) WriteHeader(code int) {
	if r.status != 0 {
		return
	}
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write forwards the body and counts it. A handler that writes a body without
// ever calling WriteHeader has implicitly sent 200, so that is recorded too.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.bytes += len(b)
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController (Go 1.20+) reach the underlying writer,
// so features like Flush keep working through this wrapper.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// RequestLogger emits one structured log line per completed request. Mount it
// in the global chain so every route — including the /{code} redirect — is
// covered. It must be mounted BEFORE Recover (see the ordering note on
// statusRecorder).
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				// The handler never touched the writer (e.g. an empty handler);
				// net/http would have sent 200 on return, so report that.
				status = http.StatusOK
			}

			log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", rec.bytes,
				"duration", time.Since(start).Round(time.Microsecond).String(),
				"request_id", RequestIDFrom(r.Context()),
			)
		})
	}
}
