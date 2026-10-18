// Package auth owns the platform identity database; gateway identities are never used.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/settings"
)

const cookieName = "studio_session"

type Service struct {
	db       *pgxpool.Pool
	cfg      config.Config
	settings *settings.Service
}

func (s *Service) SetRuntimeSettings(v *settings.Service) { s.settings = v }

func New(db *pgxpool.Pool, cfg config.Config) *Service { return &Service{db: db, cfg: cfg} }

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/register", s.register)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/verify", s.verify)
	mux.HandleFunc("POST /api/v1/auth/resend-verification", s.resendVerification)
	mux.HandleFunc("POST /api/v1/auth/forgot-password", s.forgotPassword)
	mux.HandleFunc("POST /api/v1/auth/reset-password", s.resetPassword)
	mux.Handle("GET /api/v1/me", s.Middleware(http.HandlerFunc(s.me)))
	mux.Handle("POST /api/v1/auth/logout", s.Middleware(http.HandlerFunc(s.logout)))
	mux.Handle("POST /api/v1/auth/change-password", s.Middleware(http.HandlerFunc(s.changePassword)))
	mux.Handle("GET /api/v1/auth/sessions", s.Middleware(http.HandlerFunc(s.sessions)))
	mux.Handle("DELETE /api/v1/auth/sessions/{id}", s.Middleware(http.HandlerFunc(s.revokeSession)))
	mux.Handle("GET /api/v1/admin/users", s.Admin(http.HandlerFunc(s.adminUsers)))
	mux.Handle("PATCH /api/v1/admin/users/{id}", s.Admin(http.HandlerFunc(s.adminUpdateUser)))
	s.registerAdminDetails(mux, s.Admin)
}

type sessionKey struct{}
type sessionIdentity struct{ ID, CSRF string }

