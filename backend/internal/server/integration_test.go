package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/commerce"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/storage"
	"wsapi-video/migrations"
)

func serverIntegrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for the full-platform integration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_server_" + strings.TrimPrefix(core.ID(""), "_")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

type studioClient struct {
	t      *testing.T
	origin string
	http   *http.Client
	csrf   string
	user   core.User
}

func newStudioClient(t *testing.T, origin string) *studioClient {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &studioClient{t: t, origin: origin, http: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}
func (c *studioClient) raw(method, path, contentType string, body io.Reader, want int) ([]byte, http.Header) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.origin+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Origin", c.origin)
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: got %d, want %d: %s", method, path, resp.StatusCode, want, b)
	}
	return b, resp.Header
}
func (c *studioClient) json(method, path string, body any, want int) []byte {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	b, _ := c.raw(method, path, "application/json", r, want)
	return b
}
func serverData[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var envelope struct {
		Code string `json:"code"`
		Data T      `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err, string(raw))
	}
	if envelope.Code != "ok" {
		t.Fatal(string(raw))
	}
	return envelope.Data
}
func (c *studioClient) registerAndLogin(email, password string) {
	c.t.Helper()
	raw := c.json("POST", "/api/v1/auth/register", map[string]string{"email": email, "password": password, "display_name": "Studio creator"}, 202)
	result := serverData[struct {
		URL string `json:"dev_verification_url"`
	}](c.t, raw)
	link, err := url.Parse(result.URL)
	if err != nil || link.Query().Get("token") == "" {
		c.t.Fatal("missing development verification link")
	}
	c.json("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password}, 403)
	c.json("POST", "/api/v1/auth/verify", map[string]string{"token": link.Query().Get("token")}, 200)
	c.login(email, password)
}
func (c *studioClient) login(email, password string) {
	c.t.Helper()
	raw := c.json("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password}, 200)
	login := serverData[struct {
		User core.User `json:"user"`
		CSRF string    `json:"csrf_token"`
	}](c.t, raw)
	if login.CSRF == "" || login.User.ID == "" {
		c.t.Fatal("login did not establish a server session")
	}
	c.csrf = login.CSRF
	c.user = login.User
}
func fixturePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 36))
	for y := 0; y < 36; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 3), uint8(y * 5), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// This test goes through the composed HTTP routes, real auth sessions, real
// PostgreSQL transactions and private file storage. Demo is explicit and never
// calls a gateway or merchant. It is not a substitute for paid-provider QA.
func TestPlatformProjectToArchivedResultAndZIP(t *testing.T) {
	db := serverIntegrationDB(t)
	httpServer := httptest.NewUnstartedServer(nil)
	origin := "http://" + httpServer.Listener.Addr().String()
	cfg := config.Config{Env: "test", DevMode: true, Demo: true, PublicURL: origin, AppSecret: strings.Repeat("x", 40), StorageDriver: "local", StorageDir: t.TempDir(), WebDir: t.TempDir(), MaxUploadBytes: 8 << 20, MaxMediaBytes: 8 << 20, SessionTTL: time.Hour, MediaPollInterval: 20 * time.Millisecond, WorkerLease: 2 * time.Minute, TaskReviewAfter: time.Hour}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg, db, blobs)
	httpServer.Config.Handler = app.HTTP.Handler
	httpServer.Start()
	defer httpServer.Close()
	adminEmail, adminPassword := "operator@example.test", "Operator-password-2026"
	if err = app.Auth.BootstrapAdmin(context.Background(), adminEmail, adminPassword); err != nil {
		t.Fatal(err)
	}
	admin := newStudioClient(t, origin)
	admin.login(adminEmail, adminPassword)
	creator := newStudioClient(t, origin)
	creator.registerAndLogin("creator@example.test", "Creator-password-2026")
	other := newStudioClient(t, origin)
	other.registerAndLogin("other@example.test", "Another-password-2026")
	if !creator.user.EmailVerified || creator.user.Role != "user" {
		t.Fatal("registration and verification did not create an independent regular account")
	}
	me := serverData[struct {
		User core.User `json:"user"`
		CSRF string    `json:"csrf_token"`
	}](t, creator.json("GET", "/api/v1/me", nil, 200))
	if me.User.ID != creator.user.ID || me.CSRF != creator.csrf {
		t.Fatal("cookie session did not survive another request")
	}
	admin.json("POST", "/api/v1/admin/wallet/adjust", map[string]any{"user_id": creator.user.ID, "amount": 100, "request_key": "integration-development-credit", "reason": "Explicit development test credits"}, 200)
	csrf := creator.csrf
	creator.csrf = ""
	creator.json("POST", "/api/v1/projects", map[string]string{"name": "Missing CSRF"}, 403)
	creator.csrf = csrf
	project := serverData[projects.Project](t, creator.json("POST", "/api/v1/projects", map[string]string{"name": "The quiet studio", "description": "Full-platform integration fixture"}, 201))
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	if err = form.WriteField("project_id", project.ID); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("file", "reference.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(fixturePNG(t)); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	uploadRaw, _ := creator.raw("POST", "/api/v1/assets", form.FormDataContentType(), &upload, 201)
	reference := serverData[assets.Asset](t, uploadRaw)
	if reference.Kind != "image" || reference.Width != 64 || reference.Height != 36 {
		t.Fatal("upload metadata was not detected from the PNG", reference)
	}
	products := serverData[struct {
		Items []commerce.Product `json:"items"`
		Demo  bool               `json:"demo"`
	}](t, creator.json("GET", "/api/v1/model-products", nil, 200))
	if !products.Demo || len(products.Items) != 1 || !products.Items[0].Demo {
		t.Fatal("demo catalog leaked real or unavailable products", products)
	}
	quote := serverData[commerce.Quote](t, creator.json("POST", "/api/v1/quotes", commerce.QuoteInput{ProjectID: project.ID, ProductID: products.Items[0].ID, Prompt: "Quiet amber studio light", AspectRatio: "16:9", ReferenceAssetIDs: []string{reference.ID}}, 201))
	if !quote.Demo || quote.Credits != 10 {
		t.Fatal(quote)
	}
	submission := map[string]string{"quote_id": quote.ID, "request_key": "full-platform-create-once"}
	task := serverData[commerce.Task](t, creator.json("POST", "/api/v1/tasks", submission, 201))
	wallet := serverData[commerce.Wallet](t, creator.json("GET", "/api/v1/wallet", nil, 200))
	if wallet.Available != 90 || wallet.Frozen != 10 {
		t.Fatal("task was not frozen atomically", wallet)
	}
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { app.Commerce.Run(ctx); close(workerDone) }()
	defer func() {
		cancel()
		select {
		case <-workerDone:
		case <-time.After(5 * time.Second):
			t.Error("workers did not stop cleanly")
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task = serverData[commerce.Task](t, creator.json("GET", "/api/v1/tasks/"+task.ID, nil, 200))
		if task.DeliveryStatus == "ready" {
			break
		}
		if task.ExecutionStatus == "failed" || task.ErrorCode != "" {
			t.Fatalf("demo job failed to archive: %#v", task)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !task.Demo || task.DeliveryStatus != "ready" || task.SettlementStatus != "consumed" || len(task.AssetIDs) != 1 {
		t.Fatalf("task did not become a persisted deliverable: %#v", task)
	}
	wallet = serverData[commerce.Wallet](t, creator.json("GET", "/api/v1/wallet", nil, 200))
	if wallet.Available != 90 || wallet.Frozen != 0 {
		t.Fatal("archived result was not settled exactly once", wallet)
	}
	result := serverData[assets.Asset](t, creator.json("GET", "/api/v1/assets/"+task.AssetIDs[0], nil, 200))
	if result.TaskID != task.ID || !strings.HasPrefix(result.FileName, "DEMO-") {
		t.Fatal("the result is not an explicit archived demo fixture", result)
	}
	download, _ := creator.raw("GET", result.DownloadURL, "", nil, 200)
	pngResult, err := png.Decode(bytes.NewReader(download))
	if err != nil || pngResult.Bounds().Dx() != 1024 || pngResult.Bounds().Dy() != 576 {
		t.Fatal("archived image is not an actual downloadable PNG", err)
	}
	canvasBody := map[string]any{"graph_schema_version": 1, "revision": 0, "graph": map[string]any{"nodes": []any{map[string]any{"id": "output-node", "type": "image", "asset_id": result.ID, "task_id": task.ID}}, "edges": []any{}, "viewport": map[string]any{"x": 0, "y": 0, "zoom": 1}}}
	savedCanvas := serverData[projects.Canvas](t, creator.json("PUT", "/api/v1/projects/"+project.ID+"/canvas", canvasBody, 200))
	if savedCanvas.Revision != 1 {
		t.Fatal(savedCanvas)
	}
	creator.json("PUT", "/api/v1/projects/"+project.ID+"/canvas", canvasBody, 409)
	shots := projects.ShotCollection{Revision: 0, Shots: []projects.Shot{{ID: "shot-integration", Number: 1, Title: "Opening shot", Prompt: "Quiet amber studio light", AspectRatio: "16:9", ReferenceAssetIDs: []string{reference.ID}, ImageCandidateIDs: []string{result.ID}, SelectedImageID: result.ID}}}
	savedShots := serverData[projects.ShotCollection](t, creator.json("PUT", "/api/v1/projects/"+project.ID+"/shots", shots, 200))
	if savedShots.Revision != 1 || savedShots.Shots[0].SelectedImageID != result.ID {
		t.Fatal(savedShots)
	}
	// A composed authenticated route must expose the saved locations without
	// letting another account discover them or changing the creation facts.
	beforeReferences := [][]byte{
		creator.json("GET", "/api/v1/wallet", nil, 200),
		creator.json("GET", "/api/v1/projects/"+project.ID+"/canvas", nil, 200),
		creator.json("GET", "/api/v1/projects/"+project.ID+"/shots", nil, 200),
	}
	referencesPath := "/api/v1/assets/" + result.ID + "/references"
	references := serverData[struct {
		Items []struct {
			OwnerType string `json:"owner_type"`
			OwnerID   string `json:"owner_id"`
		} `json:"items"`
		Total int `json:"total"`
	}](t, creator.json("GET", referencesPath, nil, 200))
	owners := map[string]string{}
	for _, reference := range references.Items {
		owners[reference.OwnerType] = reference.OwnerID
	}
	if references.Total != 3 || len(owners) != 3 || owners["canvas"] != project.ID || owners["shot"] != "shot-integration" || owners["task"] != task.ID {
		t.Fatal("saved canvas, shot and source task references were not reachable", references)
	}
	other.json("GET", referencesPath, nil, 404)
	newStudioClient(t, origin).json("GET", referencesPath, nil, 401)
	for i, endpoint := range []string{"/api/v1/wallet", "/api/v1/projects/" + project.ID + "/canvas", "/api/v1/projects/" + project.ID + "/shots"} {
		if !bytes.Equal(beforeReferences[i], creator.json("GET", endpoint, nil, 200)) {
			t.Fatal("reference inspection changed creation facts", endpoint)
		}
	}
	zipBody, zipHeaders := creator.raw("POST", "/api/v1/projects/"+project.ID+"/export", "application/json", strings.NewReader("{}"), 200)
	if zipHeaders.Get("Content-Type") != "application/zip" {
		t.Fatal("export did not return an archive")
	}
	archive, err := zip.NewReader(bytes.NewReader(zipBody), int64(len(zipBody)))
	if err != nil || len(archive.File) != 1 {
		t.Fatal("selected deliverable was not exported", err)
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	zipped, err := io.ReadAll(entry)
	entry.Close()
	if err != nil || !bytes.Equal(zipped, download) {
		t.Fatal("ZIP contents differ from the selected archived image", err)
	}
	repeated := serverData[commerce.Task](t, creator.json("POST", "/api/v1/tasks", submission, 200))
	if repeated.ID != task.ID {
		t.Fatal("idempotent retry created a new task")
	}
	creator.raw("POST", "/api/v1/projects/"+project.ID+"/export", "application/json", strings.NewReader("{}"), 200)
	wallet = serverData[commerce.Wallet](t, creator.json("GET", "/api/v1/wallet", nil, 200))
	if wallet.Available != 90 || wallet.Frozen != 0 {
		t.Fatal("task or export retry charged twice", wallet)
	}
	ledger := serverData[struct {
		Items []commerce.Ledger `json:"items"`
	}](t, creator.json("GET", "/api/v1/wallet/ledger", nil, 200))
	events := map[string]int{}
	for _, event := range ledger.Items {
		events[event.EventType]++
	}
	if events["freeze"] != 1 || events["consumed"] != 1 || events["adjustment"] != 1 {
		t.Fatal("financial event count is inconsistent", events)
	}
	for _, path := range []string{"/api/v1/projects/" + project.ID, "/api/v1/projects/" + project.ID + "/canvas", "/api/v1/projects/" + project.ID + "/shots", "/api/v1/tasks/" + task.ID, "/api/v1/assets/" + reference.ID, "/api/v1/assets/" + result.ID, "/api/v1/assets/" + result.ID + "/download"} {
		other.json("GET", path, nil, 404)
	}
	other.json("POST", "/api/v1/quotes", commerce.QuoteInput{ProjectID: project.ID, ProductID: products.Items[0].ID, Prompt: "Unauthorized reference"}, 404)
	other.json("POST", "/api/v1/projects/"+project.ID+"/export", map[string]any{}, 404)
	other.json("GET", "/api/v1/admin/overview", nil, 403)
	overview := serverData[map[string]json.RawMessage](t, admin.json("GET", "/api/v1/admin/overview", nil, 200))
	if len(overview["assets"]) == 0 || len(overview["wallet"]) == 0 {
		t.Fatal("operations routes are missing from the composed application")
	}
	creator.json("POST", "/api/v1/payment/orders", map[string]string{"tier_id": "starter", "method": "alipay"}, 503)
}
