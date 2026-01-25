package storage

import (
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"opspilot-backend/internal/models"
)

type Storage struct {
	db *sqlx.DB
}

func NewStorage(db *sqlx.DB) *Storage {
	return &Storage{db: db}
}

func (s *Storage) CreateAgent(agent *models.Agent) error {
	query := `
		INSERT INTO agents (id, token_hash, hostname, status, last_seen_at, meta)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (token_hash) 
		DO UPDATE SET hostname = EXCLUDED.hostname, last_seen_at = EXCLUDED.last_seen_at, meta = EXCLUDED.meta
	`

	meta := agent.Meta
	if meta == nil {
		meta = []byte("{}")
	}

	_, err := s.db.Exec(query, agent.ID, agent.TokenHash, agent.Hostname, agent.Status, agent.LastSeenAt, meta)
	return err
}

func (s *Storage) GetAgentByToken(token string) (*models.Agent, error) {
	var agent models.Agent
	query := `SELECT id, token_hash, hostname, status, last_seen_at, meta FROM agents WHERE token_hash = $1`
	err := s.db.Get(&agent, query, token)
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *Storage) GetAgent(id string) (*models.Agent, error) {
	var agent models.Agent
	query := `SELECT id, token_hash, hostname, status, last_seen_at, meta FROM agents WHERE id = $1`
	err := s.db.Get(&agent, query, id)
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *Storage) UpdateAgentStatus(id, status string) error {
	now := time.Now()
	query := `UPDATE agents SET status = $1, last_seen_at = $2 WHERE id = $3`
	_, err := s.db.Exec(query, status, now, id)
	return err
}

func (s *Storage) CreateIncident(incident *models.Incident) error {
	query := `
		INSERT INTO incidents (agent_id, type, source, raw_error, ai_analysis, ai_solution, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	err := s.db.QueryRow(query, incident.AgentID, incident.Type, incident.Source,
		incident.RawError, incident.AIAnalysis, incident.AISolution, incident.Status).
		Scan(&incident.ID, &incident.CreatedAt)
	return err
}

func (s *Storage) GetIncidents(agentID string, limit int) ([]models.Incident, error) {
	var incidents []models.Incident
	query := `
		SELECT id, agent_id, type, source, raw_error, ai_analysis, ai_solution, status, created_at
		FROM incidents
		WHERE agent_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	err := s.db.Select(&incidents, query, agentID, limit)
	return incidents, err
}

func (s *Storage) GetIncidentByID(id string) (*models.Incident, error) {
	var incident models.Incident
	query := `
		SELECT id, agent_id, type, source, raw_error, ai_analysis, ai_solution, status, created_at
		FROM incidents
		WHERE id = $1
	`
	err := s.db.Get(&incident, query, id)
	if err != nil {
		return nil, err
	}
	return &incident, nil
}

func (s *Storage) UpdateIncident(incident *models.Incident) error {
	query := `
		UPDATE incidents
		SET ai_analysis = $1, ai_solution = $2, status = $3
		WHERE id = $4
	`
	_, err := s.db.Exec(query, incident.AIAnalysis, incident.AISolution, incident.Status, incident.ID)
	return err
}

func (s *Storage) GetAgentByID(id string) (*models.Agent, error) {
	var agent models.Agent
	query := `SELECT id, token_hash, hostname, status, last_seen_at, meta FROM agents WHERE id = $1`
	err := s.db.Get(&agent, query, id)
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *Storage) Ping() error {
	return s.db.Ping()
}
