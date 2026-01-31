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

type rowScanner interface {
	Scan(dest ...any) error
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
		INSERT INTO agents (
			id, agent_id, org_id, name, hostname, status, last_seen_at, tags,
			hardware_fingerprint, enrolled_via, enrolled_at, enrolled_ip, meta
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, '[]'::jsonb), $9, $10, $11, $12, $13)
		ON CONFLICT (agent_id)
		DO UPDATE SET
			org_id = COALESCE(EXCLUDED.org_id, agents.org_id),
			name = COALESCE(NULLIF(EXCLUDED.name, ''), agents.name),
			hostname = EXCLUDED.hostname,
			status = EXCLUDED.status,
			last_seen_at = EXCLUDED.last_seen_at,
			tags = COALESCE(EXCLUDED.tags, agents.tags),
			hardware_fingerprint = COALESCE(NULLIF(EXCLUDED.hardware_fingerprint, ''), agents.hardware_fingerprint),
			enrolled_via = COALESCE(EXCLUDED.enrolled_via, agents.enrolled_via),
			enrolled_at = COALESCE(EXCLUDED.enrolled_at, agents.enrolled_at),
			enrolled_ip = COALESCE(EXCLUDED.enrolled_ip, agents.enrolled_ip),
			meta = EXCLUDED.meta
	`

	meta := agent.Meta
	if meta == nil {
		meta = []byte("{}")
	}

	var tagsJSON []byte
	var err error
	if agent.Tags != nil {
		tagsJSON, err = json.Marshal(agent.Tags)
		if err != nil {
			return err
		}
	}

	enrolledVia := nullIfEmpty("")
	if agent.EnrolledVia != nil {
		enrolledVia = nullIfEmpty(*agent.EnrolledVia)
	}

	enrolledIP := nullIfEmpty("")
	if agent.EnrolledIP != nil {
		enrolledIP = nullIfEmpty(*agent.EnrolledIP)
	}

	_, err = s.db.Exec(query,
		agent.ID,
		agent.AgentID,
		nullIfEmpty(agent.OrgID),
		agent.Name,
		agent.Hostname,
		agent.Status,
		agent.LastSeenAt,
		tagsJSON,
		nullIfEmpty(agent.HardwareFingerprint),
		enrolledVia,
		agent.EnrolledAt,
		enrolledIP,
		meta,
	)
	return err
}

func (s *Storage) GetAgentByAgentID(agentID string) (*models.Agent, error) {
	query := `
		SELECT id, agent_id, org_id,
		       COALESCE(name, '') AS name,
		       COALESCE(hostname, '') AS hostname,
		       status, last_seen_at, tags, hardware_fingerprint,
		       enrolled_via, enrolled_at, enrolled_ip::text, meta
		FROM agents
		WHERE agent_id = $1
	`
	agent, err := scanAgentRow(s.db.QueryRow(query, agentID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *Storage) GetAgent(id string) (*models.Agent, error) {
	query := `
		SELECT id, agent_id, org_id,
		       COALESCE(name, '') AS name,
		       COALESCE(hostname, '') AS hostname,
		       status, last_seen_at, tags, hardware_fingerprint,
		       enrolled_via, enrolled_at, enrolled_ip::text, meta
		FROM agents
		WHERE id = $1
	`
	agent, err := scanAgentRow(s.db.QueryRow(query, id))
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

func (s *Storage) InsertInventorySnapshot(agentID, hash string, payload []byte) error {
	query := `
		INSERT INTO agents_inventory (agent_id, hash, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (agent_id, hash) DO NOTHING
	`
	_, err := s.db.Exec(query, agentID, hash, payload)
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
		SET ai_analysis = $1, is_critical = $2, suggested_action = $3::jsonb, status = $4
		WHERE id = $5
	`
	var suggestedAction interface{}
	if len(actionJSON) > 0 {
		suggestedAction = string(actionJSON)
	} else {
		suggestedAction = nil
	}
	_, err := s.db.Exec(query, incident.AIAnalysis, incident.IsCritical, suggestedAction, incident.Status, incident.ID)
	return err
}

func (s *Storage) GetAgentByID(id string) (*models.Agent, error) {
	query := `
		SELECT id, agent_id, org_id,
		       COALESCE(name, '') AS name,
		       COALESCE(hostname, '') AS hostname,
		       status, last_seen_at, tags, hardware_fingerprint,
		       enrolled_via, enrolled_at, enrolled_ip::text, meta
		FROM agents
		WHERE id = $1
	`
	agent, err := scanAgentRow(s.db.QueryRow(query, id))
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

func scanAgentRow(scanner rowScanner) (models.Agent, error) {
	var agent models.Agent
	var orgID sql.NullString
	var tagsJSON []byte
	var hardwareFingerprint sql.NullString
	var enrolledVia sql.NullString
	var enrolledIP sql.NullString

	err := scanner.Scan(
		&agent.ID,
		&agent.AgentID,
		&orgID,
		&agent.Name,
		&agent.Hostname,
		&agent.Status,
		&agent.LastSeenAt,
		&tagsJSON,
		&hardwareFingerprint,
		&enrolledVia,
		&agent.EnrolledAt,
		&enrolledIP,
		&agent.Meta,
	)
	if err != nil {
		return models.Agent{}, err
	}

	tags, err := decodeStringArray(tagsJSON)
	if err != nil {
		return models.Agent{}, err
	}
	agent.Tags = tags
	if orgID.Valid {
		agent.OrgID = orgID.String
	}
	agent.HardwareFingerprint = hardwareFingerprint.String

	if enrolledVia.Valid {
		value := enrolledVia.String
		agent.EnrolledVia = &value
	}

	if enrolledIP.Valid {
		value := enrolledIP.String
		agent.EnrolledIP = &value
	}

	return agent, nil
}
