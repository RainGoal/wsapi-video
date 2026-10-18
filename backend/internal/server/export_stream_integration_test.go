package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wsapi-video/internal/config"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/storage"
)

func TestExportProductionHandlerAbortsTruncatedLargeObject(t *testing.T) {
	db := serverIntegrationDB(t)
	host := httptest.NewUnstartedServer(nil)
	origin := "http://" + host.Listener.Addr().String()
	cfg := config.Config{Env: "test", DevMode: true, Demo: true, PublicURL: origin, AppSecret: strings.Repeat("export-production-chain", 2), StorageDriver: "local", StorageDir: t.TempDir(), WebDir: t.TempDir(), MaxUploadBytes: 8 << 20, MaxMediaBytes: 512 << 20, SessionTTL: time.Hour}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg, db, blobs)
	host.Config.Handler = app.HTTP.Handler
	host.Start()
	defer host.Close()
	creator := newStudioClient(t, origin)
	creator.registerAndLogin("large-export@example.test", "Export-chain-password-2026")
	project := serverData[projects.Project](t, creator.json("POST", "/api/v1/projects", map[string]string{"name": "Large transport export"}, 201))
	const size int64 = 256 << 20
	block := make([]byte, 1<<20)
	for i := range block {
		block[i] = byte(i*29 + i/251)
	}
	key := creator.user.ID + "/" + project.ID + "/large-transport.bin"
	writeObject := func() string {
		t.Helper()
		parts := make([]io.Reader, int(size/int64(len(block))))
		for i := range parts {
			parts[i] = bytes.NewReader(block)
		}
		hash := sha256.New()
		if err := blobs.Put(context.Background(), key, io.TeeReader(io.MultiReader(parts...), hash), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(hash.Sum(nil))
	}
	digest := writeObject()
	if _, err = db.Exec(context.Background(), "INSERT INTO assets(id,user_id,project_id,kind,file_name,content_type,object_key,size_bytes,sha256) VALUES('large-transport',$1,$2,'video','large-transport.bin','application/octet-stream',$3,$4,$5)", creator.user.ID, project.ID, key, size, digest); err != nil {
		t.Fatal(err)
	}
	state := func() [6]int64 {
		t.Helper()
		var out [6]int64
		err := db.QueryRow(context.Background(), `SELECT available,frozen,(SELECT count(*) FROM wallet_ledger),(SELECT count(*) FROM generation_tasks),(SELECT count(*) FROM payment_orders),(SELECT count(*) FROM assets) FROM wallet_accounts WHERE user_id=$1`, creator.user.ID).Scan(&out[0], &out[1], &out[2], &out[3], &out[4], &out[5])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := state()
	prepared := serverData[struct {
		URL string `json:"download_url"`
	}](t, creator.json("POST", "/api/v1/projects/"+project.ID+"/export/prepare", map[string]any{"asset_ids": []string{"large-transport"}}, 200))
	response, err := creator.http.Get(origin + prepared.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	failedPath := filepath.Join(t.TempDir(), "interrupted.zip")
	output, err := os.Create(failedPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := io.CopyN(output, response.Body, 1<<20)
	if err != nil {
		output.Close()
		t.Fatal(err)
	}
	// The authenticated native GET is already streaming. Truncate its actual
	// local object, exercising export -> secure -> net/http without mocks.
	if err = os.Truncate(filepath.Join(cfg.StorageDir, filepath.FromSlash(key)), 1<<20); err != nil {
		output.Close()
		t.Fatal(err)
	}
	rest, terminalErr := io.Copy(output, response.Body)
	output.Close()
	response.Body.Close()
	if response.StatusCode != 200 || terminalErr == nil || first+rest >= size {
		t.Fatal("production middleware swallowed the stream abort", response.StatusCode, first+rest, terminalErr)
	}
	if archive, err := zip.OpenReader(failedPath); err == nil {
		archive.Close()
		t.Fatal("truncated object produced a complete ZIP through production handler")
	}
	if restored := writeObject(); restored != digest {
		t.Fatal("fixture restoration changed the original media bytes")
	}
	retry, err := creator.http.Get(origin + prepared.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Body.Close()
	retryPath := filepath.Join(t.TempDir(), "retried.zip")
	output, err = os.Create(retryPath)
	if err != nil {
		t.Fatal(err)
	}
	retried, retryErr := io.Copy(output, retry.Body)
	output.Close()
	if retryErr != nil || retry.StatusCode != 200 || retried <= size {
		t.Fatal("same-link retry failed after object recovery", retry.StatusCode, retried, retryErr)
	}
	archive, err := zip.OpenReader(retryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].UncompressedSize64 != uint64(size) {
		t.Fatal("retry did not preserve the single 256MiB asset")
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	hash := sha256.New()
	decoded, decodeErr := io.Copy(hash, entry)
	if decodeErr != nil || decoded != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		t.Fatal("retry ZIP CRC or original SHA256 failed", decoded, decodeErr)
	}
	if after := state(); after != before {
		t.Fatal("interrupted native GET or retry changed financial/tasks/assets state", before, after)
	}
	t.Logf("production server/auth/CSRF/secure chain: initial headers=%d, partial bytes=%d, terminal error=%v, incomplete ZIP rejected; same-link retry=%d bytes, CRC+SHA256 verified, unchanged financial/task/asset state", response.StatusCode, first+rest, terminalErr, retried)
}
