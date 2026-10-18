package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

func (s *Service) registerAdminDetails(mux *http.ServeMux, admin func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/admin/users/{id}", admin(http.HandlerFunc(s.adminUser)))
	mux.Handle("GET /api/v1/admin/users/{id}/ledger", admin(http.HandlerFunc(s.adminUserLedger)))
	mux.Handle("POST /api/v1/admin/users/{id}/revoke-sessions", admin(http.HandlerFunc(s.adminRevokeSessions)))
}

func (s *Service) adminUser(w http.ResponseWriter, r *http.Request) {
	var u core.User
	var available, frozen int64
	err := s.db.QueryRow(r.Context(), `SELECT u.id,u.email,u.display_name,u.role,u.status,u.email_verified,u.created_at,COALESCE(w.available,0),COALESCE(w.frozen,0) FROM users u LEFT JOIN wallet_accounts w ON w.user_id=u.id WHERE u.id=$1`, r.PathValue("id")).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.EmailVerified, &u.CreatedAt, &available, &frozen)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, 404, "not_found", "用户不存在")
		return
	}
	if err != nil {
		httpx.Fail(w, 500, "user_failed", "无法读取用户详情")
		return
	}
	var sessions int64
	if err = s.db.QueryRow(r.Context(), `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>now()`, u.ID).Scan(&sessions); err != nil {
		httpx.Fail(w, 500, "user_failed", "无法读取用户会话")
		return
	}
	httpx.JSON(w, 200, map[string]any{"user": u, "wallet": map[string]int64{"available": available, "frozen": frozen}, "active_sessions": sessions})
}

func (s *Service) adminRevokeSessions(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || len([]rune(in.Reason)) > 1000 {
		httpx.Fail(w, 422, "reason_required", "请填写操作原因（最多 1000 个字符）")
		return
	}
	if r.PathValue("id") == core.UserFrom(r.Context()).ID {
		httpx.Fail(w, 422, "self_revoke", "请在账户安全中管理自己的会话")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	var status string
	err = tx.QueryRow(r.Context(), "SELECT status FROM users WHERE id=$1 FOR UPDATE", r.PathValue("id")).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, 404, "not_found", "用户不存在")
		return
	}
	var revoked int64
	if err == nil {
		result, e := tx.Exec(r.Context(), "UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>now()", r.PathValue("id"))
		err, revoked = e, result.RowsAffected()
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO identity_admin_audit(id,actor_id,target_user_id,action,reason,previous_status,next_status) VALUES($1,$2,$3,'revoke_sessions',$4,$5,$5)`, core.ID("aud"), core.UserFrom(r.Context()).ID, r.PathValue("id"), in.Reason, status)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.Fail(w, 500, "revoke_failed", "无法撤销用户会话")
		return
	}
	httpx.JSON(w, 200, map[string]int64{"revoked": revoked})
}

func (s *Service) adminUserLedger(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	var exists bool
	if err := s.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)", r.PathValue("id")).Scan(&exists); err != nil {
		httpx.Fail(w, 500, "ledger_failed", "无法读取积分流水")
		return
	}
	if !exists {
		httpx.Fail(w, 404, "not_found", "用户不存在")
		return
	}
	var total int64
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM wallet_ledger WHERE user_id=$1", r.PathValue("id")).Scan(&total); err != nil {
		httpx.Fail(w, 500, "ledger_failed", "无法读取积分流水")
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT jsonb_build_object('id',id,'event_type',event_type,'available_delta',available_delta,'frozen_delta',frozen_delta,'available_after',available_after,'frozen_after',frozen_after,'task_id',task_id,'order_id',order_id,'reason',reason,'created_at',created_at) FROM wallet_ledger WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, r.PathValue("id"), page.Limit, page.Offset)
	if err != nil {
		httpx.Fail(w, 500, "ledger_failed", "无法读取积分流水")
		return
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			httpx.Fail(w, 500, "ledger_failed", "无法读取积分流水")
			return
		}
		items = append(items, raw)
	}
	if rows.Err() != nil {
		httpx.Fail(w, 500, "ledger_failed", "无法读取积分流水")
		return
	}
	httpx.JSON(w, 200, page.Result(items, total))
}
