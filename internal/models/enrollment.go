package models

import "time"

// POST /api/v1/agents/enroll request
// Signature is over nonce + timestamp (Unix ms).
type EnrollAgentRequest struct {
	AgentID             string `json:"agent_id" validate:"required,len=12,hexadecimal,lowercase"`
	PublicKey           string `json:"public_key" validate:"required,startswith=U"`
	Hostname            string `json:"hostname" validate:"required,max=255"`
	HardwareFingerprint string `json:"hardware_fingerprint" validate:"required"`
	OS                  string `json:"os"`
	Arch                string `json:"arch"`
	AgentVersion        string `json:"agent_version"`
	Nonce               string `json:"nonce" validate:"required"`
	Timestamp           int64  `json:"timestamp" validate:"required"`
	Signature           string `json:"signature" validate:"required"`
}

// POST /api/v1/agents/enroll response

type EnrollAgentResponse struct {
	AgentID   string   `json:"agent_id"`
	OrgID     string   `json:"org_id"`
	JWT       string   `json:"jwt"`
	NATSURLs  []string `json:"nats_urls"`
	Tags      []string `json:"tags"`
	ExpiresAt string   `json:"expires_at"`
}

type AgentConnection struct {
	ID               string     `db:"id" json:"id"`
	AgentID          string     `db:"agent_id" json:"agent_id"`
	NATSClientID     string     `db:"nats_client_id" json:"nats_client_id"`
	RemoteIP         string     `db:"remote_ip" json:"remote_ip"`
	Hostname         string     `db:"hostname" json:"hostname"`
	ConnectedAt      time.Time  `db:"connected_at" json:"connected_at"`
	DisconnectedAt   *time.Time `db:"disconnected_at" json:"disconnected_at,omitempty"`
	DisconnectReason string     `db:"disconnect_reason" json:"disconnect_reason,omitempty"`
}

type AgentConflict struct {
	ID               string     `db:"id" json:"id"`
	AgentID          string     `db:"agent_id" json:"agent_id"`
	ExistingIP       string     `db:"existing_ip" json:"existing_ip"`
	NewIP            string     `db:"new_ip" json:"new_ip"`
	ExistingHostname string     `db:"existing_hostname" json:"existing_hostname"`
	NewHostname      string     `db:"new_hostname" json:"new_hostname"`
	Resolution       string     `db:"resolution" json:"resolution"`
	ResolvedBy       *string    `db:"resolved_by" json:"resolved_by,omitempty"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	ResolvedAt       *time.Time `db:"resolved_at" json:"resolved_at,omitempty"`
}
