package models

import "time"

type Agent struct {
	ID                  string     `json:"id" db:"id"`
	AgentID             string     `json:"agent_id" db:"agent_id"`
	OrgID               string     `json:"org_id" db:"org_id"`
	Name                string     `json:"name" db:"name"`
	Hostname            string     `json:"hostname" db:"hostname"`
	Status              string     `json:"status" db:"status"`
	Tags                []string   `json:"tags" db:"tags"`
	HardwareFingerprint string     `json:"hardware_fingerprint" db:"hardware_fingerprint"`
	EnrolledVia         *string    `json:"enrolled_via,omitempty" db:"enrolled_via"`
	EnrolledAt          *time.Time `json:"enrolled_at,omitempty" db:"enrolled_at"`
	EnrolledIP          *string    `json:"enrolled_ip,omitempty" db:"enrolled_ip"`
	LastSeenAt          *time.Time `json:"last_seen_at" db:"last_seen_at"`
	Meta                []byte     `json:"meta" db:"meta"`
}

type Incident struct {
	ID                  int                    `json:"id" db:"id"`
	AgentID             string                 `json:"agent_id" db:"agent_id"`
	Type                string                 `json:"type" db:"type"`
	Source              string                 `json:"source" db:"source"`
	RawError            string                 `json:"raw_error" db:"raw_error"`
	Context             map[string]interface{} `json:"context,omitempty" db:"-"`
	ContextJSON         []byte                 `json:"-" db:"context"`
	AIAnalysis          string                 `json:"ai_analysis" db:"ai_analysis"`
	IsCritical          bool                   `json:"is_critical" db:"is_critical"`
	SuggestedAction     *SuggestedAction       `json:"suggested_action,omitempty" db:"-"`
	SuggestedActionJSON []byte                 `json:"-" db:"suggested_action"`
	Status              string                 `json:"status" db:"status"`
	CreatedAt           time.Time              `json:"created_at" db:"created_at"`
}

type CommandPayload struct {
	Command string      `json:"command"`
	Params  interface{} `json:"params"`
}

type SuggestedAction struct {
	Cmd   string            `json:"cmd"`
	Args  map[string]string `json:"args"`
	Label string            `json:"label"`
}

type AIAnalysis struct {
	Analysis        string           `json:"analysis"`
	IsCritical      bool             `json:"is_critical"`
	SuggestedAction *SuggestedAction `json:"suggested_action,omitempty"`
}
