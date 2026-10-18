package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"wsapi-video/internal/config"
	"wsapi-video/internal/media"
	"wsapi-video/internal/observability"
)

func captureBusinessLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous, writer, flags, prefix := slog.Default(), log.Writer(), log.Flags(), log.Prefix()
	var out bytes.Buffer
	slog.SetDefault(observability.NewJSONLogger(&out))
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(writer)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
	return &out
}

func businessRecords(t *testing.T, out *bytes.Buffer, secrets ...string) []map[string]any {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(out.String(), secret) {
			t.Fatal("sensitive business value reached logs")
		}
	}
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal("invalid business log JSON", err)
		}
		for key := range record {
			switch key {
			case "time", "level", "msg", "event", "code", "task_id", "attempt", "order_id":
			default:
				t.Fatalf("unexpected business log field %q", key)
			}
		}
		records = append(records, record)
	}
	return records
}

func requireBusinessEvent(t *testing.T, records []map[string]any, event, code, field, id string, count int) {
	t.Helper()
	found := 0
	for _, record := range records {
		if record["event"] == event && record["code"] == code && record[field] == id {
			found++
		}
	}
	if found != count {
		t.Fatalf("%s/%s correlated events: got %d, want %d", event, code, found, count)
	}
}

func TestPostgresBusinessLoggingTaskFlow(t *testing.T) {
	db := integrationDB(t)
	out := captureBusinessLogs(t)
	ctx := context.Background()
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	seedRealProduct(t, db)
	secret := "SENSITIVE_TASK_SENTINEL"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			_, _ = fmt.Fprintf(w, `{"id":%q}`, secret)
		case strings.HasPrefix(r.URL.Path, "/v1/images/tasks/"):
			_, _ = fmt.Fprintf(w, `{"id":%q,"status":"completed","result":{"data":[{"url":%q}]}}`, secret, srv.URL+"/image.png?signature="+secret)
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(demoPNG("test", "16:9"))
		}
	}))
	defer srv.Close()
	s := New(db, config.Config{}, &archiveFixture{failures: 1})
	var err error
	s.gateway, err = media.NewConnector(media.Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{secret: secret}, ImageCredential: secret})
	if err != nil {
		t.Fatal(err)
	}
	create := func(key string) Task {
		t.Helper()
		quote := doRequest(s, user, "POST", "/api/v1/quotes", QuoteInput{ProjectID: project, ProductID: "real-image", Prompt: secret})
		if quote.Code != 201 {
			t.Fatal("quote failed", quote.Code)
		}
		q := data[Quote](t, quote)
		rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": key})
		if rec.Code != 201 {
			t.Fatal("task failed", rec.Code)
		}
		return data[Task](t, rec)
	}
	completed := create("logging-completed")
	for i := 0; i < 3; i++ {
		s.process(ctx, runnable(t, s, completed.ID))
	}
	if err = s.settle(ctx, completed.ID, "", "consumed", "completed", "ready", "", secret); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, db, user, 90, 0)
	reviewed := create("logging-reviewed")
	s.process(ctx, runnable(t, s, reviewed.ID))
	job := runnable(t, s, reviewed.ID)
	s.holdReview(ctx, job, "upstream_review", secret+" reason evidence")
	if err = s.settle(ctx, job.ID, job.LeaseToken, "released", "failed", "error", "test", secret); err != nil {
		t.Fatal(err)
	}
	// A stale lease and an idempotent terminal observation must not emit success.
	s.retry(ctx, job, "poll_retry", secret, false)
	if err = s.settle(ctx, job.ID, "", "released", "failed", "error", "test", secret); err != nil {
		t.Fatal(err)
	}
	rolledBack := create("logging-rollback")
	if _, err = db.Exec(ctx, "ALTER TABLE generation_tasks ADD CONSTRAINT logging_test_execution CHECK (execution_status <> 'invalid-execution')"); err != nil {
		t.Fatal(err)
	}
	if err = s.settle(ctx, rolledBack.ID, "", "consumed", "invalid-execution", "ready", "test", secret); err == nil {
		t.Fatal("expected transaction rollback")
	}
	assertBalance(t, db, user, 80, 10)
	canceled := create("logging-canceled")
	if rec := doRequest(s, user, "POST", "/api/v1/tasks/"+canceled.ID+"/cancel", nil); rec.Code != 200 {
		t.Fatal("queued cancellation failed", rec.Code)
	}
	if rec := doRequest(s, user, "POST", "/api/v1/tasks/"+canceled.ID+"/cancel", nil); rec.Code != 409 {
		t.Fatal("repeated cancellation response changed", rec.Code)
	}
	assertBalance(t, db, user, 80, 10)
	records := businessRecords(t, out, secret, srv.URL, user+"@example.test")
	requireBusinessEvent(t, records, "task_created", "frozen", "task_id", completed.ID, 1)
	requireBusinessEvent(t, records, "task_claimed", "prepare", "task_id", completed.ID, 1)
	requireBusinessEvent(t, records, "task_claimed", "poll", "task_id", completed.ID, 1)
	requireBusinessEvent(t, records, "task_claimed", "archive", "task_id", completed.ID, 1)
	requireBusinessEvent(t, records, "task_submission", "submitting", "task_id", completed.ID, 1)
	requireBusinessEvent(t, records, "task_submission", "accepted", "task_id", completed.ID, 1)
	requireBusinessEvent(t, records, "task_retry", "archive_retry", "task_id", completed.ID, 1)
	requireBusinessEvent(t, records, "task_settlement", "consumed", "task_id", completed.ID, 1)
	requireBusinessEvent(t, records, "task_review", "upstream_review", "task_id", reviewed.ID, 1)
	requireBusinessEvent(t, records, "task_settlement", "released", "task_id", reviewed.ID, 1)
	requireBusinessEvent(t, records, "task_retry", "poll_retry", "task_id", reviewed.ID, 0)
	requireBusinessEvent(t, records, "task_settlement", "consumed", "task_id", rolledBack.ID, 0)
	requireBusinessEvent(t, records, "task_settlement_failed", "database_error", "task_id", rolledBack.ID, 1)
	requireBusinessEvent(t, records, "task_settlement", "released", "task_id", canceled.ID, 1)
	for _, record := range records {
		if record["event"] == "task_claimed" || record["event"] == "task_submission" || record["event"] == "task_retry" || record["event"] == "task_review" {
			if attempt, ok := record["attempt"].(float64); !ok || attempt < 1 {
				t.Fatal("worker event lost attempt correlation")
			}
		}
	}
}

