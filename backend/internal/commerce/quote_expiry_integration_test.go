package commerce

import (
	"context"
	"encoding/json"
	"testing"

	"wsapi-video/internal/config"
	"wsapi-video/internal/httpx"
)

func TestPostgresQuoteExpirySeparatesRejectionFromAcceptedRecovery(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	s := New(db, config.Config{DevMode: true, Demo: true}, nil)
	expire := func(id string) {
		t.Helper()
		if _, err := db.Exec(ctx, "UPDATE quotes SET expires_at=now()-interval '1 hour' WHERE id=$1", id); err != nil {
			t.Fatal(err)
		}
	}
	assertFacts := func(wantTasks, wantFreezes int, available, frozen int64) {
		t.Helper()
		var tasks, freezes int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM generation_tasks WHERE user_id=$1", user).Scan(&tasks); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger WHERE user_id=$1 AND event_type='freeze'", user).Scan(&freezes); err != nil {
			t.Fatal(err)
		}
		if tasks != wantTasks || freezes != wantFreezes {
			t.Fatalf("unexpected task/freeze facts: %d/%d, want %d/%d", tasks, freezes, wantTasks, wantFreezes)
		}
		assertBalance(t, db, user, available, frozen)
	}
	expired := createQuote(t, s, user, project, "demo-image")
	expire(expired.ID)
	rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": expired.ID, "request_key": "expired-unaccepted-key"})
	var rejection httpx.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &rejection); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 409 || rejection.Code != "quote_expired" || rejection.Message != "报价已过期，请重新确认费用" {
		t.Fatalf("expired unused quote must have its stable rejection: status=%d code=%q", rec.Code, rejection.Code)
	}
	assertFacts(0, 0, 100, 0)
	accepted := createQuote(t, s, user, project, "demo-image")
	payload := map[string]string{"quote_id": accepted.ID, "request_key": "accepted-recovery-key"}
	rec = doRequest(s, user, "POST", "/api/v1/tasks", payload)
	if rec.Code != 201 {
		t.Fatal("initial submit failed", rec.Code)
	}
	original := data[Task](t, rec)
	expire(accepted.ID)
	rec = doRequest(s, user, "POST", "/api/v1/tasks", payload)
	if rec.Code != 200 || data[Task](t, rec).ID != original.ID {
		t.Fatal("accepted task was not recovered after quote expiry", rec.Code)
	}
	// Generic conflicts can refer to an already accepted task and must stay distinct.
	for _, conflicting := range []map[string]string{
		{"quote_id": accepted.ID, "request_key": "different-recovery-key"},
		{"quote_id": expired.ID, "request_key": payload["request_key"]},
	} {
		rec = doRequest(s, user, "POST", "/api/v1/tasks", conflicting)
		if err := json.Unmarshal(rec.Body.Bytes(), &rejection); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 409 || rejection.Code != "conflict" {
			t.Fatalf("accepted-request conflict code changed: status=%d code=%q", rec.Code, rejection.Code)
		}
	}
	assertFacts(1, 1, 90, 10)
}
