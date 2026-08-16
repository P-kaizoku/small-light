package middleware

// HOW TO TEST Recover:
//
// Recover's behavior depends on whether the writer it receives is a
// *statusRecorder with a committed status. There are three scenarios:
//
//  1. Plain panic (no RequestLogger): the writer is httptest.NewRecorder (NOT
//     a statusRecorder) → expect JSON 500 with body {"error":...}.
//  2. Panic before any write, inside the full chain:
//     RequestLogger(log)(Recover(log)(panickingHandler)) → Recover sees the
//     statusRecorder with status == 0 → expect JSON 500.
//  3. Panic AFTER w.WriteHeader(200): statusRecorder.status != 0 → Recover
//     must NOT write anything → expect status 200, empty body. This asserts
//     the "never corrupt an in-flight body" contract.

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRecoverPlainPanic covers scenario 1: a panic with no statusRecorder in
// play must still yield a JSON 500. Required — panics must never take the
// server down, even if the middleware chain is misconfigured.
func TestRecoverPlainPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Recover(logger)(panicHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	// writeJSONError encodes {"error": msg} + a trailing newline.
	if want := `{"error":"internal server error"}` + "\n"; rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

// TestRecoverPanicInChain covers scenario 2: the real production chain
// (RequestLogger -> Recover) turns a pre-commit panic into a JSON 500. This
// is the ordering the middleware comments document, so it must work.
func TestRecoverPanicInChain(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	chain := RequestLogger(logger)(Recover(logger)(panicHandler))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// TestRecoverAfterCommit covers scenario 3: headers already sent, so a 500
// body would corrupt the response. Required to prove Recover respects the
// committed-state guard instead of double-writing.
func TestRecoverAfterCommit(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		panic("after commit")
	})

	// Must go through RequestLogger: only then is the handler's writer a
	// statusRecorder carrying status 200 for Recover to detect.
	chain := RequestLogger(logger)(Recover(logger)(handler))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (headers already sent)", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty (must not append a 500 body)", rec.Body.String())
	}
}
