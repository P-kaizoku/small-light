package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/P-kaizoku/small-light/internal/middleware"
	"github.com/P-kaizoku/small-light/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CreateLink handles POST /api/v1/links.
type newLink struct {
	URL string `json:"url"`
}

func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	uid, ok := middleware.UserID(r.Context())
	if !ok {
		h.writeError(w, r, service.ErrInvalidCredentials)
		return
	}
	body := newLink{}
	err := decodeJSON(w, r, &body)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	link, err := h.links.Create(r.Context(), uid, body.URL)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, link)
}

// ListLinks handles GET /api/v1/links.
func (h *Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
	uid, ok := middleware.UserID(r.Context())
	if !ok {
		h.writeError(w, r, service.ErrInvalidCredentials)
		return
	}

	links, err := h.links.List(r.Context(), uid)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, links)
}

// GetLink handles GET /api/v1/links/{id}.
func (h *Handler) GetLink(w http.ResponseWriter, r *http.Request) {
	uid, ok := middleware.UserID(r.Context())
	if !ok {
		h.writeError(w, r, service.ErrInvalidCredentials)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid id",
		})
		return
	}

	link, err := h.links.Get(r.Context(), id, uid)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// UpdateLink handles PATCH /api/v1/links/{id} (full replace).
type updateLink struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

func (h *Handler) UpdateLink(w http.ResponseWriter, r *http.Request) {
	uid, ok := middleware.UserID(r.Context())
	if !ok {
		h.writeError(w, r, service.ErrInvalidCredentials)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid id",
		})
		return
	}

	body := updateLink{}

	err = decodeJSON(w, r, &body)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	expiresAt := time.Now().Add(h.linkTTL)
	if body.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, body.ExpiresAt)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "parse error"})
			return
		}
		expiresAt = parsed
	}
	link, err := h.links.Update(r.Context(), id, uid, body.URL, expiresAt)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, link)
}

// DeleteLink handles DELETE /api/v1/links/{id}.
func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {

	uid, ok := middleware.UserID(r.Context())
	if !ok {
		h.writeError(w, r, service.ErrInvalidCredentials)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	err = h.links.Delete(r.Context(), id, uid)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
