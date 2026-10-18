CREATE TABLE users (
    id text PRIMARY KEY,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    display_name text NOT NULL,
    role text NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    email_verified boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id),
    token_hash text NOT NULL UNIQUE,
    csrf_token text NOT NULL,
    user_agent text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);
CREATE INDEX sessions_user_idx ON sessions(user_id,expires_at);
CREATE TABLE auth_tokens (
    token_hash text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id),
    purpose text NOT NULL CHECK (purpose IN ('verify','reset')),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE auth_mail_outbox (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id),
    to_email text NOT NULL,
    subject text NOT NULL,
    body text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_mail_pending_idx ON auth_mail_outbox(next_attempt_at) WHERE sent_at IS NULL;
CREATE TABLE auth_rate_limits (
    key text NOT NULL,
    bucket bigint NOT NULL,
    count integer NOT NULL DEFAULT 1,
    PRIMARY KEY(key,bucket)
);
CREATE TABLE identity_admin_audit (
    id text PRIMARY KEY,
    actor_id text NOT NULL REFERENCES users(id),
    target_user_id text NOT NULL REFERENCES users(id),
    action text NOT NULL,
    reason text NOT NULL,
    previous_status text NOT NULL,
    next_status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE projects (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id),
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    cover_asset_id text,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    revision bigint NOT NULL DEFAULT 0,
    shots_revision bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    UNIQUE(id,user_id)
);
CREATE INDEX projects_user_recent_idx ON projects(user_id,updated_at DESC) WHERE deleted_at IS NULL;
CREATE TABLE canvases (
    project_id text PRIMARY KEY REFERENCES projects(id),
    graph_schema_version integer NOT NULL DEFAULT 1 CHECK(graph_schema_version >= 1),
    revision bigint NOT NULL DEFAULT 0,
    graph jsonb NOT NULL DEFAULT '{"nodes":[],"edges":[]}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE shots (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    user_id text NOT NULL,
    position integer NOT NULL,
    state jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(project_id,user_id) REFERENCES projects(id,user_id)
);
CREATE INDEX shots_project_position_idx ON shots(project_id,position);
CREATE TABLE assets (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    project_id text NOT NULL,
    task_id text UNIQUE,
    kind text NOT NULL CHECK(kind IN ('image','video','audio')),
    file_name text NOT NULL,
    content_type text NOT NULL,
    object_key text NOT NULL UNIQUE,
    size_bytes bigint NOT NULL CHECK(size_bytes > 0),
    width integer NOT NULL DEFAULT 0,
    height integer NOT NULL DEFAULT 0,
    duration_seconds double precision NOT NULL DEFAULT 0,
    sha256 text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    FOREIGN KEY(project_id,user_id) REFERENCES projects(id,user_id)
);
CREATE INDEX assets_user_project_idx ON assets(user_id,project_id,created_at DESC) WHERE deleted_at IS NULL;
CREATE TABLE asset_references (
    asset_id text NOT NULL REFERENCES assets(id),
    project_id text NOT NULL REFERENCES projects(id),
    owner_type text NOT NULL CHECK(owner_type IN ('canvas','shot','task','cover')),
    owner_id text NOT NULL,
    PRIMARY KEY(asset_id,owner_type,owner_id)
);
