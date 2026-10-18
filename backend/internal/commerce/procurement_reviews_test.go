package commerce

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/auth"
	"wsapi-video/internal/config"
	"wsapi-video/migrations"
)

type procurementFixture struct {
	db                      *pgxpool.Pool
	service                 *Service
	mux                     *http.ServeMux
	taskID, frozenTaskID    string
	adminID, secondAdminID  string
	adminToken, secondToken string
	userToken               string
}

func newProcurementFixture(t *testing.T) procurementFixture {
	t.Helper()
	db := integrationDB(t)
	adminID, secondAdminID, userID := seedUser(t, db, 100), seedUser(t, db, 100), seedUser(t, db, 100)
	ctx := context.Background()
	if _, err := db.Exec(ctx, "UPDATE users SET role='user' WHERE id=$1", userID); err != nil {
		t.Fatal(err)
	}
	projectID := seedProject(t, db, userID)
	s := New(db, config.Config{DevMode: true, Demo: true}, nil)
	create := func(key string) Task {
		t.Helper()
		quote := createQuote(t, s, userID, projectID, "demo-image")
		rec := doRequest(s, userID, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": key})
		if rec.Code != 201 {
			t.Fatal("could not seed procurement task", rec.Code)
		}
		return data[Task](t, rec)
	}
	old, frozen := create("old-procurement-task"), create("frozen-procurement-task")
	if err := s.settle(ctx, old.ID, "", "consumed", "completed", "ready", "", "Archived fixture"); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(ctx, `UPDATE generation_tasks SET created_at='2020-01-01',upstream_id='private-provider-id',credential_ref='private-original-credential',outputs='[{"url":"https://private-media.invalid?signature=secret"}]' WHERE id=$1`, old.ID)
	if err == nil {
		_, err = db.Exec(ctx, `UPDATE generation_tasks SET execution_status='submission_unknown',delivery_status='error',procurement_status='disputed',lease_token='private-lease',lease_until=now()+interval '1 hour',next_attempt_at=now()+interval '1 day',error_code='private-error',error_message='private-error-message' WHERE id=$1`, frozen.ID)
	}
	if err == nil {
		_, err = db.Exec(ctx, `INSERT INTO assets(id,user_id,project_id,task_id,kind,file_name,content_type,object_key,size_bytes,sha256) VALUES('procurement-asset',$1,$2,$3,'image','result.png','image/png','private-object-key',100,'private-sha');`, userID, projectID, old.ID)
	}
	if err == nil {
		_, err = db.Exec(ctx, `INSERT INTO payment_orders(id,user_id,out_trade_no,tier_id,tier_version,amount_fen,credits,method,credential_ref,merchant_pid,provider_origin,checkout_url,expires_at) VALUES('procurement-order',$1,'CV-PROCUREMENT','starter',1,1000,1000,'alipay','private-payment-ref','private-merchant','https://private-payment.invalid','https://private-checkout.invalid?signature=secret',now()+interval '1 hour')`, userID)
	}
	if err != nil {
		t.Fatal(err)
	}
	f := procurementFixture{db: db, service: s, taskID: old.ID, frozenTaskID: frozen.ID, adminID: adminID, secondAdminID: secondAdminID, adminToken: strings.Repeat("a", 64), secondToken: strings.Repeat("b", 64), userToken: strings.Repeat("c", 64)}
	for userID, token := range map[string]string{adminID: f.adminToken, secondAdminID: f.secondToken, userID: f.userToken} {
		digest := sha256.Sum256([]byte(token))
		if _, err := db.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,'review-csrf',now()+interval '1 hour')`, "session-"+userID, userID, hex.EncodeToString(digest[:])); err != nil {
			t.Fatal(err)
		}
	}
	// Successful requests with nil dependencies prove this path never invokes
	// gateway or media services, including for old and frozen disputed tasks.
	s.gateway, s.assets = nil, nil
	f.mux = http.NewServeMux()
	identity := auth.New(db, config.Config{})
	s.Register(f.mux, identity.Middleware, identity.Admin)
	return f
}

func (f procurementFixture) request(method, taskID, query, token string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, "/api/v1/admin/tasks/"+taskID+"/procurement-reviews"+query, bytes.NewReader(raw))
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "studio_session", Value: token})
	}
	if method == http.MethodPost {
		req.Header.Set("X-CSRF-Token", "review-csrf")
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func procurementBusinessFingerprint(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var fingerprint string
	err := db.QueryRow(context.Background(), `SELECT md5(jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM generation_tasks t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM task_attempts t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM quotes t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id) FROM wallet_accounts t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM wallet_ledger t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM assets t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY asset_id,owner_type,owner_id) FROM asset_references t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM payment_orders t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM projects t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY project_id) FROM canvases t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM shots t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM sessions t))::text)`).Scan(&fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

type procurementPage struct {
	Items   []ProcurementReview `json:"items"`
	Current ProcurementReview   `json:"current"`
	Total   int64               `json:"total"`
	Limit   int                 `json:"limit"`
	Offset  int                 `json:"offset"`
}

func reviewBody(revision int64, status string) map[string]any {
	return map[string]any{"expected_revision": revision, "status": status, "reason": "Provider record checked", "evidence": "Recorded evidence <script>plain text</script>"}
}

func assertProcurementResponse(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("procurement status=%d, want=%d: %s", rec.Code, status, rec.Body.String())
	}
	for _, value := range []string{"private-", "credential_ref", "object_key", "lease_token", "outputs", "merchant_pid", "checkout_url", "password_hash"} {
		if strings.Contains(rec.Body.String(), value) {
			t.Fatal("procurement response leaked unrelated private fields")
		}
	}
}

func TestPostgresProcurementReviewPermissionsValidationAndIsolation(t *testing.T) {
	f := newProcurementFixture(t)
	before := procurementBusinessFingerprint(t, f.db)
	empty := f.request(http.MethodGet, f.taskID, "", f.adminToken, nil)
	assertProcurementResponse(t, empty, 200)
	emptyPage := data[procurementPage](t, empty)
	if emptyPage.Current != (ProcurementReview{Status: "pending"}) || emptyPage.Total != 0 || emptyPage.Limit != 25 || emptyPage.Offset != 0 || emptyPage.Items == nil || len(emptyPage.Items) != 0 {
		t.Fatal("unreviewed historical task did not return an explicit empty default")
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		assertProcurementResponse(t, f.request(method, f.taskID, "", "", reviewBody(0, "matched")), 401)
		assertProcurementResponse(t, f.request(method, f.taskID, "", f.userToken, reviewBody(0, "matched")), 403)
		assertProcurementResponse(t, f.request(method, "unknown-task", "", f.adminToken, reviewBody(0, "matched")), 404)
	}
	badBodies := []map[string]any{
		{"status": "matched", "reason": "checked"},
		{"expected_revision": nil, "status": "matched", "reason": "checked"},
		{"expected_revision": -1, "status": "matched", "reason": "checked"},
		{"expected_revision": 0.5, "status": "matched", "reason": "checked"},
		{"expected_revision": "0", "status": "matched", "reason": "checked"},
		{"expected_revision": 0, "status": "invalid", "reason": "checked"},
		{"expected_revision": 0, "status": "matched", "reason": "  \n  "},
		{"expected_revision": 0, "status": "matched", "reason": "ab"},
		{"expected_revision": 0, "status": "matched", "reason": strings.Repeat("😀", 2001)},
		{"expected_revision": 0, "status": "matched", "reason": "checked", "evidence": strings.Repeat("😀", 2001)},
		{"expected_revision": 0, "status": "matched", "reason": "checked", "amount": 1},
		{"expected_revision": 0, "status": "matched", "reason": "checked", "supplier_refunded": true},
		{"expected_revision": 0, "status": "matched", "reason": "checked", "credential_ref": "forbidden"},
	}
	for _, body := range badBodies {
		assertProcurementResponse(t, f.request(http.MethodPost, f.taskID, "", f.adminToken, body), 422)
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?offset=-1", "?limit=2&limit=3", "?offset=10000001"} {
		assertProcurementResponse(t, f.request(http.MethodGet, f.taskID, query, f.adminToken, nil), 422)
	}
	raw, _ := json.Marshal(reviewBody(0, "matched"))
	noCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tasks/"+f.taskID+"/procurement-reviews", bytes.NewReader(raw))
	noCSRF.AddCookie(&http.Cookie{Name: "studio_session", Value: f.adminToken})
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, noCSRF)
	assertProcurementResponse(t, rec, 403)
	maximum := reviewBody(0, "matched")
	maximum["reason"], maximum["evidence"] = strings.Repeat("😀", 2000), strings.Repeat("界", 2000)
	accepted := f.request(http.MethodPost, f.taskID, "", f.adminToken, maximum)
	assertProcurementResponse(t, accepted, 201)
	review := data[ProcurementReview](t, accepted)
	if review.ID == "" || review.TaskID != f.taskID || review.AdminID != f.adminID || review.Revision != 1 || review.Status != "matched" || review.Reason != maximum["reason"] || review.Evidence != maximum["evidence"] {
		t.Fatal("Unicode boundary review did not preserve the public record")
	}
	if _, err := time.Parse(time.RFC3339Nano, review.CreatedAt); err != nil {
		t.Fatal("review timestamp is not RFC3339", err)
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(accepted.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "task_id", "revision", "admin_id", "status", "reason", "evidence", "created_at"} {
		if _, ok := envelope.Data[field]; !ok || len(envelope.Data) != 8 {
			t.Fatal("review response is not restricted to its eight public fields")
		}
	}
	assertProcurementResponse(t, f.request(http.MethodPost, f.taskID, "", f.adminToken, maximum), 409)
	minimum := map[string]any{"expected_revision": 0, "status": "disputed", "reason": " 汉 ", "evidence": "  "}
	frozenReview := f.request(http.MethodPost, f.frozenTaskID, "", f.secondToken, minimum)
	assertProcurementResponse(t, frozenReview, 201)
	if got := data[ProcurementReview](t, frozenReview); got.Reason != "汉" || got.Evidence != "" || got.AdminID != f.secondAdminID {
		t.Fatal("minimum byte reason or optional trimmed evidence was not preserved")
	}
	var reviews, audits int
	if err := f.db.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM task_procurement_reviews),(SELECT count(*) FROM admin_audit WHERE action='task.procurement_review')`).Scan(&reviews, &audits); err != nil || reviews != 2 || audits != 2 {
		t.Fatal("rejected or duplicate requests created a review or audit", reviews, audits, err)
	}
	if after := procurementBusinessFingerprint(t, f.db); after != before {
		t.Fatal("procurement review changed task states, leases, wallets, ledger, media or orders")
	}
}

