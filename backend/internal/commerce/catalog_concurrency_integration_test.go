package commerce

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
)

func TestPostgresCatalogConcurrentCreatePreservesWinningVersion(t *testing.T) {
	for _, kind := range []string{"products", "tiers"} {
		t.Run(kind, func(t *testing.T) {
			db := integrationDB(t)
			admins := [2]string{seedUser(t, db, 500), seedUser(t, db, 700)}
			s := New(db, config.Config{DevMode: true, Demo: true}, nil)
			id := "concurrent-" + kind
			table, action, path := "model_products", "product.save", "/api/v1/admin/model-products/"+id
			bodies := [2]map[string]any{
				{"id": id, "name": "First offer", "credits": 17, "version": 0, "active": false, "reason": "First administrator creates offer"},
				{"id": id, "name": "Second offer", "credits": 39, "version": 0, "active": false, "reason": "Second administrator creates offer"},
			}
			for i, body := range bodies {
				if kind == "products" {
					body["kind"], body["model"] = "image", fmt.Sprintf("image-model-%d", i)
					body["capabilities"] = Capabilities{AspectRatios: []string{"1:1"}}
					body["verified"], body["demo"] = false, false
				} else {
					table, action, path = "payment_tiers", "payment_tier.save", "/api/v1/admin/payment/tiers/"+id
					body["amount_fen"] = 1000 + i*1700
				}
			}
			before := catalogWalletSnapshot(t, db)
			results := concurrentCatalogCreates(t, s, db, table, admins, path, bodies)
			winner, successes, conflicts := -1, 0, 0
			for _, result := range results {
				switch result.response.Code {
				case 200:
					winner, successes = result.index, successes+1
				case 409:
					conflicts++
					var failure struct{ Code string }
					if err := json.Unmarshal(result.response.Body.Bytes(), &failure); err != nil || failure.Code != "conflict" {
						t.Fatalf("invalid conflict receipt: %s (%v)", result.response.Body.String(), err)
					}
				default:
					t.Errorf("unexpected save response: %d %s", result.response.Code, result.response.Body.String())
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("concurrent create: successes=%d conflicts=%d; want one of each", successes, conflicts)
			}
			assertCatalogRecord(t, db, table, id, bodies[winner], 1)
			assertCatalogAudit(t, db, action, id, admins[winner], bodies[winner], 1)
			if after := catalogWalletSnapshot(t, db); after != before {
				t.Fatal("catalog creation changed wallet accounts or ledger")
			}
			// Existing rows still accept their current version and reject stale
			// edits without adding an audit entry or changing any saved fields.
			updated := make(map[string]any)
			for key, value := range bodies[winner] {
				updated[key] = value
			}
			updated["version"], updated["credits"] = 1, 61
			updated["name"], updated["reason"] = "Updated winning offer", "Update the winning offer at version one"
			if kind == "tiers" {
				updated["amount_fen"] = 9900
			}
			rec := doRequest(s, admins[winner], "PUT", path, updated)
			if rec.Code != 200 {
				t.Fatalf("current-version update failed: %d %s", rec.Code, rec.Body.String())
			}
			stale := bodies[1-winner]
			stale["version"] = 1
			rec = doRequest(s, admins[1-winner], "PUT", path, stale)
			if rec.Code != 409 {
				t.Fatalf("stale version update was not rejected: %d %s", rec.Code, rec.Body.String())
			}
			assertCatalogRecord(t, db, table, id, updated, 2)
			assertCatalogAudit(t, db, action, id, admins[winner], updated, 2)
			if after := catalogWalletSnapshot(t, db); after != before {
				t.Fatal("catalog updates changed wallet accounts or ledger")
			}
		})
	}
}

type catalogSaveResult struct {
	index    int
	response *httptest.ResponseRecorder
}

func concurrentCatalogCreates(t *testing.T, s *Service, db *pgxpool.Pool, table string, admins [2]string, path string, bodies [2]map[string]any) []catalogSaveResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blocker, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	// SHARE permits both initial SELECT FOR UPDATE reads, but blocks INSERT's
	// RowExclusiveLock. Observe both waiting inserts before releasing the gate.
	if _, err = blocker.Exec(ctx, "LOCK TABLE "+table+" IN SHARE MODE"); err != nil {
		t.Fatal(err)
	}
	results := make(chan catalogSaveResult, 2)
	for i := range bodies {
		go func(i int) {
			results <- catalogSaveResult{i, doRequest(s, admins[i], "PUT", path, bodies[i])}
		}(i)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		err = db.QueryRow(ctx, "SELECT count(*) FROM pg_locks WHERE relation=$1::regclass AND mode='RowExclusiveLock' AND NOT granted", table).Scan(&waiting)
		if err != nil {
			t.Fatal("read concurrent insert barrier", err)
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("both creates did not reach the insert barrier", ctx.Err())
		}
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	completed := make([]catalogSaveResult, 0, 2)
	for len(completed) < 2 {
		select {
		case result := <-results:
			completed = append(completed, result)
		case <-ctx.Done():
			t.Fatal("concurrent creates did not finish", ctx.Err())
		}
	}
	return completed
}

func catalogWalletSnapshot(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	err := db.QueryRow(context.Background(), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(w) ORDER BY user_id) FROM wallet_accounts w),
 (SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM wallet_ledger l))::text`).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertCatalogRecord(t *testing.T, db *pgxpool.Pool, table, id string, body map[string]any, version int) {
	t.Helper()
	var raw []byte
	if err := db.QueryRow(context.Background(), "SELECT to_jsonb(r) FROM "+table+" r WHERE id=$1", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if value, ok := stored["is_demo"]; ok {
		stored["demo"] = value
	}
	assertCatalogFields(t, stored, body, version)
}

func assertCatalogFields(t *testing.T, actual map[string]any, body map[string]any, version int) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err = json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	delete(expected, "reason")
	expected["version"] = float64(version)
	for key, value := range expected {
		if !reflect.DeepEqual(actual[key], value) {
			t.Errorf("%s changed: got %#v, want %#v", key, actual[key], value)
		}
	}
}

func assertCatalogAudit(t *testing.T, db *pgxpool.Pool, action, id, adminID string, body map[string]any, count int) {
	t.Helper()
	ctx := context.Background()
	var actualCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM admin_audit WHERE action=$1 AND target_id=$2", action, id).Scan(&actualCount); err != nil {
		t.Fatal(err)
	}
	if actualCount != count {
		t.Fatalf("saved audit count=%d, want %d", actualCount, count)
	}
	var actor, reason string
	var raw []byte
	err := db.QueryRow(ctx, "SELECT admin_id,reason,details FROM admin_audit WHERE action=$1 AND target_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1", action, id).Scan(&actor, &reason, &raw)
	if err != nil {
		t.Fatal(err)
	}
	if actor != adminID || reason != body["reason"] {
		t.Fatalf("audit actor/reason did not match the successful save: %q %q", actor, reason)
	}
	var details struct {
		Before map[string]any `json:"before"`
		After  map[string]any `json:"after"`
	}
	if err = json.Unmarshal(raw, &details); err != nil {
		t.Fatal(err)
	}
	if details.Before["version"] != float64(count-1) {
		t.Errorf("audit previous version=%v, want %d", details.Before["version"], count-1)
	}
	assertCatalogFields(t, details.After, body, count)
}