// Middleware resolves every session against the database so account disable and
// session revocation take effect immediately across all devices.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil || len(cookie.Value) != 64 {
			httpx.Fail(w, 401, "unauthorized", "请先登录")
			return
		}
		var u core.User
		var identity sessionIdentity
		err = s.db.QueryRow(r.Context(), `SELECT u.id,u.email,u.display_name,u.role,u.status,u.email_verified,u.created_at,s.id,s.csrf_token
			FROM sessions s JOIN users u ON u.id=s.user_id
			WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND u.status='active' AND u.email_verified`, digest(cookie.Value)).Scan(
			&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.EmailVerified, &u.CreatedAt, &identity.ID, &identity.CSRF)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpx.Fail(w, 401, "unauthorized", "会话已失效，请重新登录")
			} else {
				httpx.Fail(w, 503, "auth_unavailable", "暂时无法验证会话")
			}
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(identity.CSRF)) != 1 {
				httpx.Fail(w, 403, "csrf_invalid", "会话安全令牌已失效，请刷新页面")
				return
			}
		}
		ctx := context.WithValue(core.WithUser(r.Context(), u), sessionKey{}, identity)
		_, _ = s.db.Exec(ctx, `UPDATE sessions SET last_seen_at=now() WHERE id=$1 AND last_seen_at<now()-interval '5 minutes'`, identity.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) Admin(next http.Handler) http.Handler {
	return s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if core.UserFrom(r.Context()).Role != "admin" {
			httpx.Fail(w, 403, "forbidden", "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	identity := r.Context().Value(sessionKey{}).(sessionIdentity)
	httpx.JSON(w, 200, map[string]any{"user": core.UserFrom(r.Context()), "csrf_token": identity.CSRF})
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > 254 {
		return "", errors.New("请输入有效邮箱")
	}
	return value, nil
}

func validatePassword(value string) error {
	if len([]rune(value)) < 10 || len([]byte(value)) > 72 {
		return errors.New("密码需至少 10 个字符，且不超过 72 字节")
	}
	return nil
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Only a configured direct peer may supply Nginx's overwritten X-Real-IP.
// X-Forwarded-For chains and malformed/multiple header values are never trusted.
func (s *Service) rateLimitIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	peer, err := netip.ParseAddr(ip)
	if err != nil || peer.Zone() != "" {
		return ip
	}
	for _, prefix := range s.cfg.TrustedProxyCIDRs {
		if !prefix.Contains(peer) && !prefix.Contains(peer.Unmap()) {
			continue
		}
		values := r.Header.Values("X-Real-IP")
		if len(values) == 1 {
			client, parseErr := netip.ParseAddr(values[0])
			if parseErr == nil && client.Zone() == "" {
				return client.Unmap().String()
			}
		}
		break
	}
	return peer.Unmap().String()
}

// A database counter also applies when multiple API instances share an account.
func (s *Service) limit(w http.ResponseWriter, r *http.Request, action string, max int) bool {
	ip := s.rateLimitIP(r)
	var count int
	err := s.db.QueryRow(r.Context(), `INSERT INTO auth_rate_limits(key,bucket,count) VALUES($1,$2,1)
		ON CONFLICT(key,bucket) DO UPDATE SET count=auth_rate_limits.count+1 RETURNING count`, action+":"+digest(ip), time.Now().Unix()/60).Scan(&count)
	if err != nil {
		httpx.Fail(w, 503, "auth_unavailable", "暂时无法验证请求")
		return false
	}
	if count > max {
		w.Header().Set("Retry-After", "60")
		httpx.Fail(w, 429, "rate_limited", "操作频繁，请稍后重试")
		return false
	}
	return true
}

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	if s.settings != nil {
		st, err := s.settings.Current(r.Context())
		if err != nil {
			httpx.Fail(w, 503, "settings_unavailable", "注册服务暂不可用")
			return
		}
		if !s.settings.Ready(st).Registration {
			httpx.Fail(w, 503, "registration_unavailable", "注册暂未开放，请稍后再试")
			return
		}
	}
	if !s.limit(w, r, "register", 5) {
		return
	}
	var in struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
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
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		in.DisplayName = strings.Split(email, "@")[0]
	}
	if len([]rune(in.DisplayName)) > 80 {
		httpx.Fail(w, 422, "invalid_name", "昵称过长")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.Fail(w, 500, "internal_error", "无法保存密码")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	id := core.ID("usr")
	result, err := tx.Exec(r.Context(), `INSERT INTO users(id,email,password_hash,display_name) VALUES($1,$2,$3,$4) ON CONFLICT(email) DO NOTHING`, id, email, string(hash), in.DisplayName)
	if err != nil {
		httpx.Fail(w, 500, "registration_failed", "暂时无法注册")
		return
	}
	var token string
	if result.RowsAffected() > 0 {
		_, err = tx.Exec(r.Context(), `INSERT INTO wallet_accounts(user_id,available,frozen) VALUES($1,0,0) ON CONFLICT(user_id) DO NOTHING`, id)
		if err == nil {
			token, err = s.queueToken(r.Context(), tx, id, email, "verify")
		}
		if err != nil {
			httpx.Fail(w, 500, "registration_failed", "暂时无法注册")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpx.Fail(w, 500, "registration_failed", "暂时无法注册")
		return
	}
	out := map[string]any{"message": "如邮箱可用，验证邮件已发送，请查收"}
	if s.cfg.DevMode && token != "" {
		out["dev_verification_url"] = s.tokenURL("verify", token)
	}
	httpx.JSON(w, 202, out)
}

func (s *Service) queueToken(ctx context.Context, tx pgx.Tx, userID, email, purpose string) (string, error) {
	token := randomToken()
	lifetime := 24 * time.Hour
	subject := "验证您的创作平台邮箱"
	path := "/verify-email"
	if purpose == "reset" {
		lifetime = 30 * time.Minute
		subject = "重置您的创作平台密码"
		path = "/reset-password"
	}
	// Superseding a request invalidates older unused tickets in the same transaction.
	if _, err := tx.Exec(ctx, `UPDATE auth_tokens SET used_at=now() WHERE user_id=$1 AND purpose=$2 AND used_at IS NULL`, userID, purpose); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO auth_tokens(token_hash,user_id,purpose,expires_at) VALUES($1,$2,$3,$4)`, digest(token), userID, purpose, time.Now().Add(lifetime)); err != nil {
		return "", err
	}
	link := strings.TrimRight(s.cfg.PublicURL, "/") + path + "?token=" + url.QueryEscape(token)
	body := "请打开以下链接完成操作：\r\n\r\n" + link + "\r\n\r\n如非本人操作，请忽略此邮件。"
	_, err := tx.Exec(ctx, `INSERT INTO auth_mail_outbox(id,user_id,to_email,subject,body) VALUES($1,$2,$3,$4,$5)`, core.ID("mail"), userID, email, subject, body)
	return token, err
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	if !s.limit(w, r, "login", 15) {
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		httpx.Fail(w, 401, "invalid_credentials", "邮箱或密码错误")
		return
	}
	var u core.User
	var hash string
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "auth_unavailable", "暂时无法登录")
		return
	}
	defer tx.Rollback(r.Context())
	// Password changes take an exclusive user lock. Holding a share lock until
	// session creation prevents an old-password login from escaping revocation.
	err = tx.QueryRow(r.Context(), `SELECT id,email,display_name,role,status,email_verified,created_at,password_hash FROM users WHERE email=$1 FOR SHARE`, email).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.EmailVerified, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		hash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	} else if err != nil {
		httpx.Fail(w, 503, "auth_unavailable", "暂时无法登录")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil || u.ID == "" {
		httpx.Fail(w, 401, "invalid_credentials", "邮箱或密码错误")
		return
	}
	if u.Status != "active" {
		httpx.Fail(w, 403, "account_disabled", "账号已停用")
		return
	}
	if !u.EmailVerified {
		httpx.Fail(w, 403, "email_unverified", "请先完成邮箱验证")
		return
	}
	token, csrf := randomToken(), randomToken()
	ttl := s.cfg.SessionTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	expiry := time.Now().Add(ttl)
	_, err = tx.Exec(r.Context(), `INSERT INTO sessions(id,user_id,token_hash,csrf_token,user_agent,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, core.ID("ses"), u.ID, digest(token), csrf, truncate(r.UserAgent(), 512), expiry)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.Fail(w, 503, "auth_unavailable", "暂时无法登录")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, Expires: expiry, MaxAge: int(ttl.Seconds())})
	httpx.JSON(w, 200, map[string]any{"user": u, "csrf_token": csrf})
}

