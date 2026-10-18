package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/media"
	"wsapi-video/internal/payments"
	"wsapi-video/migrations"
)

func integrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL financial integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_commerce_" + strings.TrimPrefix(core.ID(""), "_")
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 30
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE")
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}
func seedUser(t *testing.T, db *pgxpool.Pool, amount int64) string {
	t.Helper()
	id := core.ID("usr")
	ctx := context.Background()
	if _, err := db.Exec(ctx, "INSERT INTO users(id,email,password_hash,display_name,role,email_verified) VALUES($1,$2,'test','Tester','admin',true)", id, id+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO wallet_accounts(user_id,available) VALUES($1,$2)", id, amount); err != nil {
		t.Fatal(err)
	}
	return id
}
func seedProject(t *testing.T, db *pgxpool.Pool, userID string) string {
	t.Helper()
	id := core.ID("prj")
	if _, err := db.Exec(context.Background(), "INSERT INTO projects(id,user_id,name) VALUES($1,$2,'Test project')", id, userID); err != nil {
		t.Fatal(err)
	}
	return id
}
func doRequest(s *Service, userID, method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req = req.WithContext(core.WithUser(req.Context(), core.User{ID: userID, Role: "admin"}))
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	identity := func(h http.Handler) http.Handler { return h }
	s.Register(mux, identity, identity)
	mux.ServeHTTP(rec, req)
	return rec
}
func data[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var e struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return e.Data
}
func createQuote(t *testing.T, s *Service, userID, projectID, productID string) Quote {
	t.Helper()
	rec := doRequest(s, userID, "POST", "/api/v1/quotes", QuoteInput{ProjectID: projectID, ProductID: productID, Prompt: "A quiet studio scene"})
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	return data[Quote](t, rec)
}
func assertBalance(t *testing.T, db *pgxpool.Pool, userID string, wantAvailable, wantFrozen int64) {
	t.Helper()
	var a, f int64
	if err := db.QueryRow(context.Background(), "SELECT available,frozen FROM wallet_accounts WHERE user_id=$1", userID).Scan(&a, &f); err != nil {
		t.Fatal(err)
	}
	if a != wantAvailable || f != wantFrozen {
		t.Fatalf("balance=%d/%d; expected=%d/%d", a, f, wantAvailable, wantFrozen)
	}
}

