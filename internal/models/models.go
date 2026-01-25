package models

import "time"

type Agent struct {
	ID         string     `json:"id" db:"id"`
	TokenHash  string     `json:"token_hash" db:"token_hash"`
	Hostname   string     `json:"hostname" db:"hostname"`
	Status     string     `json:"status" db:"status"`
	LastSeenAt *time.Time `json:"last_seen_at" db:"last_seen_at"`
	Meta       []byte     `json:"meta" db:"meta"`
}

type Incident struct {
	ID         int       `json:"id" db:"id"`
	AgentID    string    `json:"agent_id" db:"agent_id"`
	Type       string    `json:"type" db:"type"`
	Source     string    `json:"source" db:"source"`
	RawError   string    `json:"raw_error" db:"raw_error"`
	AIAnalysis string    `json:"ai_analysis" db:"ai_analysis"`
	AISolution string    `json:"ai_solution" db:"ai_solution"`
	Status     string    `json:"status" db:"status"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type AgentPayload struct {
	Version     string     `json:"ver"`
	Timestamp   int64      `json:"ts"`
	Token       string     `json:"token"`
	Type        string     `json:"type"`
	Data        string     `json:"data"`
	Compression string     `json:"compression"`
	ParsedData  *AlertData `json:"-"`
}

type AlertData struct {
	Type     string                 `json:"type"`
	Source   string                 `json:"source"`
	Message  string                 `json:"message"`
	Logs     string                 `json:"logs"`
	ExitCode int                    `json:"exit_code"`
	Status   string                 `json:"status"`
	Context  map[string]interface{} `json:"context"`
}

type CommandPayload struct {
	Command string      `json:"command"`
	Params  interface{} `json:"params"`
}

type AIAnalysis struct {
	Cause  string `json:"cause"`
	FixCmd string `json:"fix_cmd"`
}
