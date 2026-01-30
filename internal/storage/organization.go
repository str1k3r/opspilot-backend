package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"opspilot-backend/internal/models"
)

var (
	ErrTokenNotFound          = errors.New("bootstrap token not found")
	ErrTokenRevoked           = errors.New("bootstrap token revoked")
	ErrTokenExpired           = errors.New("bootstrap token expired")
	ErrTokenUsageLimitReached = errors.New("bootstrap token usage limit reached")
	ErrTokenIPNotAllowed      = errors.New("bootstrap token ip not allowed")
	ErrOrgNotFound            = errors.New("organization not found")
	ErrSlugTaken              = errors.New("organization slug already taken")
)

const (
	TokenPrefix       = "ops_bt_"
	TokenLength       = 32
	tokenPrefixLength = 12
)

type bootstrapTokenRow struct {
	ID               string
	OrgID            string
	TokenPrefix      string
	TokenHash        string
	Description      sql.NullString
	TagsJSON         []byte
	AllowedCIDRsJSON []byte
	ExpiresAt        *time.Time
	MaxUses          sql.NullInt64
	UseCount         int
	CreatedBy        sql.NullString
	CreatedAt        time.Time
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
}

func (s *Storage) CreateOrganization(ctx context.Context, input models.CreateOrganizationInput) (*models.Organization, error) {
	query := `
		INSERT INTO organizations (name, slug)
		VALUES ($1, $2)
		RETURNING id, name, slug, created_at
	`

	var org models.Organization
	err := s.db.QueryRowContext(ctx, query, input.Name, input.Slug).
		Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrSlugTaken
		}
		return nil, err
	}

	return &org, nil
}

func (s *Storage) GetOrganization(ctx context.Context, id string) (*models.Organization, error) {
	query := `
		SELECT id, name, slug, created_at
		FROM organizations
		WHERE id = $1
	`

	var org models.Organization
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrOrgNotFound
	}
	if err != nil {
		return nil, err
	}

	return &org, nil
}

func (s *Storage) GetOrganizationBySlug(ctx context.Context, slug string) (*models.Organization, error) {
	query := `
		SELECT id, name, slug, created_at
		FROM organizations
		WHERE slug = $1
	`

	var org models.Organization
	err := s.db.QueryRowContext(ctx, query, slug).
		Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrOrgNotFound
	}
	if err != nil {
		return nil, err
	}

	return &org, nil
}

func (s *Storage) GetDefaultOrganization(ctx context.Context) (*models.Organization, error) {
	return s.GetOrganizationBySlug(ctx, "default")
}

func (s *Storage) CreateBootstrapToken(ctx context.Context, orgID, userID string, input models.CreateBootstrapTokenInput) (*models.CreateBootstrapTokenResponse, error) {
	token, prefix, hash, err := GenerateBootstrapToken()
	if err != nil {
		return nil, err
	}

	tagsJSON := "[]"
	if input.Tags != nil {
		data, err := json.Marshal(input.Tags)
		if err != nil {
			return nil, err
		}
		tagsJSON = string(data)
	}

	var allowedCIDRsJSON *string
	if len(input.AllowedCIDRs) > 0 {
		data, err := json.Marshal(input.AllowedCIDRs)
		if err != nil {
			return nil, err
		}
		value := string(data)
		allowedCIDRsJSON = &value
	}

	query := `
		INSERT INTO bootstrap_tokens (
			org_id, token_hash, token_prefix, description, tags, allowed_cidrs,
			expires_at, max_uses, use_count, created_by, created_at, last_used_at, revoked_at
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, 0, $9, NOW(), NULL, NULL)
		RETURNING id, org_id, token_prefix, description, tags, allowed_cidrs, expires_at,
			max_uses, use_count, created_by, created_at, last_used_at, revoked_at
	`

	row := bootstrapTokenRow{}
	err = s.db.QueryRowContext(ctx, query,
		orgID,
		hash,
		prefix,
		nullIfEmpty(input.Description),
		tagsJSON,
		allowedCIDRsJSON,
		input.ExpiresAt,
		input.MaxUses,
		nullIfEmpty(userID),
	).Scan(
		&row.ID,
		&row.OrgID,
		&row.TokenPrefix,
		&row.Description,
		&row.TagsJSON,
		&row.AllowedCIDRsJSON,
		&row.ExpiresAt,
		&row.MaxUses,
		&row.UseCount,
		&row.CreatedBy,
		&row.CreatedAt,
		&row.LastUsedAt,
		&row.RevokedAt,
	)
	if err != nil {
		return nil, err
	}

	bt, err := mapBootstrapTokenRow(row)
	if err != nil {
		return nil, err
	}

	return &models.CreateBootstrapTokenResponse{
		BootstrapToken: bt,
		Token:          token,
	}, nil
}

