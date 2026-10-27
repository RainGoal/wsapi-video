package server

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/commerce"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/storage"
)

func adminUsersFixture(t *testing.T) (*pgxpool.Pool, *studioClient, string) {
	t.Helper()
	db := serverIntegrationDB(t)
	host := httptest.NewUnstartedServer(nil)
	origin := "http://" + host.Listener.Addr().String()
	cfg := config.Config{Env: "test", DevMode: true, Demo: true, PublicURL: origin, AppSecret: strings.Repeat("a", 40), StorageDriver: "local", StorageDir: t.TempDir(), WebDir: t.TempDir(), SessionTTL: time.Hour}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg, db, blobs)
	host.Config.Handler = app.HTTP.Handler
	host.Start()
	t.Cleanup(host.Close)
	if err = app.Auth.BootstrapAdmin(context.Background(), "phase1-admin@example.test", "Phase1-admin-password"); err != nil {
		t.Fatal(err)
	}
	admin := newStudioClient(t, origin)
	admin.login("phase1-admin@example.test", "Phase1-admin-password")
	return db, admin, origin
}

// Read only the isolated test outbox; production APIs never return mail tickets.
func adminUsersMailToken(t *testing.T, db *pgxpool.Pool, userID, purpose string) string {
	t.Helper()
	var body string
	if err := db.QueryRow(context.Background(), `SELECT body FROM auth_mail_outbox WHERE user_id=$1 AND purpose=$2 AND cancelled_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 1`, userID, purpose).Scan(&body); err != nil {
		t.Fatal(err)
	}
	for _, word := range strings.Fields(body) {
		link, err := url.Parse(word)
		if err == nil && link.Query().Get("token") != "" {
			return link.Query().Get("token")
		}
	}
	t.Fatal("missing isolated outbox ticket")
	return ""
}

type phase1UserDetail struct {
	User      core.User       `json:"user"`
	Wallet    commerce.Wallet `json:"wallet"`
	AdminNote string          `json:"admin_note"`
	Mail      struct {
		Status  string `json:"status"`
		Purpose string `json:"purpose"`
	} `json:"mail_delivery"`
}

func createPhase1User(t *testing.T, admin *studioClient, email string) core.User {
	t.Helper()
	raw := admin.json("POST", "/api/v1/admin/users", map[string]string{"email": email, "display_name": "运营开户测试", "password": "Initial-password-2026", "admin_note": "仅用于隔离集成测试", "reason": "一期开户验收"}, 201)
	if strings.Contains(string(raw), "Initial-password-2026") || strings.Contains(string(raw), "token") {
		t.Fatal("creation returned private credentials")
	}
	return serverData[struct {
		User core.User `json:"user"`
	}](t, raw).User
}

