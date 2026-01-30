package storage

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"opspilot-backend/internal/models"
)

func (s *Storage) RecordAgentConnection(ctx context.Context, conn models.AgentConnection) error {
	if conn.ID == "" {
		conn.ID = uuid.New().String()
	}
	if conn.ConnectedAt.IsZero() {
		conn.ConnectedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_connections (
			id, agent_id, nats_client_id, remote_ip, hostname, connected_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, conn.ID, conn.AgentID, nullIfEmpty(conn.NATSClientID), conn.RemoteIP, nullIfEmpty(conn.Hostname), conn.ConnectedAt)
	return err
}

func (s *Storage) GetActiveConnection(ctx context.Context, agentID string) (*models.AgentConnection, error) {
	query := `
		SELECT id, agent_id, nats_client_id, remote_ip::text, hostname, connected_at, disconnected_at, disconnect_reason
		FROM agent_connections
		WHERE agent_id = $1 AND disconnected_at IS NULL
		ORDER BY connected_at DESC
		LIMIT 1
	`

	var conn models.AgentConnection
	if err := s.db.QueryRowContext(ctx, query, agentID).Scan(
		&conn.ID,
		&conn.AgentID,
		&conn.NATSClientID,
		&conn.RemoteIP,
		&conn.Hostname,
		&conn.ConnectedAt,
		&conn.DisconnectedAt,
		&conn.DisconnectReason,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &conn, nil
}

func (s *Storage) RecordAgentDisconnect(ctx context.Context, agentID string, reason string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_connections
		SET disconnected_at = NOW(), disconnect_reason = $2
		WHERE agent_id = $1 AND disconnected_at IS NULL
	`, agentID, nullIfEmpty(reason))
	return err
}

func (s *Storage) RecordAgentConflict(ctx context.Context, conflict models.AgentConflict) error {
	if conflict.ID == "" {
		conflict.ID = uuid.New().String()
	}
	if conflict.CreatedAt.IsZero() {
		conflict.CreatedAt = time.Now().UTC()
	}
	if conflict.Resolution == "" {
		conflict.Resolution = "pending"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_conflicts (
			id, agent_id, existing_ip, new_ip, existing_hostname, new_hostname,
			resolution, resolved_by, created_at, resolved_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, conflict.ID, conflict.AgentID, nullIfEmpty(conflict.ExistingIP), nullIfEmpty(conflict.NewIP),
		nullIfEmpty(conflict.ExistingHostname), nullIfEmpty(conflict.NewHostname), conflict.Resolution,
		nullIfEmpty(ptrValue(conflict.ResolvedBy)), conflict.CreatedAt, conflict.ResolvedAt)
	return err
}

func (s *Storage) GetUnresolvedConflicts(ctx context.Context, orgID string) ([]models.AgentConflict, error) {
	query := `
		SELECT c.id, c.agent_id, c.existing_ip::text, c.new_ip::text,
			c.existing_hostname, c.new_hostname, c.resolution, c.resolved_by,
			c.created_at, c.resolved_at
		FROM agent_conflicts c
		JOIN agents a ON a.agent_id = c.agent_id
		WHERE a.org_id = $1 AND c.resolved_at IS NULL
		ORDER BY c.created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conflicts := make([]models.AgentConflict, 0)
	for rows.Next() {
		var conflict models.AgentConflict
		var resolvedBy sql.NullString
		if err := rows.Scan(
			&conflict.ID,
			&conflict.AgentID,
			&conflict.ExistingIP,
			&conflict.NewIP,
			&conflict.ExistingHostname,
			&conflict.NewHostname,
			&conflict.Resolution,
			&resolvedBy,
			&conflict.CreatedAt,
			&conflict.ResolvedAt,
		); err != nil {
			return nil, err
		}
		if resolvedBy.Valid {
			value := resolvedBy.String
			conflict.ResolvedBy = &value
		}
		conflicts = append(conflicts, conflict)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return conflicts, nil
}

func (s *Storage) GetConflict(ctx context.Context, conflictID string) (*models.AgentConflict, error) {
	query := `
		SELECT id, agent_id, existing_ip::text, new_ip::text,
			existing_hostname, new_hostname, resolution, resolved_by,
			created_at, resolved_at
		FROM agent_conflicts
		WHERE id = $1
	`

	var conflict models.AgentConflict
	var resolvedBy sql.NullString
	if err := s.db.QueryRowContext(ctx, query, conflictID).Scan(
		&conflict.ID,
		&conflict.AgentID,
		&conflict.ExistingIP,
		&conflict.NewIP,
		&conflict.ExistingHostname,
		&conflict.NewHostname,
		&conflict.Resolution,
		&resolvedBy,
		&conflict.CreatedAt,
		&conflict.ResolvedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if resolvedBy.Valid {
		value := resolvedBy.String
		conflict.ResolvedBy = &value
	}

	return &conflict, nil
}

func (s *Storage) ResolveConflict(ctx context.Context, conflictID string, resolution string, resolvedBy *string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_conflicts
		SET resolution = $2, resolved_by = $3, resolved_at = NOW()
		WHERE id = $1
	`, conflictID, resolution, nullIfEmpty(ptrValue(resolvedBy)))
	return err
}

func ptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
