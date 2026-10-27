ALTER TABLE users
    ADD COLUMN admin_note text NOT NULL DEFAULT '',
    ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;

ALTER TABLE identity_admin_audit
    ADD COLUMN previous_values jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN next_values jsonb NOT NULL DEFAULT '{}';
CREATE INDEX identity_admin_audit_target_created_idx
    ON identity_admin_audit(target_user_id, created_at DESC, id DESC);

ALTER TABLE auth_mail_outbox
    ADD COLUMN purpose text NOT NULL DEFAULT '',
    ADD COLUMN cancelled_at timestamptz,
    ADD COLUMN last_error_code text NOT NULL DEFAULT '';
UPDATE auth_mail_outbox SET purpose = CASE
    WHEN subject = '验证您的创作平台邮箱' THEN 'verify'
    WHEN subject = '重置您的创作平台密码' THEN 'reset'
    ELSE '' END;
CREATE INDEX auth_mail_user_recent_idx ON auth_mail_outbox(user_id, created_at DESC, id DESC);
