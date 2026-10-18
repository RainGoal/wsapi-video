package commerce

import (
	"context"
	"errors"
	"net/http"
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
	assertCatalogPublicProduct(t, s, admin, p.ID, false)
	if _, err = runtime.Gateway(ctx, ""); !errors.Is(err, media.ErrUnavailable) {
		t.Fatal("production generation lost its storage readiness gate", err)
	}
	project := seedProject(t, db, admin)
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/quotes", QuoteInput{ProjectID: project, ProductID: p.ID, Prompt: "Storage is not configured"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatal("production quote bypassed the storage readiness gate", rec.Code, rec.Body.String())
	}
	// A quote created while a connector was previously available must not bypass
	// the current production storage gate when it is later submitted.
	s.gateway = catalogVerificationGateway{}
	q := createQuote(t, s, admin, project, p.ID)
	s.SetRuntimeSettings(runtime)
	before := catalogWalletSnapshot(t, db)
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "runtime-storage-unavailable"})
	if rec.Code != http.StatusServiceUnavailable || catalogWalletSnapshot(t, db) != before {
		t.Fatal("task acceptance bypassed storage readiness or changed the wallet", rec.Code, rec.Body.String())
	}
}
