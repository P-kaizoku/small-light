package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/P-kaizoku/small-light/internal/service"
)

// Handler is the HTTP adapter layer. It never touches SQL or JWT internals:
// it decodes requests, calls a service, and maps results to HTTP.
type Handler struct {
	auth    *service.AuthService
	links   *service.LinkService
	linkTTL time.Duration
	log     *slog.Logger
}

func New(auth *service.AuthService, links *service.LinkService, linkTTL time.Duration, log *slog.Logger) *Handler {
	return &Handler{auth: auth, links: links, linkTTL: linkTTL, log: log}
}

// AuthService exposes the auth service so the router can mount the auth
// middleware on protected routes.
func (h *Handler) AuthService() *service.AuthService { return h.auth }

// writeError translates err into a JSON error response. 5xx responses are
// logged server-side; the client only ever sees a generic message for those.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, msg := statusFromError(err)
	if status >= http.StatusInternalServerError {
		h.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
