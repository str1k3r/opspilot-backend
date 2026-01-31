package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/nats-io/nats.go"
	"github.com/swaggo/http-swagger/v2"
	_ "opspilot-backend/docs" // swagger docs
	"opspilot-backend/internal/auth"
	"opspilot-backend/internal/cache"
	rl "opspilot-backend/internal/middleware"
	"opspilot-backend/internal/models"
	"opspilot-backend/internal/natsauth"
	"opspilot-backend/internal/rpc"
	"opspilot-backend/internal/services"
	"opspilot-backend/internal/storage"
)

type Handler struct {
	storage     *storage.Storage
	db          *sqlx.DB
	aiClient    *services.OpenRouterClient
	slackClient *services.SlackClient
	rpc         *rpc.Client
	cache       cache.Client
}

func New(storage *storage.Storage, db *sqlx.DB, ai *services.OpenRouterClient, slack *services.SlackClient, rpcClient *rpc.Client, cacheClient cache.Client) *Handler {
	return &Handler{
		storage:     storage,
		db:          db,
		aiClient:    ai,
		slackClient: slack,
		rpc:         rpcClient,
		cache:       cacheClient,
	}
}

// @title OpsPilot API
// @version 1.0
// @description OpsPilot Backend API - AI-powered Virtual System Administrator
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.email support@opspilot.io

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host localhost:8080
// @BasePath /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

func (h *Handler) RegisterRoutes(r chi.Router) {
	authHandler := auth.NewHandler(h.db)
	issuer, err := natsauth.NewJWTIssuer(
		os.Getenv("NATS_SIGNING_KEY_SEED"),
		os.Getenv("NATS_AGENTS_ACCOUNT_PUBLIC_KEY"),
	)
	if err != nil {
		log.Printf("WARN NATS JWT issuer disabled: %v", err)
	}
	credsHandler := natsauth.NewHandler(h.db, h.storage, issuer)
	enrollmentHandler := natsauth.NewEnrollmentHandler(h.storage, issuer, natsauth.EnrollmentConfig{
		NATSURLs: getNATSURLs(),
	})

	// Swagger UI
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	// Auth
	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.With(rl.RateLimitLogin(h.cache)).Post("/login", authHandler.Login)
			r.With(auth.Middleware).Post("/logout", authHandler.Logout)
			r.With(auth.Middleware).Get("/me", authHandler.Me)
		})

		// Slack (stubbed)
		r.Post("/slack/interactive", h.HandleSlackInteractive)

		// Public enrollment endpoint
		r.With(rl.RateLimitEnrollIP(h.cache), rl.RateLimitEnrollToken(h.cache)).Post("/agents/enroll", enrollmentHandler.EnrollAgent)

		// Protected API
		r.With(auth.Middleware).Group(func(r chi.Router) {
			r.Get("/events/stream", credsHandler.EventStream)

			r.Route("/bootstrap-tokens", func(r chi.Router) {
				r.Get("/", credsHandler.ListBootstrapTokens)
				r.Post("/", credsHandler.CreateBootstrapToken)
				r.Get("/{id}", credsHandler.GetBootstrapToken)
				r.Delete("/{id}", credsHandler.RevokeBootstrapToken)
			})

			r.Route("/agents/conflicts", func(r chi.Router) {
				r.Get("/", credsHandler.ListAgentConflicts)
				r.Post("/{id}/resolve", credsHandler.ResolveConflict)
			})

			// Agents (list + create + delete + creds)
			r.Get("/agents", h.GetAgents)
			r.Post("/agents", credsHandler.CreateAgent)
			r.Delete("/agents/{id}", credsHandler.DeleteAgent)
			r.Get("/agents/{id}/credentials", credsHandler.ListCredentials)
			r.Post("/agents/{id}/rotate-credentials", credsHandler.RotateCredentials)
			r.Get("/agents/{id}/incidents", h.GetIncidents)

			// Incidents
			r.Post("/incidents/{id}/analyze", h.AnalyzeIncident)
			r.Post("/incidents/{id}/execute", h.ExecuteSuggestedAction)

			// Agent direct execution (replaces /admin/exec)
			r.Post("/agents/{id}/execute", h.HandleAgentExec)
		})
	})
}

