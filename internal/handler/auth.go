package handler

import "net/http"

type credentials struct {
	Email    string
	Password string
}

// Register handles POST /api/v1/auth/register.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {

	body := credentials{}
	err := decodeJSON(w, r, &body)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	user, err := h.auth.Register(r.Context(), body.Email, body.Password)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

// Login handles POST /api/v1/auth/login.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	body := credentials{}
	err := decodeJSON(w, r, &body)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	user, err := h.auth.Login(r.Context(), body.Email, body.Password)
	if err != nil {

		h.writeError(w, r, err)
		return
	}

	token, err := h.auth.GenerateToken(user.ID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"access_token": token})

}
