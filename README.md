# OpsPilot Backend

Go backend for OpsPilot monitoring platform with WebSocket, PostgreSQL, AI, and Slack integration.

## Architecture

- **Transport Layer**: WebSocket connections with Gzip+Base64 decompression
- **Service Layer**: Event routing, agent management, incident handling
- **Integration Layer**: OpenRouter AI, Slack alerts

## Setup

### Prerequisites
- Docker & Docker Compose
- Go 1.23+ (for local development)

### Quick Start

1. **Configure environment variables** in `.env`:
```env
OPENROUTER_KEY=sk-or-v1-...
SLACK_TOKEN=xoxb-...
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...
```

2. **Start services**:
```bash
docker-compose up --build
```

3. **Verify**:
```bash
curl http://localhost:8080/
curl http://localhost:8080/v1/agents
```

## API Endpoints

### WebSocket
- `WS /v1/stream` - Agent connection endpoint

### REST API
- `GET /v1/agents` - List all agents
- `GET /v1/agents/{id}/incidents` - Get incidents for agent
- `POST /v1/admin/exec` - Send command to agent

```bash
# Send command to agent
curl -X POST http://localhost:8080/v1/admin/exec \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "a0eebc99...", "command": "restart_service", "params": {"service": "nginx"}}'
```

## Database Schema

### Agents
```sql
CREATE TABLE agents (
    id UUID PRIMARY KEY,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    hostname VARCHAR(255),
    status VARCHAR(20) DEFAULT 'offline',
    last_seen_at TIMESTAMP,
    meta JSONB
);
```

### Incidents
```sql
CREATE TABLE incidents (
    id SERIAL PRIMARY KEY,
    agent_id UUID REFERENCES agents(id),
    type VARCHAR(50),
    source VARCHAR(255),
    raw_error TEXT,
    ai_analysis TEXT,
    ai_solution TEXT,
    status VARCHAR(20) DEFAULT 'new',
    created_at TIMESTAMP DEFAULT NOW()
);
```

## Testing

### Manual Test
```bash
# Connect via WebSocket (using wscat or similar)
wscat -c ws://localhost:8080/v1/stream

# Send heartbeat
{"ver":"1.0","ts":1706140800,"token":"my-secret-token","type":"heartbeat","data":"","compression":"none"}

# Send alert (with base64+gzip)
# Alert data: {"type":"systemd","source":"nginx","message":"nginx crashed","logs":"error logs","exit_code":1}
# Compress with gzip, encode with base64
{"ver":"1.0","ts":1706140800,"token":"my-secret-token","type":"alert","data":"<base64+gzip>","compression":"gzip"}
```

## Services

### AI Integration (OpenRouter)
- Model: `qwen/qwen-2.5-coder-32b-instruct`
- Analyzes incidents and suggests fixes
- Falls back to heuristics if API unavailable

### Slack Integration
- Sends formatted Block Kit messages
- Includes action buttons: Restart, Logs, Ignore
- Configurable via `SLACK_WEBHOOK_URL`

## Development

### Hot Reload
Backend uses `air` for hot reload. Changes are automatically reloaded in the container.

### Add Dependencies
```bash
cd opspilot-backend
go get <package>
go mod tidy
```

### View Logs
```bash
# Backend
docker logs opspilot-backend -f

# Database
docker logs opspilot-db -f
```

## Project Structure

```
backend/
├── cmd/
│   └── server/
│       └── main.go          # Entry point
├── internal/
│   ├── handlers/            # HTTP/WebSocket handlers
│   ├── hub/                 # Connection manager
│   ├── models/              # Data structures
│   ├── services/            # AI, Slack integrations
│   └── storage/             # Database operations
├── Dockerfile               # Dev container
├── .air.toml                # Hot reload config
└── go.mod
```