func TestAdminUserCreationActivationAndFirstPasswordChange(t *testing.T) {
	db, admin, origin := adminUsersFixture(t)
	u := createPhase1User(t, admin, "phase1-user@example.test")
	if u.Role != "user" || u.EmailVerified || !u.MustChangePassword {
		t.Fatalf("incorrect new user state: %+v", u)
	}
	detail := serverData[phase1UserDetail](t, admin.json("GET", "/api/v1/admin/users/"+u.ID, nil, 200))
	if detail.Wallet.Available != 0 || detail.Wallet.Frozen != 0 || detail.AdminNote != "仅用于隔离集成测试" || detail.Mail.Status != "unconfigured" {
		t.Fatalf("incomplete new user: %+v", detail)
	}
	admin.json("POST", "/api/v1/admin/users", map[string]string{"email": "PHASE1-USER@example.test", "display_name": "重复开户", "password": "Initial-password-2026", "reason": "重复提交"}, 409)
	var users, wallets, mails int
	if err := db.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM wallet_accounts),(SELECT count(*) FROM auth_mail_outbox WHERE user_id=$1)`, u.ID).Scan(&users, &wallets, &mails); err != nil {
		t.Fatal(err)
	}
	if users != 2 || wallets != 2 || mails != 1 {
		t.Fatalf("duplicate creation left partial records: %d/%d/%d", users, wallets, mails)
	}
	creator := newStudioClient(t, origin)
	creator.json("POST", "/api/v1/auth/login", map[string]string{"email": u.Email, "password": "Initial-password-2026"}, 403)
	creator.json("POST", "/api/v1/auth/verify", map[string]string{"token": adminUsersMailToken(t, db, u.ID, "verify")}, 200)
	creator.login(u.Email, "Initial-password-2026")
	if !creator.user.MustChangePassword {
		t.Fatal("login omitted required password change")
	}
	creator.json("GET", "/api/v1/me", nil, 200)
	creator.json("GET", "/api/v1/wallet", nil, 403)
	creator.json("POST", "/api/v1/auth/change-password", map[string]string{"current_password": "Initial-password-2026", "password": "Initial-password-2026"}, 422)
	creator.json("POST", "/api/v1/auth/change-password", map[string]string{"current_password": "Initial-password-2026", "password": "Personal-password-2026"}, 200)
	creator.json("GET", "/api/v1/me", nil, 401)
	creator.login(u.Email, "Personal-password-2026")
	if creator.user.MustChangePassword {
		t.Fatal("password change flag was not cleared")
	}
	creator.json("GET", "/api/v1/wallet", nil, 200)
	creator.json("POST", "/api/v1/admin/users", map[string]string{"email": "not-created@example.test", "password": "Initial-password-2026", "display_name": "权限测试", "reason": "普通用户"}, 403)
	admin.json("POST", "/api/v1/admin/users/"+u.ID+"/send-verification", map[string]string{"reason": "已验证无需补发"}, 422)
	admin.json("POST", "/api/v1/admin/users/"+u.ID+"/send-password-reset", map[string]string{"reason": "用户申请找回"}, 202)
	creator.json("POST", "/api/v1/auth/reset-password", map[string]string{"token": adminUsersMailToken(t, db, u.ID, "reset"), "password": "Recovered-password-2026"}, 200)
	creator.login(u.Email, "Recovered-password-2026")
	if creator.user.MustChangePassword {
		t.Fatal("password recovery left initial credential flag")
	}
}

func TestAdminUserProfileEmailRevocationAndExactAudit(t *testing.T) {
	db, admin, origin := adminUsersFixture(t)
	u := createPhase1User(t, admin, "profile-original@example.test")
	oldVerify := adminUsersMailToken(t, db, u.ID, "verify")
	creator := newStudioClient(t, origin)
	creator.json("POST", "/api/v1/auth/verify", map[string]string{"token": oldVerify}, 200)
	creator.login(u.Email, "Initial-password-2026")
	creator.json("POST", "/api/v1/auth/change-password", map[string]string{"current_password": "Initial-password-2026", "password": "Profile-personal-2026"}, 200)
	creator.login(u.Email, "Profile-personal-2026")
	admin.json("POST", "/api/v1/admin/users/"+u.ID+"/send-password-reset", map[string]string{"reason": "变更前票据"}, 202)
	admin.json("PATCH", "/api/v1/admin/users/"+u.ID+"/profile", map[string]string{"display_name": "新昵称", "admin_note": "内部客服备注", "email": "profile-new@example.test", "reason": "用户更换邮箱"}, 200)
	creator.json("GET", "/api/v1/me", nil, 401)
	var oldTickets, oldMails int
	if err := db.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM auth_tokens WHERE user_id=$1 AND purpose='reset' AND used_at IS NULL),(SELECT count(*) FROM auth_mail_outbox WHERE user_id=$1 AND to_email=$2 AND sent_at IS NULL AND cancelled_at IS NULL)`, u.ID, u.Email).Scan(&oldTickets, &oldMails); err != nil {
		t.Fatal(err)
	}
	if oldTickets != 0 || oldMails != 0 {
		t.Fatal("email change did not retire previous reset requests and queued messages")
	}
	detail := serverData[phase1UserDetail](t, admin.json("GET", "/api/v1/admin/users/"+u.ID, nil, 200))
	if detail.User.EmailVerified || detail.User.Email != "profile-new@example.test" || detail.AdminNote != "内部客服备注" {
		t.Fatalf("email revalidation state incorrect: %+v", detail)
	}
	creator.json("POST", "/api/v1/auth/login", map[string]string{"email": "profile-new@example.test", "password": "Profile-personal-2026"}, 403)
	creator.json("POST", "/api/v1/auth/verify", map[string]string{"token": adminUsersMailToken(t, db, u.ID, "verify")}, 200)
	creator.login("profile-new@example.test", "Profile-personal-2026")
	private := creator.json("GET", "/api/v1/me", nil, 200)
	if strings.Contains(string(private), "admin_note") || strings.Contains(string(private), "内部客服备注") {
		t.Fatal("internal note exposed to account owner")
	}
	admin.json("POST", "/api/v1/admin/wallet/adjust", map[string]any{"user_id": u.ID, "amount": 25, "request_key": "phase1-profile-credit", "reason": "审计目标测试"}, 200)
	audit := serverData[struct {
		Items []struct {
			Action  string `json:"action"`
			Details struct {
				Previous map[string]any `json:"previous_values"`
				Next     map[string]any `json:"next_values"`
			} `json:"details"`
		} `json:"items"`
		Total int `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/audit?target_user_id="+u.ID+"&action=user.update_profile", nil, 200))
	if audit.Total != 1 || audit.Items[0].Details.Previous["email"] != "profile-original@example.test" || audit.Items[0].Details.Next["email"] != "profile-new@example.test" {
		t.Fatalf("profile audit incomplete: %+v", audit)
	}
	none := serverData[struct {
		Total int `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/audit?target_user_id=unrelated-user", nil, 200))
	if none.Total != 0 {
		t.Fatal("audit target matched unrelated events")
	}
	admin.json("GET", "/api/v1/admin/audit?target_user_id=a&target_user_id=b", nil, 422)
	admin.json("PATCH", "/api/v1/admin/users/"+u.ID+"/profile", map[string]string{"display_name": "不可保存", "admin_note": "", "email": "phase1-admin@example.test", "reason": "重复邮箱"}, 409)
	detail = serverData[phase1UserDetail](t, admin.json("GET", "/api/v1/admin/users/"+u.ID, nil, 200))
	if detail.User.DisplayName != "新昵称" || !detail.User.EmailVerified {
		t.Fatal("conflicting email update was not rolled back")
	}
}

func TestAdminUsersIndependentFiltersAndValidation(t *testing.T) {
	db, admin, origin := adminUsersFixture(t)
	u := createPhase1User(t, admin, "filter-first@example.test")
	v := createPhase1User(t, admin, "filter-second@example.test")
	if _, err := db.Exec(context.Background(), `UPDATE users SET created_at='2026-10-20T12:00:00Z',email_verified=true WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `UPDATE users SET created_at='2026-10-21T12:00:00Z',status='disabled' WHERE id=$1`, v.ID); err != nil {
		t.Fatal(err)
	}
	type page struct {
		Items []struct {
			core.User
			Available int64 `json:"available"`
		} `json:"items"`
		Total int `json:"total"`
	}
	for _, query := range []string{
		"role=user&email_verified=true&status=active",
		"q=FILTER-FIRST&created_from=2026-10-20&created_to=2026-10-20",
	} {
		result := serverData[page](t, admin.json("GET", "/api/v1/admin/users?"+query, nil, 200))
		if result.Total != 1 || result.Items[0].ID != u.ID || result.Items[0].Available != 0 {
			t.Fatalf("independent filters failed: %s %+v", query, result)
		}
	}
	result := serverData[page](t, admin.json("GET", "/api/v1/admin/users?role=user&email_verified=false&status=disabled", nil, 200))
	if result.Total != 1 || result.Items[0].ID != v.ID {
		t.Fatal("verification/state filters were conflated")
	}
	result = serverData[page](t, admin.json("GET", "/api/v1/admin/users?role=user&sort=created_asc&limit=1&offset=1", nil, 200))
	if result.Total != 2 || len(result.Items) != 1 || result.Items[0].ID != v.ID {
		t.Fatal("user sorting and pagination lost target")
	}
	if _, err := db.Exec(context.Background(), `UPDATE users SET created_at='2026-10-20T17:00:00Z' WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	result = serverData[page](t, admin.json("GET", "/api/v1/admin/users?created_from=2026-10-20T16:00:00.000Z&created_to=2026-10-21T16:00:00.000Z&role=user", nil, 200))
	if result.Total != 2 {
		t.Fatal("local calendar timestamp bounds lost a matching user")
	}
	for _, query := range []string{"created_from=2026-02-30", "created_from=2026-10-22&created_to=2026-10-20", "email_verified=maybe", "sort=unsafe", "role=user&role=admin"} {
		admin.json("GET", "/api/v1/admin/users?"+query, nil, 422)
	}
	admin.json("POST", "/api/v1/admin/users", map[string]string{"email": "invalid-password@example.test", "display_name": "无效密码", "password": "short", "reason": "校验"}, 422)
	admin.json("POST", "/api/v1/admin/users", map[string]string{"email": "invalid-reason@example.test", "display_name": "缺少原因", "password": "Initial-password-2026"}, 422)
	admin.json("POST", "/api/v1/admin/users/"+v.ID+"/send-verification", map[string]string{"reason": "停用不可发送"}, 422)
	stranger := newStudioClient(t, origin)
	stranger.json("POST", "/api/v1/admin/users", map[string]string{}, 401)
	admin.json("POST", "/api/v1/admin/users/"+u.ID+"/send-password-reset", map[string]string{"reason": ""}, 422)
	for i := 0; i < 3; i++ {
		admin.json("POST", "/api/v1/admin/users/"+u.ID+"/send-password-reset", map[string]string{"reason": "限流验收"}, 202)
	}
	admin.json("POST", "/api/v1/admin/users/"+u.ID+"/send-password-reset", map[string]string{"reason": "限流验收"}, 429)
	var pending int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM auth_mail_outbox WHERE user_id=$1 AND purpose='reset' AND sent_at IS NULL AND cancelled_at IS NULL`, u.ID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatal("repeated mail requests left obsolete live messages")
	}
}
