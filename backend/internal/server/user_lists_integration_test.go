package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/storage"
)

type userListPage struct {
	Items  []map[string]json.RawMessage `json:"items"`
	Total  int                          `json:"total"`
	Limit  int                          `json:"limit"`
	Offset int                          `json:"offset"`
}

type historyClient struct{ *studioClient }

func (c *historyClient) json(method, path string, body any, want int) []byte {
	if !strings.HasPrefix(path, "/api/v1/") {
		path = "/api/v1" + path
	}
	return c.studioClient.json(method, path, body, want)
}

// Equal timestamps deliberately exercise the ID tie-breaker. More than the
// former 200/500 caps proves old records remain reachable rather than merely
// testing page metadata on a small fixture.
func seedUserHistory(t *testing.T, db *pgxpool.Pool, userID, projectID, prefix string, count int) {
	t.Helper()
	queries := []string{
		`INSERT INTO projects(id,user_id,name,description,status,created_at,updated_at)
		 SELECT $3||'_project_'||lpad(i::text,4,'0'),$1,CASE WHEN i=1 THEN 'needle-project' ELSE 'History project '||i END,'History fixture',CASE WHEN i=231 THEN 'archived' ELSE 'active' END,'2024-01-01'::timestamptz,'2024-01-01'::timestamptz FROM generate_series(1,$4::integer) i`,
		`INSERT INTO quotes(id,user_id,project_id,product_id,input,input_hash,product_snapshot,credits,expires_at)
		 SELECT $3||'_quote_'||lpad(i::text,4,'0'),$1,$2,'demo-image',jsonb_build_object('prompt',CASE WHEN i=1 THEN 'needle-task' ELSE 'Historical prompt' END,'shot_id',CASE WHEN i=1 THEN 'history-shot' ELSE 'other-shot' END),'fixture','{}',10,now()+interval '1 day' FROM generate_series(1,$4::integer) i`,
		`INSERT INTO generation_tasks(id,user_id,project_id,quote_id,product_id,request_key,request_hash,input,product_snapshot,credits,kind,model,credential_ref,is_demo,execution_status,delivery_status,settlement_status,created_at,updated_at)
		 SELECT $3||'_task_'||lpad(i::text,4,'0'),$1,$2,$3||'_quote_'||lpad(i::text,4,'0'),'demo-image',$3||'_request_'||i,'fixture',jsonb_build_object('prompt',CASE WHEN i=1 THEN 'needle-task' ELSE 'Historical prompt' END,'shot_id',CASE WHEN i=1 THEN 'history-shot' ELSE 'other-shot' END),'{}',10,'image','demo-image','demo',true,CASE WHEN i=1 THEN 'queued' ELSE 'completed' END,CASE WHEN i=1 THEN 'pending' ELSE 'ready' END,CASE WHEN i=1 THEN 'frozen' ELSE 'consumed' END,'2024-01-01'::timestamptz,'2024-01-01'::timestamptz FROM generate_series(1,$4::integer) i`,
		`INSERT INTO payment_orders(id,user_id,out_trade_no,tier_id,tier_version,amount_fen,credits,method,credential_ref,merchant_pid,provider_origin,checkout_url,expires_at,payment_status,credit_status,created_at)
		 SELECT $3||'_order_'||lpad(i::text,4,'0'),$1,$3||'_trade_'||lpad(i::text,4,'0'),'starter',1,1000,1000,'alipay','fixture','fixture','https://fixture.example','https://fixture.example/checkout',now()+interval '1 hour',CASE WHEN i=1 THEN 'expired' ELSE 'paid' END,CASE WHEN i=1 THEN 'pending' ELSE 'credited' END,'2024-01-01'::timestamptz FROM generate_series(1,$4::integer) i`,
		`INSERT INTO wallet_ledger(id,user_id,event_key,event_type,available_delta,frozen_delta,available_after,frozen_after,task_id,reason,created_at)
		 SELECT $3||'_ledger_'||lpad(i::text,4,'0'),$1,$3||'_event_'||i,CASE WHEN i=1 THEN 'freeze' ELSE 'adjustment' END,0,0,0,0,$3||'_task_'||lpad(i::text,4,'0'),CASE WHEN i=1 THEN 'needle-ledger' ELSE 'History fixture' END,'2024-01-01'::timestamptz FROM generate_series(1,$4::integer) i`,
	}
	for _, query := range queries {
		if _, err := db.Exec(context.Background(), query+" WHERE $2::text IS NOT NULL", userID, projectID, prefix, count); err != nil {
			t.Fatal(err)
		}
	}
}

