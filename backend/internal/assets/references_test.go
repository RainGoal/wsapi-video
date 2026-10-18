package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/auth"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/migrations"
)

func referencesTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_asset_refs_" + strings.TrimPrefix(core.ID(""), "_")
	ident := pgx.Identifier{schema}.Sanitize()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA IF EXISTS "+ident+" CASCADE"); err != nil {
			t.Error("could not remove isolated asset reference schema")
		}
		admin.Close()
	})
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

func referenceStateFingerprint(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var result string
	err := db.QueryRow(context.Background(), `SELECT md5(jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM users t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM sessions t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM projects t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY project_id) FROM canvases t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM shots t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM assets t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY asset_id,owner_type,owner_id) FROM asset_references t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM generation_tasks t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id) FROM wallet_accounts t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM wallet_ledger t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM payment_orders t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM admin_audit t))::text)`).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func seedReferences(t *testing.T, db *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,email_verified) VALUES
 ('owner','private-owner@example.test','private-password-hash','Owner',true),
 ('other','private-other@example.test','private-password-hash','Other',true);
 INSERT INTO wallet_accounts(user_id,available,frozen) VALUES('owner',100,300),('other',50,0);
 INSERT INTO projects(id,user_id,name,description,status,deleted_at) VALUES
 ('p-a','owner','Active project','private-description','active',NULL),
 ('p-b','owner','Archived project','private-description','archived',NULL),
 ('p-c','owner','Deleted project','private-description','archived',now()),
 ('p-d','other','private-foreign-project','private-description','active',NULL);
 INSERT INTO canvases(project_id) VALUES('p-a'),('p-b'),('p-c'),('p-d');
 INSERT INTO shots(id,project_id,user_id,position,state) VALUES
 ('shot-a','p-a','owner',0,'{"title":"Opening shot","prompt":"private-prompt"}'),
 ('shot-b','p-b','owner',0,'{"title":"Archived shot","prompt":"private-prompt"}'),
 ('shot-foreign','p-d','other',0,'{"title":"private-foreign-shot-title"}');
 INSERT INTO quotes(id,user_id,project_id,product_id,input,input_hash,product_snapshot,credits,expires_at)
 SELECT 'quote-'||lpad(i::text,2,'0'),'owner','p-a','demo-image','{}','private-input-hash','{}',10,now()+interval '1 hour' FROM generate_series(0,29) i;
 INSERT INTO generation_tasks(id,user_id,project_id,quote_id,product_id,request_key,request_hash,input,product_snapshot,credits,kind,model,credential_ref)
 SELECT 'task-'||lpad(i::text,2,'0'),'owner','p-a','quote-'||lpad(i::text,2,'0'),'demo-image','request-'||i,'private-request-hash','{"prompt":"private-task-prompt"}','{}',10,'image','demo','private-credential' FROM generate_series(0,29) i;
 INSERT INTO assets(id,user_id,project_id,task_id,kind,file_name,content_type,object_key,size_bytes,sha256,deleted_at) VALUES
 ('referenced','owner','p-a','task-00','image','image.png','image/png','private-object-key',100,'private-sha',NULL),
 ('empty','owner','p-a',NULL,'image','upload.png','image/png','private-upload-key',100,'private-sha',NULL),
 ('deleted','owner','p-a',NULL,'image','deleted.png','image/png','private-deleted-key',100,'private-sha',now()),
 ('foreign','other','p-d',NULL,'image','foreign.png','image/png','private-foreign-key',100,'private-sha',NULL);
 UPDATE projects SET cover_asset_id='referenced' WHERE id='p-a';
 INSERT INTO asset_references(asset_id,project_id,owner_type,owner_id) VALUES
 ('referenced','p-a','canvas','p-a'),('referenced','p-a','cover','p-a'),
 ('referenced','p-a','shot','shot-a'),('referenced','p-b','shot','shot-b'),
 ('referenced','p-c','canvas','p-c'),
 ('referenced','p-d','canvas','p-d'),
 ('referenced','p-a','shot','shot-foreign');
 INSERT INTO asset_references(asset_id,project_id,owner_type,owner_id)
 SELECT 'referenced','p-a','task','task-'||lpad(i::text,2,'0') FROM generate_series(0,29) i`)
	if err != nil {
		t.Fatal(err)
	}
	ownerToken, otherToken := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for user, token := range map[string]string{"owner": ownerToken, "other": otherToken} {
		digest := sha256.Sum256([]byte(token))
		_, err = db.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,'private-csrf',now()+interval '1 hour')`, "session-"+user, user, hex.EncodeToString(digest[:]))
		if err != nil {
			t.Fatal(err)
		}
	}
	return ownerToken, otherToken
}

