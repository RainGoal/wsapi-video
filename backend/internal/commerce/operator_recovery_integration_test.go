package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/media"
	"wsapi-video/internal/storage"
)

func operatorRecoveryFixture(t *testing.T, handler http.HandlerFunc) (*Service, Task, *httptest.Server) {
	t.Helper()
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	seedRealProduct(t, db)
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	cfg := config.Config{StorageDriver: "local", StorageDir: t.TempDir(), MaxMediaBytes: 8 << 20, TaskReviewAfter: time.Hour}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, cfg, assets.New(db, cfg, blobs))
	connector := func(current string) *media.Connector {
		g, e := media.NewConnector(media.Config{Origin: upstream.URL, AllowHTTP: true, Keys: map[string]string{"original": "original-secret", "replacement": "replacement-secret"}, ImageCredential: current})
		if e != nil {
			t.Fatal(e)
		}
		return g
	}
	s.gateway = connector("original")
	q := createQuote(t, s, user, project, "real-image")
	rec := doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "operator-recovery-request"})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	task, err := s.getTask(context.Background(), user, data[Task](t, rec).ID, false)
	if err != nil {
		t.Fatal(err)
	}
	// New work uses the replacement; recovery must keep the accepted credential.
	s.gateway = connector("replacement")
	return s, task, upstream
}

func TestPostgresOperatorBindsOldTerminalTask(t *testing.T) {
	for _, state := range []string{"completed", "failed", "canceled", "processing"} {
		t.Run(state, func(t *testing.T) {
			var creates, polls, downloads atomic.Int32
			var origin string
			s, original, upstream := operatorRecoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method != http.MethodGet:
					creates.Add(1)
					w.WriteHeader(http.StatusBadRequest)
				case r.URL.Path == "/v1/images/tasks/original-job":
					polls.Add(1)
					if r.Header.Get("Authorization") != "Bearer original-secret" {
						t.Error("recovery did not use the original credential")
					}
					_, _ = fmt.Fprintf(w, `{"id":"original-job","model":"fixture-model","status":%q,"result":{"data":[{"url":%q}]}}`, state, origin+"/output")
				case r.URL.Path == "/output":
					downloads.Add(1)
					if r.Header.Get("Authorization") != "" {
						t.Error("output received a service credential")
					}
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(demoPNG("operator-recovery", "16:9"))
				default:
					t.Error("recovery requested a different upstream task", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			origin = upstream.URL
			ctx := context.Background()
			if _, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='submission_unknown',procurement_status='disputed',created_at=now()-interval '2 hours' WHERE id=$1", original.ID); err != nil {
				t.Fatal(err)
			}
			rec := doRequest(s, original.UserID, "POST", "/api/v1/admin/tasks/"+original.ID+"/resolve", map[string]string{"action": "bind_upstream", "upstream_id": "original-job", "reason": "Verify original accepted task", "evidence": "Original gateway request correlated by operator"})
			if rec.Code != http.StatusOK {
				t.Fatal(rec.Code, rec.Body.String())
			}
			task := data[Task](t, rec)
			switch state {
			case "completed":
				if task.ExecutionStatus != "completed" || task.SettlementStatus != "frozen" {
					t.Fatal("verified completion was discarded", task)
				}
				active := runnable(t, s, task.ID)
				s.process(ctx, active)
				s.process(ctx, active)
			case "failed", "canceled":
				if task.ExecutionStatus != state || task.SettlementStatus != "released" {
					t.Fatal("verified terminal failure was not released", task)
				}
			case "processing":
				s.process(ctx, runnable(t, s, task.ID))
			}
			assertOperatorRecoveryResult(t, s, original, state, creates.Load(), polls.Load(), downloads.Load())
		})
	}
}