func TestPostgresConcurrentFreezeAndIdempotency(t *testing.T) {
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	s := New(db, config.Config{DevMode: true, Demo: true}, nil)
	// Twenty distinct requests compete for ten affordable 10-credit tasks.
	quotes := make([]Quote, 20)
	for i := range quotes {
		quotes[i] = createQuote(t, s, user, project, "demo-image")
	}
	var accepted, rejected atomic.Int32
	var wg sync.WaitGroup
	for i, q := range quotes {
		wg.Add(1)
		go func(i int, q Quote) {
			defer wg.Done()
			rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": fmt.Sprintf("freeze-key-%d", i)})
			switch rec.Code {
			case 201:
				accepted.Add(1)
			case 402:
				rejected.Add(1)
			default:
				t.Errorf("unexpected concurrent submit: %d %s", rec.Code, rec.Body.String())
			}
		}(i, q)
	}
	wg.Wait()
	if accepted.Load() != 10 || rejected.Load() != 10 {
		t.Fatal(accepted.Load(), rejected.Load())
	}
	assertBalance(t, db, user, 0, 100)
	// The accepted result remains retrievable after quote expiry.
	var id, key, quoteID string
	if err := db.QueryRow(context.Background(), "SELECT id,request_key,quote_id FROM generation_tasks LIMIT 1").Scan(&id, &key, &quoteID); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(context.Background(), "UPDATE quotes SET expires_at=now()-interval '1 hour' WHERE id=$1", quoteID)
	for i := 0; i < 8; i++ {
		rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": quoteID, "request_key": key})
		if rec.Code != 200 || data[Task](t, rec).ID != id {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	bad := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": "different", "request_key": key})
	if bad.Code != 409 {
		t.Fatal("same key with different input was accepted", bad.Code)
	}
	var ledger int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM wallet_ledger WHERE event_type='freeze'").Scan(&ledger); err != nil || ledger != 10 {
		t.Fatal(ledger, err)
	}
	// Repeated terminal observations release once; late completion never consumes again.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.settle(context.Background(), id, "", "released", "failed", "error", "test", "test failure"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := s.settle(context.Background(), id, "", "consumed", "completed", "ready", "", "late result"); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, db, user, 10, 90)
}

type archiveFixture struct {
	mu       sync.Mutex
	failures int
	calls    int
	bytes    int
}

func (a *archiveFixture) Resolve(context.Context, string, string) (assets.Asset, error) {
	return assets.Asset{}, errors.New("unused")
}
func (a *archiveFixture) ReferenceURL(context.Context, string, string) (string, error) {
	return "", errors.New("unused")
}
func (a *archiveFixture) Archived(context.Context, string, string, string) (assets.Asset, error) {
	return assets.Asset{}, assets.ErrNotFound
}
func (a *archiveFixture) Archive(ctx context.Context, user, project, task, fileName, ct string, r io.Reader) (assets.Asset, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	b, err := io.ReadAll(r)
	if err != nil {
		return assets.Asset{}, err
	}
	a.bytes = len(b)
	if a.failures > 0 {
		a.failures--
		return assets.Asset{}, errors.New("storage temporarily unavailable")
	}
	return assets.Asset{ID: "archived-" + task, Kind: "image", ProjectID: project}, nil
}
func runnable(t *testing.T, s *Service, id string) Task {
	t.Helper()
	_, err := s.db.Exec(context.Background(), "UPDATE generation_tasks SET next_attempt_at=now(),lease_until=NULL WHERE id=$1", id)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.claim(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return job
}
func seedRealProduct(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	_, err := db.Exec(context.Background(), "INSERT INTO model_products(id,name,kind,model,credits,capabilities,active,verified) VALUES('real-image','Fixture','image','fixture-model',10,'{\"aspect_ratios\":[],\"durations\":[],\"resolutions\":[],\"sizes\":[],\"qualities\":[],\"max_images\":0,\"max_videos\":0,\"max_audios\":0}',true,true)")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresUnknownSubmissionIsNeverResent(t *testing.T) {
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	seedRealProduct(t, db)
	var creates atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { creates.Add(1); w.WriteHeader(503) }))
	defer srv.Close()
	s := New(db, config.Config{}, nil)
	s.gateway, _ = media.NewConnector(media.Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"original": "secret"}, ImageCredential: "original"})
	q := createQuote(t, s, user, project, "real-image")
	rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "unknown-key-1"})
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	task := data[Task](t, rec)
	s.process(context.Background(), runnable(t, s, task.ID))
	got, err := s.getTask(context.Background(), user, task.ID, false)
	if err != nil || got.ExecutionStatus != "submission_unknown" {
		t.Fatal(got, err)
	}
	if _, err = s.claim(context.Background()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("unknown task was eligible for retry", err)
	}
	assertBalance(t, db, user, 90, 10)
	if creates.Load() != 1 {
		t.Fatal("upstream was resubmitted", creates.Load())
	}
	// Process restart finding an expired sending lease also retains the reservation.
	_, _ = db.Exec(context.Background(), "UPDATE generation_tasks SET execution_status='sending',lease_until=now()-interval '1 second' WHERE id=$1", task.ID)
	s.recoverSending(context.Background())
	got, _ = s.getTask(context.Background(), user, task.ID, false)
	if got.ExecutionStatus != "submission_unknown" {
		t.Fatal("expired send did not become unknown")
	}
}
func TestPostgresArchiveRetryConsumesOnlyAfterSave(t *testing.T) {
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	seedRealProduct(t, db)
	var creates atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			creates.Add(1)
			_, _ = io.WriteString(w, `{"id":"original-job"}`)
		case strings.HasPrefix(r.URL.Path, "/v1/images/tasks/"):
			_, _ = fmt.Fprintf(w, `{"id":"original-job","status":"completed","result":{"data":[{"url":%q}]}}`, srv.URL+"/image.png")
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(demoPNG("test", "16:9"))
		}
	}))
	defer srv.Close()
	a := &archiveFixture{failures: 1}
	s := New(db, config.Config{}, a)
	s.gateway, _ = media.NewConnector(media.Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"original": "secret"}, ImageCredential: "original"})
	q := createQuote(t, s, user, project, "real-image")
	rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "archive-key-1"})
	task := data[Task](t, rec)
	s.process(context.Background(), runnable(t, s, task.ID))
	s.process(context.Background(), runnable(t, s, task.ID))
	assertBalance(t, db, user, 90, 10)
	got, _ := s.getTask(context.Background(), user, task.ID, false)
	if got.DeliveryStatus != "archive_retry" {
		t.Fatal(got)
	}
	s.process(context.Background(), runnable(t, s, task.ID))
	assertBalance(t, db, user, 90, 0)
	got, _ = s.getTask(context.Background(), user, task.ID, false)
	if got.DeliveryStatus != "ready" || got.SettlementStatus != "consumed" || len(got.AssetIDs) != 1 {
		t.Fatal(got)
	}
	if creates.Load() != 1 || a.calls != 2 || a.bytes == 0 {
		t.Fatal("archive retry re-generated or lost bytes", creates.Load(), a.calls, a.bytes)
	}
}