func TestPostgresProcurementReviewHistoryAndCurrentSnapshot(t *testing.T) {
	f := newProcurementFixture(t)
	before := procurementBusinessFingerprint(t, f.db)
	created := []ProcurementReview{}
	statuses := []string{"matched", "disputed", "resolved", "pending"}
	for i := 0; i < 31; i++ {
		token := f.adminToken
		if i%2 != 0 {
			token = f.secondToken
		}
		rec := f.request(http.MethodPost, f.taskID, "", token, reviewBody(int64(i), statuses[i%4]))
		assertProcurementResponse(t, rec, 201)
		created = append(created, data[ProcurementReview](t, rec))
	}
	for _, page := range []struct {
		query        string
		limit, start int
		end          int
	}{{"", 25, 0, 25}, {"?offset=25", 25, 25, 31}, {"?limit=2&offset=3", 2, 3, 5}, {"?offset=31", 25, 31, 31}, {"?offset=100", 25, 100, 31}} {
		rec := f.request(http.MethodGet, f.taskID, page.query, f.adminToken, nil)
		assertProcurementResponse(t, rec, 200)
		got := data[procurementPage](t, rec)
		wantItems := []ProcurementReview{}
		for i := page.start; i < page.end; i++ {
			wantItems = append(wantItems, created[len(created)-1-i])
		}
		if got.Total != 31 || got.Limit != page.limit || got.Offset != page.start || got.Current != created[30] || !reflect.DeepEqual(got.Items, wantItems) {
			t.Fatal("history pagination changed current, dropped records or reordered revisions", page.query)
		}
	}
	var auditRaw []byte
	var reason string
	err := f.db.QueryRow(context.Background(), `SELECT reason,details FROM admin_audit WHERE action='task.procurement_review' AND details->'after'->>'revision'='31'`).Scan(&reason, &auditRaw)
	if err != nil || reason != created[30].Reason {
		t.Fatal("latest review audit missing its reason", err)
	}
	var auditDetails struct {
		Before ProcurementReview `json:"before"`
		After  ProcurementReview `json:"after"`
	}
	if err = json.Unmarshal(auditRaw, &auditDetails); err != nil || auditDetails.Before != created[29] || auditDetails.After != created[30] {
		t.Fatal("review audit did not preserve immutable before/after records", err)
	}
	if after := procurementBusinessFingerprint(t, f.db); after != before {
		t.Fatal("review history changed unrelated business facts")
	}
}