func TestPostgresAssetReferencesOwnershipPaginationAndNoMutation(t *testing.T) {
	db := referencesTestDB(t)
	ownerToken, otherToken := seedReferences(t, db)
	before := referenceStateFingerprint(t, db)
	mux := http.NewServeMux()
	identity := auth.New(db, config.Config{})
	New(db, config.Config{}, nil).Register(mux, identity.Middleware, identity.Admin)
	request := func(path, token string, status int) []byte {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+path, nil)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "studio_session", Value: token})
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("references status %d, want %d for %s: %s", rec.Code, status, path, rec.Body.String())
		}
		for _, secret := range []string{"private-", ownerToken, otherToken, "object_key", "credential_ref", "password_hash", "csrf_token", "prompt", "email", "signed_url"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatal("references response exposed private data")
			}
		}
		return rec.Body.Bytes()
	}
	type resultPage struct {
		Items  []AssetReference `json:"items"`
		Total  int64            `json:"total"`
		Limit  int              `json:"limit"`
		Offset int              `json:"offset"`
	}
	readPage := func(path, token string) resultPage {
		t.Helper()
		raw := request(path, token, 200)
		var result struct {
			Code string     `json:"code"`
			Data resultPage `json:"data"`
		}
		if err := json.Unmarshal(raw, &result); err != nil || result.Code != "ok" || result.Data.Items == nil {
			t.Fatal("references page must use the standard envelope and an array", err)
		}
		var fields struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		allowed := map[string]bool{"project_id": true, "project_name": true, "project_status": true, "project_deleted": true, "owner_type": true, "owner_id": true, "owner_label": true}
		for _, item := range fields.Data.Items {
			if len(item) != len(allowed) {
				t.Fatal("references item did not use the explicit public whitelist")
			}
			for key := range item {
				if !allowed[key] {
					t.Fatal("references item included an unexpected field", key)
				}
			}
		}
		return result.Data
	}
	all := readPage("referenced/references?limit=100", ownerToken)
	want := []AssetReference{
		{"p-a", "Active project", "active", false, "canvas", "p-a", ""},
		{"p-a", "Active project", "active", false, "cover", "p-a", ""},
		{"p-a", "Active project", "active", false, "shot", "shot-a", "Opening shot"},
		{"p-a", "Active project", "active", false, "shot", "shot-foreign", ""},
		{"p-b", "Archived project", "archived", false, "shot", "shot-b", "Archived shot"},
		{"p-c", "Deleted project", "archived", true, "canvas", "p-c", ""},
	}
	for i := 0; i < 30; i++ {
		want = append(want, AssetReference{"p-a", "Active project", "active", false, "task", fmt.Sprintf("task-%02d", i), ""})
	}
	sort.Slice(want, func(i, j int) bool {
		a, b := want[i], want[j]
		if a.ProjectID != b.ProjectID {
			return a.ProjectID < b.ProjectID
		}
		if a.OwnerType != b.OwnerType {
			return a.OwnerType < b.OwnerType
		}
		return a.OwnerID < b.OwnerID
	})
	if all.Total != 36 || all.Limit != 100 || all.Offset != 0 || !reflect.DeepEqual(all.Items, want) {
		t.Fatal("references did not preserve all four owner types and same-account project states")
	}
	for _, page := range []struct {
		query        string
		limit, start int
		end          int
	}{{"", 25, 0, 25}, {"?offset=25", 25, 25, 36}, {"?limit=2&offset=1", 2, 1, 3}, {"?offset=36", 25, 36, 36}, {"?offset=100", 25, 100, 36}} {
		got := readPage("referenced/references"+page.query, ownerToken)
		items := []AssetReference{}
		if page.start < len(want) {
			items = want[page.start:page.end]
		}
		if got.Total != 36 || got.Limit != page.limit || got.Offset != page.start || !reflect.DeepEqual(got.Items, items) {
			t.Fatal("references pagination omitted, duplicated or reordered references", page.query)
		}
	}
	for _, path := range []string{"empty/references", "foreign/references"} {
		token := ownerToken
		if strings.HasPrefix(path, "foreign") {
			token = otherToken
		}
		got := readPage(path, token)
		if got.Total != 0 || got.Limit != 25 || got.Offset != 0 || len(got.Items) != 0 {
			t.Fatal("unreferenced asset did not return an empty page")
		}
	}
	request("referenced/references", "", 401)
	missing := string(request("missing/references", ownerToken, 404))
	for _, access := range []struct{ path, token string }{
		{"deleted/references", ownerToken}, {"foreign/references", ownerToken}, {"referenced/references", otherToken},
	} {
		if string(request(access.path, access.token, 404)) != missing {
			t.Fatal("unknown, deleted and other-account asset failures must match")
		}
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=2&limit=3", "?offset=-1", "?offset=10000001", "?offset=x"} {
		request("referenced/references"+query, ownerToken, 422)
	}
	if after := referenceStateFingerprint(t, db); after != before {
		t.Fatal("reading references changed assets, references, projects, tasks, wallets, payments, audit or sessions")
	}
}