func TestPostgresConcurrentPaymentConfirmationCreditsOnce(t *testing.T) {
	db := integrationDB(t)
	user := seedUser(t, db, 0)
	var queries atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries.Add(1)
		_, _ = io.WriteString(w, `{"code":1,"out_trade_no":"CV-PAID","pid":"1001","status":1,"money":"10.00","trade_no":"trade-123"}`)
	}))
	defer srv.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = originalTransport }()
	s := New(db, config.Config{EasyPayOrigin: srv.URL, EasyPayPID: "1001", EasyPayKey: "original-secret", EasyPayCredentialRef: "old", EasyPayKeys: map[string]string{"old": "original-secret"}, EasyPayNotifyURL: "https://app.example/notify", EasyPayReturnURL: "https://app.example/wallet"}, nil)
	_, err := db.Exec(context.Background(), "INSERT INTO payment_orders(id,user_id,out_trade_no,tier_id,tier_version,amount_fen,credits,method,credential_ref,merchant_pid,provider_origin,checkout_url,expires_at) VALUES('paid-order',$1,'CV-PAID','starter',1,1000,1000,'alipay','old','1001',$2,'https://pay.example/checkout',now()-interval '1 hour')", user, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.getOrder(context.Background(), user, "paid-order", false)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.confirmOrder(context.Background(), o, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	assertBalance(t, db, user, 1000, 0)
	var events int
	if err = db.QueryRow(context.Background(), "SELECT count(*) FROM wallet_ledger WHERE event_key='order:paid-order:credit'").Scan(&events); err != nil || events != 1 {
		t.Fatal(events, err)
	}
	o, err = s.getOrder(context.Background(), user, o.ID, false)
	if err != nil || o.PaymentStatus != "paid" || o.CreditStatus != "credited" {
		t.Fatal(o, err)
	}
	// A payment page return is read-only; forged or wrong-amount notifications cannot credit.
	bad := payments.Confirmation{OrderNo: o.OutTradeNo, TradeNo: "trade-123", AmountFen: 999, Paid: true}
	var before int64
	_ = db.QueryRow(context.Background(), "SELECT available FROM wallet_accounts WHERE user_id=$1", user).Scan(&before)
	if _, err = s.paymentAdapter(o.CredentialRef, o.ProviderOrigin, o.MerchantPID); err != nil {
		t.Fatal(err)
	}
	p, _ := s.paymentAdapter(o.CredentialRef, o.ProviderOrigin, o.MerchantPID)
	if _, err = p.Confirm(context.Background(), bad); err == nil {
		t.Fatal("mismatched payment amount accepted")
	}
	assertBalance(t, db, user, before, 0)
	if queries.Load() < 1 {
		t.Fatal("payment was never confirmed with channel")
	}
}

func TestPostgresSmallPoolQuoteRaceDoesNotBorrowAnotherConnection(t *testing.T) {
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	cfg := config.Config{DevMode: true, Demo: true}
	s := New(db, cfg, nil)
	q := createQuote(t, s, user, project, "demo-image")
	pc := db.Config().Copy()
	pc.MaxConns = 1
	single, err := pgxpool.NewWithConfig(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	s = New(single, cfg, nil)
	blocker, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	var quoteID string
	if err = blocker.QueryRow(context.Background(), "SELECT id FROM quotes WHERE id=$1 FOR UPDATE", q.ID).Scan(&quoteID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		raw, _ := json.Marshal(map[string]string{"quote_id": q.ID, "request_key": "small-pool-key"})
		req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewReader(raw)).WithContext(core.WithUser(ctx, core.User{ID: user}))
		rec := httptest.NewRecorder()
		s.submit(rec, req)
		finished <- rec
	}()
	// Wait until the request passed its first lookup and is blocked on our quote lock.
	deadline := time.Now().Add(2 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		var n int
		err = db.QueryRow(context.Background(), "SELECT count(*) FROM pg_stat_activity WHERE query LIKE 'SELECT project_id,product_id,input,%' AND wait_event_type='Lock'").Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("request did not reach the serialized quote read")
	}
	id := core.ID("task")
	_, err = blocker.Exec(context.Background(), "INSERT INTO generation_tasks(id,user_id,project_id,quote_id,product_id,request_key,request_hash,input,product_snapshot,credits,kind,model,credential_ref,is_demo) SELECT $1,user_id,project_id,id,product_id,'small-pool-key',$2,input,product_snapshot,credits,'image','demo-image','demo',true FROM quotes WHERE id=$3", id, digest(q.ID), q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = changeWallet(context.Background(), blocker, user, -10, 10, "task:"+id+":freeze", "freeze", id, "", "test race"); err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case rec := <-finished:
		if rec.Code != 200 || data[Task](t, rec).ID != id {
			t.Fatal(rec.Code, rec.Body.String())
		}
	case <-time.After(4 * time.Second):
		t.Fatal("single-connection pool deadlocked on idempotent response")
	}
	assertBalance(t, db, user, 90, 10)
}

func TestPostgresStaleLeaseCannotReleaseNewWorkerFunds(t *testing.T) {
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	s := New(db, config.Config{DevMode: true, Demo: true}, nil)
	q := createQuote(t, s, user, project, "demo-image")
	rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "lease-fence-key"})
	job := data[Task](t, rec)
	_, err := db.Exec(context.Background(), "UPDATE generation_tasks SET lease_token='new-worker',execution_status='completed',delivery_status='archiving' WHERE id=$1", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.settle(context.Background(), job.ID, "old-worker", "released", "failed", "error", "stale", "stale failed response"); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, db, user, 90, 10)
	got, _ := s.getTask(context.Background(), user, job.ID, false)
	if got.ExecutionStatus != "completed" || got.SettlementStatus != "frozen" {
		t.Fatal(got)
	}
	if err = s.settle(context.Background(), job.ID, "new-worker", "consumed", "completed", "ready", "", "saved"); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, db, user, 90, 0)
}

