package dashauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mykelokuboyejo/meterbase/apps/api/internal/respond"
)

const sessionTTL = 7 * 24 * time.Hour

// Handler serves the /auth/* dashboard authentication endpoints.
type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID    string `json:"id"`
	OrgID string `json:"orgId"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type loginResponse struct {
	Token string       `json:"token"`
	User  userResponse `json:"user"`
}

// Login verifies email+password and returns a session token.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_body", "Invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		respond.Error(w, http.StatusBadRequest, "missing_fields", "email and password are required")
		return
	}

	user, hash, err := h.store.GetUserByEmailAnyOrg(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			respond.Error(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "An error occurred")
		return
	}

	if err := CheckPassword(hash, req.Password); err != nil {
		respond.Error(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
		return
	}

	rawToken, tokenHash, err := GenerateSessionToken()
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "An error occurred")
		return
	}

	if err := h.store.CreateSession(r.Context(), user.ID, user.OrgID, tokenHash, sessionTTL); err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "An error occurred")
		return
	}

	respond.JSON(w, http.StatusOK, loginResponse{
		Token: rawToken,
		User:  userResponse{ID: user.ID, OrgID: user.OrgID, Email: user.Email, Role: user.Role},
	})
}

// Logout deletes the session for the current bearer token.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	raw := bearerToken(r)
	if raw != "" {
		_ = h.store.DeleteSession(r.Context(), hashToken(raw))
	}
	respond.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Me returns the authenticated dashboard user.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "missing_session", "Not authenticated")
		return
	}
	respond.JSON(w, http.StatusOK, userResponse{
		ID:    user.ID,
		OrgID: user.OrgID,
		Email: user.Email,
		Role:  user.Role,
	})
}
