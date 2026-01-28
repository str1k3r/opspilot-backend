package storage

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"opspilot-backend/internal/models"
)

type Storage struct {
	db *sqlx.DB
}

// UpdateAgentMetaAndHostname updates meta JSON and hostname for an agent.
func (s *Storage) UpdateAgentMetaAndHostname(agentID string, meta []byte, hostname string) error {
	if meta == nil {
		meta = []byte("{}")
	}
	query := `UPDATE agents SET meta = $1, hostname = $2, last_seen_at = NOW() WHERE agent_id = $3`
	_, err := s.db.Exec(query, meta, hostname, agentID)
	return err
}

func NewStorage(db *sqlx.DB) *Storage {
	return &Storage{db: db}
}

func (s *Storage) CreateAgent(agent *models.Agent) error {
	query := `
		INSERT INTO agents (id, agent_id, hostname, status, last_seen_at, meta)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id)
		DO UPDATE SET
			hostname = EXCLUDED.hostname,
			status = EXCLUDED.status,
			last_seen_at = EXCLUDED.last_seen_at,
			meta = EXCLUDED.meta
	`

	meta := agent.Meta
	if meta == nil {
		meta = []byte("{}")
	}

	_, err := s.db.Exec(query, agent.ID, agent.AgentID, agent.Hostname, agent.Status, agent.LastSeenAt, meta)
	return err
}

func (s *Storage) GetAgentByAgentID(agentID string) (*models.Agent, error) {
	var agent models.Agent
	query := `SELECT id, agent_id, hostname, status, last_seen_at, meta FROM agents WHERE agent_id = $1`
	err := s.db.Get(&agent, query, agentID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *Storage) GetAgent(id string) (*models.Agent, error) {
	var agent models.Agent
	query := `SELECT id, agent_id, hostname, status, last_seen_at, meta FROM agents WHERE id = $1`
	err := s.db.Get(&agent, query, id)
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *Storage) UpdateAgentStatus(agentID, status string) error {
	query := `UPDATE agents SET status = $1, last_seen_at = NOW() WHERE agent_id = $2`
	_, err := s.db.Exec(query, status, agentID)
	return err
}

func (s *Storage) CreateIncident(incident *models.Incident) error {
	contextJSON := incident.ContextJSON
	if contextJSON == nil && incident.Context != nil {
		contextJSON, _ = json.Marshal(incident.Context)
	}

	query := `
		INSERT INTO incidents (agent_id, type, source, raw_error, context, ai_analysis, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	err := s.db.QueryRow(query, incident.AgentID, incident.Type, incident.Source,
		incident.RawError, contextJSON, incident.AIAnalysis, incident.Status).
		Scan(&incident.ID, &incident.CreatedAt)
	return err
}

func (s *Storage) GetIncidents(agentID string, limit int) ([]models.Incident, error) {
	incidents := make([]models.Incident, 0)
	query := `
		SELECT id, agent_id, type, source, raw_error, context, ai_analysis, is_critical, suggested_action, status, created_at
		FROM incidents
		WHERE agent_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	err := s.db.Select(&incidents, query, agentID, limit)
	if err != nil {
		return nil, err
	}

	// Парсим JSON поля для каждого инцидента
	for i := range incidents {
		if len(incidents[i].ContextJSON) > 0 {
			json.Unmarshal(incidents[i].ContextJSON, &incidents[i].Context)
		}
		if len(incidents[i].SuggestedActionJSON) > 0 {
			var action models.SuggestedAction
			if json.Unmarshal(incidents[i].SuggestedActionJSON, &action) == nil {
				incidents[i].SuggestedAction = &action
			}
		}
	}

	return incidents, nil
}

func (s *Storage) GetIncidentByID(id string) (*models.Incident, error) {
	var incident models.Incident
	query := `
		SELECT id, agent_id, type, source, raw_error, context, ai_analysis, is_critical, suggested_action, status, created_at
		FROM incidents
		WHERE id = $1
	`
	err := s.db.Get(&incident, query, id)
	if err != nil {
		return nil, err
	}

	// Парсим JSON поля
	if len(incident.ContextJSON) > 0 {
		json.Unmarshal(incident.ContextJSON, &incident.Context)
	}
	if len(incident.SuggestedActionJSON) > 0 {
		var action models.SuggestedAction
		if json.Unmarshal(incident.SuggestedActionJSON, &action) == nil {
			incident.SuggestedAction = &action
		}
	}

	return &incident, nil
}

func (s *Storage) UpdateIncident(incident *models.Incident) error {
	var actionJSON []byte
	if incident.SuggestedAction != nil {
		actionJSON, _ = json.Marshal(incident.SuggestedAction)
	}

	query := `
		UPDATE incidents
		SET ai_analysis = $1, is_critical = $2, suggested_action = $3, status = $4
		WHERE id = $5
	`
	_, err := s.db.Exec(query, incident.AIAnalysis, incident.IsCritical, actionJSON, incident.Status, incident.ID)
	return err
}

func (s *Storage) GetAgentByID(id string) (*models.Agent, error) {
	var agent models.Agent
	query := `SELECT id, agent_id, hostname, status, last_seen_at, meta FROM agents WHERE id = $1`
	err := s.db.Get(&agent, query, id)
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *Storage) MarkStaleAgentsOffline(threshold time.Duration) error {
	_, err := s.db.Exec(`
		UPDATE agents SET status = 'offline'
		WHERE status = 'online'
		AND last_seen_at < NOW() - $1::interval
	`, threshold.String())
	return err
}

func (s *Storage) Ping() error {
	return s.db.Ping()
}
