-- Tool workspaces reuse asset/task ownership without creating a canvas project.
ALTER TABLE projects ADD COLUMN workspace_kind text NOT NULL DEFAULT ''
    CHECK (workspace_kind IN ('','product_swap'));
CREATE UNIQUE INDEX projects_tool_workspace_idx ON projects(user_id,workspace_kind)
    WHERE workspace_kind <> '' AND deleted_at IS NULL;

CREATE TABLE product_swap_analyses (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id),
    input_hash text NOT NULL,
    result jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX product_swap_analyses_user_idx ON product_swap_analyses(user_id,created_at DESC);
