package auth

import (
	"encoding/json"
	"net/http"

	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"

	"opspilot-backend/internal/models"
)

type Handler struct {
	db *sqlx.DB
}

func NewHandler(db *sqlx.DB) *Handler {
	return &Handler{db: db}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login authenticates a user and returns a JWT token
// @Summary User login
// @Description Authenticates user with email and password, returns JWT token
// @Tags auth
// @Accept json
// @Produce json
// @Param credentials body loginRequest true "Login credentials"
// @Success 200 {object} map[string]interface{} "User data"
// @Failure 400 {string} string "Invalid request body or missing credentials"
// @Failure 401 {string} string "Invalid credentials"
// @Failure 500 {string} string "Failed to generate token"
// @Router /auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		http.Error(w, "Email and password required", http.StatusBadRequest)
		return
	}

	var user models.User
	query := `SELECT id, org_id, email, password_hash, created_at FROM users WHERE email=$1`
	if err := h.db.Get(&user, query, req.Email); err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := GenerateToken(user.ID)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"token": token,
		"user": map[string]any{
			"id":         user.ID,
			"email":      user.Email,
			"created_at": user.CreatedAt,
		},
	})
}

// Logout clears the authentication cookie
// @Summary User logout
// @Description Clears the authentication cookie
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]bool "Success response"
// @Security BearerAuth
// @Router /auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// Me returns the current authenticated user
// @Summary Get current user
// @Description Returns the currently authenticated user's information
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]interface{} "User data"
// @Failure 401 {string} string "Unauthorized"
// @Failure 404 {string} string "User not found"
// @Security BearerAuth
// @Router /auth/me [get]
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var user models.User
	query := `SELECT id, org_id, email, created_at FROM users WHERE id=$1`
	if err := h.db.Get(&user, query, userID); err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"user": map[string]any{
			"id":         user.ID,
			"email":      user.Email,
			"created_at": user.CreatedAt,
		},
	})
}
