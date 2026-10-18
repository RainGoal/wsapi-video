package commerce

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/media"
	"wsapi-video/internal/settings"
	"wsapi-video/internal/storage"
)

func TestPostgresImageArchiveUsesCurrentOutputPolicyAndOriginalCredential(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	seedRealProduct(t, db)
	var creates, polls, downloads, replacements atomic.Int32
	png := demoPNG("current-output-policy", "16:9")
	output := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/result.png" || r.Header.Get("Authorization") != "" {
			t.Error("image download changed its output path or leaked a gateway credential")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer output.Close()
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer original-image-secret" {
			t.Error("accepted image task lost its original credential")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/generations/async":
			creates.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "original-image-job", "status": "processing"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/tasks/original-image-job":
			polls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "original-image-job", "status": "completed", "model": "fixture-model",
				"result": map[string]any{"data": []media.Output{{URL: output.URL + "/result.png"}}},
			})
		default:
			t.Error("unexpected gateway request", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer provider.Close()
	replacement := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replacements.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer replacement.Close()
	previousTransport := http.DefaultTransport
	transport := previousTransport.(*http.Transport).Clone()
	pool := x509.NewCertPool()
	for _, server := range []*httptest.Server{provider, output, replacement} {
		pool.AddCert(server.Certificate())
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previousTransport; transport.CloseIdleConnections() }()

	// Runtime production readiness uses dummy S3 fields; archive bytes are written
	// only to this test's real temporary local store, never to an external service.
	cfg := config.Config{
		PublicURL: "https://studio.example", AppSecret: strings.Repeat("image-output-policy", 3),
		StorageDriver: "s3", SMTPPort: 587, S3Region: "auto", MaxMediaBytes: 8 << 20,
		EasyPayNotifyURL: "https://studio.example/api/v1/payment/notify/easypay", EasyPayReturnURL: "https://studio.example/wallet",
	}
	runtime := settings.New(db, cfg)
	save := func(change func(*settings.Settings)) {
		t.Helper()
		st, err := runtime.Current(ctx)
		if err != nil {
			t.Fatal(err)
		}
		u := settings.Update{Revision: st.Revision, Settings: st.Settings, Reason: "Exercise output policy repair without replacing accepted generation"}
		change(&u.Settings)
		if err = runtime.Save(ctx, user, u); err != nil {
			t.Fatal(err)
		}
	}
	save(func(v *settings.Settings) {
		v.Media.Origin, v.Media.ImageKey = provider.URL, "original-image-secret"
		v.Storage = settings.Storage{Driver: "s3", Endpoint: "https://storage.fixture.example", Region: "auto", Bucket: "image-policy-fixture", AccessKey: "fixture-access", SecretKey: "fixture-secret"}
	})
	local := config.Config{StorageDriver: "local", StorageDir: t.TempDir(), MaxUploadBytes: 8 << 20, MaxMediaBytes: 8 << 20}
	blobs, err := storage.New(local)
	if err != nil {
		t.Fatal(err)
	}
	archive := assets.New(db, local, blobs)
	s := New(db, cfg, archive)
	s.SetRuntimeSettings(runtime)
	quote := createQuote(t, s, user, project, "real-image")
	rec := doRequest(s, user, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": "repair-old-image-output-policy"})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	original, err := s.getTask(ctx, user, data[Task](t, rec).ID, false)
	if err != nil {
		t.Fatal(err)
	}
	s.process(ctx, runnable(t, s, original.ID))
	save(func(v *settings.Settings) {
		v.Media.Origin, v.Media.ImageKey = replacement.URL, "replacement-image-secret"
	})
	s.process(ctx, runnable(t, s, original.ID))
	got, err := s.getTask(ctx, user, original.ID, false)
	if err != nil || got.ExecutionStatus != "completed" || got.DeliveryStatus != "archive_retry" || got.SettlementStatus != "frozen" || got.ErrorCode != "archive_retry" || !strings.Contains(got.ErrorMessage, "白名单") || len(got.AssetIDs) != 0 {
		t.Fatal("denied output was not held with an actionable archive error", got, err)
	}
	if creates.Load() != 1 || polls.Load() != 1 || downloads.Load() != 0 || replacements.Load() != 0 {
		t.Fatal("pending archive changed provider, recreated generation or downloaded an unapproved origin", creates.Load(), polls.Load(), downloads.Load(), replacements.Load())
	}
	assertBalance(t, db, user, 90, 10)

	save(func(v *settings.Settings) { v.Media.OutputOrigins = []string{output.URL} })
	active := runnable(t, s, original.ID)
	s.process(ctx, active)
	s.process(ctx, active) // A retired worker cannot duplicate its archive or settlement.
	got, err = s.getTask(ctx, user, original.ID, false)
	if err != nil || got.DeliveryStatus != "ready" || got.SettlementStatus != "consumed" || len(got.AssetIDs) != 1 || got.CredentialRef != original.CredentialRef || got.UpstreamID != "original-image-job" || got.Credits != original.Credits || digest(got.Input) != digest(original.Input) || digest(got.Product) != digest(original.Product) {
		t.Fatal("current allowlist did not recover the unchanged accepted image", got, err)
	}
	saved, err := archive.Archived(ctx, user, project, original.ID)
	if err != nil || saved.ID != got.AssetIDs[0] {
		t.Fatal("recovered image was not persisted as the task asset", saved, err)
	}
	body, err := blobs.Open(ctx, saved.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	storedBytes, readErr := io.ReadAll(body)
	_ = body.Close()
	if readErr != nil || !bytes.Equal(storedBytes, png) {
		t.Fatal("archived bytes changed", readErr)
	}
	var stored, freezes, consumed, released int
	err = db.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM assets WHERE task_id=$1),
 (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='freeze'),
 (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='consumed'),
 (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='released')`, original.ID).Scan(&stored, &freezes, &consumed, &released)
	if err != nil || stored != 1 || freezes != 1 || consumed != 1 || released != 0 || creates.Load() != 1 || polls.Load() != 1 || downloads.Load() != 1 || replacements.Load() != 0 {
		t.Fatal("policy repair duplicated work or settlement", stored, freezes, consumed, released, creates.Load(), polls.Load(), downloads.Load(), replacements.Load(), err)
	}
	assertBalance(t, db, user, 90, 0)

	save(func(v *settings.Settings) { v.Media.OutputOrigins = nil })
	oldGateway, err := runtime.Gateway(ctx, original.CredentialRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = oldGateway.OpenImage(ctx, media.Output{URL: output.URL + "/result.png"}); !errors.Is(err, media.ErrOutputOriginDenied) || downloads.Load() != 1 {
		t.Fatal("removing the output origin failed to revoke an old credential's download permission", err, downloads.Load())
	}
}
