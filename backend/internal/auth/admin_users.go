package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

func (s *Service) registerAdminUsers(mux *http.ServeMux, admin func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/admin/users", admin(http.HandlerFunc(s.adminCreateUser)))
	mux.Handle("PATCH /api/v1/admin/users/{id}/profile", admin(http.HandlerFunc(s.adminUserProfile)))
	mux.Handle("POST /api/v1/admin/users/{id}/send-verification", admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.adminSendUserMail(w, r, "verify")
	})))
	mux.Handle("POST /api/v1/admin/users/{id}/send-password-reset", admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.adminSendUserMail(w, r, "reset")
	})))
}

func adminReason(w http.ResponseWriter, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 1000 {
		httpx.Fail(w, 422, "reason_required", "请填写操作原因（最多 1000 个字符）")
		return "", false
	}
	return value, true
}

func adminProfileFields(w http.ResponseWriter, name, note string) (string, string, bool) {
	name, note = strings.TrimSpace(name), strings.TrimSpace(note)
	if name == "" || len([]rune(name)) > 80 {
		httpx.Fail(w, 422, "invalid_name", "昵称需为 1 至 80 个字符")
		return "", "", false
	}
	if len([]rune(note)) > 2000 {
		httpx.Fail(w, 422, "invalid_note", "内部备注最多 2000 个字符")
		return "", "", false
	}
	return name, note, true
}

func identityAudit(ctx context.Context, tx pgx.Tx, actor, target, action, reason, previousStatus, nextStatus string, previous, next any) error {
	before, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	after, err := json.Marshal(next)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO identity_admin_audit(id,actor_id,target_user_id,action,reason,previous_status,next_status,previous_values,next_values) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, core.ID("aud"), actor, target, action, reason, previousStatus, nextStatus, before, after)
	return err
}

func (s *Service) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		AdminNote   string `json:"admin_note"`
		Reason      string `json:"reason"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		httpx.Fail(w, 422, "invalid_email", err.Error())
		return
	}
	if err = validatePassword(in.Password); err != nil {
		httpx.Fail(w, 422, "invalid_password", err.Error())
		return
	}
	name, note, ok := adminProfileFields(w, in.DisplayName, in.AdminNote)
	if !ok {
		return
	}
	reason, ok := adminReason(w, in.Reason)
	if !ok {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.Fail(w, 500, "user_create_failed", "无法保存用户密码")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	u := core.User{ID: core.ID("usr"), Email: email, DisplayName: name, Role: "user", Status: "active", MustChangePassword: true}
	err = tx.QueryRow(r.Context(), `INSERT INTO users(id,email,password_hash,display_name,admin_note,must_change_password) VALUES($1,$2,$3,$4,$5,true) RETURNING created_at`, u.ID, email, string(hash), name, note).Scan(&u.CreatedAt)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO wallet_accounts(user_id,available,frozen) VALUES($1,0,0)`, u.ID)
	}
	if err == nil {
		_, err = s.queueToken(r.Context(), tx, u.ID, email, "verify")
	}
	if err == nil {
		err = identityAudit(r.Context(), tx, core.UserFrom(r.Context()).ID, u.ID, "create", reason, "", "active", map[string]any{}, map[string]any{"email": email, "display_name": name, "admin_note": note, "role": "user", "email_verified": false, "must_change_password": true})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			httpx.Fail(w, 409, "email_exists", "该邮箱已存在，请查看现有用户")
			return
		}
		httpx.Fail(w, 500, "user_create_failed", "暂时无法创建用户")
		return
	}
	httpx.JSON(w, 201, map[string]any{"user": u})
}

