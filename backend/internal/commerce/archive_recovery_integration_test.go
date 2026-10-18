package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/media"
	"wsapi-video/internal/storage"
)

func TestPostgresCommittedArchiveRecoversWithoutUpstream(t *testing.T) {
	for _, objectState := range []string{"present", "missing", "corrupt"} {
		t.Run(objectState, func(t *testing.T) {
			db := integrationDB(t)
			ctx := context.Background()
			user := seedUser(t, db, 100)
			project := seedProject(t, db, user)
			seedRealProduct(t, db)
			var upstreamCalls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				w.WriteHeader(http.StatusGone)
			}))
			defer upstream.Close()
			cfg := config.Config{StorageDriver: "local", StorageDir: t.TempDir(), MaxUploadBytes: 8 << 20, MaxMediaBytes: 8 << 20}
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
			rec := doRequest(jobs, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": "committed-archive-crash"})
			if rec.Code != 201 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			task := data[Task](t, rec)
			png := demoPNG(task.ID, "16:9")
			saved, err := archive.Archive(ctx, user, project, task.ID, "recovered.png", "image/png", bytes.NewReader(png))
			if err != nil {
				t.Fatal(err)
			}
			// Simulate a crash after the asset transaction committed but before
			// the worker persisted its asset ID or consumed the frozen credits.
			outputs, _ := json.Marshal([]media.Output{{URL: upstream.URL + "/expired.png"}})
			if _, err = db.Exec(ctx, "UPDATE generation_tasks SET execution_status='completed',delivery_status='archiving',upstream_id='expired-job',outputs=$2 WHERE id=$1", task.ID, outputs); err != nil {
				t.Fatal(err)
			}
			if objectState == "missing" {
				if err = blobs.Delete(ctx, saved.ObjectKey); err != nil {
					t.Fatal(err)
				}
			} else if objectState == "corrupt" {
				corrupt := append([]byte{}, png...)
				corrupt[len(corrupt)/2] ^= 0xff
				if err = blobs.Put(ctx, saved.ObjectKey, bytes.NewReader(corrupt), "image/png"); err != nil {
					t.Fatal(err)
				}
			}
			stale := runnable(t, jobs, task.ID)
			active := runnable(t, jobs, task.ID)
			jobs.process(ctx, stale)
			assertBalance(t, db, user, 90, 10)
			got, err := jobs.getTask(ctx, user, task.ID, false)
			if err != nil || len(got.AssetIDs) != 0 {
				t.Fatal("stale worker modified recovered task", err)
			}
			jobs.process(ctx, active)
			if objectState != "present" {
				got, err = jobs.getTask(ctx, user, task.ID, false)
				if err != nil || got.DeliveryStatus != "archive_retry" || got.SettlementStatus != "frozen" || len(got.AssetIDs) != 0 {
					t.Fatal("unavailable stored object was delivered or consumed", got, err)
				}
				assertBalance(t, db, user, 90, 10)
				var consumed int
				if err = db.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='consumed'", task.ID).Scan(&consumed); err != nil || consumed != 0 {
					t.Fatal("unavailable object created a consumption ledger entry", consumed, err)
				}
				if err = blobs.Put(ctx, saved.ObjectKey, bytes.NewReader(png), "image/png"); err != nil {
					t.Fatal(err)
				}
				active = runnable(t, jobs, task.ID)
				jobs.process(ctx, active)
			}
			// Replaying a retired lease must not settle or create a second asset.
			jobs.process(ctx, active)
			got, err = jobs.getTask(ctx, user, task.ID, false)
			if err != nil || got.DeliveryStatus != "ready" || got.SettlementStatus != "consumed" || len(got.AssetIDs) != 1 || got.AssetIDs[0] != saved.ID {
				t.Fatal("committed archive was not recovered", got, err)
			}
			assertBalance(t, db, user, 90, 0)
			var stored, consumed int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM assets WHERE task_id=$1", task.ID).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger WHERE task_id=$1 AND event_type='consumed'", task.ID).Scan(&consumed); err != nil {
				t.Fatal(err)
			}
			if stored != 1 || consumed != 1 || upstreamCalls.Load() != 0 {
				t.Fatal("archive recovery duplicated work or depended on expired upstream", stored, consumed, upstreamCalls.Load())
			}
		})
	}
}
