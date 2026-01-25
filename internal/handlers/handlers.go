package handlers

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
	"opspilot-backend/internal/hub"
	"opspilot-backend/internal/models"
	"opspilot-backend/internal/services"
	"opspilot-backend/internal/storage"
)

type Handler struct {
	hub         *hub.Hub
	storage     *storage.Storage
	db          *sqlx.DB
	upgrader    websocket.Upgrader
	aiClient    *services.OpenRouterClient
	slackClient *services.SlackClient
}

func NewHandler(db *sqlx.DB) *Handler {
	return &Handler{
		hub:         hub.NewHub(),
		storage:     storage.NewStorage(db),
		db:          db,
		aiClient:    services.NewOpenRouterClient(),
		slackClient: services.NewSlackClient(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	// WebSocket
	r.HandleFunc("/v1/stream", h.HandleWebSocket)

	// Agents
	r.Get("/v1/agents", h.GetAgents)
	r.Post("/v1/agents", h.CreateAgent)
	r.Get("/v1/agents/{id}/incidents", h.GetIncidents)

	// Incidents
	r.Post("/v1/incidents/{id}/analyze", h.AnalyzeIncident)

	// Admin
	r.Post("/v1/admin/exec", h.HandleExecCommand)

	// Static files (UI)
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/*", fileServer)
}

func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	var agentID string

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if agentID != "" {
				h.hub.Remove(agentID)
				h.storage.UpdateAgentStatus(agentID, "offline")
			}
			log.Printf("WebSocket read error: %v", err)
			break
		}

		log.Printf("[DEBUG] Received message size: %d bytes", len(message))

		var payload models.AgentPayload
		if err := json.Unmarshal(message, &payload); err != nil {
			log.Printf("Payload parse error: %v", err)
			continue
		}
		
		log.Printf("[DEBUG] Payload type: %s, Data length before decode: %d", payload.Type, len(payload.Data))

		// Декодируем данные если есть
		if payload.Data != "" {
			decoded, err := base64.StdEncoding.DecodeString(payload.Data)
			if err != nil {
				log.Printf("Base64 decode error: %v", err)
				continue
			}

			var dataBytes []byte
			if payload.Compression == "gzip" {
				reader, err := gzip.NewReader(bytes.NewReader(decoded))
				if err != nil {
					log.Printf("Gzip reader error: %v", err)
					continue
				}
				dataBytes, err = io.ReadAll(reader)
				reader.Close()
				if err != nil {
					log.Printf("Gzip decompress error: %v", err)
					continue
				}
			} else {
				dataBytes = decoded
			}
			
			log.Printf("[DEBUG] Decoded data size: %d bytes", len(dataBytes))

			var alertData models.AlertData
			if err := json.Unmarshal(dataBytes, &alertData); err != nil {
				log.Printf("Alert data parse error: %v", err)
				continue
			}
			payload.ParsedData = &alertData
			log.Printf("[DEBUG] Parsed AlertData: Type=%s Source=%s MsgLen=%d", alertData.Type, alertData.Source, len(alertData.Message))
		}

		agent, err := h.storage.GetAgentByToken(payload.Token)
		if err != nil {
			// Stub user creation logic for brevity in debug logs context, keeping existing logic
			log.Printf("Agent not found, auto-registering with token: %s", payload.Token)

			newAgent := &models.Agent{
				ID:        generateUUID(),
				TokenHash: payload.Token,
				Hostname:  "unknown",
				Status:    "online",
			}

			if err := h.storage.CreateAgent(newAgent); err != nil {
				log.Printf("Failed to create agent: %v", err)
				continue
			}
			agent = newAgent
			log.Printf("Auto-registered new agent: %s", agent.ID)
		}

		agentID = agent.ID
		h.hub.Add(agentID, conn)
		h.storage.UpdateAgentStatus(agentID, "online")

		switch payload.Type {
		case "heartbeat":
			// Reduce log noise
			// log.Printf("Heartbeat from agent %s (%s)", agent.Hostname, agentID)
		case "alert":
			if payload.ParsedData == nil {
				log.Printf("Alert from agent %s has no parsed data", agent.Hostname)
				continue
			}
			log.Printf("Processing alert from agent %s: Type=%s Source=%s", agent.Hostname, payload.ParsedData.Type, payload.ParsedData.Source)
			h.handleAlert(agent, payload.ParsedData)
		}
	}
}

func generateUUID() string {
	return uuid.New().String()
}

func (h *Handler) handleAlert(agent *models.Agent, alert *models.AlertData) {
	// Собираем logs в raw_error если есть
	rawError := alert.Message
	if alert.Logs != "" {
		rawError = alert.Message + "\n\nLogs:\n" + alert.Logs
	}
	
	// Sanitize rawError for Postgres (remove null bytes)
	rawError = strings.ReplaceAll(rawError, "\x00", "")

	log.Printf("[DEBUG] Creating incident: Type=%s Source=%s RawErrorLen=%d", alert.Type, alert.Source, len(rawError))

	incident := &models.Incident{
		AgentID:  agent.ID,
		Type:     alert.Type,
		Source:   alert.Source,
		RawError: rawError,
		Status:   "new",
	}

	// НЕ отправляем автоматически в AI - пользователь сам выберет в UI

	if err := h.storage.CreateIncident(incident); err != nil {
		log.Printf("[ERROR] Error creating incident in DB: %v (Source=%s)", err, incident.Source)
		return
	}

	log.Printf("Created incident %d for agent %s (type: %s, source: %s)",
		incident.ID, agent.ID, incident.Type, incident.Source)
}

// AnalyzeIncident - ручной запуск AI анализа для инцидента
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

	incident.AIAnalysis = analysis.Cause
	incident.AISolution = analysis.FixCmd
	incident.Status = "analyzed"

	if err := h.storage.UpdateIncident(incident); err != nil {
		log.Printf("Error updating incident: %v", err)
		http.Error(w, "Failed to save analysis", http.StatusInternalServerError)
		return
	}

	log.Printf("Incident %d analyzed successfully", incident.ID)

	// Опционально отправляем в Slack после анализа
	agent, _ := h.storage.GetAgentByID(incident.AgentID)
	hostname := "unknown"
	if agent != nil {
		hostname = agent.Hostname
	}

	if err := h.slackClient.SendAlert(hostname, incident.Source, incident.Type, incident.AIAnalysis, incident.AISolution); err != nil {
		log.Printf("Slack notification error: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(incident)
}

func (h *Handler) HandleExecCommand(w http.ResponseWriter, r *http.Request) {
	var cmd struct {
		AgentID string                 `json:"agent_id"`
		Command string                 `json:"command"`
		Params  map[string]interface{} `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	payload := models.CommandPayload{
		Command: cmd.Command,
		Params:  cmd.Params,
	}

	if err := h.hub.SendCommand(cmd.AgentID, payload); err != nil {
		http.Error(w, fmt.Sprintf("Failed to send command: %v", err), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
}

func (h *Handler) GetAgents(w http.ResponseWriter, r *http.Request) {
	var agents []models.Agent
	query := `SELECT id, token_hash, hostname, status, last_seen_at, meta FROM agents`
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
