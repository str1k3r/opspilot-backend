CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(63) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS bootstrap_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    token_prefix VARCHAR(12) NOT NULL,
    description VARCHAR(255),
    tags JSONB DEFAULT '[]'::jsonb,
    allowed_cidrs JSONB DEFAULT NULL,
    expires_at TIMESTAMPTZ,
    max_uses INT,
    use_count INT DEFAULT 0,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id VARCHAR(12) NOT NULL UNIQUE,
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255),
    hostname VARCHAR(255),
    status VARCHAR(20) DEFAULT 'offline',
    last_seen_at TIMESTAMPTZ,
    tags JSONB DEFAULT '[]'::jsonb,
    hardware_fingerprint TEXT,
    enrolled_via UUID REFERENCES bootstrap_tokens(id) ON DELETE SET NULL,
    enrolled_at TIMESTAMPTZ,
    enrolled_ip INET,
    meta JSONB
);

CREATE TABLE IF NOT EXISTS agent_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
    public_key TEXT NOT NULL,
    is_pinned BOOLEAN DEFAULT false,
    fingerprint_at_registration TEXT,
    registered_from_ip INET,
    registered_hostname VARCHAR(255),
    jwt_expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    UNIQUE(agent_id, public_key)
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
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_organizations_slug ON organizations(slug);
CREATE INDEX IF NOT EXISTS idx_bootstrap_tokens_active ON bootstrap_tokens(org_id, revoked_at) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_bootstrap_tokens_prefix ON bootstrap_tokens(token_prefix);
CREATE INDEX IF NOT EXISTS idx_users_org_id ON users(org_id);
CREATE INDEX IF NOT EXISTS idx_agents_org_id ON agents(org_id);
CREATE INDEX IF NOT EXISTS idx_agents_tags ON agents USING GIN (tags);
CREATE INDEX IF NOT EXISTS idx_agents_agent_id ON agents(agent_id);
CREATE INDEX IF NOT EXISTS idx_incidents_agent_id ON incidents(agent_id);
CREATE INDEX IF NOT EXISTS idx_incidents_created_at ON incidents(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_creds_agent ON agent_credentials(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_creds_active ON agent_credentials(agent_id, revoked_at) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_pinned_credential
    ON agent_credentials(agent_id)
    WHERE is_pinned = true AND revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS agent_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id VARCHAR(12) REFERENCES agents(agent_id) ON DELETE CASCADE,
    nats_client_id VARCHAR(255),
    remote_ip INET NOT NULL,
    hostname VARCHAR(255),
    connected_at TIMESTAMPTZ DEFAULT now(),
    disconnected_at TIMESTAMPTZ,
    disconnect_reason VARCHAR(50)
);

CREATE INDEX IF NOT EXISTS idx_agent_connections_active
    ON agent_connections(agent_id, disconnected_at)
    WHERE disconnected_at IS NULL;

CREATE TABLE IF NOT EXISTS agent_conflicts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id VARCHAR(12) REFERENCES agents(agent_id) ON DELETE CASCADE,
    existing_ip INET,
    new_ip INET,
    existing_hostname VARCHAR(255),
    new_hostname VARCHAR(255),
    resolution VARCHAR(50),
    resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_conflicts_agent ON agent_conflicts(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_conflicts_unresolved ON agent_conflicts(resolved_at) WHERE resolved_at IS NULL;

INSERT INTO organizations (name, slug)
VALUES ('Default', 'default')
ON CONFLICT (slug) DO NOTHING;

INSERT INTO users (org_id, email, password_hash)
VALUES (
    (SELECT id FROM organizations WHERE slug = 'default' LIMIT 1),
    'admin@opspilot.io',
    crypt('admin', gen_salt('bf'))
)
ON CONFLICT (email) DO NOTHING;
