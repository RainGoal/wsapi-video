package commerce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func savePricingTestProduct(t *testing.T, s *Service, admin, id, unit string) Product {
	t.Helper()
	p := Product{ID: id, Name: "Priced video", Kind: "video", Model: "fixture-video", Credits: 7, Active: true, Verified: true,
		Capabilities: Capabilities{Durations: []int{5, 12}, Resolutions: []string{"720p", "1080p"}}}
	if unit != "" {
		p.Pricing = &ProductPricing{BillingUnit: unit, ResolutionCredits: map[string]int64{"1080P": 13}}
	}
	return putPricingTestProduct(t, s, admin, p)
}

func putPricingTestProduct(t *testing.T, s *Service, admin string, p Product) Product {
	t.Helper()
	rec := doRequest(s, admin, http.MethodPut, "/api/v1/admin/model-products/"+p.ID, catalogProductBody(p, "Configure the retail pricing fixture"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	return data[Product](t, rec)
}

func requestPricingTestQuote(t *testing.T, s *Service, user, project, product string, duration int, resolution string) Quote {
	t.Helper()
	rec := doRequest(s, user, http.MethodPost, "/api/v1/quotes", QuoteInput{ProjectID: project, ProductID: product, Prompt: "A pricing fixture", Duration: duration, Resolution: resolution})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	return data[Quote](t, rec)
}

func TestPostgresProductPricingQuotesUseRequestedSpecifications(t *testing.T) {
	s, admin := catalogVerificationService(t)
	project := seedProject(t, s.db, admin)
	wallet := catalogWalletSnapshot(t, s.db)
	for _, unit := range []string{"per_video", "per_second"} {
		p := savePricingTestProduct(t, s, admin, "quote-"+unit, unit)
		for _, spec := range []struct {
			name, resolution string
			duration         int
			unitCredits      int64
			quantity         int64
		}{{"default-duration", "", 0, 7, 5}, {"default-price", "720p", 12, 7, 12}, {"resolution-price", "1080p", 12, 13, 12}} {
			t.Run(unit+"/"+spec.name, func(t *testing.T) {
				quantity := spec.quantity
				if unit == "per_video" {
					quantity = 1
				}
				q := requestPricingTestQuote(t, s, admin, project, p.ID, spec.duration, spec.resolution)
				want := spec.unitCredits * quantity
				if q.Credits != want || q.Pricing == nil || q.Pricing.BillingUnit != unit || q.Pricing.UnitCredits != spec.unitCredits || int64(q.Pricing.Quantity) != quantity || q.Pricing.TotalCredits != want {
					t.Fatalf("incorrect quote or breakdown: %+v, pricing=%+v; want %d", q, q.Pricing, want)
				}
				resolution := spec.resolution
				if resolution == "" {
					resolution = "720p"
				}
				if q.Pricing.Resolution != resolution {
					t.Fatal("breakdown lost the selected/default resolution", q.Pricing)
				}
				var storedCredits int64
				var rawProduct, rawInput []byte
				err := s.db.QueryRow(context.Background(), "SELECT credits,product_snapshot,input FROM quotes WHERE id=$1", q.ID).Scan(&storedCredits, &rawProduct, &rawInput)
				if err != nil {
					t.Fatal(err)
				}
				var frozenProduct Product
				var frozenInput QuoteInput
				if json.Unmarshal(rawProduct, &frozenProduct) != nil || json.Unmarshal(rawInput, &frozenInput) != nil || !reflect.DeepEqual(frozenProduct, p) || storedCredits != want || frozenInput.Duration != int(spec.quantity) || frozenInput.Resolution != resolution {
					t.Fatal("quote did not freeze the unit price and normalized request", frozenProduct, frozenInput, storedCredits)
				}
			})
		}
	}
	if catalogWalletSnapshot(t, s.db) != wallet {
		t.Fatal("requesting price breakdowns mutated the wallet or ledger")
	}
}

func TestPostgresProductPricingFrozenQuoteConcurrentAcceptance(t *testing.T) {
	s, admin := catalogVerificationService(t)
	project := seedProject(t, s.db, admin)
	p := savePricingTestProduct(t, s, admin, "frozen-retail-price", "per_second")
	q := requestPricingTestQuote(t, s, admin, project, p.ID, 5, "1080p")
	var originalQuote string
	if err := s.db.QueryRow(context.Background(), "SELECT to_jsonb(q)::text FROM quotes q WHERE id=$1", q.ID).Scan(&originalQuote); err != nil {
		t.Fatal(err)
	}
	changed := p
	changed.Credits = 17
	changed.Pricing = &ProductPricing{BillingUnit: "per_video", ResolutionCredits: map[string]int64{"1080p": 23}}
	stored := putPricingTestProduct(t, s, admin, changed)
	if !stored.Active || !stored.Verified || stored.Version != p.Version+1 || stored.Pricing == nil || stored.Pricing.BillingUnit != "per_video" {
		t.Fatal("commercial price update revoked verification or failed to persist", stored)
	}
	newQuote := requestPricingTestQuote(t, s, admin, project, p.ID, 5, "1080p")
	if q.Credits != 65 || newQuote.Credits != 23 {
		t.Fatal("new pricing did not separate old and new quotes", q.Credits, newQuote.Credits)
	}
	body := map[string]string{"quote_id": q.ID, "request_key": "concurrent-frozen-retail-price"}
	const attempts = 12
	responses := make(chan *httptest.ResponseRecorder, attempts)
	for i := 0; i < attempts; i++ {
		go func() { responses <- doRequest(s, admin, http.MethodPost, "/api/v1/tasks", body) }()
	}
	var task Task
	created := 0
	for i := 0; i < attempts; i++ {
		rec := <-responses
		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatal("concurrent pricing submission failed", rec.Code, rec.Body.String())
		}
		if rec.Code == http.StatusCreated {
			created++
		}
		got := data[Task](t, rec)
		if got.Credits != 65 || !reflect.DeepEqual(got.Product, p) || task.ID != "" && got.ID != task.ID {
			t.Fatal("accepted task lost the quoted total or original pricing", got)
		}
		task = got
	}
	var quoteAfter string
	if err := s.db.QueryRow(context.Background(), "SELECT to_jsonb(q)::text FROM quotes q WHERE id=$1", q.ID).Scan(&quoteAfter); err != nil || quoteAfter != originalQuote || created != 1 {
		t.Fatal("concurrent acceptance rewrote its quote or accepted more than once", created, err)
	}
	var freezes int
	if err := s.db.QueryRow(context.Background(), "SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='freeze'", task.ID).Scan(&freezes); err != nil || freezes != 1 {
		t.Fatal("concurrent acceptance froze the quoted total more than once", freezes, err)
	}
	assertBalance(t, s.db, admin, 935, 65)
	if _, err := s.db.Exec(context.Background(), "UPDATE quotes SET expires_at=now()-interval '1 hour' WHERE id=$1", q.ID); err != nil {
		t.Fatal(err)
	}
	rec := doRequest(s, admin, http.MethodPost, "/api/v1/tasks", body)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(data[Task](t, rec), task) {
		t.Fatal("accepted original pricing could not be recovered after expiry", rec.Code, rec.Body.String())
	}
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": newQuote.ID, "request_key": body["request_key"]})
	if rec.Code != http.StatusConflict {
		t.Fatal("accepted request key could be reused for a differently priced quote", rec.Code, rec.Body.String())
	}
	assertBalance(t, s.db, admin, 935, 65)
}

func TestPostgresProductPricingLegacyNilSnapshotKeepsItsFixedTotal(t *testing.T) {
	s, admin := catalogVerificationService(t)
	project := seedProject(t, s.db, admin)
	p := savePricingTestProduct(t, s, admin, "legacy-fixed-price", "")
	q := requestPricingTestQuote(t, s, admin, project, p.ID, 12, "1080p")
	if q.Credits != 7 || p.Pricing != nil {
		t.Fatal("legacy product was repriced instead of using its fixed total", q, p)
	}
	// Pre-upgrade quotes do not contain a pricing field at all.
	if _, err := s.db.Exec(context.Background(), "UPDATE quotes SET product_snapshot=product_snapshot-'pricing' WHERE id=$1", q.ID); err != nil {
		t.Fatal(err)
	}
	changed := p
	changed.Credits = 99
	changed.Pricing = &ProductPricing{BillingUnit: "per_second"}
	putPricingTestProduct(t, s, admin, changed)
	body := map[string]string{"quote_id": q.ID, "request_key": "legacy-fixed-price-acceptance"}
	rec := doRequest(s, admin, http.MethodPost, "/api/v1/tasks", body)
	if rec.Code != http.StatusCreated {
		t.Fatal("legacy quote was rejected after pricing upgrade", rec.Code, rec.Body.String())
	}
	task := data[Task](t, rec)
	if task.Credits != 7 || task.Product.Credits != 7 || task.Product.Pricing != nil {
		t.Fatal("legacy accepted snapshot was rewritten using the current price", task)
	}
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/tasks", body)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(data[Task](t, rec), task) {
		t.Fatal("legacy accepted pricing retry changed", rec.Code, rec.Body.String())
	}
	assertBalance(t, s.db, admin, 993, 7)
}

