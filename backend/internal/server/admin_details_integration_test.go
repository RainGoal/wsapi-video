package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wsapi-video/internal/commerce"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/storage"
)

func TestAdminOperationsPaginationDetailsAndSessionRevocation(t *testing.T) {
	db := serverIntegrationDB(t)
	host := httptest.NewUnstartedServer(nil)
	origin := "http://" + host.Listener.Addr().String()
	cfg := config.Config{Env: "test", DevMode: true, Demo: true, PublicURL: origin, AppSecret: strings.Repeat("a", 40), StorageDriver: "local", StorageDir: t.TempDir(), WebDir: t.TempDir(), MaxUploadBytes: 8 << 20, MaxMediaBytes: 8 << 20, SessionTTL: time.Hour}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg, db, blobs)
	host.Config.Handler = app.HTTP.Handler
	host.Start()
	defer host.Close()
	if err = app.Auth.BootstrapAdmin(context.Background(), "admin@example.test", "admin-password-2026"); err != nil {
		t.Fatal(err)
	}
	admin := newStudioClient(t, origin)
	admin.login("admin@example.test", "admin-password-2026")
	alpha, beta := newStudioClient(t, origin), newStudioClient(t, origin)
	alpha.registerAndLogin("alpha@example.test", "alpha-password-2026")
	beta.registerAndLogin("beta@example.test", "beta-password-2026")
	type userPage struct {
		Items         []core.User `json:"items"`
		Total         int64       `json:"total"`
		Limit, Offset int
	}
	first := serverData[userPage](t, admin.json("GET", "/api/v1/admin/users?limit=1&offset=0", nil, 200))
	second := serverData[userPage](t, admin.json("GET", "/api/v1/admin/users?limit=1&offset=1", nil, 200))
	if first.Total != 3 || len(first.Items) != 1 || len(second.Items) != 1 || first.Items[0].ID == second.Items[0].ID || second.Offset != 1 {
		t.Fatalf("pagination lost records: %+v %+v", first, second)
	}
	filtered := serverData[userPage](t, admin.json("GET", "/api/v1/admin/users?q=ALPHA&status=active", nil, 200))
	if filtered.Total != 1 || filtered.Items[0].ID != alpha.user.ID {
		t.Fatalf("user filter: %+v", filtered)
	}
	if _, err = db.Exec(context.Background(), "UPDATE users SET email_verified=false WHERE id=$1", beta.user.ID); err != nil {
		t.Fatal(err)
	}
	unverified := serverData[userPage](t, admin.json("GET", "/api/v1/admin/users?status=unverified", nil, 200))
	if unverified.Total != 1 || unverified.Items[0].ID != beta.user.ID {
		t.Fatalf("unverified user filter: %+v", unverified)
	}
	if _, err = db.Exec(context.Background(), "UPDATE users SET email_verified=true WHERE id=$1", beta.user.ID); err != nil {
		t.Fatal(err)
	}
	admin.json("GET", "/api/v1/admin/users?limit=0", nil, 422)
	admin.json("GET", "/api/v1/admin/users?limit=1&limit=2", nil, 422)
	admin.json("GET", "/api/v1/admin/users?offset=-1", nil, 422)
	admin.json("GET", "/api/v1/admin/users/missing", nil, 404)
	admin.json("POST", "/api/v1/admin/wallet/adjust", map[string]any{"user_id": alpha.user.ID, "amount": 100, "request_key": "admin-detail-fixture", "reason": "运营详情账本验收"}, 200)
	detail := serverData[struct {
		User           core.User       `json:"user"`
		Wallet         commerce.Wallet `json:"wallet"`
		ActiveSessions int             `json:"active_sessions"`
	}](t, admin.json("GET", "/api/v1/admin/users/"+alpha.user.ID, nil, 200))
	if detail.User.ID != alpha.user.ID || detail.Wallet.Available != 100 || detail.ActiveSessions != 1 {
		t.Fatalf("user details: %+v", detail)
	}
	ledger := serverData[struct {
		Items []commerce.Ledger `json:"items"`
		Total int               `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/users/"+alpha.user.ID+"/ledger?limit=1", nil, 200))
	if ledger.Total != 1 || ledger.Items[0].AvailableAfter != 100 {
		t.Fatalf("ledger: %+v", ledger)
	}
	project := serverData[projects.Project](t, alpha.json("POST", "/api/v1/projects", map[string]string{"name": "运营任务验收"}, 201))
	quote := serverData[commerce.Quote](t, alpha.json("POST", "/api/v1/quotes", map[string]any{"project_id": project.ID, "product_id": "demo-image", "prompt": "admin operations fixture", "aspect_ratio": "1:1", "size": "1024x1024", "quality": "standard"}, 201))
	task := serverData[commerce.Task](t, alpha.json("POST", "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": "admin-task-detail-fixture"}, 201))
	tasks := serverData[struct {
		Items []struct{ ID, UserID string } `json:"items"`
		Total int                           `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/tasks?q=ALPHA&status=queued&kind=image&user_id="+alpha.user.ID+"&limit=1", nil, 200))
	if tasks.Total != 1 || tasks.Items[0].ID != task.ID {
		t.Fatalf("task filter: %+v", tasks)
	}
	taskRaw := admin.json("GET", "/api/v1/admin/tasks/"+task.ID, nil, 200)
	var taskDetails map[string]any
	taskDetails = serverData[map[string]any](t, taskRaw)
	if taskDetails["user_id"] != alpha.user.ID || taskDetails["attempt_history"] == nil || strings.Contains(string(taskRaw), "lease_token") {
		t.Fatalf("unsafe or incomplete task detail: %s", taskRaw)
	}
	if _, err = db.Exec(context.Background(), "UPDATE generation_tasks SET execution_status='submission_unknown' WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	reviewTasks := serverData[struct {
		Total int `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/tasks?status=review_required", nil, 200))
	if reviewTasks.Total != 1 {
		t.Fatalf("review task filter: %+v", reviewTasks)
	}
	for _, status := range []string{"frozen_overdue", "lease_expired", "archive_failed"} {
		fresh := serverData[struct{ Total int }](t, admin.json("GET", "/api/v1/admin/tasks?status="+status, nil, 200))
		if fresh.Total != 0 {
			t.Fatalf("fresh task incorrectly matched %s", status)
		}
	}
	if _, err = db.Exec(context.Background(), `UPDATE generation_tasks SET created_at=now()-interval '25 hours',lease_until=now()-interval '1 minute',execution_status='completed',delivery_status='archive_retry' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"frozen_overdue", "lease_expired", "archive_failed"} {
		matched := serverData[struct {
			Total int
			Items []commerce.Task
		}](t, admin.json("GET", "/api/v1/admin/tasks?status="+status+"&q="+task.ID+"&kind=image&limit=1", nil, 200))
		if matched.Total != 1 || len(matched.Items) != 1 || matched.Items[0].ID != task.ID {
			t.Fatalf("operational filter %s lost matching task", status)
		}
	}
	if _, err = db.Exec(context.Background(), "UPDATE generation_tasks SET settlement_status='released' WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"frozen_overdue", "lease_expired", "archive_failed"} {
		released := serverData[struct{ Total int }](t, admin.json("GET", "/api/v1/admin/tasks?status="+status, nil, 200))
		if released.Total != 0 {
			t.Fatalf("released task incorrectly matched %s", status)
		}
	}
	if _, err = db.Exec(context.Background(), `UPDATE generation_tasks SET created_at=$2,lease_until=NULL,execution_status='submission_unknown',delivery_status='pending',settlement_status='frozen' WHERE id=$1`, task.ID, task.CreatedAt); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(context.Background(), `INSERT INTO payment_orders(id,user_id,out_trade_no,tier_id,tier_version,amount_fen,credits,method,credential_ref,merchant_pid,provider_origin,checkout_url,expires_at) VALUES('qa-order',$1,'qa-reference','starter',1,1000,1000,'alipay','qa-ref','qa-merchant','https://pay.example.test','https://pay.example.test/checkout',now()+interval '1 hour')`, alpha.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	orders := serverData[struct {
		Items []commerce.Order `json:"items"`
		Total int              `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/payment/orders?q=alpha&status=pending&user_id="+alpha.user.ID, nil, 200))
	if orders.Total != 1 || orders.Items[0].ID != "qa-order" {
		t.Fatalf("order filters: %+v", orders)
	}
	if _, err = db.Exec(context.Background(), "UPDATE payment_orders SET expires_at=now()-interval '1 hour' WHERE id='qa-order'"); err != nil {
		t.Fatal(err)
	}
	reviewOrders := serverData[struct {
		Total int `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/payment/orders?status=review_required", nil, 200))
	if reviewOrders.Total != 1 {
		t.Fatalf("review payment filter: %+v", reviewOrders)
	}
	order := serverData[map[string]any](t, admin.json("GET", "/api/v1/admin/payment/orders/qa-order", nil, 200))
	if order["user_id"] != alpha.user.ID || order["merchant_pid"] != "qa-merchant" {
		t.Fatalf("order detail: %+v", order)
	}
	for _, path := range []string{"/admin/users", "/admin/users/" + alpha.user.ID, "/admin/users/" + alpha.user.ID + "/ledger", "/admin/tasks/" + task.ID, "/admin/payment/orders/qa-order", "/admin/audit"} {
		beta.json("GET", "/api/v1"+path, nil, 403)
	}
	admin.json("POST", "/api/v1/admin/users/"+alpha.user.ID+"/revoke-sessions", map[string]string{"reason": ""}, 422)
	revoked := serverData[struct {
		Revoked int `json:"revoked"`
	}](t, admin.json("POST", "/api/v1/admin/users/"+alpha.user.ID+"/revoke-sessions", map[string]string{"reason": "主动撤销可疑会话"}, 200))
	if revoked.Revoked != 1 {
		t.Fatalf("session revocation: %+v", revoked)
	}
	alpha.json("GET", "/api/v1/me", nil, 401)
	admin.json("PATCH", "/api/v1/admin/users/"+beta.user.ID, map[string]string{"status": "disabled", "reason": "停用用户审计验收"}, 200)
	audit := serverData[struct {
		Items []map[string]json.RawMessage `json:"items"`
		Total int                          `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/audit?action=user.set_status", nil, 200))
	if audit.Total != 1 {
		t.Fatalf("identity audit missing: %+v", audit)
	}
	revocationAudit := serverData[struct {
		Total int `json:"total"`
	}](t, admin.json("GET", "/api/v1/admin/audit?q=可疑会话&action=user.revoke_sessions", nil, 200))
	if revocationAudit.Total != 1 {
		t.Fatalf("session audit missing: %+v", revocationAudit)
	}
}