func (s *Storage) GetBootstrapTokens(ctx context.Context, orgID string) ([]models.BootstrapToken, error) {
	query := `
		SELECT id, org_id, token_prefix, token_hash, description, tags, allowed_cidrs,
			expires_at, max_uses, use_count, created_by, created_at, last_used_at, revoked_at
		FROM bootstrap_tokens
		WHERE org_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]models.BootstrapToken, 0)
	for rows.Next() {
		var row bootstrapTokenRow
		if err := rows.Scan(
			&row.ID,
			&row.OrgID,
			&row.TokenPrefix,
			&row.TokenHash,
			&row.Description,
			&row.TagsJSON,
			&row.AllowedCIDRsJSON,
			&row.ExpiresAt,
			&row.MaxUses,
			&row.UseCount,
			&row.CreatedBy,
			&row.CreatedAt,
			&row.LastUsedAt,
			&row.RevokedAt,
		); err != nil {
			return nil, err
		}

		bt, err := mapBootstrapTokenRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, bt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Storage) GetBootstrapToken(ctx context.Context, tokenID string) (*models.BootstrapToken, error) {
	query := `
		SELECT id, org_id, token_prefix, token_hash, description, tags, allowed_cidrs,
			expires_at, max_uses, use_count, created_by, created_at, last_used_at, revoked_at
		FROM bootstrap_tokens
		WHERE id = $1
	`

	var row bootstrapTokenRow
	if err := s.db.QueryRowContext(ctx, query, tokenID).Scan(
		&row.ID,
		&row.OrgID,
		&row.TokenPrefix,
		&row.TokenHash,
		&row.Description,
		&row.TagsJSON,
		&row.AllowedCIDRsJSON,
		&row.ExpiresAt,
		&row.MaxUses,
		&row.UseCount,
		&row.CreatedBy,
		&row.CreatedAt,
		&row.LastUsedAt,
		&row.RevokedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	bt, err := mapBootstrapTokenRow(row)
	if err != nil {
		return nil, err
	}
	return &bt, nil
}

func (s *Storage) ValidateBootstrapToken(ctx context.Context, token string, remoteIP string) (*models.BootstrapToken, error) {
	if len(token) < tokenPrefixLength {
		return nil, ErrTokenNotFound
	}

	prefix := token[:tokenPrefixLength]
	query := `
		SELECT id, org_id, token_prefix, token_hash, description, tags, allowed_cidrs,
			expires_at, max_uses, use_count, created_by, created_at, last_used_at, revoked_at
		FROM bootstrap_tokens
		WHERE token_prefix = $1
	`

	rows, err := s.db.QueryContext(ctx, query, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var row bootstrapTokenRow
		if err := rows.Scan(
			&row.ID,
			&row.OrgID,
			&row.TokenPrefix,
			&row.TokenHash,
			&row.Description,
			&row.TagsJSON,
			&row.AllowedCIDRsJSON,
			&row.ExpiresAt,
			&row.MaxUses,
			&row.UseCount,
			&row.CreatedBy,
			&row.CreatedAt,
			&row.LastUsedAt,
			&row.RevokedAt,
		); err != nil {
			return nil, err
		}

		if !ValidateTokenHash(token, row.TokenHash) {
			continue
		}

		if row.RevokedAt != nil {
			return nil, ErrTokenRevoked
		}
		if row.ExpiresAt != nil && row.ExpiresAt.Before(time.Now()) {
			return nil, ErrTokenExpired
		}
		if row.MaxUses.Valid && row.UseCount >= int(row.MaxUses.Int64) {
			return nil, ErrTokenUsageLimitReached
		}

		allowedCIDRs, err := decodeStringArray(row.AllowedCIDRsJSON)
		if err != nil {
			return nil, err
		}
		if len(allowedCIDRs) > 0 && !ipAllowed(remoteIP, allowedCIDRs) {
			return nil, ErrTokenIPNotAllowed
		}

		bt, err := mapBootstrapTokenRow(row)
		if err != nil {
			return nil, err
		}
		return &bt, nil
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return nil, ErrTokenNotFound
}

func (s *Storage) IncrementBootstrapTokenUsage(ctx context.Context, tokenID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE bootstrap_tokens
		SET use_count = use_count + 1, last_used_at = NOW()
		WHERE id = $1
	`, tokenID)
	return err
}

func (s *Storage) RevokeBootstrapToken(ctx context.Context, tokenID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE bootstrap_tokens
		SET revoked_at = NOW()
		WHERE id = $1
	`, tokenID)
	return err
}

func GenerateBootstrapToken() (token string, prefix string, hash string, err error) {
	bytes := make([]byte, TokenLength)
	if _, err = rand.Read(bytes); err != nil {
		return "", "", "", err
	}

	token = TokenPrefix + hex.EncodeToString(bytes)
	prefix = token[:tokenPrefixLength]

	hashBytes, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", "", "", err
	}

	return token, prefix, string(hashBytes), nil
}

func ValidateTokenHash(token, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil
}

func mapBootstrapTokenRow(row bootstrapTokenRow) (models.BootstrapToken, error) {
	tags, err := decodeStringArray(row.TagsJSON)
	if err != nil {
		return models.BootstrapToken{}, err
	}

	allowedCIDRs, err := decodeStringArray(row.AllowedCIDRsJSON)
	if err != nil {
		return models.BootstrapToken{}, err
	}

	var maxUses *int
	if row.MaxUses.Valid {
		value := int(row.MaxUses.Int64)
		maxUses = &value
	}

	bt := models.BootstrapToken{
		ID:           row.ID,
		OrgID:        row.OrgID,
		TokenPrefix:  row.TokenPrefix,
		Description:  row.Description.String,
		Tags:         tags,
		AllowedCIDRs: allowedCIDRs,
		ExpiresAt:    row.ExpiresAt,
		MaxUses:      maxUses,
		UseCount:     row.UseCount,
		CreatedBy:    row.CreatedBy.String,
		CreatedAt:    row.CreatedAt,
		LastUsedAt:   row.LastUsedAt,
		RevokedAt:    row.RevokedAt,
	}

	return bt, nil
}

func decodeStringArray(data []byte) ([]string, error) {
	if len(data) == 0 {
		return []string{}, nil
	}

	var out []string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func ipAllowed(remoteIP string, cidrs []string) bool {
	ip := net.ParseIP(remoteIP)
	if ip == nil {
		return false
	}

	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

func nullIfEmpty(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	return false
}
