package handler

import (
	"errors"
	"net/http"

	"github.com/P-kaizoku/small-light/internal/repository"
	"github.com/P-kaizoku/small-light/internal/service"
)

// statusFromError maps any error the services or repositories can return to an
// HTTP status and a client-safe message. This is the ONLY place where the
// error -> status translation happens.
func statusFromError(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrInvalidInput),
		errors.Is(err, service.ErrInvalidURL):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, service.ErrInvalidCredentials):
		return http.StatusUnauthorized, err.Error()
	case errors.Is(err, repository.ErrNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, service.ErrDuplicateEmail),
		errors.Is(err, repository.ErrConflict):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrLinkExpired):
		return http.StatusGone, err.Error()
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}
