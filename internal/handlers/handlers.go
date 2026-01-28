package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"opspilot-backend/internal/models"
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
}

func New(storage *storage.Storage, db *sqlx.DB, ai *services.OpenRouterClient, slack *services.SlackClient, rpcClient *rpc.Client) *Handler {
	return &Handler{
		storage:     storage,
		db:          db,
		aiClient:    ai,
		slackClient: slack,
		rpc:         rpcClient,
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	// Agents
	r.Get("/v1/agents", h.GetAgents)
	r.Post("/v1/agents", h.CreateAgent)
	r.Get("/v1/agents/{id}/incidents", h.GetIncidents)

	// Incidents
	r.Post("/v1/incidents/{id}/analyze", h.AnalyzeIncident)
	r.Post("/v1/incidents/{id}/execute", h.ExecuteSuggestedAction)

	// Admin
	r.Post("/v1/admin/exec", h.HandleExecCommand)
	r.Post("/v1/slack/interactive", h.HandleSlackInteractive)

	// Static files (UI)
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/*", fileServer)
}

// AnalyzeIncident - manual AI analysis for an incident
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

// ExecuteSuggestedAction - execute AI suggested action from UI
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

func (h *Handler) HandleExecCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID string            `json:"agent_id"`
		Command string            `json:"command"`
		Params  map[string]string `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := h.rpc.ExecAction(req.AgentID, req.Command, req.Params, 0)
	if err != nil {
		httpErrorFromRPC(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) GetAgents(w http.ResponseWriter, r *http.Request) {
	var agents []models.Agent
	query := `SELECT id, agent_id, hostname, status, last_seen_at, meta FROM agents`
	if err := h.db.Select(&agents, query); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(agents)
}

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
