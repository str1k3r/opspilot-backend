package models

import "time"

type BootstrapToken struct {
	ID           string     `db:"id" json:"id"`
	OrgID        string     `db:"org_id" json:"org_id"`
	TokenPrefix  string     `db:"token_prefix" json:"token_prefix"`
	Description  string     `db:"description" json:"description"`
	Tags         []string   `db:"tags" json:"tags"`
	AllowedCIDRs []string   `db:"allowed_cidrs" json:"allowed_cidrs,omitempty"`
	ExpiresAt    *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	MaxUses      *int       `db:"max_uses" json:"max_uses,omitempty"`
	UseCount     int        `db:"use_count" json:"use_count"`
	CreatedBy    string     `db:"created_by" json:"created_by"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	LastUsedAt   *time.Time `db:"last_used_at" json:"last_used_at,omitempty"`
	RevokedAt    *time.Time `db:"revoked_at" json:"revoked_at,omitempty"`
}

type CreateBootstrapTokenInput struct {
	Description  string     `json:"description" validate:"max=255"`
	Tags         []string   `json:"tags"`
	AllowedCIDRs []string   `json:"allowed_cidrs" validate:"dive,cidr"`
	ExpiresAt    *time.Time `json:"expires_at"`
	MaxUses      *int       `json:"max_uses" validate:"omitempty,min=1"`
}

type CreateBootstrapTokenResponse struct {
	BootstrapToken
	Token string `json:"token"`
}