func assertAllUserPages(t *testing.T, c *historyClient, path string, want int, oldID string) {
	t.Helper()
	seen := map[string]bool{}
	join := "?"
	if strings.Contains(path, "?") {
		join = "&"
	}
	for offset := 0; offset < want; offset += 100 {
		page := serverData[userListPage](t, c.json("GET", fmt.Sprintf("%s%slimit=100&offset=%d", path, join, offset), nil, 200))
		if page.Total != want || page.Limit != 100 || page.Offset != offset || len(page.Items) == 0 {
			t.Fatalf("wrong pagination for %s at %d: total=%d limit=%d offset=%d items=%d", path, offset, page.Total, page.Limit, page.Offset, len(page.Items))
		}
		for _, item := range page.Items {
			var id string
			if err := json.Unmarshal(item["id"], &id); err != nil {
				t.Fatal(err)
			}
			if id == "" || seen[id] || strings.HasPrefix(id, "foreign_") {
				t.Fatalf("duplicate, foreign or missing ID in %s: %q", path, id)
			}
			seen[id] = true
		}
	}
	if len(seen) != want || !seen[oldID] {
		t.Fatalf("old history missing for %s: got=%d want=%d old=%v", path, len(seen), want, seen[oldID])
	}
	end := serverData[userListPage](t, c.json("GET", fmt.Sprintf("%s%slimit=100&offset=%d", path, join, want), nil, 200))
	if end.Total != want || len(end.Items) != 0 {
		t.Fatal("past-end page was not empty", path)
	}
}