func assertOperatorRecoveryResult(t *testing.T, s *Service, original Task, state string, creates, polls, downloads int32) {
	t.Helper()
	ctx := context.Background()
	task, err := s.getTask(ctx, original.UserID, original.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if task.CredentialRef != "original" || task.UpstreamID != "original-job" || task.Credits != original.Credits || digest(task.Input) != digest(original.Input) || digest(task.Product) != digest(original.Product) {
		t.Fatal("operator recovery changed accepted snapshots", task)
	}
	var stored, consumed, released, freezes, audits int
	err = s.db.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM assets WHERE task_id=$1),
 (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='consumed'),
 (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='released'),
 (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='freeze'),
 (SELECT count(*) FROM admin_audit WHERE target_id=$1 AND action='task.bind_upstream')`, original.ID).Scan(&stored, &consumed, &released, &freezes, &audits)
	if err != nil {
		t.Fatal(err)
	}
	if creates != 0 || polls != 1 || freezes != 1 || audits != 1 {
		t.Fatal("operator recovery recreated or duplicated work", creates, polls, freezes, audits)
	}
	switch state {
	case "completed":
		if task.DeliveryStatus != "ready" || task.SettlementStatus != "consumed" || stored != 1 || consumed != 1 || released != 0 || downloads != 1 {
			t.Fatal("completion did not settle one archived result", task, stored, consumed, released, downloads)
		}
		assertBalance(t, s.db, original.UserID, 90, 0)
	case "failed", "canceled":
		if task.SettlementStatus != "released" || stored != 0 || consumed != 0 || released != 1 || downloads != 0 {
			t.Fatal("terminal failure was not released exactly once", task, stored, consumed, released, downloads)
		}
		assertBalance(t, s.db, original.UserID, 100, 0)
	case "processing":
		if task.SettlementStatus != "frozen" || task.ProcurementStatus != "disputed" || task.ErrorCode != "generation_overdue" || stored != 0 || consumed != 0 || released != 0 || downloads != 0 {
			t.Fatal("long processing escaped manual protection", task)
		}
		assertBalance(t, s.db, original.UserID, 90, 10)
	}
	if _, err = s.claim(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("settled or disputed recovery was automatically reclaimed", err)
	}
}

func TestPostgresOperatorRetriesExpiredImageURL(t *testing.T) {
	for _, scenario := range []string{"success", "query_failed", "processing", "model_mismatch", "fresh_failed", "lease_changed"} {
		t.Run(scenario, func(t *testing.T) {
			var creates, polls, expired, fresh atomic.Int32
			var origin string
			var changeLease func()
			s, original, upstream := operatorRecoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method != http.MethodGet:
					creates.Add(1)
					w.WriteHeader(http.StatusBadRequest)
				case r.URL.Path == "/expired":
					expired.Add(1)
					w.WriteHeader(http.StatusForbidden)
				case r.URL.Path == "/v1/images/tasks/original-job":
					polls.Add(1)
					if r.Header.Get("Authorization") != "Bearer original-secret" {
						t.Error("URL refresh used a different service credential")
					}
					if scenario == "query_failed" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					state, model := "completed", "fixture-model"
					if scenario == "processing" {
						state = "processing"
					}
					if scenario == "model_mismatch" {
						model = "different-model"
					}
					if scenario == "lease_changed" {
						changeLease()
					}
					_, _ = fmt.Fprintf(w, `{"id":"original-job","model":%q,"status":%q,"result":{"data":[{"url":%q}]}}`, model, state, origin+"/fresh")
				case r.URL.Path == "/fresh":
					fresh.Add(1)
					if r.Header.Get("Authorization") != "" {
						t.Error("refreshed output received a service credential")
					}
					if scenario == "fresh_failed" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(demoPNG("refreshed-output", "16:9"))
				default:
					t.Error("URL refresh requested a different task", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			origin = upstream.URL
			runExpiredImageRecovery(t, s, original, scenario, origin, &changeLease, &creates, &polls, &expired, &fresh)
		})
	}
}

func runExpiredImageRecovery(t *testing.T, s *Service, original Task, scenario, origin string, changeLease *func(), creates, polls, expired, fresh *atomic.Int32) {
	t.Helper()
	ctx := context.Background()
	outputs, _ := json.Marshal([]media.Output{{URL: origin + "/expired"}})
	if _, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='completed',delivery_status='archiving',procurement_status='disputed',upstream_id='original-job',created_at=now()-interval '2 hours',outputs=$2 WHERE id=$1", original.ID, outputs); err != nil {
		t.Fatal(err)
	}
	*changeLease = func() {
		if _, err := s.db.Exec(ctx, "UPDATE generation_tasks SET lease_token='other-owner',lease_until=now()+interval '1 hour' WHERE id=$1", original.ID); err != nil {
			t.Error(err)
		}
	}
	rec := doRequest(s, original.UserID, "POST", "/api/v1/admin/tasks/"+original.ID+"/resolve", map[string]string{"action": "retry_archive", "reason": "Retry original media after review"})
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	active := runnable(t, s, original.ID)
	s.process(ctx, active)
	if scenario == "success" {
		s.process(ctx, active)
	}
	task, err := s.getTask(ctx, original.UserID, original.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	var stored, consumed, released int
	err = s.db.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM assets WHERE task_id=$1),
 (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='consumed'),
 (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='released')`, original.ID).Scan(&stored, &consumed, &released)
	if err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 0 || polls.Load() != 1 || expired.Load() != 1 || fresh.Load() > 1 || released != 0 {
		t.Fatal("refresh recreated work, skipped original lookup or repeated download", creates.Load(), polls.Load(), expired.Load(), fresh.Load(), released)
	}
	if scenario == "success" {
		if task.SettlementStatus != "consumed" || task.DeliveryStatus != "ready" || stored != 1 || consumed != 1 || fresh.Load() != 1 || len(task.Outputs) != 1 || task.Outputs[0].URL != origin+"/fresh" {
			t.Fatal("expired original URL did not recover a unique result", task, stored, consumed, fresh.Load())
		}
		assertBalance(t, s.db, original.UserID, 90, 0)
	} else {
		if task.SettlementStatus != "frozen" || stored != 0 || consumed != 0 {
			t.Fatal("failed or retired refresh changed credits", task, stored, consumed)
		}
		if scenario == "lease_changed" {
			if task.LeaseToken != "other-owner" || task.Outputs[0].URL != origin+"/expired" || fresh.Load() != 0 {
				t.Fatal("old refresh overwrote a newer lease", task, fresh.Load())
			}
		} else if task.ProcurementStatus != "disputed" || task.ErrorCode != "archive_review" {
			t.Fatal("failed old refresh escaped manual protection", task)
		}
		assertBalance(t, s.db, original.UserID, 90, 10)
	}
	if _, err = s.claim(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("settled, held or leased refresh was automatically reclaimed", err)
	}
}