type adminMailDelivery struct {
	Status        string     `json:"status"`
	Purpose       string     `json:"purpose,omitempty"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	SentAt        *time.Time `json:"sent_at"`
	ErrorCode     string     `json:"error_code,omitempty"`
}

func (s *Service) adminMailStatus(ctx context.Context, userID string) (adminMailDelivery, error) {
	value := adminMailDelivery{Status: "none"}
	err := s.db.QueryRow(ctx, `SELECT purpose,attempts,next_attempt_at,sent_at,last_error_code FROM auth_mail_outbox WHERE user_id=$1 AND cancelled_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 1`, userID).Scan(&value.Purpose, &value.Attempts, &value.NextAttemptAt, &value.SentAt, &value.ErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if value.SentAt != nil {
		value.Status = "sent"
		value.NextAttemptAt = nil
		return value, nil
	}
	cfg := s.cfg
	if s.settings != nil {
		cfg, err = s.settings.RuntimeConfig(ctx)
		if err != nil {
			return value, err
		}
	}
	if cfg.SMTPHost == "" {
		value.Status = "unconfigured"
	} else if value.Attempts > 0 {
		value.Status = "failed"
	} else {
		value.Status = "queued"
	}
	return value, nil
}

func (s *Service) adminSendUserMail(w http.ResponseWriter, r *http.Request, purpose string) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	reason, ok := adminReason(w, in.Reason)
	if !ok {
		return
	}
	var count int
	err := s.db.QueryRow(r.Context(), `INSERT INTO auth_rate_limits(key,bucket,count) VALUES($1,$2,1) ON CONFLICT(key,bucket) DO UPDATE SET count=auth_rate_limits.count+1 RETURNING count`, "admin-mail:"+purpose+":"+r.PathValue("id"), time.Now().Unix()/60).Scan(&count)
	if err != nil {
		httpx.Fail(w, 503, "mail_unavailable", "暂时无法发送邮件")
		return
	}
	if count > 3 {
		w.Header().Set("Retry-After", "60")
		httpx.Fail(w, 429, "rate_limited", "该用户邮件操作频繁，请稍后重试")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	var email, status string
	var verified bool
	err = tx.QueryRow(r.Context(), `SELECT email,status,email_verified FROM users WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&email, &status, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, 404, "not_found", "用户不存在")
		return
	}
	if err == nil && (status != "active" || (purpose == "verify" && verified) || (purpose == "reset" && !verified)) {
		httpx.Fail(w, 422, "mail_not_applicable", "请核对用户状态和邮箱验证状态后再发送")
		return
	}
	if err == nil {
		_, err = s.queueToken(r.Context(), tx, r.PathValue("id"), email, purpose)
	}
	if err == nil {
		err = identityAudit(r.Context(), tx, core.UserFrom(r.Context()).ID, r.PathValue("id"), map[string]string{"verify": "send_verification", "reset": "send_password_reset"}[purpose], reason, status, status, map[string]any{}, map[string]any{"purpose": purpose})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.Fail(w, 500, "mail_failed", "暂时无法安排邮件发送")
		return
	}
	// Queue acceptance is distinct from SMTP delivery; detail exposes its progress.
	httpx.JSON(w, 202, map[string]string{"message": "邮件已加入发送队列，请刷新用户详情查看发送状态", "status": "queued"})
}

