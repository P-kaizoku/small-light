package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover catches a panic escaping from a downstream handler, logs it with a
// full stack trace, and returns a JSON 500 instead of letting the connection
// die mid-response. It is the middleware equivalent of a try/catch around
// every handler: a panic in one request must never take the whole server down.
//
// It MUST be mounted inside (i.e. after) RequestLogger: it relies on the
// *statusRecorder that RequestLogger wraps the writer in to know whether the
// response headers were already committed before the panic. If they were, the
// client already got a status code, and a 500 would only corrupt an in-flight
// body — so in that case we log and leave the response alone.
//
// It replaces chi's middleware.Recoverer, which logs to the server's default
// logger and writes a plain-text body; this one logs through our slog logger
// and returns JSON like the rest of the API.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log.Error("panic recovered",
						"err", err,
						"stack", string(debug.Stack()),
						"path", r.URL.Path,
						"request_id", RequestIDFrom(r.Context()),
					)

					if rec, ok := w.(*statusRecorder); ok && rec.status != 0 {
						// Headers already sent: nothing safe left to write.
						return
					}
					writeJSONError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
