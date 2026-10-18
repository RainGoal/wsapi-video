package commerce

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/media"
	"wsapi-video/internal/storage"
)

func TestPostgresArchiveRequiresTaskMediaKind(t *testing.T) {
	for _, path := range []string{"new", "recovered"} {
		for _, taskKind := range []string{"image", "video"} {
			for _, assetKind := range []string{"image", "video"} {
				t.Run(path+"/"+taskKind+"-task/"+assetKind+"-asset", func(t *testing.T) {
					testArchiveTaskKind(t, path, taskKind, assetKind)
				})
			}
		}
	}
}

func testArchiveTaskKind(t *testing.T, path, taskKind, assetKind string) {
	t.Helper()
	db := integrationDB(t)
	ctx := context.Background()
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	_, err := db.Exec(ctx, "INSERT INTO model_products(id,name,kind,model,credits,capabilities,active,verified) VALUES('kind-fixture','Kind fixture',$1,'fixture-model',10,'{}',true,true)", taskKind)
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := demoPNG("archive-kind", "16:9"), "image/png"
	if assetKind == "video" {
		body, err = os.ReadFile(filepath.Join("..", "assets", "testdata", "valid.mp4"))
		if err != nil {
			t.Fatal(err)
		}
		contentType = "video/mp4"
	}
	var creates, polls, downloads atomic.Int32
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			creates.Add(1)
			_, _ = io.WriteString(w, `{"id":"kind-job"}`)
		case r.URL.Path == "/output" || r.URL.Path == "/v1/videos/kind-job/content":
			downloads.Add(1)
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(body)
		default:
			polls.Add(1)
			_, _ = fmt.Fprintf(w, `{"id":"kind-job","status":"completed","result":{"data":[{"url":%q}]}}`, upstream.URL+"/output")
		}
	}))
	defer upstream.Close()
	cfg := config.Config{StorageDriver: "local", StorageDir: t.TempDir(), MaxMediaBytes: 8 << 20, FFprobePath: os.Getenv("FFPROBE_PATH")}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	archive := assets.New(db, cfg, blobs)
	jobs := New(db, cfg, archive)
	jobs.gateway, err = media.NewConnector(media.Config{Origin: upstream.URL, AllowHTTP: true, Keys: map[string]string{"original": "secret"}, ImageCredential: "original", VideoCredential: "original"})
	if err != nil {
		t.Fatal(err)
	}
	quote := createQuote(t, jobs, user, project, "kind-fixture")
	rec := doRequest(jobs, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": "archive-kind-check"})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	task := data[Task](t, rec)
	if path == "recovered" {
		if _, err = archive.Archive(ctx, user, project, task.ID, "recovered", contentType, bytes.NewReader(body)); err != nil {
			t.Fatal(err)
		}
		outputs, _ := json.Marshal([]media.Output{{URL: upstream.URL + "/output"}})
		if _, err = db.Exec(ctx, "UPDATE generation_tasks SET execution_status='completed',delivery_status='archiving',upstream_id='kind-job',outputs=$2 WHERE id=$1", task.ID, outputs); err != nil {
			t.Fatal(err)
		}
	} else {
		jobs.process(ctx, runnable(t, jobs, task.ID))
	}
	active := runnable(t, jobs, task.ID)
	jobs.process(ctx, active)
	got, err := jobs.getTask(ctx, user, task.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	matching := taskKind == assetKind
	if matching {
		if got.DeliveryStatus != "ready" || got.SettlementStatus != "consumed" || len(got.AssetIDs) != 1 {
			t.Fatal("matching archive was not delivered", got)
		}
		assertBalance(t, db, user, 90, 0)
	} else {
		if got.ExecutionStatus != "completed" || got.DeliveryStatus != "error" || got.SettlementStatus != "frozen" || got.ProcurementStatus != "disputed" || got.ErrorCode != "archive_kind_mismatch" || got.ErrorMessage == "" || len(got.AssetIDs) != 0 {
			t.Fatal("wrong media kind was delivered or not held for review", got)
		}
		assertBalance(t, db, user, 90, 10)
		// An explicit operator retry must reuse and recheck the committed asset,
		// including a new archive whose first delivery failed the kind check.
		rec = doRequest(jobs, user, "POST", "/api/v1/admin/tasks/"+task.ID+"/resolve", map[string]string{"action": "retry_archive", "reason": "Review original stored media"})
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		restarted := New(db, cfg, archive)
		restarted.gateway = jobs.gateway
		active = runnable(t, restarted, task.ID)
		restarted.process(ctx, active)
		got, err = restarted.getTask(ctx, user, task.ID, false)
		if err != nil || got.DeliveryStatus != "error" || got.SettlementStatus != "frozen" || got.ProcurementStatus != "disputed" || got.ErrorCode != "archive_kind_mismatch" || len(got.AssetIDs) != 0 {
			t.Fatal("recovery settled an earlier wrong-kind archive", got, err)
		}
		assertBalance(t, db, user, 90, 10)
	}
	if _, err = jobs.claim(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("delivered or disputed archive was eligible for automatic retry", err)
	}
	var stored, ledger, consumed, tasks int
	if err = db.QueryRow(ctx, "SELECT count(*) FROM assets WHERE task_id=$1", task.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "SELECT count(*),count(*) FILTER (WHERE event_type='consumed') FROM wallet_ledger WHERE task_id=$1", task.ID).Scan(&ledger, &consumed); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "SELECT count(*) FROM generation_tasks WHERE user_id=$1", user).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	wantConsumed, wantCalls := 0, int32(0)
	if matching {
		wantConsumed = 1
	}
	if path == "new" {
		wantCalls = 1
	}
	if stored != 1 || tasks != 1 || ledger != 1+wantConsumed || consumed != wantConsumed || creates.Load() != wantCalls || polls.Load() != wantCalls || downloads.Load() != wantCalls {
		t.Fatal("archive validation duplicated work or wallet events", stored, tasks, ledger, consumed, creates.Load(), polls.Load(), downloads.Load())
	}
}