func getNATSURLs() []string {
	urls := strings.TrimSpace(os.Getenv("NATS_URLS"))
	if urls != "" {
		parts := strings.Split(urls, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
		if len(result) > 0 {
			return result
		}
	}

	if url := strings.TrimSpace(os.Getenv("NATS_URL")); url != "" {
		return []string{url}
	}

	return []string{nats.DefaultURL}
}

// AnalyzeIncident manual AI analysis for an incident
// @Summary Analyze incident with AI
// @Description Performs AI analysis on an incident to determine root cause and suggest actions
// @Tags incidents
// @Accept json
// @Produce json
// @Param id path string true "Incident ID"
// @Success 200 {object} models.Incident
// @Failure 404 {string} string "Incident not found"
// @Failure 500 {string} string "Internal server error"
// @Security BearerAuth
// @Router /incidents/{id}/analyze [post]
func (h *Handler) AnalyzeIncident(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")

	incident, err := h.storage.GetIncidentByID(idStr)
	if err != nil {
		http.Error(w, "Incident not found", http.StatusNotFound)
		return
	}

	log.Printf("Analyzing incident %d with AI...", incident.ID)

	analysis, err := h.aiClient.AnalyzeIncident(incident)
	if err != nil {
		log.Printf("AI analysis error: %v", err)
		http.Error(w, fmt.Sprintf("AI analysis failed: %v", err), http.StatusInternalServerError)
		return
	}

	incident.AIAnalysis = analysis.Analysis
	incident.IsCritical = analysis.IsCritical
	incident.SuggestedAction = analysis.SuggestedAction
	incident.Status = "analyzed"

	if err := h.storage.UpdateIncident(incident); err != nil {
		log.Printf("Error updating incident: %v", err)
		http.Error(w, "Failed to save analysis", http.StatusInternalServerError)
		return
	}

	log.Printf("Incident %d analyzed successfully", incident.ID)

	agent, _ := h.storage.GetAgentByAgentID(incident.AgentID)
	if err := h.slackClient.SendAlert(incident, agent); err != nil {
		log.Printf("Slack notification error: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(incident)
}

// ExecuteSuggestedAction execute AI suggested action from UI
// @Summary Execute suggested action
// @Description Executes the AI-suggested action for an incident via RPC to the agent
// @Tags incidents
// @Accept json
// @Produce json
// @Param id path string true "Incident ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string "Incident not found or agent offline"
// @Failure 400 {string} string "No suggested action for this incident"
// @Failure 504 {string} string "Request timed out"
// @Security BearerAuth
// @Router /incidents/{id}/execute [post]
func (h *Handler) ExecuteSuggestedAction(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")

	incident, err := h.storage.GetIncidentByID(idStr)
	if err != nil {
		http.Error(w, "Incident not found", http.StatusNotFound)
		return
	}

	if incident.SuggestedAction == nil {
		http.Error(w, "No suggested action for this incident", http.StatusBadRequest)
		return
	}

	log.Printf("Executing suggested action for incident %d: cmd=%s args=%v",
		incident.ID, incident.SuggestedAction.Cmd, incident.SuggestedAction.Args)

	resp, err := h.rpc.ExecAction(incident.AgentID, incident.SuggestedAction.Cmd, incident.SuggestedAction.Args, 0)
	if err != nil {
		httpErrorFromRPC(w, err)
		return
	}

	incident.Status = "action_sent"
	if err := h.storage.UpdateIncident(incident); err != nil {
		log.Printf("Error updating incident status: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleAgentExec executes a command on a specific agent via RPC
// @Summary Execute command on agent
// @Description Sends a command to be executed on the specified agent
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body object{command=string,params=object} true "Command and parameters"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "Missing agent id or invalid request body"
// @Failure 404 {string} string "Agent is offline"
// @Failure 504 {string} string "Request timed out"
// @Security BearerAuth
// @Router /agents/{id}/execute [post]
func (h *Handler) HandleAgentExec(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		http.Error(w, "Missing agent id", http.StatusBadRequest)
		return
	}

	var req struct {
		Command string            `json:"command"`
		Params  map[string]string `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := h.rpc.ExecAction(agentID, req.Command, req.Params, 0)
	if err != nil {
		httpErrorFromRPC(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// AgentResponse represents an agent in the API response
type AgentResponse struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agent_id"`
	OrgID      string     `json:"org_id,omitempty"`
	Name       string     `json:"name"`
	Hostname   string     `json:"hostname"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	Meta       string     `json:"meta,omitempty"`
}

// GetAgents list all registered agents
// @Summary List all agents
// @Description Returns a list of all registered agents with their status and metadata
// @Tags agents
// @Produce json
// @Success 200 {array} AgentResponse
// @Failure 500 {string} string "Internal server error"
// @Security BearerAuth
// @Router /agents [get]
func (h *Handler) GetAgents(w http.ResponseWriter, r *http.Request) {
	type agentRow struct {
		ID         string     `db:"id" json:"id"`
		AgentID    string     `db:"agent_id" json:"agent_id"`
		OrgID      *string    `db:"org_id" json:"org_id,omitempty"`
		Name       string     `db:"name" json:"name"`
		Hostname   string     `db:"hostname" json:"hostname"`
		Status     string     `db:"status" json:"status"`
		LastSeenAt *time.Time `db:"last_seen_at" json:"last_seen_at,omitempty"`
		Meta       []byte     `db:"meta" json:"meta,omitempty"`
	}

	query := `
		SELECT id, agent_id, org_id,
		       COALESCE(name, '') AS name,
		       COALESCE(hostname, '') AS hostname,
		       status, last_seen_at, meta
		FROM agents
	`

	var rows []agentRow
	if err := h.db.Select(&rows, query); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type agentResponse struct {
		ID         string     `json:"id"`
		AgentID    string     `json:"agent_id"`
		OrgID      string     `json:"org_id,omitempty"`
		Name       string     `json:"name"`
		Hostname   string     `json:"hostname"`
		Status     string     `json:"status"`
		LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
		Meta       string     `json:"meta,omitempty"`
	}

	agents := make([]agentResponse, 0, len(rows))
	for _, row := range rows {
		resp := agentResponse{
			ID:         row.ID,
			AgentID:    row.AgentID,
			Name:       row.Name,
			Hostname:   row.Hostname,
			Status:     row.Status,
			LastSeenAt: row.LastSeenAt,
		}
		if row.OrgID != nil {
			resp.OrgID = *row.OrgID
		}
		if len(row.Meta) > 0 {
			resp.Meta = base64.StdEncoding.EncodeToString(row.Meta)
		}
		agents = append(agents, resp)
	}

	json.NewEncoder(w).Encode(agents)
}

// CreateAgent creates a new agent
// @Summary Create new agent
// @Description Creates a new agent record in the system
// @Tags agents
// @Accept json
// @Produce json
// @Param agent body models.Agent true "Agent data"
// @Success 201 {object} models.Agent
// @Failure 400 {string} string "Invalid request body"
// @Failure 500 {string} string "Internal server error"
// @Security BearerAuth
// @Router /agents [post]
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var agent models.Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if agent.ID == "" {
		agent.ID = generateUUID()
	}
	if agent.Status == "" {
		agent.Status = "offline"
	}

	if err := h.storage.CreateAgent(&agent); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(agent)
}

// GetIncidents list incidents for a specific agent
// @Summary List agent incidents
// @Description Returns a list of incidents for the specified agent
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {array} models.Incident
// @Failure 500 {string} string "Internal server error"
// @Security BearerAuth
// @Router /agents/{id}/incidents [get]
func (h *Handler) GetIncidents(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	incidents, err := h.storage.GetIncidents(agentID, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(incidents)
}

func (h *Handler) HandleSlackInteractive(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Slack integration disabled", http.StatusServiceUnavailable)
}

func httpErrorFromRPC(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rpc.ErrAgentOffline):
		http.Error(w, "Agent is offline", http.StatusNotFound)
	case errors.Is(err, rpc.ErrTimeout):
		http.Error(w, "Request timed out", http.StatusGatewayTimeout)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func generateUUID() string {
	return uuid.New().String()
}