func TestPostgresProcurementReviewConcurrentRevisionAndAuditRollback(t *testing.T) {
	f := newProcurementFixture(t)
	before := procurementBusinessFingerprint(t, f.db)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for _, token := range []string{f.adminToken, f.secondToken} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			<-start
			results <- f.request(http.MethodPost, f.taskID, "", token, reviewBody(0, "matched"))
		}(token)
	}
	close(start)
	wg.Wait()
	close(results)
	codes := map[int]int{}
	for rec := range results {
		codes[rec.Code]++
		if rec.Code != 201 && rec.Code != 409 {
			t.Fatal("unexpected concurrent procurement review result", rec.Code)
		}
	}
	if codes[201] != 1 || codes[409] != 1 {
		t.Fatal("same revision allowed multiple concurrent reviews", codes)
	}
	assertCounts := func(want int) {
		t.Helper()
		var reviews, audits int
		err := f.db.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM task_procurement_reviews WHERE task_id=$1),(SELECT count(*) FROM admin_audit WHERE target_id=$1 AND action='task.procurement_review')`, f.taskID).Scan(&reviews, &audits)
		if err != nil || reviews != want || audits != want {
			t.Fatal("review/audit transaction counts diverged", reviews, audits, err)
		}
	}
	assertCounts(1)
	_, err := f.db.Exec(context.Background(), `CREATE FUNCTION reject_procurement_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.action='task.procurement_review' THEN RAISE EXCEPTION 'fixture audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_procurement_audit BEFORE INSERT ON admin_audit FOR EACH ROW EXECUTE FUNCTION reject_procurement_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	assertProcurementResponse(t, f.request(http.MethodPost, f.taskID, "", f.adminToken, reviewBody(1, "resolved")), 500)
	assertCounts(1)
	rolledBack := f.request(http.MethodGet, f.taskID, "", f.adminToken, nil)
	assertProcurementResponse(t, rolledBack, 200)
	if got := data[procurementPage](t, rolledBack); got.Total != 1 || got.Current.Revision != 1 || len(got.Items) != 1 {
		t.Fatal("failed audit left a partial review or changed current")
	}
	if _, err = f.db.Exec(context.Background(), `DROP TRIGGER reject_procurement_audit ON admin_audit; DROP FUNCTION reject_procurement_audit()`); err != nil {
		t.Fatal(err)
	}
	assertProcurementResponse(t, f.request(http.MethodPost, f.taskID, "", f.adminToken, reviewBody(1, "resolved")), 201)
	assertCounts(2)
	assertProcurementResponse(t, f.request(http.MethodPost, f.taskID, "", f.adminToken, reviewBody(1, "resolved")), 409)
	assertCounts(2)
	if after := procurementBusinessFingerprint(t, f.db); after != before {
		t.Fatal("concurrency or audit failure changed unrelated task, media or financial facts")
	}
}