func TestPostgresBusinessLoggingPaymentFlow(t *testing.T) {
	db := integrationDB(t)
	out := captureBusinessLogs(t)
	ctx := context.Background()
	user := seedUser(t, db, 0)
	if _, err := db.Exec(ctx, "UPDATE payment_tiers SET active=true WHERE id='starter'"); err != nil {
		t.Fatal(err)
	}
	secret := "SENSITIVE_PAYMENT_SENTINEL"
	var reject atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reject.Load() {
			w.WriteHeader(503)
			_, _ = io.WriteString(w, secret)
			return
		}
		_, _ = fmt.Fprintf(w, `{"code":1,"out_trade_no":%q,"pid":"1001","status":1,"money":"10.00","trade_no":%q}`, r.FormValue("out_trade_no"), secret)
	}))
	defer srv.Close()
	previousTransport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = previousTransport }()
	s := New(db, config.Config{EasyPayOrigin: srv.URL, EasyPayPID: "1001", EasyPayKey: secret, EasyPayCredentialRef: secret, EasyPayKeys: map[string]string{secret: secret}, EasyPayNotifyURL: "https://studio.example/notify", EasyPayReturnURL: "https://studio.example/wallet"}, nil)
	create := func() Order {
		t.Helper()
		rec := doRequest(s, user, "POST", "/api/v1/payment/orders", map[string]string{"tier_id": "starter", "method": "alipay"})
		if rec.Code != 201 {
			t.Fatal("payment order failed", rec.Code)
		}
		created := data[Order](t, rec)
		order, err := s.getOrder(ctx, user, created.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		return order
	}
	paid := create()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.confirmOrder(ctx, paid, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	assertBalance(t, db, user, paid.Credits, 0)
	var credits int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger WHERE event_key=$1", "order:"+paid.ID+":credit").Scan(&credits); err != nil || credits != 1 {
		t.Fatal("payment ledger was not idempotent", credits, err)
	}
	reject.Store(true)
	failed := create()
	if err := s.confirmOrder(ctx, failed, nil); err == nil {
		t.Fatal("expected failed payment query")
	}
	params := url.Values{"out_trade_no": {failed.OutTradeNo}, "pid": {"1001"}, "sign": {secret}, "money": {"10.00"}, "trade_no": {secret}, "trade_status": {"TRADE_SUCCESS"}}
	req := httptest.NewRequest("POST", "/api/v1/payment/notify/easypay", strings.NewReader(params.Encode()))
	rec := httptest.NewRecorder()
	s.notify(rec, req)
	if rec.Code != 400 {
		t.Fatal("invalid signature response changed", rec.Code)
	}
	assertBalance(t, db, user, paid.Credits, 0)
	records := businessRecords(t, out, secret, srv.URL, paid.OutTradeNo, failed.OutTradeNo, user+"@example.test")
	requireBusinessEvent(t, records, "payment_order_created", "accepted", "order_id", paid.ID, 1)
	requireBusinessEvent(t, records, "payment_credited", "accepted", "order_id", paid.ID, 1)
	requireBusinessEvent(t, records, "payment_order_created", "accepted", "order_id", failed.ID, 1)
	requireBusinessEvent(t, records, "payment_confirmation_failed", "payment_query_failed", "order_id", failed.ID, 1)
	requireBusinessEvent(t, records, "payment_notification_rejected", "invalid_signature", "order_id", failed.ID, 1)
	requireBusinessEvent(t, records, "payment_credited", "accepted", "order_id", failed.ID, 0)
}
