package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"opspilot-backend/internal/models"
)

func (s *Storage) EnrollAgent(ctx context.Context, orgID string, req models.EnrollAgentRequest, bootstrapTokenID string, remoteIP string) (*models.Agent, error) {
	tags, err := s.getBootstrapTokenTags(ctx, bootstrapTokenID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	enrolledVia := bootstrapTokenID
	enrolledIP := remoteIP
	agent := &models.Agent{
		ID:                  uuid.New().String(),
		AgentID:             req.AgentID,
		OrgID:               orgID,
		Hostname:            req.Hostname,
		Status:              "online",
		Tags:                tags,
		HardwareFingerprint: req.HardwareFingerprint,
		EnrolledVia:         &enrolledVia,
		EnrolledAt:          &now,
		EnrolledIP:          &enrolledIP,
		LastSeenAt:          &now,
	}

	meta := []byte("{}")
	if req.OS != "" || req.Arch != "" || req.AgentVersion != "" {
		metaMap := map[string]string{
			"os":            req.OS,
			"arch":          req.Arch,
			"agent_version": req.AgentVersion,
		}
		if data, err := json.Marshal(metaMap); err == nil {
			meta = data
		}
	}
	agent.Meta = meta

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

	var tagsJSON []byte
	if agent.Tags != nil {
		if tagsJSON, err = json.Marshal(agent.Tags); err != nil {
			return nil, err
		}
	}

	_, err = s.db.ExecContext(ctx, query,
		agent.ID,
		agent.AgentID,
		nullIfEmpty(agent.OrgID),
		agent.Name,
		agent.Hostname,
		agent.Status,
		agent.LastSeenAt,
		tagsJSON,
		nullIfEmpty(agent.HardwareFingerprint),
		nullIfEmpty(enrolledVia),
		agent.EnrolledAt,
		nullIfEmpty(enrolledIP),
		agent.Meta,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (s *Storage) GetPinnedPublicKey(ctx context.Context, agentID string) (string, error) {
	query := `
		SELECT public_key
		FROM agent_credentials
		WHERE agent_id = $1 AND is_pinned = true AND revoked_at IS NULL
		LIMIT 1
	`
	var publicKey string
	if err := s.db.QueryRowContext(ctx, query, agentID).Scan(&publicKey); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return publicKey, nil
}

func (s *Storage) CreateAgentCredentials(ctx context.Context, agentID, publicKey string, expiresAt time.Time, isPinned bool, fingerprint, remoteIP, hostname string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_credentials (
			agent_id, public_key, is_pinned, fingerprint_at_registration, registered_from_ip, registered_hostname, jwt_expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, agentID, publicKey, isPinned, nullIfEmpty(fingerprint), nullIfEmpty(remoteIP), nullIfEmpty(hostname), expiresAt)
	return err
}

func (s *Storage) UpdateAgentTags(ctx context.Context, agentID string, tags []string) error {
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `UPDATE agents SET tags = $1 WHERE agent_id = $2`, tagsJSON, agentID)
	return err
}

func (s *Storage) getBootstrapTokenTags(ctx context.Context, tokenID string) ([]string, error) {
	var tagsJSON []byte
	query := `SELECT tags FROM bootstrap_tokens WHERE id = $1`
	if err := s.db.QueryRowContext(ctx, query, tokenID).Scan(&tagsJSON); err != nil {
		if err == sql.ErrNoRows {
			return []string{}, nil
		}
		return nil, err
	}

	return decodeStringArray(tagsJSON)
}
