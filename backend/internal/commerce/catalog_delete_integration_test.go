package commerce

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

type catalogDeletionFixture struct {
	s                      *Service
	admin, id, path, table string
	action                 string
	body                   map[string]any
}

func newCatalogDeletionFixture(t *testing.T, kind string) catalogDeletionFixture {
	t.Helper()
	s, admin := catalogVerificationService(t)
	s.cfg.EasyPayOrigin, s.cfg.EasyPayPID = "https://pay.example/epay", "1001"
	s.cfg.EasyPayCredentialRef = "delete-fixture"
	s.cfg.EasyPayKeys = map[string]string{"delete-fixture": "fixture-payment-key"}
	s.cfg.EasyPayNotifyURL = "https://app.example/api/v1/payment/notify/easypay"
	s.cfg.EasyPayReturnURL = "https://app.example/wallet"
	f := catalogDeletionFixture{s: s, admin: admin, id: "delete-" + kind}
	if kind == "product" {
		p := saveVerifiedCatalogProduct(t, s, admin, f.id, "image")
		f.path, f.table, f.action = "/api/v1/admin/model-products/"+p.ID, "model_products", "product.delete"
		f.body = catalogProductBody(p, "Remove this catalog product")
	} else {
		f.path, f.table, f.action = "/api/v1/admin/payment/tiers/"+f.id, "payment_tiers", "payment_tier.delete"
		f.body = map[string]any{"id": f.id, "name": "Deletion fixture", "amount_fen": 1000, "credits": 77, "active": true, "version": 0, "reason": "Create the deletion fixture tier"}
		rec := doRequest(s, admin, http.MethodPut, f.path, f.body)
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		f.body["version"] = data[Tier](t, rec).Version
	}
	return f
}

