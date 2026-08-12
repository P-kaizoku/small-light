package handler

import (
	"encoding/json"
	"net/http"

	"github.com/P-kaizoku/small-light/internal/middleware"
	"github.com/P-kaizoku/small-light/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Analytics handles GET /api/v1/links/{id}/analytics.
func (h *Handler) Analytics(w http.ResponseWriter, r *http.Request) {
	uid, ok := middleware.UserID(r.Context())
	if !ok {
		h.writeError(w, r, service.ErrInvalidCredentials)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "parse error"})
		return
	}
	link, err := h.links.Get(r.Context(), id, uid)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"link_id":     link.ID,
		"short_code":  link.ShortCode,
		"click_count": link.ClickCount,
	})
}
