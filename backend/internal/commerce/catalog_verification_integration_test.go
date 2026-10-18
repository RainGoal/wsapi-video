package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"wsapi-video/internal/config"
)

type catalogVerificationGateway struct{ gatewayClient }

func (catalogVerificationGateway) Credential(string) (string, error) { return "fixture", nil }

type catalogUnavailableGateway struct{ gatewayClient }

func (catalogUnavailableGateway) Credential(string) (string, error) {
	return "", errors.New("fixture generation credential unavailable")
}

func catalogVerificationService(t *testing.T) (*Service, string) {
	t.Helper()
	db := integrationDB(t)
	s := New(db, config.Config{}, nil)
	s.gateway = catalogVerificationGateway{}
	return s, seedUser(t, db, 1000)
}

func catalogProductBody(p Product, reason string) map[string]any {
	raw, _ := json.Marshal(p)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	body["reason"] = reason
	return body
}

func saveVerifiedCatalogProduct(t *testing.T, s *Service, admin, id, kind string) Product {
	t.Helper()
	p := Product{ID: id, Name: "Verified offer", Kind: kind, Model: "original-model", Credits: 10, Active: true, Verified: true}
	body := catalogProductBody(p, "Manually verify initial model capabilities")
	rec := doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, body)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	return data[Product](t, rec)
}

func TestPostgresCatalogCapabilityChangesRequireFreshVerification(t *testing.T) {
	s, admin := catalogVerificationService(t)
	changes := map[string]func(*Product){
		"kind":                           func(p *Product) { p.Kind = "image" },
		"model":                          func(p *Product) { p.Model = "replacement-model" },
		"aspect_ratios":                  func(p *Product) { p.Capabilities.AspectRatios = []string{"16:9"} },
		"durations":                      func(p *Product) { p.Capabilities.Durations = []int{7} },
		"resolutions":                    func(p *Product) { p.Capabilities.Resolutions = []string{"720p"} },
		"sizes":                          func(p *Product) { p.Capabilities.Sizes = []string{"1024x1024"} },
		"qualities":                      func(p *Product) { p.Capabilities.Qualities = []string{"high"} },
		"max_images":                     func(p *Product) { p.Capabilities.MaxImages = 2 },
		"max_videos":                     func(p *Product) { p.Capabilities.MaxVideos = 1 },
		"max_audios":                     func(p *Product) { p.Capabilities.MaxAudios = 1 },
		"supports_audio":                 func(p *Product) { p.Capabilities.SupportsAudio = true },
		"supports_frames":                func(p *Product) { p.Capabilities.SupportsFrames = true },
		"supports_cancel":                func(p *Product) { p.Capabilities.SupportsCancel = true },
		"max_reference_duration_seconds": func(p *Product) { p.Capabilities.MaxReferenceDurationSeconds = 60 },
		"max_reference_bytes":            func(p *Product) { p.Capabilities.MaxReferenceBytes = 100000 },
		"max_reference_width":            func(p *Product) { p.Capabilities.MaxReferenceWidth = 1920 },
		"max_reference_height":           func(p *Product) { p.Capabilities.MaxReferenceHeight = 1080 },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			kind := "video"
			if name == "sizes" || name == "qualities" {
				kind = "image"
			}
			original := saveVerifiedCatalogProduct(t, s, admin, "verification-"+name, kind)
			before := catalogWalletSnapshot(t, s.db)
			changed := original
			change(&changed)
			// A stale client may carry both original checkboxes as true. The
			// server must persist this new definition as unverified and offline.
			body := catalogProductBody(changed, "Change the previously verified model definition")
			rec := doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+changed.ID, body)
			if rec.Code != http.StatusOK {
				t.Fatal(rec.Code, rec.Body.String())
			}
			stored := data[Product](t, rec)
			if stored.Active || stored.Verified || stored.Version != original.Version+1 {
				t.Fatalf("changed definition retained verification: %+v", stored)
			}
			body["active"], body["verified"] = false, false
			assertCatalogRecord(t, s.db, "model_products", changed.ID, body, 2)
			assertCatalogAudit(t, s.db, "product.save", changed.ID, admin, body, 2)
			assertCatalogPublicProduct(t, s, admin, changed.ID, false)
			stale := doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+changed.ID, catalogProductBody(changed, "Stale verification must not overwrite changed definition"))
			if stale.Code != http.StatusConflict {
				t.Fatal("stale verification was accepted", stale.Code, stale.Body.String())
			}
			stored.Verified, stored.Active = true, true
			body = catalogProductBody(stored, "Manually verify the separately saved new definition")
			rec = doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+changed.ID, body)
			if rec.Code != http.StatusOK {
				t.Fatal(rec.Code, rec.Body.String())
			}
			reverified := data[Product](t, rec)
			if !reverified.Verified || !reverified.Active || reverified.Version != 3 {
				t.Fatal("separate verification could not publish new definition", reverified)
			}
			assertCatalogAudit(t, s.db, "product.save", changed.ID, admin, body, 3)
			assertCatalogPublicProduct(t, s, admin, changed.ID, true)
			if catalogWalletSnapshot(t, s.db) != before {
				t.Fatal("catalog verification changed wallet accounts or ledger")
			}
		})
	}
}