func (s *Service) verify(w http.ResponseWriter, r *http.Request) {
	if !s.limit(w, r, "verify", 20) {
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	var userID string
	err = tx.QueryRow(r.Context(), `UPDATE auth_tokens SET used_at=now() WHERE token_hash=$1 AND purpose='verify' AND used_at IS NULL AND expires_at>now() RETURNING user_id`, digest(in.Token)).Scan(&userID)
	if err != nil {
		httpx.Fail(w, 422, "invalid_token", "验证链接无效或已过期")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET email_verified=true,updated_at=now() WHERE id=$1`, userID)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.Fail(w, 500, "verification_failed", "暂时无法验证邮箱")
		return
	}
	httpx.JSON(w, 200, map[string]string{"message": "邮箱验证成功，请登录"})
}

func (s *Service) resendVerification(w http.ResponseWriter, r *http.Request) {
	s.requestToken(w, r, "verify")
}
func (s *Service) forgotPassword(w http.ResponseWriter, r *http.Request) {
	s.requestToken(w, r, "reset")
}
func (s *Service) requestToken(w http.ResponseWriter, r *http.Request, purpose string) {
	if !s.limit(w, r, "mail-"+purpose, 3) {
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		httpx.Fail(w, 422, "invalid_email", err.Error())
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	var verified bool
	err = tx.QueryRow(r.Context(), `SELECT id,email_verified FROM users WHERE email=$1 AND status='active' FOR UPDATE`, email).Scan(&id, &verified)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	var token string
	if id != "" && ((purpose == "verify" && !verified) || (purpose == "reset" && verified)) {
		token, err = s.queueToken(r.Context(), tx, id, email, purpose)
		if err != nil {
			httpx.Fail(w, 500, "mail_failed", "暂时无法发送邮件")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpx.Fail(w, 500, "mail_failed", "暂时无法发送邮件")
		return
	}
	out := map[string]any{"message": "如账号符合条件，邮件已发送，请查收"}
	if s.cfg.DevMode && token != "" {
		key := "dev_verification_url"
		if purpose == "reset" {
			key = "dev_reset_url"
		}
		out[key] = s.tokenURL(purpose, token)
	}
	httpx.JSON(w, 202, out)
}

func (s *Service) resetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.limit(w, r, "reset", 10) {
		return
	}
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if err := validatePassword(in.Password); err != nil {
		httpx.Fail(w, 422, "invalid_password", err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.Fail(w, 500, "password_failed", "暂时无法修改密码")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `UPDATE auth_tokens SET used_at=now() WHERE token_hash=$1 AND purpose='reset' AND used_at IS NULL AND expires_at>now() RETURNING user_id`, digest(in.Token)).Scan(&id)
	if err != nil {
		httpx.Fail(w, 422, "invalid_token", "重置链接无效或已过期")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,updated_at=now() WHERE id=$1`, id, string(hash))
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.Fail(w, 500, "password_failed", "暂时无法修改密码")
		return
	}
	httpx.JSON(w, 200, map[string]string{"message": "密码已重置，请重新登录"})
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	identity := r.Context().Value(sessionKey{}).(sessionIdentity)
	if _, err := s.db.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE id=$1`, identity.ID); err != nil {
		httpx.Fail(w, 503, "logout_failed", "暂时无法退出")
		return
	}
	s.clearCookie(w)
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}
func (s *Service) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (s *Service) changePassword(w http.ResponseWriter, r *http.Request) {
	if !s.limit(w, r, "change-password", 10) {
		return
	}
	var in struct {
		CurrentPassword string `json:"current_password"`
		Password        string `json:"password"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if err := validatePassword(in.Password); err != nil {
		httpx.Fail(w, 422, "invalid_password", err.Error())
		return
	}
	u := core.UserFrom(r.Context())
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "database_unavailable", "请稍后重试")
		return
	}
	defer tx.Rollback(r.Context())
	var current string
	err = tx.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&current)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(current), []byte(in.CurrentPassword)) != nil {
		httpx.Fail(w, 422, "invalid_password", "当前密码错误")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,updated_at=now() WHERE id=$1`, u.ID, string(hash))
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, u.ID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE auth_tokens SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, u.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.Fail(w, 500, "password_failed", "暂时无法修改密码")
		return
	}
	s.clearCookie(w)
	httpx.JSON(w, 200, map[string]string{"message": "密码已修改，请重新登录"})
}

func (s *Service) sessions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT id,user_agent,created_at,last_seen_at,expires_at FROM sessions WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>now() ORDER BY created_at DESC`, core.UserFrom(r.Context()).ID)
	if err != nil {
		httpx.Fail(w, 500, "sessions_failed", "无法读取会话")
		return
	}
	defer rows.Close()
	type item struct {
		ID         string    `json:"id"`
		UserAgent  string    `json:"user_agent"`
		CreatedAt  time.Time `json:"created_at"`
		LastSeenAt time.Time `json:"last_seen_at"`
		ExpiresAt  time.Time `json:"expires_at"`
		Current    bool      `json:"current"`
	}
	items := []item{}
	identity := r.Context().Value(sessionKey{}).(sessionIdentity)
	for rows.Next() {
		var value item
		if err = rows.Scan(&value.ID, &value.UserAgent, &value.CreatedAt, &value.LastSeenAt, &value.ExpiresAt); err != nil {
			httpx.Fail(w, 500, "sessions_failed", "无法读取会话")
			return
		}
		value.Current = value.ID == identity.ID
		items = append(items, value)
	}
	if rows.Err() != nil {
		httpx.Fail(w, 500, "sessions_failed", "无法读取会话")
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}
func (s *Service) revokeSession(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, r.PathValue("id"), core.UserFrom(r.Context()).ID)
	if err != nil {
		httpx.Fail(w, 500, "session_failed", "无法撤销会话")
		return
	}
	if result.RowsAffected() == 0 {
		httpx.Fail(w, 404, "not_found", "会话不存在")
		return
	}
	if r.PathValue("id") == r.Context().Value(sessionKey{}).(sessionIdentity).ID {
		s.clearCookie(w)
	}
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}

