package handler

// HOW TO TEST statusFromError:
//
// It is the single error -> HTTP status translator, so every error the
// services/repositories can produce must map to the RIGHT status. A table
// test over the whole mapping keeps one glanceable list of the contract.

import (
	"errors"
	"net/http"
	"testing"

	"github.com/P-kaizoku/small-light/internal/repository"
	"github.com/P-kaizoku/small-light/internal/service"
)

// TestStatusFromError covers the full mapping table. Required — this is the
// contract every API consumer relies on (400/401/404/409/410), and the 500
// case proves unknown errors never leak their message to the client.
func TestStatusFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		msg  string
	}{
		{"invalid input", service.ErrInvalidInput, http.StatusBadRequest, service.ErrInvalidInput.Error()},
		{"invalid url", service.ErrInvalidURL, http.StatusBadRequest, service.ErrInvalidURL.Error()},
		{"invalid credentials", service.ErrInvalidCredentials, http.StatusUnauthorized, service.ErrInvalidCredentials.Error()},
		{"not found", repository.ErrNotFound, http.StatusNotFound, repository.ErrNotFound.Error()},
		{"duplicate email", service.ErrDuplicateEmail, http.StatusConflict, service.ErrDuplicateEmail.Error()},
		{"conflict", repository.ErrConflict, http.StatusConflict, repository.ErrConflict.Error()},
		{"link expired", service.ErrLinkExpired, http.StatusGone, service.ErrLinkExpired.Error()},
		// Unknown errors are the dangerous case: the client must only ever see
		// the generic message, never the underlying error string.
		{"unexpected", errors.New("boom"), http.StatusInternalServerError, "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, msg := statusFromError(tt.err)
			if status != tt.want {
				t.Errorf("status = %d, want %d", status, tt.want)
			}
			if msg != tt.msg {
				t.Errorf("msg = %q, want %q", msg, tt.msg)
			}
		})
	}
}
