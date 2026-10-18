package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wsapi-video/internal/commerce"
	"wsapi-video/internal/config"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/settings"
	"wsapi-video/internal/storage"
)

// Runtime settings are exercised through authenticated HTTP and real Postgres,
// with TLS protocol fixtures only. No external provider, money or email is used.
func TestRuntimeSettingsRotateWithoutStrandingAcceptedWork(t *testing.T) {
	db := serverIntegrationDB(t)
	limitedConfig := db.Config()
	limitedConfig.MaxConns = 1
	limitedConfig.MinConns = 0
	limited, err := pgxpool.NewWithConfig(context.Background(), limitedConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(limited.Close)
	db = limited
	var originalMediaCalls, newProviderCalls, paymentQueries atomic.Int64
	png := fixturePNG(t)
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations/async":
			if r.Header.Get("Authorization") != "Bearer fixture-image-secret-old" {
				t.Error("accepted work lost original image credential")
				w.WriteHeader(401)
				return
			}
			originalMediaCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "fixture-image-task", "status": "queued"})
		case "/v1/images/tasks/fixture-image-task":
			if r.Header.Get("Authorization") != "Bearer fixture-image-secret-old" {
				t.Error("polling lost original credential")
				w.WriteHeader(401)
				return
			}
			originalMediaCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture-image-task", "status": "completed", "result": map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(png)}}}})
		case "/api.php":
			if r.URL.Query().Get("key") != "fixture-payment-secret-old" || r.URL.Query().Get("pid") != "merchant-old" {
				t.Error("accepted order lost original merchant credential")
				w.WriteHeader(401)
				return
			}
			paymentQueries.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "status": 1, "money": "10.00", "trade_no": "fixture-paid-trade", "out_trade_no": r.URL.Query().Get("out_trade_no"), "pid": "merchant-old"})
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer provider.Close()
	replacement := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { newProviderCalls.Add(1); w.WriteHeader(503) }))
	defer replacement.Close()
	// Trust only these local TLS fixtures; application HTTP clients retain normal
	// certificate validation and redirects remain disabled.
	previousTransport := http.DefaultTransport
	transport := previousTransport.(*http.Transport).Clone()
	pool := x509.NewCertPool()
	pool.AddCert(provider.Certificate())
	pool.AddCert(replacement.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previousTransport; transport.CloseIdleConnections() }()
	httpServer := httptest.NewUnstartedServer(nil)
	origin := "http://" + httpServer.Listener.Addr().String()
	cfg := config.Config{Env: "test", DevMode: true, PublicURL: origin, AppSecret: strings.Repeat("x", 40), StorageDriver: "local", StorageDir: t.TempDir(), WebDir: t.TempDir(), MaxUploadBytes: 8 << 20, MaxMediaBytes: 8 << 20, SessionTTL: time.Hour, SMTPPort: 587, S3Region: "auto", ImageCredential: "image-env", VideoCredential: "video-env", EasyPayCredentialRef: "payment-env", EasyPayNotifyURL: origin + "/api/v1/payment/notify/easypay", EasyPayReturnURL: origin + "/wallet", MediaPollInterval: 20 * time.Millisecond, WorkerLease: 2 * time.Minute, TaskReviewAfter: time.Hour}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg, db, blobs)
	httpServer.Config.Handler = app.HTTP.Handler
	httpServer.Start()
	defer httpServer.Close()
	if err = app.Auth.BootstrapAdmin(context.Background(), "settings-admin@example.test", "Admin-settings-password-2026"); err != nil {
		t.Fatal(err)
	}
	admin := newStudioClient(t, origin)
	admin.login("settings-admin@example.test", "Admin-settings-password-2026")
	creator := newStudioClient(t, origin)
	creator.registerAndLogin("settings-user@example.test", "Creator-settings-password-2026")
	creator.json("GET", "/api/v1/admin/settings", nil, 403)
	v := serverData[settings.View](t, admin.json("GET", "/api/v1/admin/settings", nil, 200))
	u := settings.Update{Revision: v.Revision, Reason: "Configure local TLS provider fixtures", Settings: v.Settings}
	u.Settings.Media.Origin = provider.URL
	u.Settings.Media.ImageKey = "fixture-image-secret-old"
	u.Settings.Media.VideoKey = "fixture-video-secret-old"
	u.Settings.Payment.Enabled = true
	u.Settings.Payment.Origin = provider.URL
	u.Settings.Payment.PID = "merchant-old"
	u.Settings.Payment.Key = "fixture-payment-secret-old"
	raw := admin.json("PUT", "/api/v1/admin/settings", u, 200)
	for _, secret := range []string{u.Settings.Media.ImageKey, u.Settings.Media.VideoKey, u.Settings.Payment.Key} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("settings endpoint leaked credential")
		}
	}
	v = serverData[settings.View](t, raw)
	if v.Revision != 1 || !v.Readiness.Payment || !v.Readiness.Media || !v.Settings.Media.ImageKeyConfigured {
		t.Fatal("saved settings not active", v)
	}
	probe := serverData[struct {
		OK bool `json:"ok"`
	}](t, admin.json("POST", "/api/v1/admin/settings/test/media", map[string]any{}, 200))
	if !probe.OK {
		t.Fatal("saved gateway was not used by read-only probe")
	}
	admin.json("POST", "/api/v1/admin/settings/test/storage", map[string]any{}, 200)
	var encrypted, audit []byte
	if err = db.QueryRow(context.Background(), "SELECT payload FROM runtime_settings").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(context.Background(), "SELECT details FROM admin_audit WHERE action='settings.update' ORDER BY created_at DESC LIMIT 1").Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("fixture-image-secret-old")) || bytes.Contains(audit, []byte("fixture-image-secret-old")) {
		t.Fatal("plaintext credential persisted")
	}
	admin.json("PUT", "/api/v1/admin/settings", u, 409)
	bad := settings.Update{Revision: v.Revision, Reason: "Reject unsafe gateway URLs", Settings: v.Settings}
	bad.Settings.Media.Origin = "http://unsafe.example"
	badRaw := admin.json("PUT", "/api/v1/admin/settings", bad, 422)
	if !bytes.Contains(badRaw, []byte("media.origin")) {
		t.Fatal("missing field validation")
	}
	admin.json("PUT", "/api/v1/admin/model-products/runtime-image", map[string]any{"name": "Runtime image", "kind": "image", "model": "fixture-image", "credits": 10, "capabilities": commerce.Capabilities{}, "active": true, "verified": true, "version": 0, "reason": "Test runtime configured product"}, 200)
	admin.json("PUT", "/api/v1/admin/payment/tiers/starter", map[string]any{"name": "Starter", "amount_fen": 1000, "credits": 1000, "active": true, "version": 1, "reason": "Test runtime payment merchant"}, 200)
	admin.json("POST", "/api/v1/admin/wallet/adjust", map[string]any{"user_id": creator.user.ID, "amount": 100, "request_key": "runtime-settings-test-credit", "reason": "Runtime configuration integration credits"}, 200)
	p := serverData[projects.Project](t, creator.json("POST", "/api/v1/projects", map[string]string{"name": "Runtime provider snapshot"}, 201))
	shots := serverData[projects.ShotCollection](t, creator.json("PUT", "/api/v1/projects/"+p.ID+"/shots", projects.ShotCollection{Revision: 0, Shots: []projects.Shot{{ID: "runtime-shot", Number: 1, Title: "Runtime shot"}}}, 200))
	otherProject := serverData[projects.Project](t, creator.json("POST", "/api/v1/projects", map[string]string{"name": "Other project"}, 201))
	creator.json("POST", "/api/v1/quotes", commerce.QuoteInput{ProjectID: otherProject.ID, ShotID: "runtime-shot", ProductID: "runtime-image", Prompt: "Cross project shot"}, 422)
	staleQuote := serverData[commerce.Quote](t, creator.json("POST", "/api/v1/quotes", commerce.QuoteInput{ProjectID: p.ID, ShotID: "runtime-shot", ProductID: "runtime-image", Prompt: "Deleted after quotation"}, 201))
	shots = serverData[projects.ShotCollection](t, creator.json("PUT", "/api/v1/projects/"+p.ID+"/shots", projects.ShotCollection{Revision: shots.Revision, Shots: []projects.Shot{}}, 200))
	creator.json("POST", "/api/v1/tasks", map[string]string{"quote_id": staleQuote.ID, "request_key": "deleted-shot-after-quote"}, 422)
	creator.json("PUT", "/api/v1/projects/"+p.ID+"/shots", projects.ShotCollection{Revision: shots.Revision, Shots: []projects.Shot{{ID: "runtime-shot", Number: 1, Title: "Runtime shot"}}}, 200)
	q := serverData[commerce.Quote](t, creator.json("POST", "/api/v1/quotes", commerce.QuoteInput{ProjectID: p.ID, ShotID: "runtime-shot", ProductID: "runtime-image", Prompt: "Test fixture"}, 201))
	task := serverData[commerce.Task](t, creator.json("POST", "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "runtime-original-task"}, 201))
	if task.Input.ShotID != "runtime-shot" {
		t.Fatal("shot association was not persisted in accepted task input")
	}
	order := serverData[commerce.Order](t, creator.json("POST", "/api/v1/payment/orders", map[string]string{"tier_id": "starter", "method": "alipay"}, 201))
	if !strings.HasPrefix(order.CheckoutURL, provider.URL+"/submit.php?") {
		t.Fatal("runtime payment merchant was not used")
	}
	// Rotate everything and disable new checkout before processing old work.
	u = settings.Update{Revision: v.Revision, Reason: "Rotate providers preserving accepted work", Settings: v.Settings}
	u.Settings.Site.Brand = "Runtime Studio"
	u.Settings.Site.RegistrationEnabled = false
	u.Settings.Media.Origin = replacement.URL
	u.Settings.Media.ImageKey = "fixture-image-secret-new"
	u.Settings.Media.VideoKey = "fixture-video-secret-new"
	u.Settings.Payment.Enabled = false
	u.Settings.Payment.Origin = replacement.URL
	u.Settings.Payment.PID = "merchant-new"
	u.Settings.Payment.Key = "fixture-payment-secret-new"
	v = serverData[settings.View](t, admin.json("PUT", "/api/v1/admin/settings", u, 200))
	system := serverData[struct {
		Brand        string `json:"brand"`
		Registration bool   `json:"registration_enabled"`
		Payment      bool   `json:"payment_enabled"`
	}](t, admin.json("GET", "/api/v1/system", nil, 200))
	if system.Brand != "Runtime Studio" || system.Registration || system.Payment {
		t.Fatal("site/payment runtime flags did not take effect", system)
	}
	newStudioClient(t, origin).json("POST", "/api/v1/auth/register", map[string]string{"email": "closed@example.test", "password": "Closed-account-password"}, 503)
	creator.json("POST", "/api/v1/payment/orders", map[string]string{"tier_id": "starter", "method": "alipay"}, 503)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { app.Commerce.Run(ctx); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("workers failed to stop")
		}
	}()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		task = serverData[commerce.Task](t, creator.json("GET", "/api/v1/tasks/"+task.ID, nil, 200))
		if task.DeliveryStatus == "ready" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.DeliveryStatus != "ready" || task.SettlementStatus != "consumed" {
		t.Fatalf("original work was stranded by rotation: %#v", task)
	}
	order = serverData[commerce.Order](t, creator.json("POST", "/api/v1/payment/orders/"+order.ID+"/check", map[string]any{}, 200))
	if order.CreditStatus != "credited" || paymentQueries.Load() != 1 || originalMediaCalls.Load() < 2 || newProviderCalls.Load() != 0 {
		t.Fatal("old work used a new provider or was not confirmed", order, paymentQueries.Load(), originalMediaCalls.Load(), newProviderCalls.Load())
	}
	wallet := serverData[commerce.Wallet](t, creator.json("GET", "/api/v1/wallet", nil, 200))
	if wallet.Available != 1090 || wallet.Frozen != 0 {
		t.Fatal("wrong settlement after configuration rotation", wallet)
	}
	// Blank credentials preserve current values, while clear_secrets is explicit.
	u = settings.Update{Revision: v.Revision, Reason: "Preserve omitted credentials", Settings: v.Settings}
	v = serverData[settings.View](t, admin.json("PUT", "/api/v1/admin/settings", u, 200))
	if !v.Settings.Media.ImageKeyConfigured {
		t.Fatal("blank credential erased existing secret")
	}
	u = settings.Update{Revision: v.Revision, Reason: "Clear only current image credential", Settings: v.Settings, ClearSecrets: []string{"media.image_key"}}
	v = serverData[settings.View](t, admin.json("PUT", "/api/v1/admin/settings", u, 200))
	if v.Settings.Media.ImageKeyConfigured || !v.Settings.Media.VideoKeyConfigured {
		t.Fatal("explicit secret clear failed")
	}
	u = settings.Update{Revision: v.Revision, Reason: "Prevent inaccessible asset migration", Settings: v.Settings}
	u.Settings.Storage.Driver = "s3"
	u.Settings.Storage.Bucket = "different-bucket"
	u.Settings.Storage.AccessKey = "fixture-access"
	u.Settings.Storage.SecretKey = "fixture-secret"
	admin.json("PUT", "/api/v1/admin/settings", u, 422)
	// A different master key cannot silently fall back to environment defaults.
	wrong := cfg
	wrong.AppSecret = strings.Repeat("wrong", 8)
	if _, err = settings.New(db, wrong).Current(context.Background()); err == nil {
		t.Fatal("wrong master key accepted")
	}
}
