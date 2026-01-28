# OpsPilot Backend

Go backend for the OpsPilot monitoring platform.

## Architecture

- **Transport**: NATS (JetStream for events, KV for heartbeats, Request-Reply for actions)
- **Storage**: PostgreSQL (agents + incidents)
- **AI**: OpenRouter (analysis) — optional
- **Slack**: Stubbed (disabled for now)

## Prerequisites

- Docker & Docker Compose
- Go 1.25.6+ (for local development)

## Quick Start

1) Start the stack:
```bash
docker-compose up --build
```

2) Verify services:
```bash
curl http://localhost:8222/healthz    # NATS monitor
curl http://localhost:8080/v1/agents  # Backend API
```

3) Reset DB schema:
```bash
docker-compose down -v

docker-compose up -d postgres
# init.sql runs on fresh volume
```

## Environment Variables

Set in `.env` or Docker environment:
```
OPENROUTER_KEY=sk-or-...
NATS_URL=nats://nats:4222
DB_HOST=postgres
DB_USER=ops_user
DB_PASSWORD=ops_pass
DB_NAME=opspilot
```

## API Endpoints

- `GET /v1/agents` — list agents
- `GET /v1/agents/{id}/incidents` — list incidents for an agent
- `POST /v1/admin/exec` — execute action on agent (RPC)
- `POST /v1/incidents/{id}/analyze` — run AI analysis
- `POST /v1/incidents/{id}/execute` — execute suggested action

### Exec example
```bash
curl -X POST http://localhost:8080/v1/admin/exec \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"a1b2c3d4e5f6","command":"restart_service","params":{"service":"nginx"}}'
```

If the agent is offline, the endpoint returns `404`.

## NATS Channels

- **Events** (JetStream): `ops.{agent_id}.events.*`
- **Heartbeats** (KV bucket): `AGENTS` key `{agent_id}`
- **Actions** (RPC): `ops.{agent_id}.rpc`

NATS URL for local dev (docker): `nats://nats:4222`

## Database Schema

```sql
CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id VARCHAR(12) NOT NULL UNIQUE,
    hostname VARCHAR(255),
    status VARCHAR(20) DEFAULT 'offline',
    last_seen_at TIMESTAMP,
    meta JSONB
);

CREATE TABLE IF NOT EXISTS incidents (
    id SERIAL PRIMARY KEY,
    agent_id VARCHAR(12) REFERENCES agents(agent_id),
    type VARCHAR(50),
    source VARCHAR(255),
    raw_error TEXT,
    context JSONB,
    ai_analysis TEXT,
    is_critical BOOLEAN DEFAULT FALSE,
    suggested_action JSONB,
    status VARCHAR(20) DEFAULT 'new',
    created_at TIMESTAMP DEFAULT NOW()
);
```

## Logging

Key runtime logs:
- Connected to NATS, stream/KV creation
- Events consumer started / KV watcher started
- Agent heartbeat / Agent offline
- Incident created

## Project Structure

```
opspilot-backend/
├── cmd/server/              # Entry point
├── internal/
│   ├── handlers/            # HTTP handlers (REST + RPC exec)
│   ├── ingest/              # JetStream consumer + KV watcher
│   ├── models/              # DB + v3 wire models
│   ├── natsbus/             # NATS connection + infra init
│   ├── rpc/                 # Request-Reply client
│   ├── services/            # AI + Slack (stub)
│   └── storage/             # DB operations
├── Dockerfile
├── .air.toml
└── go.mod
```

## Development

### Build
```bash
go build ./...
```

### Logs
```bash
docker logs opspilot-backend -f
```

## Notes

- Slack integration is disabled (stubbed) until re-enabled.
