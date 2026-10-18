// Package server composes the independent platform's API and workbench.
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/auth"
	"wsapi-video/internal/commerce"
	"wsapi-video/internal/config"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/operations"
	"wsapi-video/internal/projects"
	"wsapi-video/internal/settings"
	"wsapi-video/internal/storage"
)

const Version = "0.1.0"

type App struct {
	HTTP     *http.Server
	Auth     *auth.Service
	Commerce *commerce.Service
	Settings *settings.Service
}

func New(cfg config.Config, db *pgxpool.Pool, blobs storage.Store) *App {
	mux := http.NewServeMux()
	runtimeSettings := settings.New(db, cfg)
	if local, ok := blobs.(*storage.Local); ok {
		local.Register(mux)
	}
	blobs = storage.NewDynamic(runtimeSettings.RuntimeConfig, runtimeSettings.LockStorage)
	identity := auth.New(db, cfg)
	identity.SetRuntimeSettings(runtimeSettings)
	mediaAssets := assets.New(db, cfg, blobs)
	jobs := commerce.New(db, cfg, mediaAssets)
	jobs.SetRuntimeSettings(runtimeSettings)
	identity.Register(mux)
	projects.New(db, cfg, blobs).Register(mux, identity.Middleware, identity.Admin)
	mediaAssets.Register(mux, identity.Middleware, identity.Admin)
	jobs.Register(mux, identity.Middleware, identity.Admin)
	operations.New(db, cfg).Register(mux, identity.Admin)
	runtimeSettings.Register(mux, identity.Admin)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, map[string]string{"status": "ok", "version": Version})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			httpx.Fail(w, 503, "not_ready", "数据库暂不可用")
			return
		}
		st, err := runtimeSettings.Current(ctx)
		if err != nil {
			httpx.Fail(w, 503, "not_ready", "运行配置不可读取")
			return
		}
		ready := runtimeSettings.Ready(st)
		if !cfg.DevMode && (!ready.SMTP || !ready.Storage) {
			httpx.Fail(w, 503, "setup_required", "请通过管理员后台完成邮件与私有存储配置")
			return
		}
		httpx.JSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/system", func(w http.ResponseWriter, r *http.Request) {
		st, err := runtimeSettings.Current(r.Context())
		if err != nil {
			httpx.Fail(w, 503, "settings_unavailable", "站点配置暂不可用")
			return
		}
		ready := runtimeSettings.Ready(st)
		httpx.JSON(w, 200, map[string]any{
			"brand": st.Settings.Site.Brand, "version": Version, "demo": ready.Demo, "development": cfg.DevMode, "support_email": st.Settings.Site.SupportEmail,
			"retention_policy": st.Settings.Site.RetentionPolicy, "storage_quota_policy": st.Settings.Site.StorageQuotaPolicy,
			"cleanup_policy": st.Settings.Site.CleanupPolicy, "submission_recovery_policy": st.Settings.Site.SubmissionRecoveryPolicy,
			"refund_policy":        st.Settings.Site.RefundPolicy,
			"registration_enabled": ready.Registration, "storage_driver": st.Settings.Storage.Driver,
			"payment_enabled": ready.Payment, "media_configured": ready.Media,
		})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { httpx.Fail(w, 404, "not_found", "接口不存在") })
	mux.Handle("/", workbench(cfg.WebDir))
	return &App{Auth: identity, Commerce: jobs, Settings: runtimeSettings, HTTP: &http.Server{
		Addr: cfg.ListenAddr, Handler: secure(cfg, mux), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 2 * time.Minute, WriteTimeout: 5 * time.Minute, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10,
	}}
}