func TestUserHistoryPaginationSearchAndAccountIsolation(t *testing.T) {
	db := serverIntegrationDB(t)
	httpServer := httptest.NewUnstartedServer(nil)
	origin := "http://" + httpServer.Listener.Addr().String()
	cfg := config.Config{Env: "test", DevMode: true, Demo: true, PublicURL: origin, AppSecret: strings.Repeat("h", 40), StorageDriver: "local", StorageDir: t.TempDir(), WebDir: t.TempDir(), MaxUploadBytes: 8 << 20, MaxMediaBytes: 8 << 20, SessionTTL: time.Hour}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg, db, blobs)
	httpServer.Config.Handler = app.HTTP.Handler
	httpServer.Start()
	defer httpServer.Close()
	owner := &historyClient{newStudioClient(t, origin)}
	owner.registerAndLogin("history-owner@example.test", "History-owner-password-2026")
	other := &historyClient{newStudioClient(t, origin)}
	other.registerAndLogin("history-other@example.test", "History-other-password-2026")
	parent := serverData[projects.Project](t, owner.json("POST", "/api/v1/projects", map[string]string{"name": "Parent"}, 201))
	shared := serverData[projects.Project](t, owner.json("POST", "/api/v1/projects", map[string]string{"name": "Shared reference"}, 201))
	foreign := serverData[projects.Project](t, other.json("POST", "/api/v1/projects", map[string]string{"name": "Other account project"}, 201))
	seedUserHistory(t, db, owner.user.ID, parent.ID, "own", 231)
	seedUserHistory(t, db, other.user.ID, foreign.ID, "foreign", 3)
	for _, seed := range []struct {
		user, project, prefix string
		count                 int
	}{{owner.user.ID, parent.ID, "own", 501}, {other.user.ID, foreign.ID, "foreign", 3}} {
		_, err = db.Exec(context.Background(), `INSERT INTO assets(id,user_id,project_id,kind,file_name,content_type,object_key,size_bytes,width,height,sha256,created_at)
		 SELECT $3||'_asset_'||lpad(i::text,4,'0'),$1,$2,CASE WHEN i=501 THEN 'audio' ELSE 'image' END,CASE WHEN i=1 THEN 'needle-asset.png' ELSE 'Historical asset '||i||'.png' END,'image/png',$3||'/history/'||i||'.png',100,64,36,'fixture','2024-01-01'::timestamptz FROM generate_series(1,$4::integer) i`, seed.user, seed.project, seed.prefix, seed.count)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(context.Background(), "INSERT INTO asset_references(asset_id,project_id,owner_type,owner_id) VALUES('own_asset_0001',$1,'canvas','history-canvas')", shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(context.Background(), "INSERT INTO projects(id,user_id,name,deleted_at) VALUES('own_deleted_project',$1,'Deleted project',now())", owner.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(context.Background(), "INSERT INTO assets(id,user_id,project_id,kind,file_name,content_type,object_key,size_bytes,sha256,deleted_at) VALUES('own_deleted_asset',$1,$2,'image','Deleted asset','image/png','own/deleted.png',100,'fixture',now())", owner.user.ID, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path  string
		count int
		old   string
	}{{"/projects?status=all", 233, "own_project_0001"}, {"/assets", 501, "own_asset_0001"}, {"/tasks", 231, "own_task_0001"}, {"/wallet/ledger", 231, "own_ledger_0001"}, {"/payment/orders", 231, "own_order_0001"}} {
		page := serverData[userListPage](t, owner.json("GET", test.path, nil, 200))
		if len(page.Items) != 25 || page.Total != test.count || page.Limit != 25 || page.Offset != 0 {
			t.Fatal("default page is not bounded and complete", test.path, page.Total, page.Limit, page.Offset, len(page.Items))
		}
		assertAllUserPages(t, owner, test.path, test.count, test.old)
	}
	checks := []struct {
		path  string
		count int
	}{{"/projects", 232}, {"/projects?status=archived", 1}, {"/projects?status=all&q=needle-project", 1}, {"/assets?q=needle-asset", 1}, {"/assets?kind=audio", 1}, {"/assets?project_id=" + shared.ID, 1}, {"/tasks?q=needle-task", 1}, {"/tasks?shot_id=history-shot", 1}, {"/tasks?project_id=" + foreign.ID, 0}, {"/tasks?status=queued", 1}, {"/wallet/ledger?q=needle-ledger", 1}, {"/wallet/ledger?event_type=freeze", 1}, {"/payment/orders?q=own_trade_0001", 1}, {"/payment/orders?status=expired", 1}, {"/payment/orders?out_trade_no=own_trade_0001", 1}, {"/payment/orders?out_trade_no=own_trade_000", 0}, {"/payment/orders?out_trade_no=foreign_trade_0001", 0}}
	for _, check := range checks {
		page := serverData[userListPage](t, owner.json("GET", check.path, nil, 200))
		if page.Total != check.count {
			t.Fatalf("filter failed: %s total=%d want=%d", check.path, page.Total, check.count)
		}
	}
	for _, path := range []string{"/projects?q=needle-project", "/assets?q=needle-asset", "/tasks?shot_id=history-shot", "/wallet/ledger?q=needle-ledger"} {
		page := serverData[userListPage](t, other.json("GET", path, nil, 200))
		if page.Total != 1 {
			t.Fatal("other account fixture was not independently scoped", path, page.Total)
		}
	}
	for _, path := range []string{"/projects", "/assets", "/tasks", "/wallet/ledger", "/payment/orders"} {
		for _, suffix := range []string{"?limit=0", "?limit=101", "?offset=-1", "?limit=1&limit=2", "?q=a&q=b", "?q=" + url.QueryEscape(strings.Repeat("x", 201))} {
			owner.json("GET", path+suffix, nil, 422)
		}
	}
	for _, path := range []string{"/projects?status=invalid", "/assets?kind=invalid", "/tasks?kind=invalid", "/tasks?status=invalid", "/tasks?shot_id=" + strings.Repeat("x", 201), "/wallet/ledger?event_type=invalid", "/payment/orders?status=invalid", "/payment/orders?out_trade_no=a&out_trade_no=b"} {
		owner.json("GET", path, nil, 422)
	}
	// An exact old-order lookup returns the internal ID without reading or
	// crediting a callback payload, regardless of its page in the full history.
	exact := serverData[userListPage](t, owner.json("GET", "/payment/orders?out_trade_no=own_trade_0001", nil, 200))
	var exactID string
	_ = json.Unmarshal(exact.Items[0]["id"], &exactID)
	if exactID != "own_order_0001" {
		t.Fatal("exact order lookup returned wrong internal ID")
	}
}
