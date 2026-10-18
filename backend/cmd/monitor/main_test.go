package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestOptionsDefaultsAndDedicatedEnvironment(t *testing.T) {
	var reads []string
	opts, err := parseOptions(nil, func(key string) string {
		reads = append(reads, key)
		if key != "MONITOR_DATABASE_URL" {
			t.Fatalf("unexpected application environment dependency: %s", key)
		}
		return "  postgres://monitor@example.test/studio  "
	})
	if err != nil || opts.databaseURL != "postgres://monitor@example.test/studio" || opts.timeout != 10*time.Second || opts.reviewAfter != 24*time.Hour || len(reads) != 1 {
		t.Fatalf("unexpected defaults: %#v %v reads=%v", opts, err, reads)
	}
	opts, err = parseOptions([]string{"-timeout", "250ms", "-review-after", "48h"}, func(string) string { return "postgres://monitor@example.test/studio" })
	if err != nil || opts.timeout != 250*time.Millisecond || opts.reviewAfter != 48*time.Hour {
		t.Fatalf("duration overrides: %#v %v", opts, err)
	}
}

func TestOptionsRejectInvalidArgumentsWithoutEcho(t *testing.T) {
	for _, args := range [][]string{
		{"-timeout", "0"}, {"-timeout", "-1s"}, {"-review-after", "0"},
		{"-review-after", "-1h"}, {"-timeout", "dsn-secret"},
		{"-review-after", "9999999999999999999999h"}, {"-unknown=dsn-secret"},
		{"dsn-secret"}, {"-timeout"}, {"-h"}, {"--", "dsn-secret"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), args, func(string) string { return "postgres://unused" }, &stdout, &stderr)
			if code == 0 || stdout.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "dsn-secret") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunDoesNotFallBackToApplicationDatabase(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), nil, func(key string) string {
		if key == "MONITOR_DATABASE_URL" {
			return " \t\n"
		}
		t.Fatalf("read unrelated environment: %s", key)
		return "postgres://application:app-secret@localhost/studio"
	}, &stdout, &stderr)
	if code == 0 || stdout.Len() != 0 || stderr.String() != "monitor: 必须设置 MONITOR_DATABASE_URL\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestInvalidDatabaseConfigurationIsRedacted(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), nil, func(string) string {
		return "postgres://monitor:dsn-secret@example.test:invalid/studio"
	}, &stdout, &stderr)
	if code == 0 || stdout.Len() != 0 || stderr.String() != "monitor: MONITOR_DATABASE_URL 无效\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestPoolConfigurationForcesReadOnlyAndRemainingDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cfg, err := monitorPoolConfig(ctx, "postgres://monitor:secret@example.test/studio?default_transaction_read_only=off&pool_min_conns=2&pool_min_idle_conns=2&pool_max_conns=8")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 1 || cfg.MinConns != 0 || cfg.MinIdleConns != 0 || cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] != "on" || cfg.AfterConnect == nil {
		t.Fatal("monitor connection settings were not enforced")
	}
	connConfig := cfg.ConnConfig.Copy()
	if err = cfg.BeforeConnect(context.Background(), connConfig); err != nil || connConfig.ConnectTimeout <= 0 || connConfig.ConnectTimeout > time.Second {
		t.Fatalf("connection did not inherit remaining timeout: %v %v", connConfig.ConnectTimeout, err)
	}
	cancel()
	if err = cfg.BeforeConnect(context.Background(), &pgx.ConnConfig{}); err != context.Canceled {
		t.Fatalf("canceled total context was ignored: %v", err)
	}
}

func TestConnectionTimeoutIsBoundedAndRedacted(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			<-stop
		}
	}()
	t.Cleanup(func() { close(stop); listener.Close(); <-stopped })
	var stdout, stderr bytes.Buffer
	started := time.Now()
	code := run(context.Background(), []string{"-timeout=75ms"}, func(string) string {
		return "postgres://monitor:dsn-secret@" + listener.Addr().String() + "/studio?sslmode=disable&connect_timeout=30"
	}, &stdout, &stderr)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("connection exceeded total timeout: %v", elapsed)
	}
	if code == 0 || stdout.Len() != 0 || stderr.String() != "monitor: 无法连接只读监控数据库\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
