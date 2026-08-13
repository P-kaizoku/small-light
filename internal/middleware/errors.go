package middleware

import (
	"encoding/json"
	"net/http"
)

// writeJSONError writes the shared JSON error shape used by this package's
// middleware — {"error": "..."} — mirroring the responses the handlers emit.
// Keeping it here avoids duplicating the three lines in every middleware that
// rejects a request (unauthorized, rate limiting, panics).
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
