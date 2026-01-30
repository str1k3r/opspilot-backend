package natsauth

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"opspilot-backend/internal/models"
	"opspilot-backend/internal/storage"
)

type EnrollmentConfig struct {
	NATSURLs []string
}

type EnrollmentHandler struct {
	store  *storage.Storage
	issuer *JWTIssuer
	config EnrollmentConfig
}

func NewEnrollmentHandler(store *storage.Storage, issuer *JWTIssuer, cfg EnrollmentConfig) *EnrollmentHandler {
	return &EnrollmentHandler{store: store, issuer: issuer, config: cfg}
}

func (h *EnrollmentHandler) EnrollAgent(w http.ResponseWriter, r *http.Request) {
	if h.issuer == nil {
		respondError(w, http.StatusInternalServerError, "NATS JWT issuer not configured")
		return
	}

	var req models.EnrollAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.PublicKey = strings.TrimSpace(req.PublicKey)
	req.Hostname = strings.TrimSpace(req.Hostname)
	req.HardwareFingerprint = strings.TrimSpace(req.HardwareFingerprint)
	req.Nonce = strings.TrimSpace(req.Nonce)
	req.Signature = strings.TrimSpace(req.Signature)

	bootstrapToken := strings.TrimSpace(r.Header.Get("X-Bootstrap-Token"))
	if bootstrapToken == "" {
		bootstrapToken = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if bootstrapToken == "" {
		respondError(w, http.StatusUnauthorized, "missing bootstrap token")
		return
	}

	if req.AgentID == "" || req.PublicKey == "" || req.Hostname == "" || req.HardwareFingerprint == "" || req.Nonce == "" || req.Timestamp == 0 || req.Signature == "" {
		respondError(w, http.StatusBadRequest, "missing required fields")
		return
	}
	if len(req.AgentID) != 12 {
		respondError(w, http.StatusBadRequest, "invalid agent_id")
		return
	}

	if !VerifyNKeySignature(req.PublicKey, req.Nonce, req.Timestamp, req.Signature) {
		respondError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	if !isTimestampFresh(req.Timestamp, 5*time.Minute) {
		respondError(w, http.StatusUnauthorized, "timestamp expired")
		return
	}

	remoteIP := getClientIP(r)
	bt, err := h.store.ValidateBootstrapToken(r.Context(), bootstrapToken, remoteIP)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrTokenNotFound):
			respondError(w, http.StatusUnauthorized, "invalid token")
		case errors.Is(err, storage.ErrTokenRevoked):
			respondError(w, http.StatusUnauthorized, "token revoked")
		case errors.Is(err, storage.ErrTokenExpired):
			respondError(w, http.StatusUnauthorized, "token expired")
		case errors.Is(err, storage.ErrTokenUsageLimitReached):
			respondError(w, http.StatusUnauthorized, "token usage limit reached")
		case errors.Is(err, storage.ErrTokenIPNotAllowed):
			respondError(w, http.StatusUnauthorized, "token ip not allowed")
		default:
			respondError(w, http.StatusInternalServerError, "token validation failed")
		}
		return
	}

	existing, err := h.store.GetAgentByAgentID(req.AgentID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if existing != nil && existing.OrgID != "" && existing.OrgID != bt.OrgID {
		respondError(w, http.StatusForbidden, "agent belongs to different organization")
		return
	}

	existingKey, err := h.store.GetPinnedPublicKey(r.Context(), req.AgentID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if existingKey != "" && existingKey != req.PublicKey {
		respondError(w, http.StatusForbidden, "agent_id already registered with different key")
		return
	}

	agent, err := h.store.EnrollAgent(r.Context(), bt.OrgID, req, bt.ID, remoteIP)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "enrollment failed")
		return
	}

	if err := h.store.IncrementBootstrapTokenUsage(r.Context(), bt.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update token usage")
		return
	}

	jwtToken, expiresAt, err := h.issuer.IssueAgentJWT(req.AgentID, req.PublicKey, 365*24*time.Hour)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to issue JWT")
		return
	}

	if err := h.store.CreateAgentCredentials(
		r.Context(),
		req.AgentID,
		req.PublicKey,
		expiresAt,
		existingKey == "",
		req.HardwareFingerprint,
		remoteIP,
		req.Hostname,
	); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to store credentials")
		return
	}

	respondJSON(w, http.StatusCreated, models.EnrollAgentResponse{
		AgentID:   agent.AgentID,
		OrgID:     agent.OrgID,
		JWT:       jwtToken,
		NATSURLs:  h.config.NATSURLs,
		Tags:      agent.Tags,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	})
}

func isTimestampFresh(timestampMs int64, maxSkew time.Duration) bool {
	stamp := time.UnixMilli(timestampMs)
	return time.Since(stamp) <= maxSkew && time.Until(stamp) <= maxSkew
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]any{"error": message})
}