func TestPostgresArchiveRecoveryRejectsLegacyHeaderOnlyMedia(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	user := seedUser(t, db, 100)
	project := seedProject(t, db, user)
	seedRealProduct(t, db)
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusGone)
	}))
	defer upstream.Close()
	cfg := config.Config{StorageDriver: "local", StorageDir: t.TempDir(), MaxMediaBytes: 8 << 20, FFprobePath: os.Getenv("FFPROBE_PATH")}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	archive := assets.New(db, cfg, blobs)
	jobs := New(db, cfg, archive)
	jobs.gateway, err = media.NewConnector(media.Config{Origin: upstream.URL, AllowHTTP: true, Keys: map[string]string{"original": "secret"}, ImageCredential: "original"})
	if err != nil {
		t.Fatal(err)
	}
	quote := createQuote(t, jobs, user, project, "real-image")
	rec := doRequest(jobs, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": "legacy-header-archive"})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	task := data[Task](t, rec)
	png := demoPNG(task.ID, "16:9")
	saved, err := archive.Archive(ctx, user, project, task.ID, "legacy.png", "image/png", bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}
	// Retain valid PNG dimensions and IEND but omit every IDAT chunk. Updating
	// the isolated fixture's recorded hash simulates a prior header-only save.
	invalid := append(append([]byte{}, png[:33]...), png[len(png)-12:]...)
	hash := sha256.Sum256(invalid)
	if err = blobs.Put(ctx, saved.ObjectKey, bytes.NewReader(invalid), "image/png"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "UPDATE assets SET size_bytes=$2,sha256=$3 WHERE id=$1", saved.ID, len(invalid), hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "UPDATE generation_tasks SET execution_status='completed',delivery_status='archiving',upstream_id='legacy-job' WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	jobs.process(ctx, runnable(t, jobs, task.ID))
	got, err := jobs.getTask(ctx, user, task.ID, false)
	if err != nil || got.DeliveryStatus != "archive_retry" || got.SettlementStatus != "frozen" || len(got.AssetIDs) != 0 {
		t.Fatal("legacy incomplete media was delivered", got, err)
	}
	assertBalance(t, db, user, 90, 10)
	var ledger, consumed int
	if err = db.QueryRow(ctx, "SELECT count(*),count(*) FILTER (WHERE event_type='consumed') FROM wallet_ledger WHERE task_id=$1", task.ID).Scan(&ledger, &consumed); err != nil || ledger != 1 || consumed != 0 || upstreamCalls.Load() != 0 {
		t.Fatal("legacy recovery charged or regenerated", ledger, consumed, upstreamCalls.Load(), err)
	}
}
