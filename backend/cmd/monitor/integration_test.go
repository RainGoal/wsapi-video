package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/operations"
)

func monitorPostgresFixture(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for the isolated monitor test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random[:])
	schema, role, password := "test_monitor_"+suffix, "test_monitor_reader_"+suffix, "test-only-"+suffix
	schemaIdent, roleIdent := pgx.Identifier{schema}.Sanitize(), pgx.Identifier{role}.Sanitize()
	var db *pgxpool.Pool
	var schemaCreated, roleCreated bool
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if schemaCreated {
			if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schemaIdent+" CASCADE"); err != nil {
				t.Error(err)
			}
		}
		if roleCreated {
			if _, err := admin.Exec(cleanup, "DROP ROLE "+roleIdent); err != nil {
				t.Error(err)
			}
		}
		admin.Close()
	})
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatal(err)
	}
	schemaCreated = true
	if _, err = admin.Exec(ctx, "CREATE ROLE "+roleIdent+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	roleCreated = true
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, `
CREATE TABLE generation_tasks(id text,created_at timestamptz,attempts integer,lease_until timestamptz,execution_status text,settlement_status text,delivery_status text,error_code text,prompt text,credential_ref text);
CREATE TABLE payment_orders(id text,created_at timestamptz,check_attempts integer,credit_status text,payment_status text,expires_at timestamptz,checkout_url text,provider_payload text);
CREATE TABLE auth_mail_outbox(id text,created_at timestamptz,attempts integer,sent_at timestamptz,recipient text,body text);
CREATE TABLE task_procurement_reviews(task_id text,revision bigint,status text,created_at timestamptz,reason text,evidence text,admin_id text);
INSERT INTO generation_tasks VALUES('task-local-1',now()-interval '2 days',3,now()-interval '1 minute','submission_unknown','frozen','pending','','private-prompt-sentinel','private-credential-sentinel');
INSERT INTO generation_tasks VALUES('task-local-2',now(),0,NULL,'completed','consumed','ready','','private-prompt-sentinel','private-credential-sentinel');
INSERT INTO generation_tasks VALUES('task-local-3',now(),0,NULL,'completed','consumed','ready','','private-prompt-sentinel','private-credential-sentinel');
INSERT INTO task_procurement_reviews VALUES
 ('task-local-1',1,'matched',now(),'private-reason-sentinel','private-evidence-sentinel','private-admin-sentinel'),
 ('task-local-2',1,'disputed',now(),'private-reason-sentinel','private-evidence-sentinel','private-admin-sentinel'),
 ('task-local-3',1,'disputed',now(),'private-reason-sentinel','private-evidence-sentinel','private-admin-sentinel'),
 ('task-local-3',2,'resolved',now()-interval '1 day','private-reason-sentinel','private-evidence-sentinel','private-admin-sentinel');
INSERT INTO payment_orders VALUES('payment-local-1',now(),4,'pending','paid',now()+interval '1 hour','private-checkout-sentinel','private-payment-sentinel');
INSERT INTO auth_mail_outbox VALUES('mail-local-1',now(),2,NULL,'private-recipient-sentinel','private-mail-sentinel');
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, "GRANT USAGE ON SCHEMA "+schemaIdent+" TO "+roleIdent+";"+
		"GRANT SELECT (id,created_at,attempts,lease_until,execution_status,settlement_status,delivery_status,error_code) ON generation_tasks TO "+roleIdent+";"+
		"GRANT SELECT (id,created_at,check_attempts,credit_status,payment_status,expires_at) ON payment_orders TO "+roleIdent+";"+
		"GRANT SELECT (id,created_at,attempts,sent_at) ON auth_mail_outbox TO "+roleIdent+";"+
		"GRANT SELECT (task_id,revision,status,created_at) ON task_procurement_reviews TO "+roleIdent)
	if err != nil {
		t.Fatal(err)
	}
	return db, monitorTestDSN(t, dsn, map[string]string{"user": role, "password": password, "search_path": schema, "default_transaction_read_only": "off"})
}

func monitorTestDSN(t *testing.T, dsn string, params map[string]string) string {
	t.Helper()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		for key, value := range params {
			query.Set(key, value)
		}
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	for key, value := range params {
		value = strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `'`, `\'`)
		dsn += " " + key + "='" + value + "'"
	}
	return dsn
}

