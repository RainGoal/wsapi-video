package server

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"wsapi-video/internal/config"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/observability"
)

func secure(cfg config.Config, next http.Handler) http.Handler {
	public, _ := url.Parse(cfg.PublicURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				observability.Event(r.Context(), slog.LevelError, "request_failed", "panic", observability.Correlation{})
				httpx.Fail(w, 500, "internal_error", "服务暂时不可用")
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; media-src 'self' blob: https:; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self' https:")
		if !cfg.DevMode {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		write := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
		callback := r.URL.Path == "/api/v1/payment/notify/easypay"
		if write && !callback {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				if err != nil || public == nil || u.Scheme != public.Scheme || !strings.EqualFold(u.Host, public.Host) || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
					httpx.Fail(w, 403, "origin_invalid", "请从平台页面发起操作")
					return
				}
			} else if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				httpx.Fail(w, 403, "origin_invalid", "请从平台页面发起操作")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