func catalogDeletionRow(t *testing.T, f catalogDeletionFixture) string {
	t.Helper()
	var row string
	if err := f.s.db.QueryRow(context.Background(), "SELECT to_jsonb(r)::text FROM "+f.table+" r WHERE id=$1", f.id).Scan(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

func catalogDeletionHistory(t *testing.T, s *Service) string {
	t.Helper()
	var history string
	err := s.db.QueryRow(context.Background(), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(q) ORDER BY id) FROM quotes q),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM generation_tasks t),
 (SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM payment_orders o))::text`).Scan(&history)
	if err != nil {
		t.Fatal(err)
	}
	return history
}

func deleteCatalogFixture(t *testing.T, f catalogDeletionFixture) {
	t.Helper()
	reason := "Remove this outdated catalog offer"
	rec := doRequest(f.s, f.admin, http.MethodDelete, f.path, map[string]any{"version": 1, "reason": reason})
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	got := data[struct {
		OK      bool   `json:"ok"`
		ID      string `json:"id"`
		Version int    `json:"version"`
	}](t, rec)
	if !got.OK || got.ID != f.id || got.Version != 2 {
		t.Fatalf("invalid deletion receipt: %+v", got)
	}
	var tombstone bool
	if err := f.s.db.QueryRow(context.Background(), "SELECT deleted_at IS NOT NULL AND NOT active AND version=2 FROM "+f.table+" WHERE id=$1", f.id).Scan(&tombstone); err != nil || !tombstone {
		t.Fatal("deletion did not preserve a versioned inactive tombstone", tombstone, err)
	}
	var count int
	if err := f.s.db.QueryRow(context.Background(), "SELECT count(*) FROM admin_audit WHERE action=$1 AND target_id=$2", f.action, f.id).Scan(&count); err != nil || count != 1 {
		t.Fatal("delete audit count", count, err)
	}
	var actor, actualReason string
	var raw []byte
	err := f.s.db.QueryRow(context.Background(), "SELECT admin_id,reason,details FROM admin_audit WHERE action=$1 AND target_id=$2", f.action, f.id).Scan(&actor, &actualReason, &raw)
	if err != nil || actor != f.admin || actualReason != reason {
		t.Fatal("delete audit lost actor or reason", actor, actualReason, err)
	}
	var audit struct {
		Before map[string]any `json:"before"`
		After  map[string]any `json:"after"`
	}
	if err = json.Unmarshal(raw, &audit); err != nil || audit.Before["version"] != float64(1) || audit.After["version"] != float64(2) || audit.After["active"] != false || audit.After["deleted"] != true {
		t.Fatal("delete audit lost the versioned transition", string(raw), err)
	}
}

func assertCatalogDeletionVisibility(t *testing.T, f catalogDeletionFixture, want bool) {
	t.Helper()
	paths := []string{"/api/v1/model-products", "/api/v1/admin/model-products"}
	if f.table == "payment_tiers" {
		paths = []string{"/api/v1/payment/tiers", "/api/v1/admin/payment/tiers"}
	}
	for _, path := range paths {
		rec := doRequest(f.s, f.admin, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		listed := false
		for _, item := range data[struct{ Items []struct{ ID string } }](t, rec).Items {
			listed = listed || item.ID == f.id
		}
		if listed != want {
			t.Fatalf("%s visibility for %s=%v, want %v", path, f.id, listed, want)
		}
	}
}

func TestPostgresCatalogDeletionValidatesVersionReasonAndPreservesTombstones(t *testing.T) {
	for _, kind := range []string{"product", "tier"} {
		t.Run(kind, func(t *testing.T) {
			f := newCatalogDeletionFixture(t, kind)
			before, wallet := catalogDeletionRow(t, f), catalogWalletSnapshot(t, f.s.db)
			invalidBodies := []map[string]any{
				{"reason": "A valid deletion reason"},
				{"version": 0, "reason": "A valid deletion reason"},
				{"version": -1, "reason": "A valid deletion reason"},
				{"version": 1},
				{"version": 1, "reason": "ab"},
				{"version": 1, "reason": "   "},
				{"version": 1, "reason": strings.Repeat("x", 2001)},
			}
			for i, body := range invalidBodies {
				rec := doRequest(f.s, f.admin, http.MethodDelete, f.path, body)
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("invalid deletion %d accepted: %d %s", i, rec.Code, rec.Body.String())
				}
			}
			for _, attempt := range []struct {
				path    string
				version int
				status  int
			}{{f.path, 2, http.StatusConflict}, {f.path + "-missing", 1, http.StatusNotFound}} {
				rec := doRequest(f.s, f.admin, http.MethodDelete, attempt.path, map[string]any{"version": attempt.version, "reason": "Reject stale or missing catalog offers"})
				if rec.Code != attempt.status {
					t.Fatal("incorrect stale/missing delete response", rec.Code, rec.Body.String())
				}
			}
			var auditCount int
			if err := f.s.db.QueryRow(context.Background(), "SELECT count(*) FROM admin_audit").Scan(&auditCount); err != nil || auditCount != 1 || catalogDeletionRow(t, f) != before || catalogWalletSnapshot(t, f.s.db) != wallet {
				t.Fatal("rejected deletions mutated the catalog, audit or wallet", auditCount, err)
			}
			assertCatalogDeletionVisibility(t, f, true)
			deleteCatalogFixture(t, f)
			assertCatalogDeletionVisibility(t, f, false)
			deleted := catalogDeletionRow(t, f)
			rec := doRequest(f.s, f.admin, http.MethodDelete, f.path, map[string]any{"version": 2, "reason": "Duplicate delete must not add another audit entry"})
			if rec.Code != http.StatusNotFound {
				t.Fatal("duplicate deletion was accepted", rec.Code, rec.Body.String())
			}
			for _, version := range []int{0, 2} {
				f.body["version"], f.body["active"] = version, false
				rec = doRequest(f.s, f.admin, http.MethodPut, f.path, f.body)
				if rec.Code != http.StatusConflict {
					t.Fatal("deleted identifier was reused", rec.Code, rec.Body.String())
				}
			}
			if err := f.s.db.QueryRow(context.Background(), "SELECT count(*) FROM admin_audit").Scan(&auditCount); err != nil || auditCount != 2 || catalogDeletionRow(t, f) != deleted || catalogWalletSnapshot(t, f.s.db) != wallet {
				t.Fatal("deletion or tombstone retries changed catalog, audit or wallet", auditCount, err)
			}
		})
	}
}

func TestPostgresCatalogProductDeletionPreservesAcceptedWorkAndRejectsOldQuotes(t *testing.T) {
	f := newCatalogDeletionFixture(t, "product")
	project := seedProject(t, f.s.db, f.admin)
	acceptedQuote := createQuote(t, f.s, f.admin, project, f.id)
	unacceptedQuote := createQuote(t, f.s, f.admin, project, f.id)
	body := map[string]string{"quote_id": acceptedQuote.ID, "request_key": "accepted-before-product-delete"}
	rec := doRequest(f.s, f.admin, http.MethodPost, "/api/v1/tasks", body)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	original := data[Task](t, rec)
	history, wallet := catalogDeletionHistory(t, f.s), catalogWalletSnapshot(t, f.s.db)
	deleteCatalogFixture(t, f)
	assertCatalogDeletionVisibility(t, f, false)
	if catalogDeletionHistory(t, f.s) != history || catalogWalletSnapshot(t, f.s.db) != wallet {
		t.Fatal("product deletion modified historical snapshots or wallet state")
	}
	rec = doRequest(f.s, f.admin, http.MethodPost, "/api/v1/tasks", body)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(data[Task](t, rec), original) {
		t.Fatal("accepted idempotent retry changed after deletion", rec.Code, rec.Body.String())
	}
	rec = doRequest(f.s, f.admin, http.MethodPost, "/api/v1/tasks", map[string]string{"quote_id": unacceptedQuote.ID, "request_key": "unaccepted-before-product-delete"})
	var failure struct{ Code string }
	if err := json.Unmarshal(rec.Body.Bytes(), &failure); err != nil || rec.Code != http.StatusConflict || failure.Code != "product_unavailable" {
		t.Fatal("old unaccepted quote was not rejected", rec.Code, rec.Body.String(), err)
	}
	rec = doRequest(f.s, f.admin, http.MethodPost, "/api/v1/quotes", QuoteInput{ProjectID: project, ProductID: f.id, Prompt: "A request after deletion"})
	if rec.Code != http.StatusNotFound {
		t.Fatal("deleted product still accepted new quotes", rec.Code, rec.Body.String())
	}
	if catalogDeletionHistory(t, f.s) != history || catalogWalletSnapshot(t, f.s.db) != wallet {
		t.Fatal("rejected new work changed historical snapshots, reservations or ledger")
	}
}

func TestPostgresCatalogDeletionAuditFailureRollsBack(t *testing.T) {
	for _, kind := range []string{"product", "tier"} {
		t.Run(kind, func(t *testing.T) {
			f := newCatalogDeletionFixture(t, kind)
			before, wallet := catalogDeletionRow(t, f), catalogWalletSnapshot(t, f.s.db)
			ctx := context.Background()
			_, err := f.s.db.Exec(ctx, `CREATE FUNCTION reject_delete_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture audit unavailable'; END $$;
