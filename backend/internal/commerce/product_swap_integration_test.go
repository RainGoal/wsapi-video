package commerce

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/media"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/settings"
	"wsapi-video/internal/storage"
)

func TestPostgresProductSwapWorkspaceQuoteAndIdempotency(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	user := seedUser(t, db, 100)
	other := seedUser(t, db, 100)
	cfg := config.Config{DevMode: true, StorageDriver: "local", StorageDir: t.TempDir(), PublicURL: "http://127.0.0.1:8090", AppSecret: strings.Repeat("test", 12)}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, cfg, assets.New(db, cfg, blobs))
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer gateway.Close()
	s.gateway, err = media.NewConnector(media.Config{Origin: gateway.URL, AllowHTTP: true, Keys: map[string]string{"video": "fixture"}, VideoCredential: "video"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doRequest(s, user, "POST", "/api/v1/product-swap/workspace", nil)
			if rec.Code != 200 {
				t.Error(rec.Code, rec.Body.String())
				return
			}
			ids <- data[struct {
				ID string `json:"project_id"`
			}](t, rec).ID
		}()
	}
	wg.Wait()
	close(ids)
	workspace := ""
	for id := range ids {
		if workspace != "" && workspace != id {
			t.Fatal("concurrent workspaces duplicated")
		}
		workspace = id
	}
	if workspace == "" {
		t.Fatal("workspace not created")
	}
	otherWorkspace := data[struct {
		ID string `json:"project_id"`
	}](t, doRequest(s, other, "POST", "/api/v1/product-swap/workspace", nil)).ID
	if otherWorkspace == workspace {
		t.Fatal("workspace crossed accounts")
	}
	var canvasCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM canvases WHERE project_id=$1", workspace).Scan(&canvasCount); err != nil || canvasCount != 0 {
		t.Fatal("tool created a canvas", err)
	}
	mux := http.NewServeMux()
	identity := func(h http.Handler) http.Handler { return h }
	projects.New(db, cfg, blobs).Register(mux, identity, identity)
	req := httptest.NewRequest("GET", "/api/v1/projects?status=all", nil)
	req = req.WithContext(core.WithUser(req.Context(), core.User{ID: user}))
	list := httptest.NewRecorder()
	mux.ServeHTTP(list, req)
	if list.Code != 200 || strings.Contains(list.Body.String(), workspace) {
		t.Fatal("tool leaked into project list", list.Body.String())
	}
	for _, item := range []struct{ id, owner, project, kind string }{{"source", user, workspace, "video"}, {"image", user, workspace, "image"}, {"other-image", other, otherWorkspace, "image"}} {
		_, err = db.Exec(ctx, `INSERT INTO assets(id,user_id,project_id,kind,file_name,content_type,object_key,size_bytes,width,height,duration_seconds,sha256) VALUES($1,$2,$3,$4,$1,'application/octet-stream',$1,100,720,1280,5,'fixture')`, item.id, item.owner, item.project, item.kind)
		if err != nil {
			t.Fatal(err)
		}
	}
	p := swapProductFixture()
	caps, _ := json.Marshal(p.Capabilities)
	_, err = db.Exec(ctx, `INSERT INTO model_products(id,name,kind,model,credits,capabilities,active,verified) VALUES($1,'Seedance','video',$2,10,$3,true,true)`, p.ID, p.Model, caps)
	if err != nil {
		t.Fatal(err)
	}
	newInput := func() QuoteInput {
		return QuoteInput{ProjectID: workspace, ProductID: p.ID, Duration: 5, Resolution: "720p", AspectRatio: "9:16", ProductSwap: &ProductSwapInput{SourceVideoID: "source", Images: []ProductSwapImage{{"image", "front"}}, Target: "手中白瓶", PreserveScene: true, AudioMode: "generated"}}
	}
	for _, tc := range []string{"foreign", "wrong_kind", "ordinary_project", "extra_prompt", "unverified_tool"} {
		t.Run(tc, func(t *testing.T) {
			in := newInput()
			switch tc {
			case "foreign":
				in.ProductSwap.Images[0].AssetID = "other-image"
			case "wrong_kind":
				in.ProductSwap.SourceVideoID = "image"
			case "ordinary_project":
				in.ProjectID = seedProject(t, db, user)
			case "extra_prompt":
				in.Prompt = "override"
			case "unverified_tool":
				_, err = db.Exec(ctx, `UPDATE model_products SET capabilities=jsonb_set(capabilities,'{supports_product_swap}','false') WHERE id=$1`, p.ID)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Exec(ctx, "UPDATE model_products SET capabilities=$2 WHERE id=$1", p.ID, caps)
			}
			rec := doRequest(s, user, "POST", "/api/v1/quotes", in)
			if rec.Code != 422 {
				t.Fatal(rec.Code, rec.Body.String())
			}
		})
	}
	analysis := swapAnalysis{ID: "analysis-test", Target: "白瓶", Action: "举起", Occlusion: "手指", ProductDescription: "新蓝瓶"}
	raw, _ := json.Marshal(analysis)
	analysisInput := swapAnalysisInput{workspace, "source", []ProductSwapImage{{"image", "front"}}}
	if _, err = db.Exec(ctx, "INSERT INTO product_swap_analyses(id,user_id,input_hash,result) VALUES($1,$2,$3,$4)", analysis.ID, user, digest(analysisInput), raw); err != nil {
		t.Fatal(err)
	}
	in := newInput()
	in.ProductSwap.AnalysisID = analysis.ID
	in.ProductSwap.Images[0].Angle = "back"
	if rec := doRequest(s, user, "POST", "/api/v1/quotes", in); rec.Code != 422 {
		t.Fatal("stale analysis accepted", rec.Body.String())
	}
	in.ProductSwap.Images[0].Angle = "front"
	rec := doRequest(s, user, "POST", "/api/v1/quotes", in)
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	quote := data[Quote](t, rec)
	if quote.Credits != 10 || !strings.Contains(quote.Prompt, analysis.Action) {
		t.Fatal("missing server quote or analysis", quote)
	}
	assertBalance(t, db, user, 100, 0)
	var accepted string
	for i := 0; i < 2; i++ {
		rec = doRequest(s, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": "swap-original-request"})
		if rec.Code != 201 && rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		task := data[Task](t, rec)
		if accepted != "" && task.ID != accepted {
			t.Fatal("idempotent request created a duplicate")
		}
		accepted = task.ID
		if task.Input.ProductSwap == nil || task.Prompt != quote.Prompt {
			t.Fatal("immutable swap snapshot missing")
		}
		// UserID is deliberately omitted from the public task JSON.
		task, err = s.getTask(ctx, user, task.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		gatewayInput, err := s.buildGatewayInput(ctx, task)
		if err != nil || len(gatewayInput.Images) != 1 || len(gatewayInput.Videos) != 1 {
			t.Fatal(fmt.Sprintf("invalid references: %+v", gatewayInput), err)
		}
	}
	assertVideoProtocolFacts(t, db, user, 1, 1, 90, 10)
	if calls.Load() != 0 {
		t.Fatal("quote or acceptance called upstream")
	}
	unauthorized := doRequest(s, other, "GET", "/api/v1/tasks/"+accepted, nil)
	if unauthorized.Code != 404 {
		t.Fatal("task crossed accounts", unauthorized.Code)
	}
}

func TestPostgresSwapVisionCredentialIsEncryptedAndRedacted(t *testing.T) {
	db := integrationDB(t)
	admin := seedUser(t, db, 100)
	ctx := context.Background()
	cfg := config.Config{DevMode: true, PublicURL: "https://studio.example", AppSecret: strings.Repeat("swap-test", 6), StorageDriver: "local", SMTPPort: 587,
		EasyPayNotifyURL: "https://studio.example/api/v1/payment/notify/easypay", EasyPayReturnURL: "https://studio.example/wallet"}
	runtime := settings.New(db, cfg)
	st, err := runtime.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	in := settings.Update{Revision: st.Revision, Reason: "Configure optional vision for product replacement", Settings: st.Settings}
	in.Settings.Media.Origin, in.Settings.Media.AnalysisModel, in.Settings.Media.AnalysisKey = "https://gateway.example", "configured-vision", "private-vision-fixture"
	if err = runtime.Save(ctx, admin, in); err != nil {
		t.Fatal(err)
	}
	view, err := runtime.View(ctx)
	if err != nil || !view.Settings.Media.AnalysisKeyConfigured || view.Settings.Media.AnalysisKey != "" {
		t.Fatal("vision secret returned", err)
	}
	public, _ := json.Marshal(view)
	var encrypted, audit string
	if err = db.QueryRow(ctx, "SELECT encode(payload,'escape') FROM runtime_settings").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "SELECT details::text FROM admin_audit WHERE action='settings.update'").Scan(&audit); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{string(public), encrypted, audit} {
		if strings.Contains(text, in.Settings.Media.AnalysisKey) {
			t.Fatal("vision credential exposed")
		}
	}
	in.Revision, in.Settings.Media.AnalysisKey = view.Revision, ""
	if err = runtime.Save(ctx, admin, in); err != nil {
		t.Fatal(err)
	}
	st, err = runtime.Current(ctx)
	if err != nil || st.Settings.Media.AnalysisKey != "private-vision-fixture" {
		t.Fatal("blank secret did not preserve original", err)
	}
	in.Revision, in.ClearSecrets, in.Settings.Media.AnalysisModel = st.Revision, []string{"media.analysis_key"}, ""
	if err = runtime.Save(ctx, admin, in); err != nil {
		t.Fatal(err)
	}
	view, err = runtime.View(ctx)
	if err != nil || view.Settings.Media.AnalysisKeyConfigured {
		t.Fatal("explicit secret clearing failed", err)
	}
	assertBalance(t, db, admin, 100, 0)
}