func TestPostgresProcurementReviewMigrationUpgradeConstraintsAndFrozenChecksums(t *testing.T) {
	f := newProcurementFixture(t)
	ctx := context.Background()
	// Reapply only the new migration after historical tasks and financial facts
	// already exist, matching the upgrade case without touching another schema.
	if _, err := f.db.Exec(ctx, `DROP TABLE task_procurement_reviews; DELETE FROM schema_migrations WHERE name='005_task_procurement_reviews.sql'`); err != nil {
		t.Fatal(err)
	}
	before := procurementBusinessFingerprint(t, f.db)
	for i := 0; i < 2; i++ {
		if err := migrations.Apply(ctx, f.db); err != nil {
			t.Fatal("new migration could not upgrade or repeat", err)
		}
	}
	for name, checksum := range map[string]string{
		"003_runtime_settings.sql":      "2805fca8de6a7ce76365d55279f0a07c7ded492254ace468f31d1598f42a5ff5",
		"004_storage_location_lock.sql": "5d57b26a7e1196da198d9ebb0af7ae89d610aed3fd52e0b22950aad7459868cf",
	} {
		var actual string
		if err := f.db.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE name=$1", name).Scan(&actual); err != nil || actual != checksum {
			t.Fatal("a frozen prior migration changed", name, err)
		}
	}
	empty := f.request(http.MethodGet, f.taskID, "", f.adminToken, nil)
	assertProcurementResponse(t, empty, 200)
	if got := data[procurementPage](t, empty); got.Current.Revision != 0 || got.Current.Status != "pending" || got.Total != 0 {
		t.Fatal("migration did not preserve unreviewed historical task semantics")
	}
	assertProcurementResponse(t, f.request(http.MethodPost, f.taskID, "", f.adminToken, reviewBody(0, "matched")), 201)
	for i, invalid := range []struct {
		task, admin, status, reason, evidence, code string
		revision                                    int64
	}{
		{"missing-task", f.adminID, "matched", "checked", "", "23503", 1},
		{f.taskID, "missing-admin", "matched", "checked", "", "23503", 2},
		{f.taskID, f.adminID, "other", "checked", "", "23514", 2},
		{f.taskID, f.adminID, "matched", "checked", "", "23514", 0},
		{f.taskID, f.adminID, "matched", "ab", "", "23514", 2},
		{f.taskID, f.adminID, "matched", strings.Repeat("界", 2001), "", "23514", 2},
		{f.taskID, f.adminID, "matched", "checked", strings.Repeat("😀", 2001), "23514", 2},
		{f.taskID, f.adminID, "matched", "checked", "", "23505", 1},
	} {
		_, err := f.db.Exec(ctx, `INSERT INTO task_procurement_reviews(id,task_id,revision,admin_id,status,reason,evidence) VALUES($1,$2,$3,$4,$5,$6,$7)`, fmt.Sprintf("invalid-review-%d", i), invalid.task, invalid.revision, invalid.admin, invalid.status, invalid.reason, invalid.evidence)
		var postgresErr *pgconn.PgError
		if !errors.As(err, &postgresErr) || postgresErr.Code != invalid.code {
			t.Fatal("review migration failed its foreign key, revision, status, text or uniqueness constraint", i, err)
		}
	}
	if after := procurementBusinessFingerprint(t, f.db); after != before {
		t.Fatal("new migration or historical task review changed existing business facts")
	}
}

