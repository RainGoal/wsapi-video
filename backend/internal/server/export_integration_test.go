package server

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/storage"
)

func TestExportPreparedDownloadIsScopedSnapshotAndReadOnly(t *testing.T) {
	db := serverIntegrationDB(t)
	host := httptest.NewUnstartedServer(nil)
	origin := "http://" + host.Listener.Addr().String()
	cfg := config.Config{Env: "test", DevMode: true, Demo: true, PublicURL: origin, AppSecret: strings.Repeat("x", 40), StorageDriver: "local", StorageDir: t.TempDir(), WebDir: t.TempDir(), MaxUploadBytes: 8 << 20, MaxMediaBytes: 8 << 20, SessionTTL: time.Hour}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg, db, blobs)
	host.Config.Handler = app.HTTP.Handler
	host.Start()
	defer host.Close()
	creator, other := newStudioClient(t, origin), newStudioClient(t, origin)
	creator.registerAndLogin("creator-export@example.test", "Creator-password-2026")
	other.registerAndLogin("other-export@example.test", "Another-password-2026")
	project := serverData[projects.Project](t, creator.json("POST", "/api/v1/projects", map[string]string{"name": "Export snapshot"}, 201))
	secondProject := serverData[projects.Project](t, creator.json("POST", "/api/v1/projects", map[string]string{"name": "Another owned project"}, 201))
	png := fixturePNG(t)
	upload := func(client *studioClient, projectID, name string) assets.Asset {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if err := form.WriteField("project_id", projectID); err != nil {
			t.Fatal(err)
		}
		part, err := form.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(png); err != nil {
			t.Fatal(err)
		}
		if err = form.Close(); err != nil {
			t.Fatal(err)
		}
		raw, _ := client.raw("POST", "/api/v1/assets", form.FormDataContentType(), &body, 201)
		return serverData[assets.Asset](t, raw)
	}
	first := upload(creator, project.ID, "first-version.png")
	second := upload(creator, project.ID, "second-version.png")
	foreignProject := serverData[projects.Project](t, other.json("POST", "/api/v1/projects", map[string]string{"name": "Foreign project"}, 201))
	foreign := upload(other, foreignProject.ID, "private.png")
	path := "/api/v1/projects/" + project.ID
	shots := projects.ShotCollection{Shots: []projects.Shot{{ID: "shot-export", Number: 1, Title: "Opening", Duration: 5, ImageCandidateIDs: []string{first.ID, second.ID}, SelectedImageID: first.ID}}}
	shots = serverData[projects.ShotCollection](t, creator.json("PUT", path+"/shots", shots, 200))

	type financialState struct{ Available, Frozen, Ledger, Tasks int64 }
	financial := func() financialState {
		t.Helper()
		var state financialState
		err := db.QueryRow(context.Background(), `SELECT available,frozen,(SELECT count(*) FROM wallet_ledger WHERE user_id=$1),(SELECT count(*) FROM generation_tasks WHERE user_id=$1) FROM wallet_accounts WHERE user_id=$1`, creator.user.ID).Scan(&state.Available, &state.Frozen, &state.Ledger, &state.Tasks)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := financial()
	type preparedExport struct {
		DownloadURL string    `json:"download_url"`
		ExpiresAt   time.Time `json:"expires_at"`
		AssetCount  int       `json:"asset_count"`
		TotalBytes  int64     `json:"total_bytes"`
	}
	raw, headers := creator.raw("POST", path+"/export/prepare", "application/json", strings.NewReader("{}"), 200)
	prepared := serverData[preparedExport](t, raw)
	if prepared.AssetCount != 1 || prepared.TotalBytes != int64(len(png)) || prepared.ExpiresAt.Before(time.Now().Add(4*time.Minute)) || prepared.ExpiresAt.After(time.Now().Add(5*time.Minute)) || !strings.HasPrefix(prepared.DownloadURL, path+"/export/download?token=") {
		t.Fatal("prepare did not return a five-minute snapshot", prepared)
	}
	if headers.Get("Cache-Control") != "private, no-store" || headers.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("export ticket can leak through caches or referrers")
	}
	shots.Shots[0].SelectedImageID = second.ID
	creator.json("PUT", path+"/shots", shots, 200)
	assertArchive := func(raw []byte, names ...string) {
		t.Helper()
		archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil || len(archive.File) != len(names) {
			t.Fatal("invalid or incomplete ZIP", err)
		}
		for i, file := range archive.File {
			if !strings.HasSuffix(file.Name, names[i]) {
				t.Fatal("snapshot changed after selection was updated", file.Name, names[i])
			}
			entry, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			contents, err := io.ReadAll(entry)
			entry.Close()
			if err != nil || !bytes.Equal(contents, png) {
				t.Fatal("ZIP media bytes differ from uploaded file", err)
			}
		}
	}
	// Native navigation sends its authenticated cookie without a CSRF header.
	csrf := creator.csrf
	creator.csrf = ""
	zipBody, headers := creator.raw("GET", prepared.DownloadURL, "", nil, 200)
	creator.csrf = csrf
	assertArchive(zipBody, first.FileName)
	if headers.Get("Content-Type") != "application/zip" || !strings.Contains(headers.Get("Content-Disposition"), "attachment") || headers.Get("Cache-Control") != "private, no-store" {
		t.Fatal("download is not a private attachment")
	}
	repeated, _ := creator.raw("GET", prepared.DownloadURL, "", nil, 200)
	if !bytes.Equal(repeated, zipBody) {
		t.Fatal("repeated download changed the signed snapshot")
	}
	other.json("GET", prepared.DownloadURL, nil, 404)
	newStudioClient(t, origin).json("GET", prepared.DownloadURL, nil, 401)
	creator.json("GET", strings.Replace(prepared.DownloadURL, project.ID, secondProject.ID, 1), nil, 404)
	parsedURL, err := url.Parse(prepared.DownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedURL.Query()
	query.Set("token", query.Get("token")+"A")
	parsedURL.RawQuery = query.Encode()
	creator.json("GET", parsedURL.String(), nil, 404)
	creator.json("GET", path+"/export/download", nil, 404)
	other.json("POST", path+"/export/prepare", map[string]any{}, 404)
	creator.json("POST", path+"/export/prepare", map[string]any{"asset_ids": []string{foreign.ID}}, 422)
	creator.csrf = ""
	creator.json("POST", path+"/export/prepare", map[string]any{}, 403)
	creator.csrf = csrf
	explicit := serverData[preparedExport](t, creator.json("POST", path+"/export/prepare", map[string]any{"asset_ids": []string{first.ID, first.ID, second.ID}}, 200))
	if explicit.AssetCount != 2 || explicit.TotalBytes != 2*int64(len(png)) {
		t.Fatal("explicit export was not deduplicated", explicit)
	}
	explicitZIP, _ := creator.raw("GET", explicit.DownloadURL, "", nil, 200)
	assertArchive(explicitZIP, first.FileName, second.FileName)
	legacyZIP, _ := creator.raw("POST", path+"/export", "application/json", strings.NewReader("{}"), 200)
	assertArchive(legacyZIP, second.FileName)

	// A signature cannot resurrect a deleted asset or deleted project.
	transient := upload(creator, project.ID, "transient.png")
	transientExport := serverData[preparedExport](t, creator.json("POST", path+"/export/prepare", map[string]any{"asset_ids": []string{transient.ID}}, 200))
	creator.json("DELETE", "/api/v1/assets/"+transient.ID, nil, 200)
	creator.json("GET", transientExport.DownloadURL, nil, 422)
	secondAsset := upload(creator, secondProject.ID, "other-project.png")
	secondExport := serverData[preparedExport](t, creator.json("POST", "/api/v1/projects/"+secondProject.ID+"/export/prepare", map[string]any{"asset_ids": []string{secondAsset.ID}}, 200))
	secondProject = serverData[projects.Project](t, creator.json("PATCH", "/api/v1/projects/"+secondProject.ID, map[string]any{"revision": secondProject.Revision, "status": "archived"}, 200))
	creator.json("DELETE", "/api/v1/projects/"+secondProject.ID, map[string]any{"revision": secondProject.Revision}, 200)
	creator.json("GET", secondExport.DownloadURL, nil, 404)

	// Missing objects fail before ZIP headers, so the browser receives an error
	// instead of a deceptively successful, truncated archive.
	missing := upload(creator, project.ID, "missing-object.png")
	missingExport := serverData[preparedExport](t, creator.json("POST", path+"/export/prepare", map[string]any{"asset_ids": []string{first.ID, missing.ID}}, 200))
	var key string
	if err := db.QueryRow(context.Background(), `SELECT object_key FROM assets WHERE id=$1`, missing.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	_, headers = creator.raw("GET", missingExport.DownloadURL, "", nil, 503)
	if strings.Contains(headers.Get("Content-Type"), "zip") || headers.Get("Content-Disposition") != "" {
		t.Fatal("missing object emitted an archive attachment")
	}
	oversized := upload(creator, project.ID, "oversized-metadata.png")
	if _, err := db.Exec(context.Background(), `UPDATE assets SET size_bytes=$2 WHERE id=$1`, oversized.ID, int64(2<<30)+1); err != nil {
		t.Fatal(err)
	}
	creator.json("POST", path+"/export/prepare", map[string]any{"asset_ids": []string{oversized.ID}}, 422)
	tooMany := make([]string, 201)
	for i := range tooMany {
		tooMany[i] = first.ID
	}
	creator.json("POST", path+"/export/prepare", map[string]any{"asset_ids": tooMany}, 422)
	if after := financial(); after != before {
		t.Fatal("preparing or retrying exports changed task or financial state", before, after)
	}
}
