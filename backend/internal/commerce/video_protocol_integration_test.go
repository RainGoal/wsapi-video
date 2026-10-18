package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/media"
)

func videoProtocolFixture(t *testing.T) (*Service, *pgxpool.Pool, string, string, *atomic.Int32) {
	t.Helper()
	db := integrationDB(t)
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	_, err := db.Exec(context.Background(), `INSERT INTO model_products(id,name,kind,model,credits,capabilities,active,verified) VALUES('real-video','Video fixture','video','fixture-video',10,'{"resolutions":["720p"],"sizes":[],"qualities":[]}',true,true)`)
	if err != nil {
		t.Fatal(err)
	}
	calls := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"id":"unexpected-upstream","status":"queued"}`))
	}))
	t.Cleanup(srv.Close)
	s := New(db, config.Config{}, nil)
	s.gateway, err = media.NewConnector(media.Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"original": "fixture-only"}, VideoCredential: "original"})
	if err != nil {
		t.Fatal(err)
	}
	return s, db, user, project, calls
}

func assertVideoProtocolFacts(t *testing.T, db *pgxpool.Pool, user string, wantTasks, wantLedger int, available, frozen int64) {
	t.Helper()
	var tasks, ledger int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM generation_tasks WHERE user_id=$1", user).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM wallet_ledger WHERE user_id=$1", user).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if tasks != wantTasks || ledger != wantLedger {
		t.Fatalf("unexpected task/ledger side effects: %d/%d; want %d/%d", tasks, ledger, wantTasks, wantLedger)
	}
	assertBalance(t, db, user, available, frozen)
}

func TestPostgresVideoProtocolRejectsQuotesAndUnacceptedSnapshots(t *testing.T) {
	for _, field := range []string{"size", "quality", "legacy_size", "legacy_quality"} {
		t.Run(field, func(t *testing.T) {
			s, db, user, project, calls := videoProtocolFixture(t)
			ctx := context.Background()
			q := createQuote(t, s, user, project, "real-video")
			p, err := scanProduct(db.QueryRow(ctx, "SELECT "+productColumns+" FROM model_products WHERE id='real-video'"))
			if err != nil {
				t.Fatal(err)
			}
			in := QuoteInput{ProjectID: project, ProductID: p.ID, Prompt: "test", Resolution: "720p"}
			switch field {
			case "size":
				in.Size = "1024x1024"
			case "quality":
				in.Quality = "high"
			case "legacy_size":
				p.Capabilities.Sizes = []string{"720p"}
			case "legacy_quality":
				p.Capabilities.Qualities = []string{"high"}
			}
			caps, _ := json.Marshal(p.Capabilities)
			input, _ := json.Marshal(in)
			product, _ := json.Marshal(p)
			if _, err = db.Exec(ctx, "UPDATE model_products SET capabilities=$1 WHERE id=$2", caps, p.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(ctx, "UPDATE quotes SET input=$2,product_snapshot=$3 WHERE id=$1", q.ID, input, product); err != nil {
				t.Fatal(err)
			}
			for _, req := range []struct {
				path string
				body any
			}{
				{"/api/v1/quotes", in},
				{"/api/v1/tasks", map[string]string{"quote_id": q.ID, "request_key": "legacy-video-unused-quote"}},
			} {
				rec := doRequest(s, user, "POST", req.path, req.body)
				var envelope httpx.Envelope
				if err = json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if rec.Code != 422 || envelope.Code != "invalid_input" {
					t.Fatalf("unsupported specifications accepted at %s: %d %s", req.path, rec.Code, rec.Body.String())
				}
			}
			var quotes int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM quotes WHERE user_id=$1", user).Scan(&quotes); err != nil || quotes != 1 {
				t.Fatalf("rejected input created another quote: %d %v", quotes, err)
			}
			assertVideoProtocolFacts(t, db, user, 0, 0, 100, 0)
			if calls.Load() != 0 {
				t.Fatal("rejected quote reached the upstream")
			}
		})
	}
}

func TestPostgresLegacyQueuedVideoProtocolReleasesWithoutSubmission(t *testing.T) {
	for _, field := range []string{"size", "quality"} {
		t.Run(field, func(t *testing.T) {
			s, db, user, project, calls := videoProtocolFixture(t)
			ctx := context.Background()
			q := createQuote(t, s, user, project, "real-video")
			payload := map[string]string{"quote_id": q.ID, "request_key": "legacy-video-accepted-key"}
			rec := doRequest(s, user, "POST", "/api/v1/tasks", payload)
			if rec.Code != 201 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			original := data[Task](t, rec)
			if field == "size" {
				original.Input.Size = "1024x1024"
				original.Product.Capabilities.Sizes = []string{"1024x1024"}
			} else {
				original.Input.Quality = "high"
				original.Product.Capabilities.Qualities = []string{"high"}
			}
			input, _ := json.Marshal(original.Input)
			product, _ := json.Marshal(original.Product)
			if _, err := db.Exec(ctx, "UPDATE generation_tasks SET input=$2,product_snapshot=$3 WHERE id=$1", original.ID, input, product); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, "UPDATE quotes SET input=$2,product_snapshot=$3,expires_at=now()-interval '1 hour' WHERE id=$1", q.ID, input, product); err != nil {
				t.Fatal(err)
			}
			rec = doRequest(s, user, "POST", "/api/v1/tasks", payload)
			if rec.Code != 200 || data[Task](t, rec).ID != original.ID {
				t.Fatal("original accepted request was not recovered", rec.Code)
			}
			assertVideoProtocolFacts(t, db, user, 1, 1, 90, 10)
			job := runnable(t, s, original.ID)
			s.process(ctx, job)
			s.process(ctx, job)
			got, err := s.getTask(ctx, user, original.ID, false)
			if err != nil || got.ExecutionStatus != "failed" || got.SettlementStatus != "released" || got.ErrorCode != "submission_rejected" {
				t.Fatalf("legacy unsupported input was not safely rejected: %#v %v", got, err)
			}
			if digest(got.Input) != digest(original.Input) || digest(got.Product) != digest(original.Product) {
				t.Fatal("legacy task snapshots were rewritten")
			}
			if _, err = s.claim(ctx); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal("rejected task was eligible for a new submission", err)
			}
			rec = doRequest(s, user, "POST", "/api/v1/tasks", payload)
			if rec.Code != 200 || data[Task](t, rec).ID != original.ID {
				t.Fatal("settled original request was not recoverable", rec.Code)
			}
			assertVideoProtocolFacts(t, db, user, 1, 2, 100, 0)
			if calls.Load() != 0 {
				t.Fatal("legacy unsupported input reached the upstream", calls.Load())
			}
		})
	}
}
