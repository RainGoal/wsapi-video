package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/migrations"
)

func operationsTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	adminDB, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_operations_" + strings.TrimPrefix(core.ID(""), "_")
	if !strings.HasPrefix(schema, "test_operations_") {
		t.Fatal("unsafe schema")
	}
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err = adminDB.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		adminDB.Close()
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
	t.Cleanup(func() {
		db.Close()
		_, _ = adminDB.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE")
		adminDB.Close()
	})
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPostgresOverviewReportsFactsWithoutMutation(t *testing.T) {
	db := operationsTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, `
	INSERT INTO users(id,email,password_hash,display_name,email_verified) VALUES('u1','u1@example.test','unused','One',true),('u2','u2@example.test','unused','Two',false);
	UPDATE users SET status='disabled' WHERE id='u2';
	INSERT INTO wallet_accounts(user_id,available,frozen) VALUES('u1',1000,20),('u2',50,0);
	INSERT INTO projects(id,user_id,name,status,deleted_at) VALUES('p1','u1','Active','active',NULL),('p2','u1','Archive','archived',NULL),('p3','u1','Deleted','archived',now());
	INSERT INTO assets(id,user_id,project_id,kind,file_name,content_type,object_key,size_bytes,sha256,deleted_at) VALUES('a1','u1','p1','image','a.png','image/png','a1',25,'hash',NULL),('a2','u1','p1','image','b.png','image/png','a2',75,'hash',now());
	INSERT INTO quotes(id,user_id,project_id,product_id,input,input_hash,product_snapshot,credits,expires_at) VALUES('q1','u1','p1','demo-image','{}','h','{}',10,now()),('q2','u1','p1','demo-image','{}','h','{}',10,now());
	INSERT INTO generation_tasks(id,user_id,project_id,quote_id,product_id,request_key,request_hash,input,product_snapshot,credits,kind,model,credential_ref,execution_status,delivery_status,created_at) VALUES('t1','u1','p1','q1','demo-image','request-1','h','{}','{}',10,'image','demo','demo','submission_unknown','pending',now()-interval '2 days'),('t2','u1','p1','q2','demo-image','request-2','h','{}','{}',10,'image','demo','demo','completed','archive_retry',now());
	INSERT INTO payment_orders(id,user_id,out_trade_no,tier_id,tier_version,amount_fen,credits,method,credential_ref,merchant_pid,provider_origin,checkout_url,expires_at,check_attempts) VALUES('o1','u1','CV-OP-TEST','starter',1,1000,1000,'alipay','unused','unused','https://example.test','https://example.test/checkout',now()-interval '1 hour',3);
	`)
	if err != nil {
		t.Fatal(err)
	}
	usedMiddleware := false
	mux := http.NewServeMux()
	New(db, config.Config{TaskReviewAfter: 24 * time.Hour}).Register(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { usedMiddleware = true; next.ServeHTTP(w, r) })
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/admin/overview", nil))
	if rec.Code != 200 || !usedMiddleware {
		t.Fatalf("status=%d middleware=%v %s", rec.Code, usedMiddleware, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Users    struct{ Total, Active, Disabled, Unverified int }
			Projects struct{ Active, Archived, Deleted int }
			Assets   struct {
				ActiveCount   int   `json:"active_count"`
				ActiveBytes   int64 `json:"active_bytes"`
				RetainedCount int   `json:"retained_count"`
				RetainedBytes int64 `json:"retained_bytes"`
			}
			Tasks struct {
				ReviewRequired int `json:"review_required"`
				FrozenOverdue  int `json:"frozen_overdue"`
				ArchiveFailed  int `json:"archive_failed"`
			}
			Wallet struct {
				AvailableTotal int64 `json:"available_total"`
				FrozenTotal    int64 `json:"frozen_total"`
			}
			Payments struct {
				PendingCount   int `json:"pending_count"`
				ReviewRequired int `json:"review_required"`
			}
			ProcurementBalance struct{ Status string } `json:"procurement_balance"`
		}
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	value := envelope.Data
	if value.Users.Total != 2 || value.Users.Active != 1 || value.Users.Disabled != 1 || value.Users.Unverified != 1 || value.Projects.Active != 1 || value.Projects.Archived != 1 || value.Projects.Deleted != 1 {
		t.Fatalf("incorrect ownership counts: %s", rec.Body.String())
	}
	if value.Assets.ActiveCount != 1 || value.Assets.ActiveBytes != 25 || value.Assets.RetainedCount != 2 || value.Assets.RetainedBytes != 100 {
		t.Fatalf("incorrect logical capacity: %s", rec.Body.String())
	}
	if value.Tasks.ReviewRequired != 1 || value.Tasks.FrozenOverdue != 1 || value.Tasks.ArchiveFailed != 1 || value.Wallet.AvailableTotal != 1050 || value.Wallet.FrozenTotal != 20 || value.Payments.PendingCount != 1 || value.Payments.ReviewRequired != 1 || value.ProcurementBalance.Status != "unavailable" {
		t.Fatalf("incorrect operational facts: %s", rec.Body.String())
	}
	var events int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM wallet_ledger`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("overview changed funds: %d %v", events, err)
	}
	var status string
	if err = db.QueryRow(ctx, `SELECT execution_status FROM generation_tasks WHERE id='t1'`).Scan(&status); err != nil || status != "submission_unknown" {
		t.Fatalf("overview changed task: %s %v", status, err)
	}
}