type metadataFixture map[string]assets.Asset

func (m metadataFixture) Resolve(_ context.Context, _ string, id string) (assets.Asset, error) {
	a, ok := m[id]
	if !ok {
		return a, errors.New("missing asset")
	}
	return a, nil
}
func (m metadataFixture) ReferenceURL(context.Context, string, string) (string, error) {
	return "", errors.New("not used")
}
func (m metadataFixture) Archived(context.Context, string, string, string) (assets.Asset, error) {
	return assets.Asset{}, assets.ErrNotFound
}
func (m metadataFixture) Archive(context.Context, string, string, string, string, string, io.Reader) (assets.Asset, error) {
	return assets.Asset{}, errors.New("not used")
}

func TestPostgresQuoteRejectsLargeLongAndDuplicateReferences(t *testing.T) {
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	caps := Capabilities{MaxImages: 2, MaxVideos: 1, MaxAudios: 1, MaxReferenceBytes: 100, MaxReferenceDurationSeconds: 10, MaxReferenceWidth: 1024, MaxReferenceHeight: 1024}
	raw, _ := json.Marshal(caps)
	_, err := db.Exec(context.Background(), "INSERT INTO model_products(id,name,kind,model,credits,capabilities,active,verified) VALUES('bounded-video','Bounded video','video','verified-fixture',40,$1,true,true)", raw)
	if err != nil {
		t.Fatal(err)
	}
	m := metadataFixture{"long": {ID: "long", Kind: "video", DurationSeconds: 11, SizeBytes: 50, Width: 512, Height: 512}, "large": {ID: "large", Kind: "video", DurationSeconds: 5, SizeBytes: 101, Width: 512, Height: 512}, "wide": {ID: "wide", Kind: "image", SizeBytes: 20, Width: 2048, Height: 512}, "tall": {ID: "tall", Kind: "image", SizeBytes: 20, Width: 512, Height: 2048}, "video": {ID: "video", Kind: "video", DurationSeconds: 6, SizeBytes: 50, Width: 512, Height: 512}, "audio": {ID: "audio", Kind: "audio", DurationSeconds: 5, SizeBytes: 20}}
	s := New(db, config.Config{}, m)
	s.gateway, _ = media.NewConnector(media.Config{Origin: "https://gateway.example", Keys: map[string]string{"k": "test-secret"}, VideoCredential: "k"})
	for _, ids := range [][]string{{"long"}, {"large"}, {"wide"}, {"tall"}, {"video", "audio"}, {"video", "video"}, {""}} {
		in := QuoteInput{ProjectID: project, ProductID: "bounded-video", Prompt: "A test", ReferenceAssetIDs: ids}
		rec := doRequest(s, user, "POST", "/api/v1/quotes", in)
		if rec.Code != 422 {
			t.Fatalf("%v must be rejected before freezing: %d %s", ids, rec.Code, rec.Body.String())
		}
	}
	rec := doRequest(s, user, "POST", "/api/v1/quotes", QuoteInput{ProjectID: project, ProductID: "bounded-video", Prompt: "A test", ReferenceAssetIDs: []string{"video"}})
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	assertBalance(t, db, user, 100, 0)
	if _, err = db.Exec(context.Background(), "UPDATE projects SET status='archived' WHERE id=$1", project); err != nil {
		t.Fatal(err)
	}
	rec = doRequest(s, user, "POST", "/api/v1/quotes", QuoteInput{ProjectID: project, ProductID: "bounded-video", Prompt: "A test"})
	if rec.Code != 404 {
		t.Fatal("archived project accepted new quote", rec.Code)
	}
}

func TestPostgresExpiredArchiveDownloadFailureNeedsReview(t *testing.T) {
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	seedRealProduct(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer srv.Close()
	s := New(db, config.Config{TaskReviewAfter: time.Hour}, nil)
	s.gateway, _ = media.NewConnector(media.Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"original": "secret"}, ImageCredential: "original"})
	q := createQuote(t, s, user, project, "real-image")
	rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "archive-expiry-test"})
	job := data[Task](t, rec)
	outputs, _ := json.Marshal([]media.Output{{URL: srv.URL + "/expired.png"}})
	_, err := db.Exec(context.Background(), "UPDATE generation_tasks SET execution_status='completed',delivery_status='archive_retry',created_at=now()-interval '2 hours',outputs=$2 WHERE id=$1", job.ID, outputs)
	if err != nil {
		t.Fatal(err)
	}
	s.process(context.Background(), runnable(t, s, job.ID))
	got, err := s.getTask(context.Background(), user, job.ID, false)
	if err != nil || got.ErrorCode != "archive_review" || got.ProcurementStatus != "disputed" {
		t.Fatal(got, err)
	}
	if _, err = s.claim(context.Background()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("expired archive kept retrying", err)
	}
	assertBalance(t, db, user, 90, 10)
}