func TestPostgresProcurementDisputedTaskFilterPermissionsPaginationAndNoMutation(t *testing.T) {
	f := newProcurementFixture(t)
	ctx := context.Background()
	task, err := f.service.getTask(ctx, "", f.taskID, true)
	if err != nil {
		t.Fatal(err)
	}
	type taskPage struct {
		Items  []adminTaskRecord `json:"items"`
		Total  int64             `json:"total"`
		Limit  int               `json:"limit"`
		Offset int               `json:"offset"`
	}
	read := func(query, token string, status int) taskPage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tasks?"+query, nil)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "studio_session", Value: token})
		}
		rec := httptest.NewRecorder()
		f.mux.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("task list status=%d, want=%d: %s", rec.Code, status, rec.Body.String())
		}
		return data[taskPage](t, rec)
	}
	const filter = "status=procurement_review_disputed"
	read(filter, "", 401)
	read(filter, f.userToken, 403)
	if got := read(filter, f.adminToken, 200); got.Total != 0 {
		t.Fatal("worker procurement_status must not match manual review filter")
	}
	ids := []string{f.taskID}
	for i := 0; i < 7; i++ {
		quote := createQuote(t, f.service, task.UserID, task.ProjectID, "demo-image")
		rec := doRequest(f.service, task.UserID, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": fmt.Sprintf("disputed-list-%d", i)})
		if rec.Code != 201 {
			t.Fatal("could not seed consumed task", rec.Code)
		}
		created := data[Task](t, rec)
		if err = f.service.settle(ctx, created.ID, "", "consumed", "completed", "ready", "", "Archived list fixture"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	for _, id := range ids {
		assertProcurementResponse(t, f.request(http.MethodPost, id, "", f.adminToken, reviewBody(0, "disputed")), 201)
		if _, err = f.db.Exec(ctx, "UPDATE generation_tasks SET created_at='2020-01-01' WHERE id=$1", id); err != nil {
			t.Fatal(err)
		}
	}
	assertProcurementResponse(t, f.request(http.MethodPost, f.frozenTaskID, "", f.adminToken, reviewBody(0, "matched")), 201)
	fingerprint := func() string {
		t.Helper()
		var records string
		if err := f.db.QueryRow(ctx, `SELECT md5(jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY task_id,revision) FROM task_procurement_reviews t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM admin_audit t))::text)`).Scan(&records); err != nil {
			t.Fatal(err)
		}
		return procurementBusinessFingerprint(t, f.db) + records
	}
	before := fingerprint()
	seen := map[string]bool{}
	previousID := ""
	for offset := 0; offset < 8; offset += 3 {
		page := read(fmt.Sprintf("%s&limit=3&offset=%d", filter, offset), f.adminToken, 200)
		wantCount := 3
		if offset == 6 {
			wantCount = 2
		}
		if page.Total != 8 || page.Limit != 3 || page.Offset != offset || len(page.Items) != wantCount {
			t.Fatal("manual procurement dispute pagination lost records", page)
		}
		for _, item := range page.Items {
			if seen[item.ID] || (previousID != "" && previousID <= item.ID) || item.SettlementStatus != "consumed" {
				t.Fatal("manual dispute list duplicated, misordered or excluded consumed tasks", item.ID)
			}
			seen[item.ID], previousID = true, item.ID
		}
	}
	if len(seen) != 8 || seen[f.frozenTaskID] {
		t.Fatal("manual dispute list confused old worker status and latest manual review")
	}
	for _, extra := range []string{"&kind=video", "&user_id=" + f.adminID, "&project_id=missing-project", "&q=not-a-task"} {
		if got := read(filter+extra, f.adminToken, 200); got.Total != 0 || len(got.Items) != 0 {
			t.Fatal("manual procurement filter ignored another filter", extra)
		}
	}
	matched := read(filter+"&q="+task.ID+"&kind=image&user_id="+task.UserID+"&project_id="+task.ProjectID, f.adminToken, 200)
	if matched.Total != 1 || len(matched.Items) != 1 || matched.Items[0].ID != task.ID {
		t.Fatal("manual dispute filter did not compose with search and ownership fields")
	}
	for _, oldFilter := range []string{"disputed", "review_required"} {
		got := read("status="+oldFilter, f.adminToken, 200)
		if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != f.frozenTaskID {
			t.Fatal("manual review changed the old worker exception filter", oldFilter)
		}
	}
	read(filter+"&limit=0", f.adminToken, 422)
	if after := fingerprint(); after != before {
		t.Fatal("reading manual procurement dispute pages changed business, reviews or audit")
	}
	assertProcurementResponse(t, f.request(http.MethodPost, f.taskID, "", f.adminToken, reviewBody(1, "resolved")), 201)
	resolvedBefore := fingerprint()
	got := read(filter+"&limit=100", f.adminToken, 200)
	if got.Total != 7 || len(got.Items) != 7 {
		t.Fatal("resolved latest review still counted the historical disputed record")
	}
	for _, item := range got.Items {
		if item.ID == f.taskID {
			t.Fatal("resolved task remained in the manual dispute filter")
		}
	}
	if after := fingerprint(); after != resolvedBefore {
		t.Fatal("resolved-filter read changed business or review records")
	}
}
