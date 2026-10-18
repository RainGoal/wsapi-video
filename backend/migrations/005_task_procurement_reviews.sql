CREATE TABLE task_procurement_reviews (
 id text PRIMARY KEY,
 task_id text NOT NULL REFERENCES generation_tasks(id),
 revision bigint NOT NULL CHECK (revision > 0),
 admin_id text NOT NULL REFERENCES users(id),
 status text NOT NULL CHECK (status IN ('pending','matched','disputed','resolved')),
 reason text NOT NULL CHECK (octet_length(btrim(reason)) >= 3 AND char_length(reason) <= 2000),
 evidence text NOT NULL DEFAULT '' CHECK (char_length(evidence) <= 2000),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(task_id,revision)
);
CREATE INDEX task_procurement_reviews_admin ON task_procurement_reviews(admin_id,created_at DESC);