func TestPostgresOperatorTerminalBindingRollsBackWithAudit(t *testing.T) {
	var polls atomic.Int32
	s, original, _ := operatorRecoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		polls.Add(1)
		_, _ = fmt.Fprint(w, `{"id":"original-job","model":"fixture-model","status":"failed"}`)
	})
	ctx := context.Background()
	if _, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='submission_unknown',procurement_status='disputed',created_at=now()-interval '2 hours' WHERE id=$1", original.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.db.Exec(ctx, `CREATE FUNCTION reject_bind_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.action='task.bind_upstream' THEN RAISE EXCEPTION 'fixture audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_bind_audit BEFORE INSERT ON admin_audit FOR EACH ROW EXECUTE FUNCTION reject_bind_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	bind := func() *httptest.ResponseRecorder {
		return doRequest(s, original.UserID, "POST", "/api/v1/admin/tasks/"+original.ID+"/resolve", map[string]string{"action": "bind_upstream", "upstream_id": "original-job", "reason": "Verify terminal original task", "evidence": "Original accepted request has definitively failed"})
	}
	if rec := bind(); rec.Code != http.StatusInternalServerError {
		t.Fatal("audit rejection did not abort binding", rec.Code, rec.Body.String())
	}
	task, err := s.getTask(ctx, original.UserID, original.ID, false)
	if err != nil || task.ExecutionStatus != "submission_unknown" || task.UpstreamID != "" || task.SettlementStatus != "frozen" {
		t.Fatal("failed audit left a partial binding", task, err)
	}
	assertBalance(t, s.db, original.UserID, 90, 10)
	var releases, audits int
	err = s.db.QueryRow(ctx, "SELECT (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='released'),(SELECT count(*) FROM admin_audit WHERE target_id=$1)", original.ID).Scan(&releases, &audits)
	if err != nil || releases != 0 || audits != 0 {
		t.Fatal("failed audit left a partial credit or audit", releases, audits, err)
	}
	if _, err = s.db.Exec(ctx, "DROP TRIGGER reject_bind_audit ON admin_audit; DROP FUNCTION reject_bind_audit()"); err != nil {
		t.Fatal(err)
	}
	if rec := bind(); rec.Code != http.StatusOK {
		t.Fatal("same binding could not recover after audit repair", rec.Code, rec.Body.String())
	}
	if rec := bind(); rec.Code != http.StatusConflict {
		t.Fatal("settled binding could be executed again", rec.Code)
	}
	task, err = s.getTask(ctx, original.UserID, original.ID, false)
	if err != nil || task.ExecutionStatus != "failed" || task.SettlementStatus != "released" {
		t.Fatal("recovered binding did not keep terminal failure", task, err)
	}
	assertBalance(t, s.db, original.UserID, 100, 0)
	err = s.db.QueryRow(ctx, "SELECT (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='released'),(SELECT count(*) FROM admin_audit WHERE target_id=$1)", original.ID).Scan(&releases, &audits)
	if err != nil || releases != 1 || audits != 1 || polls.Load() != 2 {
		t.Fatal("audit recovery duplicated settlement or accepted a repeat", releases, audits, polls.Load(), err)
	}
}

