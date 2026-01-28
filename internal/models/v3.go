package models

// EventV3 is the wire format for JetStream events from agents.
type EventV3 struct {
	V         int                    `msgpack:"v"`
	TS        int64                  `msgpack:"ts"`
	AgentID   string                 `msgpack:"agent_id"`
	AlertType string                 `msgpack:"alert_type"`
	Message   string                 `msgpack:"message"`
	Details   map[string]interface{} `msgpack:"details"`
	Truncated bool                   `msgpack:"truncated"`
}

// HeartbeatV3 is the wire format for KV heartbeat entries.
type HeartbeatV3 struct {
	V              int          `msgpack:"v"`
	AgentID        string       `msgpack:"agent_id"`
	AgentVersion   string       `msgpack:"agent_version"`
	Hostname       string       `msgpack:"hostname"`
	OS             string       `msgpack:"os"`
	Arch           string       `msgpack:"arch"`
	Uptime         int64        `msgpack:"uptime"`
	ConnectedSince int64        `msgpack:"connected_since"`
	Capabilities   []string     `msgpack:"capabilities"`
	CPUPercent     float64      `msgpack:"cpu_percent"`
	MemPercent     float64      `msgpack:"mem_percent"`
	Watchers       int          `msgpack:"watchers"`
	Actions        []string     `msgpack:"actions"`
	Inventory      *InventoryV3 `msgpack:"inventory,omitempty"`
}

// InventoryV3 is the discovery data sent on first heartbeat.
type InventoryV3 struct {
	Platform        string             `msgpack:"platform"`
	PlatformVersion string             `msgpack:"platform_version"`
	KernelVersion   string             `msgpack:"kernel_version"`
	CPUModel        string             `msgpack:"cpu_model"`
	RAMTotal        int64              `msgpack:"ram_total"`
	Candidates      []ProcessCandidate `msgpack:"candidates"`
}

// ProcessCandidate is a discovered service/process.
type ProcessCandidate struct {
	Name          string       `msgpack:"name"`
	Cmdline       string       `msgpack:"cmdline"`
	PID           int          `msgpack:"pid"`
	Type          string       `msgpack:"type"`
	ListenPorts   []int        `msgpack:"listen_ports"`
	SourceSystemd string       `msgpack:"source_systemd"`
	SourceDocker  string       `msgpack:"source_docker"`
	Stats         ProcessStats `msgpack:"stats"`
}

// ProcessStats contains resource usage for a process.
type ProcessStats struct {
	CPUPercent float64 `msgpack:"cpu_percent"`
	MemRSS     int64   `msgpack:"mem_rss"`
}

// ActionRequestV3 is the RPC request sent to agents.
type ActionRequestV3 struct {
	Action    string            `msgpack:"action"`
	Args      map[string]string `msgpack:"args"`
	RequestID string            `msgpack:"request_id"`
	TimeoutMS int               `msgpack:"timeout_ms,omitempty"`
}

// ActionResponseV3 is the RPC response from agents.
type ActionResponseV3 struct {
	RequestID  string `msgpack:"request_id"`
	Success    bool   `msgpack:"success"`
	Output     string `msgpack:"output,omitempty"`
	ExitCode   int    `msgpack:"exit_code,omitempty"`
	DurationMS int64  `msgpack:"duration_ms,omitempty"`
	Error      string `msgpack:"error,omitempty"`
	ErrorCode  string `msgpack:"error_code,omitempty"`
	Truncated  bool   `msgpack:"truncated"`
}

// Helper method to extract source from event details (same logic as v2).
func (e *EventV3) GetSource() string {
	for _, key := range []string{"source", "container_name", "service", "path"} {
		if v, ok := e.Details[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "unknown"
}

// GetLogs extracts logs from event details.
func (e *EventV3) GetLogs() string {
	if v, ok := e.Details["logs"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
