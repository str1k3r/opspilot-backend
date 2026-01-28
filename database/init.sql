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

CREATE INDEX IF NOT EXISTS idx_agents_agent_id ON agents(agent_id);
CREATE INDEX IF NOT EXISTS idx_incidents_agent_id ON incidents(agent_id);
CREATE INDEX IF NOT EXISTS idx_incidents_created_at ON incidents(created_at DESC);
