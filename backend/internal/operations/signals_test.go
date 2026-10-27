package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/auth"
	"wsapi-video/internal/config"
)

var signalCodes = []string{"task_submission_unknown", "task_frozen_overdue", "task_archive_failed", "task_lease_expired", "payment_review", "mail_pending", "mail_retry", "procurement_review_disputed"}

func TestPostgresSignalsEmptyAndFallbackThreshold(t *testing.T) {
	db := operationsTestDB(t)
	for _, threshold := range []time.Duration{0, -time.Hour} {
		result, err := Snapshot(context.Background(), db, threshold)
		if err != nil {
			t.Fatal(err)
		}
		if result.GeneratedAt.IsZero() || result.ReviewAfterSeconds != 86400 || result.SampleLimit != 5 || len(result.Signals) != len(signalCodes) {
			t.Fatal("invalid empty signal metadata", result)
		}
		for i, signal := range result.Signals {
			if signal.Code != signalCodes[i] || signal.Count != 0 || signal.OldestAt != nil || signal.Samples == nil || len(signal.Samples) != 0 {
				t.Fatal("empty signal must include zero count, null oldest time and empty samples", signal)
			}
		}
	}
}

func seedSignalRows(t *testing.T, db *pgxpool.Pool) time.Time {
	t.Helper()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	_, err := db.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,role,email_verified) VALUES
 ('operator','operator@example.test','unused','Operator','admin',true),('creator','secret-mail@example.test','unused','Creator','user',true);
 INSERT INTO wallet_accounts(user_id,available,frozen) VALUES('creator',500,120);
 INSERT INTO projects(id,user_id,name) VALUES('project','creator','Signal fixtures')`)
	if err != nil {
		t.Fatal(err)
	}
	insertTask := func(id, execution, delivery, settlement, code string, created time.Time, lease any, attempts int) {
		t.Helper()
		_, err := db.Exec(ctx, `INSERT INTO quotes(id,user_id,project_id,product_id,input,input_hash,product_snapshot,credits,expires_at)
 VALUES($1,'creator','project','demo-image','{}','unused','{}',10,now());`, "quote-"+id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(ctx, `INSERT INTO generation_tasks(id,user_id,project_id,quote_id,product_id,request_key,request_hash,input,product_snapshot,credits,kind,model,credential_ref,execution_status,delivery_status,settlement_status,error_code,error_message,lease_token,lease_until,attempts,created_at)
 VALUES($1,'creator','project',$2,'demo-image',$1,'unused','{"prompt":"private-prompt"}','{}',10,'image','demo','private-credential',$3,$4,$5,$6,'private-error-message','private-lease-token',$7,$8,$9)`, id, "quote-"+id, execution, delivery, settlement, code, lease, attempts, created)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 6; i >= 0; i-- {
		insertTask(fmt.Sprintf("unknown-%02d", i), "submission_unknown", "pending", "frozen", "", base.Add(-48*time.Hour), nil, i+1)
	}
	insertTask("unknown-recent", "submission_unknown", "pending", "frozen", "", base.Add(-time.Hour), nil, 1)
	insertTask("overdue-other", "queued", "pending", "frozen", "", base.Add(-72*time.Hour), nil, 0)
	insertTask("archive-retry", "completed", "archive_retry", "frozen", "", base.Add(-30*time.Minute), nil, 2)
	insertTask("archive-error", "completed", "error", "frozen", "", base.Add(-30*time.Minute), nil, 3)
	insertTask("archive-review", "completed", "pending", "frozen", "archive_review", base.Add(-30*time.Minute), nil, 4)
	insertTask("archive-normal", "completed", "pending", "frozen", "", base.Add(-30*time.Minute), nil, 0)
	for _, id := range []string{"lease-b", "lease-a", "lease-c"} {
		expired := base.Add(-3 * time.Hour)
		if id == "lease-c" {
			expired = base.Add(-2 * time.Hour)
		}
		insertTask(id, "generating", "pending", "frozen", "", base.Add(-6*time.Hour), expired, 4)
	}
	insertTask("lease-future", "generating", "pending", "frozen", "", base.Add(-6*time.Hour), base.Add(time.Hour), 1)
	insertTask("settled-unknown", "submission_unknown", "pending", "released", "", base.Add(-96*time.Hour), base.Add(-time.Hour), 8)
	insertTask("settled-archive", "completed", "error", "consumed", "archive_review", base.Add(-96*time.Hour), base.Add(-time.Hour), 8)
	for i, status := range []string{"paid", "expired", "attempts", "credited", "normal"} {
		payment, credit, expires, attempts := "pending", "pending", base.Add(time.Hour), 0
		switch status {
		case "paid":
			payment = "paid"
		case "expired":
			expires = base.Add(-time.Hour)
		case "attempts":
			attempts = 3
		case "credited":
			payment, credit, attempts = "paid", "credited", 4
		case "normal":
			attempts = 2
		}
		_, err = db.Exec(ctx, `INSERT INTO payment_orders(id,user_id,out_trade_no,tier_id,tier_version,amount_fen,credits,method,credential_ref,merchant_pid,provider_origin,checkout_url,expires_at,payment_status,credit_status,check_attempts,created_at)
 VALUES($1,'creator',$1,'starter',1,1000,1000,'alipay','private-payment-ref','private-merchant','https://private-origin.invalid','https://private-checkout.invalid?signature=secret',$2,$3,$4,$5,$6)`, "payment-"+status, expires, payment, credit, attempts, base.Add(time.Duration(-6+i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 6; i >= 0; i-- {
		_, err = db.Exec(ctx, `INSERT INTO auth_mail_outbox(id,user_id,to_email,subject,body,attempts,created_at)
 VALUES($1,'creator','private-recipient@example.test','private-subject','private-auth-ticket',$2,$3)`, fmt.Sprintf("mail-%02d", i), i, base.Add(-4*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(ctx, `INSERT INTO auth_mail_outbox(id,user_id,to_email,subject,body,attempts,next_attempt_at,sent_at,created_at) VALUES
 ('mail-future','creator','private-recipient@example.test','private-subject','private-auth-ticket',0,$1,NULL,$2),
 ('mail-sent','creator','private-recipient@example.test','private-subject','private-auth-ticket',9,$1,$2,$3)`, base.Add(time.Hour), base.Add(-time.Hour), base.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, `INSERT INTO auth_mail_outbox(id,user_id,to_email,subject,body,attempts,cancelled_at,created_at)
 VALUES('mail-cancelled','creator','private-recipient@example.test','private-subject','',9,now(),$1)`, base.Add(-96*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func signalBusinessFingerprint(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var fingerprint string
	err := db.QueryRow(context.Background(), `SELECT md5(jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM generation_tasks t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM payment_orders t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM auth_mail_outbox t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id) FROM wallet_accounts t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM wallet_ledger t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM admin_audit t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY task_id,revision) FROM task_procurement_reviews t))::text)`).Scan(&fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func TestPostgresSignalsSnapshotSamplesPermissionsAndNoMutation(t *testing.T) {
	db := operationsTestDB(t)
	ctx := context.Background()
	base := seedSignalRows(t, db)
	before := signalBusinessFingerprint(t, db)
	result, err := Snapshot(ctx, db, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.SampleLimit != 5 || result.ReviewAfterSeconds != 86400 || result.GeneratedAt.Before(base) || len(result.Signals) != len(signalCodes) {
		t.Fatal("incorrect snapshot metadata", result)
	}
	sample := func(id string, age time.Duration, attempts int) SignalSample {
		return SignalSample{ID: id, OccurredAt: base.Add(-age), Attempts: attempts}
	}
	unknown, mail, retry := []SignalSample{}, []SignalSample{}, []SignalSample{}
	for i := 0; i < 5; i++ {
		unknown = append(unknown, sample(fmt.Sprintf("unknown-%02d", i), 48*time.Hour, i+1))
		mail = append(mail, sample(fmt.Sprintf("mail-%02d", i), 4*time.Hour, i))
		retry = append(retry, sample(fmt.Sprintf("mail-%02d", i+1), 4*time.Hour, i+1))
	}
	expected := []struct {
		count   int64
		oldest  time.Time
		samples []SignalSample
	}{
		{8, base.Add(-48 * time.Hour), unknown},
		{8, base.Add(-72 * time.Hour), append([]SignalSample{sample("overdue-other", 72*time.Hour, 0)}, unknown[:4]...)},
		{3, base.Add(-30 * time.Minute), []SignalSample{sample("archive-error", 30*time.Minute, 3), sample("archive-retry", 30*time.Minute, 2), sample("archive-review", 30*time.Minute, 4)}},
		{3, base.Add(-3 * time.Hour), []SignalSample{sample("lease-a", 3*time.Hour, 4), sample("lease-b", 3*time.Hour, 4), sample("lease-c", 2*time.Hour, 4)}},
		{3, base.Add(-6 * time.Hour), []SignalSample{sample("payment-paid", 6*time.Hour, 0), sample("payment-expired", 5*time.Hour, 0), sample("payment-attempts", 4*time.Hour, 3)}},
		{8, base.Add(-4 * time.Hour), mail},
		{6, base.Add(-4 * time.Hour), retry},
	}
	for i, want := range expected {
		got := result.Signals[i]
		if got.Code != signalCodes[i] || got.Count != want.count || got.OldestAt == nil || !got.OldestAt.Equal(want.oldest) || len(got.Samples) != len(want.samples) {
			t.Fatalf("incorrect %s snapshot: %#v", signalCodes[i], got)
		}
		for j, sample := range got.Samples {
			wantSample := want.samples[j]
			if sample.ID != wantSample.ID || sample.Attempts != wantSample.Attempts || !sample.OccurredAt.Equal(wantSample.OccurredAt) {
				t.Fatalf("incorrect %s sample at position %d", signalCodes[i], j)
			}
		}
	}
	if got := result.Signals[7]; got.Code != "procurement_review_disputed" || got.Count != 0 || got.OldestAt != nil || len(got.Samples) != 0 {
		t.Fatal("old worker exceptions must not become manual procurement disputes", got)
	}
	custom, err := Snapshot(ctx, db, 60*time.Hour)
	if err != nil || custom.ReviewAfterSeconds != 60*3600 || custom.Signals[1].Count != 1 {
		t.Fatal("configured review threshold was ignored", err)
	}
	adminToken, userToken := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for token, userID := range map[string]string{adminToken: "operator", userToken: "creator"} {
		digest := sha256.Sum256([]byte(token))
		_, err = db.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,'fixture-csrf',now()+interval '1 hour')`, "session-"+userID, userID, hex.EncodeToString(digest[:]))
		if err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	identity := auth.New(db, config.Config{})
	service := New(db, config.Config{TaskReviewAfter: 24 * time.Hour})
	service.Register(mux, identity.Admin)
	wantSignals, err := json.Marshal(result.Signals)
	if err != nil {
		t.Fatal("encoding shared-query signals", err)
	}
	for _, access := range []struct {
		token  string
		status int
	}{{"", 401}, {userToken, 403}, {adminToken, 200}} {
		req := httptest.NewRequest("GET", "/api/v1/admin/operations/signals", nil)
		if access.token != "" {
			req.AddCookie(&http.Cookie{Name: "studio_session", Value: access.token})
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != access.status {
			t.Fatalf("signals access got %d, want %d", rec.Code, access.status)
		}
		for _, secret := range []string{"private-", "secret-mail", "operator@example.test", adminToken, userToken, "lease_token", "checkout_url", "error_message", "to_email", "credential_ref"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatal("signal endpoint exposed private data")
			}
		}
		if rec.Code == 200 {
			var envelope struct {
				Code string         `json:"code"`
				Data SnapshotResult `json:"data"`
			}
			if err = json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatal("decoding HTTP signal snapshot", err)
			}
			if envelope.Code != "ok" {
				t.Fatalf("HTTP signal snapshot code = %q, want ok", envelope.Code)
			}
			// Compare exact wire values: PostgreSQL's +00:00 and HTTP's Z can
			// decode to different time.Time locations for the same instant.
			gotSignals, err := json.Marshal(envelope.Data.Signals)
			if err != nil {
				t.Fatal("encoding HTTP signals", err)
			}
			if !bytes.Equal(gotSignals, wantSignals) {
				t.Fatalf("HTTP signals differ from shared query:\n got: %s\nwant: %s", gotSignals, wantSignals)
			}
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	failed := httptest.NewRecorder()
	service.signals(failed, httptest.NewRequest("GET", "/api/v1/admin/operations/signals", nil).WithContext(canceled))
	if failed.Code != 503 || !strings.Contains(failed.Body.String(), "signals_unavailable") {
		t.Fatal("failed snapshot did not return a bounded, sanitized failure")
	}
	if after := signalBusinessFingerprint(t, db); after != before {
		t.Fatal("reading snapshots changed tasks, payments, mail, wallets, ledger or audit records")
	}
}

