package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/observability"
	"wsapi-video/internal/server"
	"wsapi-video/internal/storage"
	"wsapi-video/migrations"
)

func main() {
	slog.SetDefault(observability.NewJSONLogger(os.Stderr))
	if err := run(); err != nil {
		observability.Event(context.Background(), slog.LevelError, "server_lifecycle", "run_failed", observability.Correlation{})
		os.Exit(1)
	}
	observability.Event(context.Background(), slog.LevelInfo, "server_lifecycle", "stopped", observability.Correlation{})
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	poolCfg.MaxConns = 20
	poolCfg.MinConns = 2
	poolCfg.ConnConfig.ConnectTimeout = 10 * time.Second
	db, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return errors.New("cannot initialize database")
	}
	defer db.Close()
	startup, done := context.WithTimeout(ctx, 60*time.Second)
	defer done()
	if err = db.Ping(startup); err != nil {
		return errors.New("cannot connect to PostgreSQL")
	}
	if err = migrations.Apply(startup, db); err != nil {
		return err
	}
	blobs, err := storage.New(cfg)
	if err != nil {
		return err
	}
	app := server.New(cfg, db, blobs)
	if cfg.BootstrapAdminEmail != "" || cfg.BootstrapAdminPassword != "" {
		if err = app.Auth.BootstrapAdmin(startup, cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword); err != nil {
			return err
		}
	}
	if cfg.SetupMode && !cfg.DevMode {
		var exists bool
		if err = db.QueryRow(startup, "SELECT EXISTS(SELECT 1 FROM users WHERE role='admin' AND status='active' AND email_verified)").Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("production setup requires an active administrator; configure BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD")
		}
	}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); app.Auth.RunMailer(ctx) }()
	go func() { defer workers.Done(); app.Commerce.Run(ctx) }()
	failed := make(chan error, 1)
	go func() {
		observability.Event(ctx, slog.LevelInfo, "server_lifecycle", "starting", observability.Correlation{})
		failed <- app.HTTP.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err = <-failed:
	}
	cancel()
	shutdown, finish := context.WithTimeout(context.Background(), 20*time.Second)
	defer finish()
	_ = app.HTTP.Shutdown(shutdown)
	workers.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
