package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/core"
)

func TestMigrationRepeatAndChecksumGuard(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for PostgreSQL migrations")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "test_migrations_" + strings.TrimPrefix(core.ID("s"), "s_")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, db); err != nil {
		t.Fatalf("repeat migration failed: %v", err)
	}
	var tables int
	if err = db.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name IN ('users','generation_tasks','wallet_ledger','projects','assets')", schema).Scan(&tables); err != nil || tables != 5 {
		t.Fatalf("missing tables: %d %v", tables, err)
	}
	if _, err = db.Exec(ctx, "UPDATE schema_migrations SET checksum='tampered' WHERE name='001_identity_projects_assets.sql'"); err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, db); err == nil || !strings.Contains(err.Error(), "has changed") {
		t.Fatalf("missing checksum rejection: %v", err)
	}
}