func (s *Service) adminUserProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       *string `json:"email"`
		DisplayName string  `json:"display_name"`
		AdminNote   string  `json:"admin_note"`
		Reason      string  `json:"reason"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	name, note, ok := adminProfileFields(w, in.DisplayName, in.AdminNote)
	if !ok {
		return
	}
	reason, ok := adminReason(w, in.Reason)
	if !ok {
		return
	}
	var requestedEmail string
	var err error
	if in.Email != nil {
		requestedEmail, err = normalizeEmail(*in.Email)
		if err != nil {
			httpx.Fail(w, 422, "invalid_email", err.Error())
			return
		}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	var email, oldName, oldNote, status, role string
	var verified bool
	err = tx.QueryRow(r.Context(), `SELECT email,display_name,admin_note,status,role,email_verified FROM users WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&email, &oldName, &oldNote, &status, &role, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, 404, "not_found", "用户不存在")
		return
	}
	if err != nil {
		httpx.Fail(w, 500, "user_update_failed", "无法读取用户资料")
		return
	}
	nextEmail := email
	if in.Email != nil {
		nextEmail = requestedEmail
	}
	changedEmail := nextEmail != email
	if changedEmail && role == "admin" {
		httpx.Fail(w, 422, "admin_email_protected", "管理员邮箱暂不支持在用户管理中变更")
		return
	}
	nextVerified := verified && !changedEmail
	_, err = tx.Exec(r.Context(), `UPDATE users SET email=$2,display_name=$3,admin_note=$4,email_verified=$5,updated_at=now() WHERE id=$1`, r.PathValue("id"), nextEmail, name, note, nextVerified)
	if err == nil && changedEmail {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, r.PathValue("id"))
	}
	if err == nil && changedEmail {
		_, err = tx.Exec(r.Context(), `UPDATE auth_tokens SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, r.PathValue("id"))
	}
	if err == nil && changedEmail {
		_, err = tx.Exec(r.Context(), `UPDATE auth_mail_outbox SET cancelled_at=now(),body='' WHERE user_id=$1 AND sent_at IS NULL AND cancelled_at IS NULL`, r.PathValue("id"))
	}
	if err == nil && changedEmail {
		_, err = s.queueToken(r.Context(), tx, r.PathValue("id"), nextEmail, "verify")
	}
	if err == nil {
		err = identityAudit(r.Context(), tx, core.UserFrom(r.Context()).ID, r.PathValue("id"), "update_profile", reason, status, status, map[string]any{"email": email, "display_name": oldName, "admin_note": oldNote, "email_verified": verified}, map[string]any{"email": nextEmail, "display_name": name, "admin_note": note, "email_verified": nextVerified})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			httpx.Fail(w, 409, "email_exists", "该邮箱已属于其他用户")
			return
		}
		httpx.Fail(w, 500, "user_update_failed", "暂时无法更新用户资料")
		return
	}
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

func (s *Service) pagedAdminUsers(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	q, ok := httpx.QueryFilters(w, r, map[string]int{"q": 200, "status": 20, "role": 10, "email_verified": 5, "created_from": 35, "created_to": 35, "sort": 20})
	if !ok {
		return
	}
	if (q["status"] != "" && q["status"] != "active" && q["status"] != "disabled" && q["status"] != "unverified") || (q["role"] != "" && q["role"] != "user" && q["role"] != "admin") || (q["email_verified"] != "" && q["email_verified"] != "true" && q["email_verified"] != "false") || (q["sort"] != "" && q["sort"] != "created_desc" && q["sort"] != "created_asc") {
		httpx.Fail(w, 422, "invalid_query", "搜索或筛选参数无效")
		return
	}
	var verified *bool
	if q["email_verified"] != "" {
		value := q["email_verified"] == "true"
		verified = &value
	}
	var from, to *time.Time
	for name, destination := range map[string]**time.Time{"created_from": &from, "created_to": &to} {
		if q[name] == "" {
			continue
		}
		value, err := time.Parse("2006-01-02", q[name])
		if err == nil && name == "created_to" {
			value = value.AddDate(0, 0, 1)
		} else if err != nil {
			// Timestamp bounds are inclusive from / exclusive to. Date-only API
			// callers retain their inclusive UTC calendar-day behavior.
			value, err = time.Parse(time.RFC3339Nano, q[name])
		}
		if err != nil {
			httpx.Fail(w, 422, "invalid_date", "注册日期或时间格式无效")
			return
		}
		*destination = &value
	}
	if from != nil && to != nil && !from.Before(*to) {
		httpx.Fail(w, 422, "invalid_date", "开始日期不能晚于结束日期")
		return
	}
	const where = ` WHERE ($1='' OR position(lower($1) IN lower(u.id||' '||u.email||' '||u.display_name))>0) AND ($2='' OR u.status=$2 OR ($2='unverified' AND NOT u.email_verified)) AND ($3='' OR u.role=$3) AND ($4::boolean IS NULL OR u.email_verified=$4) AND ($5::timestamptz IS NULL OR u.created_at>=$5) AND ($6::timestamptz IS NULL OR u.created_at<$6)`
	args := []any{q["q"], q["status"], q["role"], verified, from, to}
	var total int64
	if err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM users u`+where, args...).Scan(&total); err != nil {
		httpx.Fail(w, 500, "users_failed", "无法读取用户")
		return
	}
	order := " DESC"
	if q["sort"] == "created_asc" {
		order = " ASC"
	}
	rows, err := s.db.Query(r.Context(), `SELECT u.id,u.email,u.display_name,u.role,u.status,u.email_verified,u.created_at,u.must_change_password,COALESCE(w.available,0),COALESCE(w.frozen,0) FROM users u LEFT JOIN wallet_accounts w ON w.user_id=u.id`+where+` ORDER BY u.created_at`+order+`,u.id`+order+` LIMIT $7 OFFSET $8`, append(args, page.Limit, page.Offset)...)
	if err != nil {
		httpx.Fail(w, 500, "users_failed", "无法读取用户")
		return
	}
	defer rows.Close()
	type item struct {
		core.User
		Available int64 `json:"available"`
		Frozen    int64 `json:"frozen"`
	}
	items := []item{}
	for rows.Next() {
		var value item
		if err = rows.Scan(&value.ID, &value.Email, &value.DisplayName, &value.Role, &value.Status, &value.EmailVerified, &value.CreatedAt, &value.MustChangePassword, &value.Available, &value.Frozen); err != nil {
			httpx.Fail(w, 500, "users_failed", "无法读取用户")
			return
		}
		items = append(items, value)
	}
	if rows.Err() != nil {
		httpx.Fail(w, 500, "users_failed", "无法读取用户")
		return
	}
	httpx.JSON(w, 200, page.Result(items, total))
}