func TestPostgresProductPricingRejectsInconsistentUnacceptedQuoteWithoutFreezing(t *testing.T) {
	s, admin := catalogVerificationService(t)
	project := seedProject(t, s.db, admin)
	p := savePricingTestProduct(t, s, admin, "inconsistent-pricing", "per_second")
	wallet := catalogWalletSnapshot(t, s.db)
	for _, mutation := range []struct {
		name       string
		resolution string
		query      string
	}{
		{"quoted-total", "720p", "UPDATE quotes SET credits=credits-1 WHERE id=$1"},
		{"requested-duration", "720p", "UPDATE quotes SET input=jsonb_set(input,'{duration}','5') WHERE id=$1"},
		{"default-unit-price", "720p", "UPDATE quotes SET product_snapshot=jsonb_set(product_snapshot,'{credits}','8') WHERE id=$1"},
		{"resolution-unit-price", "1080p", "UPDATE quotes SET product_snapshot=jsonb_set(product_snapshot,'{pricing,resolution_credits}','{\"1080p\":14}') WHERE id=$1"},
		{"billing-unit", "1080p", "UPDATE quotes SET product_snapshot=jsonb_set(product_snapshot,'{pricing,billing_unit}','\"per_video\"') WHERE id=$1"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			q := requestPricingTestQuote(t, s, admin, project, p.ID, 12, mutation.resolution)
			if _, err := s.db.Exec(context.Background(), mutation.query, q.ID); err != nil {
				t.Fatal(err)
			}
			rec := doRequest(s, admin, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "inconsistent-" + mutation.name})
			var failure struct{ Code string }
			if err := json.Unmarshal(rec.Body.Bytes(), &failure); err != nil || rec.Code != http.StatusUnprocessableEntity || failure.Code != "invalid_input" {
				t.Fatal("inconsistent quoted price was accepted", rec.Code, rec.Body.String(), err)
			}
			var tasks int
			if err := s.db.QueryRow(context.Background(), "SELECT count(*) FROM generation_tasks WHERE quote_id=$1", q.ID).Scan(&tasks); err != nil || tasks != 0 || catalogWalletSnapshot(t, s.db) != wallet {
				t.Fatal("rejected price inconsistency created a task, reservation or ledger entry", tasks, err)
			}
		})
	}
}

