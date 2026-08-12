package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Resolve handles GET /{code} — the public redirect (no auth).
func (h *Handler) Resolve(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	link, err := h.links.Resolve(r.Context(), code)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	http.Redirect(w, r, link.OriginalURL, http.StatusFound)
}