CREATE TRIGGER reject_delete_audit BEFORE INSERT ON admin_audit FOR EACH ROW EXECUTE FUNCTION reject_delete_audit()`)
			if err != nil {
				t.Fatal(err)
			}
			rec := doRequest(f.s, f.admin, http.MethodDelete, f.path, map[string]any{"version": 1, "reason": "Do not allow an unaudited deletion"})
			if rec.Code != http.StatusInternalServerError {
				t.Fatal("audit outage did not fail deletion", rec.Code, rec.Body.String())
			}
			var auditCount int
			if err = f.s.db.QueryRow(ctx, "SELECT count(*) FROM admin_audit").Scan(&auditCount); err != nil || auditCount != 1 || catalogDeletionRow(t, f) != before || catalogWalletSnapshot(t, f.s.db) != wallet {
				t.Fatal("failed audit partially deleted catalog, changed audit or wallet", auditCount, err)
			}
			assertCatalogDeletionVisibility(t, f, true)
		})
	}
}

func TestPostgresCatalogTierDeletionPreservesPendingOrderReconciliation(t *testing.T) {
	f := newCatalogDeletionFixture(t, "tier")
	var queries atomic.Int32
	var orderNo string
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/epay/api.php" || r.ParseForm() != nil || r.PostForm.Get("out_trade_no") != orderNo || r.PostForm.Get("pid") != "1001" {
			t.Error("historical order did not query its original payment identity")
			http.Error(w, "invalid fixture query", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintf(w, `{"code":1,"out_trade_no":%q,"pid":"1001","status":1,"money":"10.00","trade_no":"delete-tier-trade"}`, orderNo)
	}))
	defer provider.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = provider.Client().Transport
	defer func() { http.DefaultTransport = originalTransport }()
	f.s.cfg.EasyPayOrigin = provider.URL + "/epay"
	body := map[string]string{"tier_id": f.id, "method": "alipay"}
	rec := doRequest(f.s, f.admin, http.MethodPost, "/api/v1/payment/orders", body)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	original := data[Order](t, rec)
	orderNo = original.OutTradeNo
	history, wallet := catalogDeletionHistory(t, f.s), catalogWalletSnapshot(t, f.s.db)
	deleteCatalogFixture(t, f)
	assertCatalogDeletionVisibility(t, f, false)
	rec = doRequest(f.s, f.admin, http.MethodPost, "/api/v1/payment/orders", body)
	if rec.Code != http.StatusNotFound {
		t.Fatal("deleted tier accepted a new order", rec.Code, rec.Body.String())
	}
	if catalogDeletionHistory(t, f.s) != history || catalogWalletSnapshot(t, f.s.db) != wallet || queries.Load() != 0 {
		t.Fatal("tier deletion or rejected order changed history, wallet or contacted the provider")
	}
	for i := 0; i < 2; i++ {
		rec = doRequest(f.s, f.admin, http.MethodPost, "/api/v1/payment/orders/"+original.ID+"/check", nil)
		if rec.Code != http.StatusOK {
			t.Fatal("pending historical order could not reconcile", rec.Code, rec.Body.String())
		}
		paid := data[Order](t, rec)
		if paid.ID != original.ID || paid.OutTradeNo != original.OutTradeNo || paid.AmountFen != original.AmountFen || paid.Credits != original.Credits || paid.PaymentStatus != "paid" || paid.CreditStatus != "credited" {
			t.Fatal("reconciled order lost its accepted snapshot", paid)
		}
	}
	assertBalance(t, f.s.db, f.admin, 1077, 0)
	var tierID string
	var version, credits int
	err := f.s.db.QueryRow(context.Background(), "SELECT tier_id,tier_version,(SELECT count(*) FROM wallet_ledger WHERE order_id=$1) FROM payment_orders WHERE id=$1", original.ID).Scan(&tierID, &version, &credits)
	if err != nil || tierID != f.id || version != 1 || credits != 1 || queries.Load() != 1 {
		t.Fatal("historical order changed tier version or credited more than once", tierID, version, credits, queries.Load(), err)
	}
}
