package storage

import (
	"context"
	"database/sql"

	"opspilot-backend/internal/models"
)

func (s *Storage) GetUser(ctx context.Context, id string) (*models.User, error) {
	query := `
		SELECT id, org_id, email, password_hash, created_at
		FROM users
		WHERE id = $1
	`

	var user models.User
	if err := s.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.OrgID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}
