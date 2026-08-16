package middleware

// HOW TO TEST a middleware:
//
// Drive it with net/http/httptest — no server, no ports:
//
//	req := httptest.NewRequest(http.MethodGet, "/", nil)
//	rec := httptest.NewRecorder()
//	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		// assert on the request here, then mark the response
//		w.WriteHeader(http.StatusOK)
//	})
//	RequestID(next).ServeHTTP(rec, req)
//	// assert on rec.Code, rec.Header(), rec.Body
//
// Pattern to remember: a middleware is just a wrapper around `next` — assert
// BOTH what the wrapper adds (headers, context, status) and that `next` still
// ran (the recorder got the inner handler's status/body).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRequestIDGenerates asserts a request WITHOUT an incoming X-Request-ID
// header gets a fresh id. Required because the id is what ties a log line to
// a single request — if it were empty, request correlation breaks.
func TestRequestIDGenerates(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	RequestID(next).ServeHTTP(rec, req)

	header := rec.Header().Get("X-Request-ID")
	if header == "" {
		t.Error("X-Request-ID header is empty, want a generated id")
	}
	// The context value and the response header must be the SAME id — that is
	// the whole point of the context propagation.
	if got != header {
		t.Errorf("context id %q != response header %q", got, header)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (next must still run)", rec.Code)
	}
}

// TestRequestIDEchoesClient asserts a client-supplied X-Request-ID is honored
// verbatim. Required so a load balancer / client can correlate its own
// requests instead of getting an unrelated UUID back.
func TestRequestIDEchoesClient(t *testing.T) {
	const clientID = "abc-123"
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestIDFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", clientID)
	rec := httptest.NewRecorder()
	RequestID(next).ServeHTTP(rec, req)

	if got != clientID {
		t.Errorf("context id = %q, want client id %q (must not be replaced)", got, clientID)
	}
	if rec.Header().Get("X-Request-ID") != clientID {
		t.Errorf("response header = %q, want %q", rec.Header().Get("X-Request-ID"), clientID)
	}
}

// TestRequestIDFromEmpty documents the contract RequestLogger and Recover rely
// on: a context that never passed through RequestID yields "", so log lines
// can always call RequestIDFrom without a nil-check dance.
func TestRequestIDFromEmpty(t *testing.T) {
	if got := RequestIDFrom(context.Background()); got != "" {
		t.Errorf("RequestIDFrom(bare context) = %q, want empty", got)
	}
}