func assertCatalogPublicProduct(t *testing.T, s *Service, admin, id string, want bool) {
	t.Helper()
	rec := doRequest(s, admin, http.MethodGet, "/api/v1/model-products", nil)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	listed := false
	for _, p := range data[struct{ Items []Product }](t, rec).Items {
		listed = listed || p.ID == id
	}
	if listed != want {
		t.Fatalf("public product visibility=%v, want %v", listed, want)
	}
}

func TestPostgresCatalogPriceAndEquivalentCapabilitiesKeepVerification(t *testing.T) {
	s, admin := catalogVerificationService(t)
	p := saveVerifiedCatalogProduct(t, s, admin, "equivalent-capabilities", "image")
	p.Name, p.Credits = "Renamed offer", 31
	p.Capabilities = Capabilities{AspectRatios: []string{}, Durations: []int{}, Resolutions: []string{}, Sizes: []string{}, Qualities: []string{}}
	body := catalogProductBody(p, "Change commercial terms without changing verified capabilities")
	rec := doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, body)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	stored := data[Product](t, rec)
	if !stored.Active || !stored.Verified || stored.Version != 2 || stored.Credits != 31 || stored.Name != p.Name {
		t.Fatal("equivalent nil and empty choices or price change invalidated verification", stored)
	}
	assertCatalogAudit(t, s.db, "product.save", p.ID, admin, body, 2)
	assertCatalogPublicProduct(t, s, admin, p.ID, true)
	// Array order defines the displayed default, so reordering is a definition
	// change even when the available choices contain the same strings.
	stored.Capabilities.AspectRatios = []string{"1:1", "16:9"}
	rec = doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, catalogProductBody(stored, "Save new ordered aspect ratio choices"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	stored = data[Product](t, rec)
	stored.Active, stored.Verified = true, true
	rec = doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, catalogProductBody(stored, "Verify the new ordered aspect ratio choices"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	stored = data[Product](t, rec)
	stored.Capabilities.AspectRatios = []string{"16:9", "1:1"}
	rec = doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, catalogProductBody(stored, "Change which ratio is the default option"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	stored = data[Product](t, rec)
	if stored.Active || stored.Verified {
		t.Fatal("reordered default option retained old verification", stored)
	}
}

func TestPostgresCatalogVerificationAuditFailureRollsBack(t *testing.T) {
	s, admin := catalogVerificationService(t)
	p := saveVerifiedCatalogProduct(t, s, admin, "audit-rollback", "image")
	before := catalogWalletSnapshot(t, s.db)
	ctx := context.Background()
	_, err := s.db.Exec(ctx, `CREATE FUNCTION reject_product_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture audit unavailable'; END $$;
CREATE TRIGGER reject_product_audit BEFORE INSERT ON admin_audit FOR EACH ROW EXECUTE FUNCTION reject_product_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	changed := p
	changed.Model = "replacement-model"
	rec := doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, catalogProductBody(changed, "Attempt an unauditable definition change"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatal("audit outage did not fail save", rec.Code, rec.Body.String())
	}
	actual, err := scanProduct(s.db.QueryRow(ctx, "SELECT "+productColumns+" FROM model_products WHERE id=$1", p.ID))
	if err != nil || !reflect.DeepEqual(actual, p) {
		t.Fatal("audit failure partially changed the product", actual, err)
	}
	if catalogWalletSnapshot(t, s.db) != before {
		t.Fatal("failed product save changed wallet accounts or ledger")
	}
}

func TestPostgresCatalogDefinitionChangesPreserveAcceptedSnapshots(t *testing.T) {
	s, admin := catalogVerificationService(t)
	p := saveVerifiedCatalogProduct(t, s, admin, "snapshot-preservation", "image")
	project := seedProject(t, s.db, admin)
	q := createQuote(t, s, admin, project, p.ID)
	rec := doRequest(s, admin, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "catalog-original-task"})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	originalTask := data[Task](t, rec)
	ctx := context.Background()
	var originalQuote, originalTaskRow string
	if err := s.db.QueryRow(ctx, "SELECT to_jsonb(q)::text FROM quotes q WHERE id=$1", q.ID).Scan(&originalQuote); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM generation_tasks t WHERE id=$1", originalTask.ID).Scan(&originalTaskRow); err != nil {
		t.Fatal(err)
	}
	before := catalogWalletSnapshot(t, s.db)
	p.Model, p.Credits = "replacement-model", 57
	rec = doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, catalogProductBody(p, "Replace the retail model while accepted work retains its snapshot"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var quoteAfter, taskAfter string
	if err := s.db.QueryRow(ctx, "SELECT to_jsonb(q)::text FROM quotes q WHERE id=$1", q.ID).Scan(&quoteAfter); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM generation_tasks t WHERE id=$1", originalTask.ID).Scan(&taskAfter); err != nil {
		t.Fatal(err)
	}
	if quoteAfter != originalQuote || taskAfter != originalTaskRow || catalogWalletSnapshot(t, s.db) != before {
		t.Fatal("product edit changed accepted snapshots or wallet state")
	}
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "catalog-original-task"})
	if rec.Code != http.StatusOK || !reflect.DeepEqual(data[Task](t, rec), originalTask) {
		t.Fatal("accepted task retry changed after product edit", rec.Code, rec.Body.String())
	}
	// New quotes must be refused while the replacement is awaiting verification.
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/quotes", QuoteInput{ProjectID: project, ProductID: p.ID, Prompt: "A new request"})
	if rec.Code != http.StatusNotFound {
		t.Fatal("unverified replacement could still be quoted", rec.Code, rec.Body.String())
	}
}

func TestPostgresCatalogDefinitionCanBeSavedOfflineWithoutCredential(t *testing.T) {
	s, admin := catalogVerificationService(t)
	p := saveVerifiedCatalogProduct(t, s, admin, "offline-definition", "image")
	s.gateway = catalogUnavailableGateway{}
	p.Model = "replacement-model"
	body := catalogProductBody(p, "Save replacement definition before restoring generation credentials")
	rec := doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, body)
	if rec.Code != http.StatusOK {
		t.Fatal("offline definition change incorrectly required publishing credentials", rec.Code, rec.Body.String())
	}
	stored := data[Product](t, rec)
	if stored.Active || stored.Verified {
		t.Fatal("replacement was published without credentials", stored)
	}
	body["active"], body["verified"] = false, false
	assertCatalogAudit(t, s.db, "product.save", p.ID, admin, body, 2)
	stored.Active, stored.Verified = true, true
	rec = doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, catalogProductBody(stored, "Publishing still requires valid generation credentials"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatal("separate verification bypassed the credential gate", rec.Code, rec.Body.String())
	}
	assertCatalogRecord(t, s.db, "model_products", p.ID, body, 2)
	assertCatalogAudit(t, s.db, "product.save", p.ID, admin, body, 2)
}