func TestPostgresProductPricingLifecycleUsesAcceptedTotals(t *testing.T) {
	s, admin := catalogVerificationService(t)
	project := seedProject(t, s.db, admin)
	p := savePricingTestProduct(t, s, admin, "priced-lifecycle", "per_second")
	tasks := make([]Task, 0, 3)
	for _, spec := range []struct {
		key, resolution string
		duration        int
		credits         int64
	}{{"cancel-priced-task", "1080p", 5, 65}, {"release-priced-task", "720p", 12, 84}, {"consume-priced-task", "1080p", 12, 156}} {
		q := requestPricingTestQuote(t, s, admin, project, p.ID, spec.duration, spec.resolution)
		rec := doRequest(s, admin, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": spec.key})
		if rec.Code != http.StatusCreated || q.Credits != spec.credits {
			t.Fatal("priced lifecycle task was not accepted", rec.Code, rec.Body.String(), q.Credits)
		}
		task := data[Task](t, rec)
		if task.Credits != spec.credits || !reflect.DeepEqual(task.Product, p) {
			t.Fatal("task total or unit-price snapshot differs from its accepted quote", task)
		}
		tasks = append(tasks, task)
	}
	assertBalance(t, s.db, admin, 695, 305)
	changed := p
	changed.Credits, changed.Pricing = 999, &ProductPricing{BillingUnit: "per_video"}
	putPricingTestProduct(t, s, admin, changed)
	rec := doRequest(s, admin, http.MethodPost, "/api/v1/tasks/"+tasks[0].ID+"/cancel", nil)
	if rec.Code != http.StatusOK || data[Task](t, rec).SettlementStatus != "released" {
		t.Fatal("queued cancellation did not release its accepted total", rec.Code, rec.Body.String())
	}
	rec = doRequest(s, admin, http.MethodPost, "/api/v1/tasks/"+tasks[0].ID+"/cancel", nil)
	if rec.Code != http.StatusConflict {
		t.Fatal("already canceled pricing task was not terminal", rec.Code, rec.Body.String())
	}
	assertBalance(t, s.db, admin, 760, 240)
	for i := 0; i < 2; i++ {
		if err := s.settle(context.Background(), tasks[1].ID, "", "released", "failed", "error", "fixture_failure", "Release the accepted priced task"); err != nil {
			t.Fatal(err)
		}
	}
	assertBalance(t, s.db, admin, 844, 156)
	for i := 0; i < 2; i++ {
		if err := s.settle(context.Background(), tasks[2].ID, "", "consumed", "completed", "ready", "", "Consume the accepted priced task"); err != nil {
			t.Fatal(err)
		}
	}
	assertBalance(t, s.db, admin, 844, 0)
	for i := 0; i < 2; i++ {
		rec = doRequest(s, admin, http.MethodPost, "/api/v1/admin/tasks/"+tasks[2].ID+"/refund", map[string]string{"reason": "Refund the original accepted priced total"})
		if rec.Code != http.StatusOK || data[Task](t, rec).SettlementStatus != "refunded" {
			t.Fatal("refund did not retain the accepted amount", rec.Code, rec.Body.String())
		}
	}
	assertBalance(t, s.db, admin, 1000, 0)
	for _, event := range []struct {
		task      int
		kind      string
		available int64
		frozen    int64
	}{{0, "freeze", -65, 65}, {0, "released", 65, -65}, {1, "freeze", -84, 84}, {1, "released", 84, -84}, {2, "freeze", -156, 156}, {2, "consumed", 0, -156}, {2, "refund", 156, 0}} {
		var count int
		var available, frozen int64
		err := s.db.QueryRow(context.Background(), "SELECT count(*),COALESCE(sum(available_delta),0),COALESCE(sum(frozen_delta),0) FROM wallet_ledger WHERE task_id=$1 AND event_type=$2", tasks[event.task].ID, event.kind).Scan(&count, &available, &frozen)
		if err != nil || count != 1 || available != event.available || frozen != event.frozen {
			t.Fatal("lifecycle ledger did not use the accepted computed total exactly once", event, count, available, frozen, err)
		}
	}
	for _, original := range tasks {
		stored, err := s.getTask(context.Background(), admin, original.ID, false)
		if err != nil || stored.Credits != original.Credits || !reflect.DeepEqual(stored.Product, p) {
			t.Fatal("lifecycle changed the task's original pricing snapshot", stored, err)
		}
	}
}
