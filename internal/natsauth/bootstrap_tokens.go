package natsauth

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"opspilot-backend/internal/auth"
	"opspilot-backend/internal/models"
)

// GET /api/v1/bootstrap-tokens
func (h *Handler) ListBootstrapTokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.storage.GetUser(r.Context(), userID)
	if err != nil {
		log.Printf("ERROR bootstrap tokens: load user: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user == nil || user.OrgID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tokens, err := h.storage.GetBootstrapTokens(r.Context(), user.OrgID)
	if err != nil {
		log.Printf("ERROR bootstrap tokens: list org_id=%s: %v", user.OrgID, err)
		respondError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	respondJSON(w, http.StatusOK, tokens)
}

// POST /api/v1/bootstrap-tokens
func (h *Handler) CreateBootstrapToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.storage.GetUser(r.Context(), userID)
	if err != nil {
		log.Printf("ERROR bootstrap tokens: load user: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user == nil || user.OrgID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req models.CreateBootstrapTokenInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	result, err := h.storage.CreateBootstrapToken(r.Context(), user.OrgID, userID, req)
	if err != nil {
		log.Printf("ERROR bootstrap tokens: create org_id=%s user_id=%s: %v", user.OrgID, userID, err)
		respondError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	respondJSON(w, http.StatusCreated, result)
}

// GET /api/v1/bootstrap-tokens/{id}
func (h *Handler) GetBootstrapToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.storage.GetUser(r.Context(), userID)
	if err != nil {
		log.Printf("ERROR bootstrap tokens: load user: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user == nil || user.OrgID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tokenID := chi.URLParam(r, "id")
	if tokenID == "" {
		respondError(w, http.StatusBadRequest, "missing token id")
		return
	}

	token, err := h.storage.GetBootstrapToken(r.Context(), tokenID)
	if err != nil {
		log.Printf("ERROR bootstrap tokens: load token id=%s: %v", tokenID, err)
		respondError(w, http.StatusInternalServerError, "failed to load token")
		return
	}
	if token == nil || token.OrgID != user.OrgID {
		respondError(w, http.StatusNotFound, "token not found")
		return
	}

	respondJSON(w, http.StatusOK, token)
}

// DELETE /api/v1/bootstrap-tokens/{id}
func (h *Handler) RevokeBootstrapToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.storage.GetUser(r.Context(), userID)
	if err != nil {
		log.Printf("ERROR bootstrap tokens: load user: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user == nil || user.OrgID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tokenID := chi.URLParam(r, "id")
	if tokenID == "" {
		respondError(w, http.StatusBadRequest, "missing token id")
		return
	}

	token, err := h.storage.GetBootstrapToken(r.Context(), tokenID)
	if err != nil {
		log.Printf("ERROR bootstrap tokens: load token id=%s: %v", tokenID, err)
		respondError(w, http.StatusInternalServerError, "failed to load token")
		return
	}
	if token == nil || token.OrgID != user.OrgID {
		respondError(w, http.StatusNotFound, "token not found")
		return
	}

	if err := h.storage.RevokeBootstrapToken(r.Context(), tokenID); err != nil {
		log.Printf("ERROR bootstrap tokens: revoke id=%s: %v", tokenID, err)
		respondError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
