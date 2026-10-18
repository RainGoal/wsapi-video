package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wsapi-video/internal/config"
	"wsapi-video/internal/media"
	"wsapi-video/internal/settings"
)

func TestPostgresCatalogPublishingUsesRuntimeCredentialWithoutStorageGate(t *testing.T) {
	db := integrationDB(t)
	admin := seedUser(t, db, 1000)
	ctx := context.Background()
	cfg := config.Config{
		PublicURL: "https://studio.example", AppSecret: strings.Repeat("catalog-runtime", 4),
		StorageDriver: "s3", SetupMode: true, SMTPPort: 587, S3Region: "auto",
		EasyPayNotifyURL: "https://studio.example/api/v1/payment/notify/easypay", EasyPayReturnURL: "https://studio.example/wallet",
	}
	runtime := settings.New(db, cfg)
	st, err := runtime.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u := settings.Update{Revision: st.Revision, Settings: st.Settings, Reason: "Configure only the image credential before private storage setup"}
	u.Settings.Media.Origin, u.Settings.Media.ImageKey = "https://gateway.example", "fixture-runtime-image-secret"
	if err = runtime.Save(ctx, admin, u); err != nil {
		t.Fatal(err)
	}
	s := New(db, cfg, nil)
	s.SetRuntimeSettings(runtime)
	p := saveVerifiedCatalogProduct(t, s, admin, "runtime-image-without-storage", "image")
	if !p.Active || !p.Verified {
		t.Fatal("configured runtime image credential could not publish a product", p)
	}
	video := Product{ID: "runtime-video-missing-key", Name: "Video draft", Kind: "video", Model: "fixture-video", Credits: 20, Active: true, Verified: true}
	rec := doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+video.ID, catalogProductBody(video, "Publishing video still requires its own credential"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatal("image credential incorrectly enabled video publication", rec.Code, rec.Body.String())
	}
	video.Active, video.Verified = false, false
	rec = doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+video.ID, catalogProductBody(video, "Save an offline draft before a video credential exists"))
	if rec.Code != http.StatusOK {
		t.Fatal("offline draft incorrectly required a generation credential", rec.Code, rec.Body.String())
	}
	before := catalogWalletSnapshot(t, db)
	assertStorageUnavailable := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		var failure struct{ Code, Message string }
		if err := json.Unmarshal(rec.Body.Bytes(), &failure); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusServiceUnavailable || failure.Code != "storage_unavailable" || failure.Message != "私有存储尚未配置，请管理员在系统设置中完成私有存储配置后重试" {
			t.Fatal("missing storage was not reported accurately", rec.Code, rec.Body.String())
		}
		if catalogWalletSnapshot(t, db) != before {
			t.Fatal("storage readiness rejection changed the wallet or ledger")
		}
	}
	assertStorageUnavailable(doRequest(s, admin, http.MethodGet, "/api/v1/model-products", nil))
	if _, err = runtime.Gateway(ctx, ""); !errors.Is(err, media.ErrUnavailable) || !errors.Is(err, settings.ErrStorageUnavailable) {
		t.Fatal("production generation lost its storage readiness gate", err)
	}
	project := seedProject(t, db, admin)
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/quotes", QuoteInput{ProjectID: project, ProductID: p.ID, Prompt: "Storage is not configured"})
	assertStorageUnavailable(rec)
	// A quote created while a connector was previously available must not bypass
	// the current production storage gate when it is later submitted.
	s.gateway = catalogVerificationGateway{}
	q := createQuote(t, s, admin, project, p.ID)
	s.SetRuntimeSettings(runtime)
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "runtime-storage-unavailable"})
	assertStorageUnavailable(rec)
	// Configuration readiness checks and quoting do not contact these fixture
	// endpoints or write an object. Actual storage access is tested separately.
	st, err = runtime.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u = settings.Update{Revision: st.Revision, Settings: st.Settings, Reason: "Complete dummy private storage and restore image catalog availability"}
	u.Settings.Storage = settings.Storage{
		Driver: "s3", Endpoint: "https://storage.fixture.example", Region: "auto",
		Bucket: "catalog-fixture-private", AccessKey: "fixture-storage-access", SecretKey: "fixture-storage-secret",
	}
	if err = runtime.Save(ctx, admin, u); err != nil {
		t.Fatal(err)
	}
	assertCatalogPublicProduct(t, s, admin, p.ID, true)
	restored := createQuote(t, s, admin, project, p.ID)
	if restored.Credits != p.Credits || restored.ProductID != p.ID || catalogWalletSnapshot(t, db) != before {
		t.Fatal("completing storage did not restore the existing product and quote without a wallet change", restored)
	}
}
