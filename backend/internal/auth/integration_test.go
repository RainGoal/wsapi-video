package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/auth"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/storage"
)

type fixture struct {
	t      *testing.T
	db     *pgxpool.Pool
	server *httptest.Server
	auth   *auth.Service
	assets *assets.Service
}
type apiClient struct {
	f            *fixture
	client       *http.Client
	csrf, userID string
}
type envelope struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setup(t *testing.T) *fixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for isolated PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_foundation_" + strings.TrimPrefix(core.ID("s"), "s_")
	if !strings.HasPrefix(schema, "test_foundation_") {
		t.Fatal("unsafe test schema")
	}
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	poolConfig.MaxConns = 8
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		admin.Close()
	})
	_, currentFile, _, _ := runtime.Caller(0)
	for _, name := range []string{"001_identity_projects_assets.sql", "008_product_swap.sql", "009_user_management.sql"} {
		migration, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(ctx, `CREATE TABLE wallet_accounts(user_id text PRIMARY KEY REFERENCES users(id),available bigint NOT NULL DEFAULT 0,frozen bigint NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	cfg := config.Config{DevMode: true, PublicURL: server.URL, SessionTTL: 24 * time.Hour, StorageDir: t.TempDir(), AppSecret: "integration-test-secret-32-characters", MaxUploadBytes: 4 << 20, MaxMediaBytes: 4 << 20}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if local, ok := blobs.(*storage.Local); ok {
		local.Register(mux)
	}
	identity := auth.New(db, cfg)
	identity.Register(mux)
	media := assets.New(db, cfg, blobs)
	media.Register(mux, identity.Middleware, identity.Admin)
	projects.New(db, cfg, blobs).Register(mux, identity.Middleware, identity.Admin)
	return &fixture{t: t, db: db, server: server, auth: identity, assets: media}
}
func (f *fixture) client() *apiClient {
	jar, _ := cookiejar.New(nil)
	return &apiClient{f: f, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}
func (c *apiClient) request(method, path string, body any, expected int) json.RawMessage {
	c.f.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			c.f.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, c.f.server.URL+"/api/v1"+path, reader)
	if err != nil {
		c.f.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		request.Header.Set("X-CSRF-Token", c.csrf)
	}
	response, err := c.client.Do(request)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		c.f.t.Fatal(err)
	}
	if response.StatusCode != expected {
		c.f.t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, response.StatusCode, expected, raw)
	}
	var out envelope
	if err = json.Unmarshal(raw, &out); err != nil {
		c.f.t.Fatalf("decode %s: %v", raw, err)
	}
	return out.Data
}
func (c *apiClient) signup(email string) {
	c.f.t.Helper()
	raw := c.request("POST", "/auth/register", map[string]any{"email": email, "password": "correct-password-123", "display_name": "创作者"}, 202)
	var registration struct {
		Link string `json:"dev_verification_url"`
	}
	_ = json.Unmarshal(raw, &registration)
	link, err := url.Parse(registration.Link)
	if err != nil || link.Query().Get("token") == "" {
		c.f.t.Fatalf("missing development verification URL: %s", raw)
	}
	c.request("POST", "/auth/login", map[string]any{"email": email, "password": "correct-password-123"}, 403)
	c.request("POST", "/auth/verify", map[string]any{"token": link.Query().Get("token")}, 200)
	c.request("POST", "/auth/verify", map[string]any{"token": link.Query().Get("token")}, 422)
	c.login(email, "correct-password-123")
}
func (c *apiClient) login(email, password string) {
	raw := c.request("POST", "/auth/login", map[string]any{"email": email, "password": password}, 200)
	var out struct {
		User core.User `json:"user"`
		CSRF string    `json:"csrf_token"`
	}
	_ = json.Unmarshal(raw, &out)
	c.csrf = out.CSRF
	c.userID = out.User.ID
	if c.csrf == "" || c.userID == "" {
		c.f.t.Fatalf("invalid login result: %s", raw)
	}
}
func object(raw json.RawMessage) map[string]any {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value
}
func (c *apiClient) project(name string) string {
	return object(c.request("POST", "/projects", map[string]any{"name": name, "description": "测试项目"}, 201))["id"].(string)
}
func pngData(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	bitmap := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	bitmap.Set(1, 1, color.NRGBA{R: 210, G: 170, B: 100, A: 255})
	if err := png.Encode(&b, bitmap); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func (c *apiClient) upload(projectID string) assets.Asset {
	c.f.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("project_id", projectID)
	part, _ := form.CreateFormFile("file", "reference.png")
	_, _ = part.Write(pngData(c.f.t))
	_ = form.Close()
	request, _ := http.NewRequest("POST", c.f.server.URL+"/api/v1/assets", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-CSRF-Token", c.csrf)
	response, err := c.client.Do(request)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != 201 {
		c.f.t.Fatalf("upload: %d %s", response.StatusCode, raw)
	}
	var out envelope
	var asset assets.Asset
	_ = json.Unmarshal(raw, &out)
	_ = json.Unmarshal(out.Data, &asset)
	return asset
}

func TestFoundationPostgresIntegration(t *testing.T) {
	f := setup(t)
	first, second := f.client(), f.client()
	first.signup("first@example.com")
	second.signup("second@example.com")
	projectID := first.project("主项目")
	otherProject := second.project("其他项目")
	second.request("GET", "/projects/"+projectID, nil, 404)
	second.request("GET", "/projects/"+projectID+"/canvas", nil, 404)
	second.request("GET", "/projects/"+projectID+"/shots", nil, 404)
	t.Run("csrf and concurrent revision", func(t *testing.T) {
		body := map[string]any{"graph_schema_version": 1, "revision": 0, "graph": map[string]any{"nodes": []any{}, "edges": []any{}}}
		savedCSRF := first.csrf
		first.csrf = ""
		first.request("PUT", "/projects/"+projectID+"/canvas", body, 403)
		first.csrf = savedCSRF
		result := object(first.request("PUT", "/projects/"+projectID+"/canvas", body, 200))
		if result["revision"] != float64(1) {
			t.Fatalf("revision not incremented: %v", result)
		}
		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				payload := fmt.Sprintf(`{"graph_schema_version":1,"revision":1,"graph":{"nodes":[{"id":"n%d"}],"edges":[]}}`, index)
				request, _ := http.NewRequest("PUT", f.server.URL+"/api/v1/projects/"+projectID+"/canvas", strings.NewReader(payload))
				request.Header.Set("X-CSRF-Token", first.csrf)
				response, err := first.client.Do(request)
				if err != nil {
					statuses <- 0
					return
				}
				defer response.Body.Close()
				_, _ = io.Copy(io.Discard, response.Body)
				statuses <- response.StatusCode
			}(i)
		}
		wg.Wait()
		close(statuses)
		counts := map[int]int{}
		for status := range statuses {
			counts[status]++
		}
		if counts[200] != 1 || counts[409] != 1 {
			t.Fatalf("concurrent save must accept exactly one: %v", counts)
		}
	})
	asset := first.upload(projectID)
	if asset.Kind != "image" || asset.Width != 4 || asset.Height != 3 {
		t.Fatalf("incorrect actual media metadata: %+v", asset)
	}
	t.Run("private assets and durable references", func(t *testing.T) {
		second.request("GET", "/assets/"+asset.ID, nil, 404)
		second.request("GET", "/assets/"+asset.ID+"/download", nil, 404)
		for _, field := range []string{"referenceIds", "connectedAssetIds", "startFrameId", "endFrameId"} {
			var reference any = asset.ID
			if field == "referenceIds" || field == "connectedAssetIds" {
				reference = []string{asset.ID}
			}
			second.request("PUT", "/projects/"+otherProject+"/canvas", map[string]any{"graph_schema_version": 1, "revision": 0, "graph": map[string]any{"nodes": []any{map[string]any{"id": "n", "metadata": map[string]any{"generationDraft": map[string]any{field: reference}}}}, "edges": []any{}}}, 422)
		}
		second.request("PUT", "/projects/"+otherProject+"/canvas", map[string]any{"graph_schema_version": 1, "revision": 0, "graph": map[string]any{"nodes": []any{map[string]any{"id": "n", "asset_id": asset.ID}}, "edges": []any{}}}, 422)
		first.request("PUT", "/projects/"+projectID+"/canvas", map[string]any{"graph_schema_version": 1, "revision": 2, "graph": map[string]any{"nodes": []any{map[string]any{"id": "n", "metadata": map[string]any{"generationDraft": map[string]any{"referenceIds": []string{asset.ID}}}}}, "edges": []any{}}}, 200)
		first.request("DELETE", "/assets/"+asset.ID, map[string]any{}, 409)
		response, err := first.client.Get(f.server.URL + asset.PreviewURL)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("signed preview status %d", response.StatusCode)
		}
		raw, _ := io.ReadAll(response.Body)
		if !bytes.Equal(raw, pngData(t)) {
			t.Fatal("preview media differs from upload")
		}
	})
	t.Run("shots revision and candidate selection", func(t *testing.T) {
		shot := projects.Shot{ID: "shot_test", Title: "镜头一", Prompt: "warm studio", ImageCandidateIDs: []string{asset.ID}, SelectedImageID: asset.ID}
		first.request("PUT", "/projects/"+projectID+"/shots", projects.ShotCollection{Revision: 0, Shots: []projects.Shot{shot}}, 200)
		first.request("PUT", "/projects/"+projectID+"/shots", projects.ShotCollection{Revision: 0, Shots: []projects.Shot{}}, 409)
		invalid := shot
		invalid.SelectedImageID = "unlisted"
		first.request("PUT", "/projects/"+projectID+"/shots", projects.ShotCollection{Revision: 1, Shots: []projects.Shot{invalid}}, 422)
		var collection projects.ShotCollection
		_ = json.Unmarshal(first.request("GET", "/projects/"+projectID+"/shots", nil, 200), &collection)
		if collection.Revision != 1 || collection.Shots[0].SelectedImageID != asset.ID {
			t.Fatalf("conflict mutated selection: %+v", collection)
		}
		request, _ := http.NewRequest("POST", f.server.URL+"/api/v1/projects/"+projectID+"/export", strings.NewReader(`{"asset_ids":[]}`))
		request.Header.Set("X-CSRF-Token", first.csrf)
		response, err := first.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 || !bytes.HasPrefix(data, []byte("PK")) {
			t.Fatalf("invalid selected asset export: %d", response.StatusCode)
		}
	})
	t.Run("conflict draft copy keeps private immutable references", func(t *testing.T) {
		draft := map[string]any{"nodes": []any{map[string]any{"id": "node_copy", "data": map[string]any{"asset_id": asset.ID, "shot_id": "shot_test", "task_id": "historical_readonly"}}}, "edges": []any{}}
		copyID := object(first.request("POST", "/projects/"+projectID+"/duplicate", map[string]any{"name": "冲突草稿副本", "graph": draft}, 201))["id"].(string)
		var copiedShots projects.ShotCollection
		_ = json.Unmarshal(first.request("GET", "/projects/"+copyID+"/shots", nil, 200), &copiedShots)
		if len(copiedShots.Shots) != 1 || copiedShots.Shots[0].ID == "shot_test" || copiedShots.Shots[0].SelectedImageID != asset.ID {
			t.Fatalf("invalid copied candidates: %+v", copiedShots)
		}
		var canvas projects.Canvas
		_ = json.Unmarshal(first.request("GET", "/projects/"+copyID+"/canvas", nil, 200), &canvas)
		if !bytes.Contains(canvas.Graph, []byte(copiedShots.Shots[0].ID)) || !bytes.Contains(canvas.Graph, []byte("historical_readonly")) {
			t.Fatalf("copy did not retain provenance/remap shot: %s", canvas.Graph)
		}
		var listed struct {
			Items []assets.Asset `json:"items"`
		}
		_ = json.Unmarshal(first.request("GET", "/assets?project_id="+copyID, nil, 200), &listed)
		if len(listed.Items) != 1 || listed.Items[0].ID != asset.ID {
			t.Fatalf("referenced media missing in duplicate: %+v", listed)
		}
		localShot := copiedShots.Shots[0]
		localShot.Title = "未保存的分镜草稿"
		draftCopyID := object(first.request("POST", "/projects/"+copyID+"/duplicate", map[string]any{"name": "分镜草稿副本", "shots": []projects.Shot{localShot}}, 201))["id"].(string)
		var draftShots projects.ShotCollection
		_ = json.Unmarshal(first.request("GET", "/projects/"+draftCopyID+"/shots", nil, 200), &draftShots)
		if len(draftShots.Shots) != 1 || draftShots.Shots[0].Title != localShot.Title || draftShots.Shots[0].ID == localShot.ID || draftShots.Shots[0].SelectedImageID != asset.ID {
			t.Fatalf("shot draft was not atomically preserved: %+v", draftShots)
		}
		invalidDraft := localShot
		invalidDraft.SelectedImageID = "not-a-candidate"
		first.request("POST", "/projects/"+copyID+"/duplicate", map[string]any{"shots": []projects.Shot{invalidDraft}}, 422)
		first.request("PUT", "/projects/"+projectID+"/canvas", map[string]any{"graph_schema_version": 1, "revision": 3, "graph": map[string]any{"nodes": []any{}, "edges": []any{}}}, 200)
		first.request("PUT", "/projects/"+projectID+"/shots", projects.ShotCollection{Revision: 1, Shots: []projects.Shot{}}, 200)
		first.request("DELETE", "/assets/"+asset.ID, map[string]any{}, 409)
	})
	t.Run("archive idempotency", func(t *testing.T) {
		archived, err := f.assets.Archive(context.Background(), first.userID, projectID, "task_fixed", "generated.png", "image/png", bytes.NewReader(pngData(t)))
		if err != nil {
			t.Fatal(err)
		}
		again, err := f.assets.Archive(context.Background(), first.userID, projectID, "task_fixed", "generated.png", "image/png", strings.NewReader("not media"))
		if err != nil || again.ID != archived.ID {
			t.Fatalf("archive idempotency failed: %+v %v", again, err)
		}
		first.request("DELETE", "/assets/"+archived.ID, map[string]any{}, 409)
	})
	t.Run("multi device session revoke and password reset", func(t *testing.T) {
		another := f.client()
		another.login("first@example.com", "correct-password-123")
		var sessions struct {
			Items []struct {
				ID      string `json:"id"`
				Current bool   `json:"current"`
			} `json:"items"`
		}
		_ = json.Unmarshal(another.request("GET", "/auth/sessions", nil, 200), &sessions)
		for _, session := range sessions.Items {
			if !session.Current {
				another.request("DELETE", "/auth/sessions/"+session.ID, nil, 200)
			}
		}
		first.request("GET", "/me", nil, 401)
		raw := another.request("POST", "/auth/forgot-password", map[string]any{"email": "first@example.com"}, 202)
		link, _ := url.Parse(object(raw)["dev_reset_url"].(string))
		token := link.Query().Get("token")
		another.request("POST", "/auth/reset-password", map[string]any{"token": token, "password": "replacement-password-123"}, 200)
		another.request("GET", "/me", nil, 401)
		another.request("POST", "/auth/reset-password", map[string]any{"token": token, "password": "replacement-password-123"}, 422)
		first.login("first@example.com", "replacement-password-123")
	})
	t.Run("admin disable is audited and revokes sessions", func(t *testing.T) {
		if err := f.auth.BootstrapAdmin(context.Background(), "admin@example.com", "administrator-password-123"); err != nil {
			t.Fatal(err)
		}
		admin := f.client()
		admin.login("admin@example.com", "administrator-password-123")
		second.request("GET", "/admin/users", nil, 403)
		admin.request("PATCH", "/admin/users/"+second.userID, map[string]any{"status": "disabled", "reason": "integration check"}, 200)
		second.request("GET", "/me", nil, 401)
		var auditCount int
		if err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM identity_admin_audit WHERE target_user_id=$1`, second.userID).Scan(&auditCount); err != nil || auditCount != 1 {
			t.Fatalf("missing immutable admin audit: %d %v", auditCount, err)
		}
	})
}
