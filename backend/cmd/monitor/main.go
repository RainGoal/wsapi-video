// Command monitor prints one read-only operational snapshot as JSON.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/operations"
)

type options struct {
	databaseURL string
	timeout     time.Duration
	reviewAfter time.Duration
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	var opts options
	flags := flag.NewFlagSet("monitor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.DurationVar(&opts.timeout, "timeout", 10*time.Second, "total connection and snapshot timeout")
	flags.DurationVar(&opts.reviewAfter, "review-after", 24*time.Hour, "overdue frozen task threshold")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || opts.timeout <= 0 || opts.reviewAfter <= 0 {
		return options{}, errors.New("monitor: 参数无效，请使用正数时长参数 -timeout 和 -review-after")
	}
	opts.databaseURL = strings.TrimSpace(getenv("MONITOR_DATABASE_URL"))
	if opts.databaseURL == "" {
		return options{}, errors.New("monitor: 必须设置 MONITOR_DATABASE_URL")
	}
	return opts, nil
}

func run(parent context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args, getenv)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(parent, opts.timeout)
	defer cancel()
	poolConfig, err := monitorPoolConfig(ctx, opts.databaseURL)
	if err != nil {
		fmt.Fprintln(stderr, "monitor: MONITOR_DATABASE_URL 无效")
		return 1
	}
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		fmt.Fprintln(stderr, "monitor: 无法初始化监控数据库连接")
		return 1
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		fmt.Fprintln(stderr, "monitor: 无法连接只读监控数据库")
		return 1
	}
	snapshot, err := operations.Snapshot(ctx, db, opts.reviewAfter)
	if err != nil {
		fmt.Fprintln(stderr, "monitor: 无法读取运营快照")
		return 1
	}
	if err = json.NewEncoder(stdout).Encode(snapshot); err != nil {
		fmt.Fprintln(stderr, "monitor: 无法输出运营快照")
		return 1
	}
	return 0
}

func monitorPoolConfig(ctx context.Context, dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid monitor database configuration")
	}
	cfg.MaxConns, cfg.MinConns, cfg.MinIdleConns = 1, 0, 0
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	// pgxpool may detach cancellation while constructing a pooled connection.
	// Bound each attempt by the same remaining deadline as the snapshot query.
	cfg.BeforeConnect = func(_ context.Context, connConfig *pgx.ConnConfig) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if deadline, ok := ctx.Deadline(); ok {
			connConfig.ConnectTimeout = time.Until(deadline)
			if connConfig.ConnectTimeout <= 0 {
				return context.DeadlineExceeded
			}
		}
		return nil
	}
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		var defaultReadOnly, transactionReadOnly string
		if err := conn.QueryRow(ctx, "SELECT current_setting('default_transaction_read_only'), current_setting('transaction_read_only')").Scan(&defaultReadOnly, &transactionReadOnly); err != nil {
			return err
		}
		if defaultReadOnly != "on" || transactionReadOnly != "on" {
			return errors.New("monitor database connection is not read-only")
		}
		return nil
	}
	return cfg, nil
}