func TestPostgresOperatorBindingCannotOverwriteConcurrentRelease(t *testing.T) {
	started, proceed := make(chan struct{}, 1), make(chan struct{}, 1)
	defer close(proceed)
	s, original, _ := operatorRecoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-proceed:
		case <-r.Context().Done():
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"original-job","model":"fixture-model","status":"completed","result":{"data":[{"b64_json":"unused"}]}}`)
	})
	ctx := context.Background()
	if _, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='submission_unknown',procurement_status='disputed',created_at=now()-interval '2 hours' WHERE id=$1", original.ID); err != nil {
		t.Fatal(err)
	}
	bound := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		bound <- doRequest(s, original.UserID, "POST", "/api/v1/admin/tasks/"+original.ID+"/resolve", map[string]string{"action": "bind_upstream", "upstream_id": "original-job", "reason": "Verify original pending task", "evidence": "Correlate original provider task before recovery"})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("binding did not query the original task")
	}
	rec := doRequest(s, original.UserID, "POST", "/api/v1/admin/tasks/"+original.ID+"/resolve", map[string]string{"action": "release", "reason": "Resolve concurrent original request"})
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	proceed <- struct{}{}
	select {
	case rec = <-bound:
	case <-time.After(5 * time.Second):
		t.Fatal("binding did not finish after original query")
	}
	if rec.Code != http.StatusConflict {
		t.Fatal("late binding overwrote an operator decision", rec.Code, rec.Body.String())
	}
	task, err := s.getTask(ctx, original.UserID, original.ID, false)
	if err != nil || task.SettlementStatus != "released" || task.UpstreamID != "" || len(task.Outputs) != 0 {
		t.Fatal("late binding changed the released task", task, err)
	}
	assertBalance(t, s.db, original.UserID, 100, 0)
	var releases, audits int
	err = s.db.QueryRow(ctx, "SELECT (SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='released'),(SELECT count(*) FROM admin_audit WHERE target_id=$1)", original.ID).Scan(&releases, &audits)
	if err != nil || releases != 1 || audits != 1 {
		t.Fatal("binding/release race duplicated credit or audit", releases, audits, err)
	}
}
