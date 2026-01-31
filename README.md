# OpsPilot Backend

Go backend for the OpsPilot monitoring platform.

## Architecture

- **Transport**: NATS (JetStream for events & inventory, KV for heartbeats, Request-Reply for actions)
- **Storage**: PostgreSQL (agents, incidents, inventory history)
- **Cache**: Redis (agent presence + rate limits + cache)
- **AI**: OpenRouter (analysis) — optional
- **Slack**: Stubbed (disabled for now)

Protocol details are documented in `opspilot-agent/PROTOCOL.md`.

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
curl http://localhost:8080/api/v1/agents  # Backend API
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
NATS_URLS=nats://nats:4222
DB_HOST=postgres
DB_USER=ops_user
DB_PASSWORD=ops_pass
DB_NAME=opspilot
REDIS_URL=redis://redis:6379/0
JWT_SECRET=change_me
```

Redis keyspace notifications are required for online/offline transitions:
```
notify-keyspace-events Ex
```
If disabled, the backend uses a fallback reconciler.

## API Endpoints

- `POST /api/v1/auth/login` — login (Bearer token)
- `GET /api/v1/auth/me` — current user
- `POST /api/v1/agents/enroll` — enroll agent (bootstrap token)
- `GET /api/v1/agents` — list agents
- `GET /api/v1/agents/{id}/incidents` — list incidents for an agent
- `POST /api/v1/agents/{id}/execute` — execute action on agent (RPC)
- `POST /api/v1/incidents/{id}/analyze` — run AI analysis
- `POST /api/v1/incidents/{id}/execute` — execute suggested action

### Auth (Bearer)
```
Authorization: Bearer <jwt>
```

### Exec example
```bash
curl -X POST http://localhost:8080/api/v1/agents/{id}/execute \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"a1b2c3d4e5f6","command":"restart_service","params":{"service":"nginx"}}'
```

If the agent is offline, the endpoint returns `404`.

## NATS Channels

- **Events** (JetStream): `ops.{agent_id}.events.*`
- **Inventory** (JetStream): `ops.{agent_id}.inventory`
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

CREATE TABLE IF NOT EXISTS agents_inventory (
    id BIGSERIAL PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
    ts TIMESTAMPTZ NOT NULL DEFAULT now(),
    hash TEXT NOT NULL,
    payload JSONB NOT NULL
);
```

## Logging

Key runtime logs:
- Connected to NATS, stream/KV creation
- Events consumer started / Inventory consumer started / KV watcher started
- Redis keyevent worker / fallback reconciler
- Agent heartbeat cached / Agent offline transitions
- Incident created

## Project Structure

```
opspilot-backend/
├── cmd/server/              # Entry point
├── internal/
│   ├── cache/               # Redis helpers
│   ├── handlers/            # HTTP handlers (REST + RPC exec)
│   ├── ingest/              # JetStream consumers + KV watcher
│   ├── middleware/          # HTTP middleware (rate limiting)
│   ├── models/              # DB + wire models
│   ├── natsbus/             # NATS connection + infra init
│   ├── rpc/                 # Request-Reply client
│   ├── services/            # AI + Slack (stub)
│   ├── storage/             # DB operations
│   └── workers/             # Redis keyevents + fallback reconciler
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
