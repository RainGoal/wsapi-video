CREATE TABLE runtime_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 revision bigint NOT NULL CHECK (revision > 0),
 payload bytea NOT NULL,
 updated_by text NOT NULL REFERENCES users(id),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE runtime_credential_versions (
 credential_ref text PRIMARY KEY,
 kind text NOT NULL CHECK (kind IN ('media', 'payment')),
 payload bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