func TestPostgresSignalsLatestProcurementDisputesAndNoMutation(t *testing.T) {
	db := operationsTestDB(t)
	ctx := context.Background()
	base := seedSignalRows(t, db)
	if _, err := db.Exec(ctx, `UPDATE generation_tasks SET delivery_status='ready',error_code='' WHERE id='settled-archive';
 UPDATE generation_tasks SET procurement_status='disputed' WHERE id='overdue-other'`); err != nil {
		t.Fatal(err)
	}
	previous, err := Snapshot(ctx, db, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"unknown-00", "unknown-01", "unknown-02", "unknown-03", "unknown-04", "unknown-05", "unknown-06", "unknown-recent", "settled-archive"}
	for _, id := range ids {
		_, err = db.Exec(ctx, `INSERT INTO task_procurement_reviews(id,task_id,revision,admin_id,status,reason,evidence,created_at)
 VALUES($1,$2,1,'operator','disputed','private-review-reason','private-review-evidence',$3)`, "review-"+id, id, base.Add(-2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	// Revision, not timestamp or any historical disputed row, selects current.
	for i, status := range []string{"resolved", "matched", "pending"} {
		_, err = db.Exec(ctx, `INSERT INTO task_procurement_reviews(id,task_id,revision,admin_id,status,reason,created_at)
 VALUES($1,$2,2,'operator',$3,'private-replacement-reason',$4)`, "replacement-"+ids[i], ids[i], status, base.Add(-8*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	before := signalBusinessFingerprint(t, db)
	result, err := Snapshot(ctx, db, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous.Signals[:7], result.Signals[:7]) {
		t.Fatal("manual review records changed the seven existing signals")
	}
	got := result.Signals[7]
	if got.Code != "procurement_review_disputed" || got.Count != 6 || got.OldestAt == nil || !got.OldestAt.Equal(base.Add(-2*time.Hour)) || len(got.Samples) != 5 {
		t.Fatal("latest manual dispute count, time or sample limit is incorrect", got)
	}
	wantIDs := []string{"settled-archive", "unknown-03", "unknown-04", "unknown-05", "unknown-06"}
	for i, sample := range got.Samples {
		if sample.ID != wantIDs[i] || sample.Attempts != 0 || !sample.OccurredAt.Equal(base.Add(-2*time.Hour)) {
			t.Fatal("manual dispute sample must use local task ID, current review time and zero attempts", sample)
		}
	}
	raw, err := json.Marshal(result)
	if err != nil || strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "reason") || strings.Contains(string(raw), "evidence") {
		t.Fatal("snapshot exposed private review fields", err)
	}
	if after := signalBusinessFingerprint(t, db); after != before {
		t.Fatal("reading procurement dispute signals changed business or review records")
	}
}