// RunMailer delivers durable tickets. The database row lock prevents two API
// instances from sending the same pending message concurrently. Delivery is at
// least once, while ticket consumption remains exactly once in PostgreSQL.
func (s *Service) RunMailer(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.db.Exec(ctx, `DELETE FROM auth_rate_limits WHERE bucket<$1`, time.Now().Add(-24*time.Hour).Unix()/60)
			s.deliverMail(ctx)
		}
	}
}
func (s *Service) deliverMail(ctx context.Context) {
	cfg := s.cfg
	if s.settings != nil {
		var err error
		cfg, err = s.settings.RuntimeConfig(ctx)
		if err != nil {
			return
		}
	}
	if cfg.SMTPHost == "" {
		return
	}
	for i := 0; i < 10; i++ {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return
		}
		var id, to, subject, body string
		var attempts int
		err = tx.QueryRow(ctx, `SELECT id,to_email,subject,body,attempts FROM auth_mail_outbox WHERE sent_at IS NULL AND next_attempt_at<=now() ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &to, &subject, &body, &attempts)
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		addr := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
		var authentication smtp.Auth
		if cfg.SMTPUsername != "" {
			authentication = smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPHost)
		}
		message := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: =?UTF-8?B?%s?=\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", cfg.SMTPFrom, to, encodeSubject(subject), body))
		err = sendMailContext(ctx, addr, authentication, cfg.SMTPFrom, to, message)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE auth_mail_outbox SET sent_at=now(),body='' WHERE id=$1`, id)
		} else {
			delay := time.Duration(1<<min(attempts, 10)) * time.Minute
			_, err = tx.Exec(ctx, `UPDATE auth_mail_outbox SET attempts=attempts+1,next_attempt_at=$2 WHERE id=$1`, id, time.Now().Add(delay))
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
	}
}
