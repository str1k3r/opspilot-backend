package natsauth

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"opspilot-backend/internal/storage"
)

const defaultJWTExpiry = 365 * 24 * time.Hour

type Handler struct {
	db        *sqlx.DB
	storage   *storage.Storage
	jwtIssuer *JWTIssuer
}

func NewHandler(db *sqlx.DB, storage *storage.Storage, issuer *JWTIssuer) *Handler {
	return &Handler{db: db, storage: storage, jwtIssuer: issuer}
}

type createAgentRequest struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
}

type credentialsResponse struct {
	CredsContent string    `json:"creds_content"` // .creds file format (recommended)
	NKeySeed     string    `json:"nkey_seed"`     // Legacy: separate seed
	JWT          string    `json:"jwt"`           // Legacy: separate JWT
	ExpiresAt    time.Time `json:"expires_at"`
}

type createAgentResponse struct {
	Agent struct {
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
	} `json:"agent"`
	Credentials    credentialsResponse `json:"credentials"`
	InstallCommand string              `json:"install_command"`
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	if h.jwtIssuer == nil {
		http.Error(w, "NATS JWT issuer not configured", http.StatusInternalServerError)
		return
	}
	var req createAgentRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	agentID := generateAgentID()
	seed, publicKey, err := GenerateUserKeyPair()
	if err != nil {
		http.Error(w, "Failed to generate NKey", http.StatusInternalServerError)
		return
	}

	jwtToken, expiresAt, err := h.jwtIssuer.IssueAgentJWT(agentID, publicKey, defaultJWTExpiry)
	if err != nil {
		http.Error(w, "Failed to issue JWT", http.StatusInternalServerError)
		return
	}

	tx, err := h.db.Beginx()
	if err != nil {
		http.Error(w, "Failed to create agent", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	recordID := uuid.New().String()
	_, err = tx.Exec(`
		INSERT INTO agents (id, agent_id, name, hostname, status)
		VALUES ($1, $2, $3, $4, 'pending')
	`, recordID, agentID, strings.TrimSpace(req.Name), strings.TrimSpace(req.Hostname))
	if err != nil {
		http.Error(w, "Failed to create agent", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(`
		INSERT INTO agent_credentials (agent_id, public_key, is_pinned, jwt_expires_at)
		VALUES ($1, $2, true, $3)
	`, agentID, publicKey, expiresAt)
	if err != nil {
		http.Error(w, "Failed to store credentials", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to create agent", http.StatusInternalServerError)
		return
	}

	credsContent := BuildCredsFile(jwtToken, seed)

	resp := createAgentResponse{
		Credentials: credentialsResponse{
			CredsContent: credsContent,
			NKeySeed:     seed,
			JWT:          jwtToken,
			ExpiresAt:    expiresAt,
		},
		InstallCommand: buildInstallCommand(credsContent),
	}
	resp.Agent.ID = recordID
	resp.Agent.AgentID = agentID
	resp.Agent.Name = strings.TrimSpace(req.Name)
	resp.Agent.Status = "pending"

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) RotateCredentials(w http.ResponseWriter, r *http.Request) {
	if h.jwtIssuer == nil {
		http.Error(w, "NATS JWT issuer not configured", http.StatusInternalServerError)
		return
	}
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		http.Error(w, "Missing agent id", http.StatusBadRequest)
		return
	}

	seed, publicKey, err := GenerateUserKeyPair()
	if err != nil {
		http.Error(w, "Failed to generate NKey", http.StatusInternalServerError)
		return
	}

	jwtToken, expiresAt, err := h.jwtIssuer.IssueAgentJWT(agentID, publicKey, defaultJWTExpiry)
	if err != nil {
		http.Error(w, "Failed to issue JWT", http.StatusInternalServerError)
		return
	}

	tx, err := h.db.Beginx()
	if err != nil {
		http.Error(w, "Failed to rotate credentials", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE agent_credentials
		SET revoked_at = now()
		WHERE agent_id = $1 AND revoked_at IS NULL
	`, agentID)
	if err != nil {
		http.Error(w, "Failed to revoke old credentials", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(`
		INSERT INTO agent_credentials (agent_id, public_key, is_pinned, jwt_expires_at)
		VALUES ($1, $2, false, $3)
	`, agentID, publicKey, expiresAt)
	if err != nil {
		http.Error(w, "Failed to store credentials", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to rotate credentials", http.StatusInternalServerError)
		return
	}

	credsContent := BuildCredsFile(jwtToken, seed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"credentials": credentialsResponse{
			CredsContent: credsContent,
			NKeySeed:     seed,
			JWT:          jwtToken,
			ExpiresAt:    expiresAt,
		},
		"warning": "Old credentials revoked. Update agent configuration immediately.",
	})
}

func (h *Handler) ListCredentials(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		http.Error(w, "Missing agent id", http.StatusBadRequest)
		return
	}

	var rows []struct {
		PublicKey string       `db:"public_key"`
		CreatedAt time.Time    `db:"created_at"`
		ExpiresAt time.Time    `db:"jwt_expires_at"`
		RevokedAt sql.NullTime `db:"revoked_at"`
	}

	query := `
		SELECT public_key, created_at, jwt_expires_at, revoked_at
		FROM agent_credentials
		WHERE agent_id = $1
		ORDER BY created_at DESC
	`
	if err := h.db.Select(&rows, query, agentID); err != nil {
		http.Error(w, "Failed to load credentials", http.StatusInternalServerError)
		return
	}

	creds := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		status := "active"
		revoked := any(nil)
		if row.RevokedAt.Valid {
			status = "revoked"
			revoked = row.RevokedAt.Time
		}
		creds = append(creds, map[string]any{
			"public_key": row.PublicKey,
			"created_at": row.CreatedAt,
			"expires_at": row.ExpiresAt,
			"revoked_at": revoked,
			"status":     status,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"credentials": creds})
}

func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		http.Error(w, "Missing agent id", http.StatusBadRequest)
		return
	}

	res, err := h.db.Exec(`DELETE FROM agents WHERE agent_id = $1`, agentID)
	if err != nil {
		http.Error(w, "Failed to delete agent", http.StatusInternalServerError)
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func buildInstallCommand(credsContent string) string {
	installURL := os.Getenv("OPSPILOT_INSTALL_URL")
	if installURL == "" {
		installURL = "https://get.opspilot.io/install.sh"
	}

	// Pass .creds content as single argument (base64 to avoid shell escaping issues)
	// Note: install script should decode and write to /etc/opspilot/credentials/agent.creds
	encoded := base64.StdEncoding.EncodeToString([]byte(credsContent))
	return "curl -sSL " + installURL + " | bash -s -- --creds-b64 '" + encoded + "'"
}

func generateAgentID() string {
	id := strings.ReplaceAll(uuid.New().String(), "-", "")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