func TestMonitorPostgresUsesLimitedColumnsAndPureJSON(t *testing.T) {
	owner, dsn := monitorPostgresFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"-review-after=1h"}, func(key string) string {
		if key != "MONITOR_DATABASE_URL" {
			t.Fatalf("unrelated application environment requested: %s", key)
		}
		return dsn
	}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || strings.Contains(stdout.String(), "private-") || strings.Contains(stdout.String(), "test-only-") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	decoder := json.NewDecoder(&stdout)
	var snapshot operations.SnapshotResult
	if err := decoder.Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("stdout was not exactly one JSON document: %v", err)
	}
	if snapshot.ReviewAfterSeconds != 3600 || len(snapshot.Signals) != 8 || snapshot.Signals[0].Count != 1 || snapshot.Signals[0].Samples[0].ID != "task-local-1" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if got := snapshot.Signals[7]; got.Code != "procurement_review_disputed" || got.Count != 1 || len(got.Samples) != 1 || got.Samples[0].ID != "task-local-2" || got.Samples[0].Attempts != 0 {
		t.Fatalf("latest consumed procurement dispute was not readable with limited columns: %#v", got)
	}
	cfg, err := monitorPoolConfig(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	limited, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer limited.Close()
	for _, query := range []string{"SELECT prompt FROM generation_tasks", "SELECT credential_ref FROM generation_tasks", "SELECT checkout_url FROM payment_orders", "SELECT provider_payload FROM payment_orders", "SELECT recipient FROM auth_mail_outbox", "SELECT body FROM auth_mail_outbox", "SELECT reason FROM task_procurement_reviews", "SELECT evidence FROM task_procurement_reviews", "SELECT admin_id FROM task_procurement_reviews"} {
		var secret string
		err = limited.QueryRow(ctx, query).Scan(&secret)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("private column was readable: %s %v", query, err)
		}
	}
	var tables int
	if err = owner.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema()").Scan(&tables); err != nil || tables != 4 {
		t.Fatalf("monitor created unexpected schema objects: count=%d err=%v", tables, err)
	}
	assertMonitorWriteForbidden(t, ctx, owner)
	assertMonitorQueryDeadline(t, owner, dsn)
}

func assertMonitorWriteForbidden(t *testing.T, ctx context.Context, owner *pgxpool.Pool) {
	t.Helper()
	schema := owner.Config().ConnConfig.RuntimeParams["search_path"]
	dsn := monitorTestDSN(t, os.Getenv("TEST_DATABASE_URL"), map[string]string{"search_path": schema, "default_transaction_read_only": "off"})
	cfg, err := monitorPoolConfig(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	readOnlyOwner, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnlyOwner.Close()
	_, err = readOnlyOwner.Exec(ctx, "UPDATE generation_tasks SET attempts=99 WHERE id='task-local-1'")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("owner connection could write despite monitor configuration: %v", err)
	}
	var attempts int
	if err = owner.QueryRow(ctx, "SELECT attempts FROM generation_tasks WHERE id='task-local-1'").Scan(&attempts); err != nil || attempts != 3 {
		t.Fatalf("monitor changed fixture: attempts=%d err=%v", attempts, err)
	}
	_, err = readOnlyOwner.Exec(ctx, "UPDATE task_procurement_reviews SET status='resolved' WHERE task_id='task-local-2'")
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("monitor connection could change procurement reviews: %v", err)
	}
	var reviewStatus string
	if err = owner.QueryRow(ctx, "SELECT status FROM task_procurement_reviews WHERE task_id='task-local-2'").Scan(&reviewStatus); err != nil || reviewStatus != "disputed" {
		t.Fatalf("monitor changed procurement review: status=%s err=%v", reviewStatus, err)
	}
}

func assertMonitorQueryDeadline(t *testing.T, owner *pgxpool.Pool, dsn string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "LOCK TABLE generation_tasks IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	started := time.Now()
	code := run(ctx, []string{"-timeout=1s"}, func(string) string { return dsn }, &stdout, &stderr)
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("query exceeded total timeout: %v", elapsed)
	}
	if code == 0 || stdout.Len() != 0 || stderr.String() != "monitor: 无法读取运营快照\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
