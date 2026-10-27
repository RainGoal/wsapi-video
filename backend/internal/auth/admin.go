package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

// BootstrapAdmin is opt-in via deployment configuration. It never promotes an
// existing user or silently changes an existing administrator's password.
func (s *Service) BootstrapAdmin(ctx context.Context, email, password string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	if err = validatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	id := core.ID("usr")
	var existingID, role string
	err = tx.QueryRow(ctx, `SELECT id,role FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&existingID, &role)
	if err == nil {
		if role != "admin" {
			return errors.New("bootstrap email belongs to a regular user")
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,role,email_verified) VALUES($1,$2,$3,'管理员','admin',true)`, id, email, string(hash))
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO wallet_accounts(user_id,available,frozen) VALUES($1,0,0) ON CONFLICT(user_id) DO NOTHING`, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) adminUsers(w http.ResponseWriter, r *http.Request) {
	s.pagedAdminUsers(w, r)
}

func (s *Service) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Status != "active" && in.Status != "disabled" {
		httpx.Fail(w, 422, "invalid_status", "无效用户状态")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || len([]rune(in.Reason)) > 1000 {
		httpx.Fail(w, 422, "reason_required", "请填写操作原因（最多 1000 个字符）")
		return
	}
	if r.PathValue("id") == core.UserFrom(r.Context()).ID && in.Status == "disabled" {
		httpx.Fail(w, 422, "self_disable", "不能停用当前管理员账号")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	var previous string
	err = tx.QueryRow(r.Context(), `SELECT status FROM users WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, 404, "not_found", "用户不存在")
		return
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE users SET status=$2,updated_at=now() WHERE id=$1`, r.PathValue("id"), in.Status)
	}
	if err == nil && in.Status == "disabled" {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, r.PathValue("id"))
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO identity_admin_audit(id,actor_id,target_user_id,action,reason,previous_status,next_status) VALUES($1,$2,$3,'set_status',$4,$5,$6)`, core.ID("aud"), core.UserFrom(r.Context()).ID, r.PathValue("id"), in.Reason, previous, in.Status)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.Fail(w, 500, "user_update_failed", "无法更新用户")
		return
	}
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}
