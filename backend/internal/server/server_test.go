package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"wsapi-video/internal/config"
)

func TestWriteOriginProtection(t *testing.T) {
	h := secure(config.Config{PublicURL: "https://studio.example", DevMode: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		origin, path string
		want         int
	}{
		{"https://studio.example", "/api/v1/auth/login", 204},
		{"https://evil.example", "/api/v1/auth/login", 403},
		{"https://studio.example.evil.example", "/api/v1/tasks", 403},
		{"https://evil.example", "/api/v1/payment/notify/easypay", 204},
	} {
		t.Run(tc.origin+tc.path, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, nil)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-site write without Origin accepted")
	}
}

func TestSPADeepLinkAndMissingAsset(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("studio-shell"), 0600); err != nil {
		t.Fatal(err)
	}
	h := workbench(root)
	for _, tc := range []struct {
		path string
		want int
	}{{"/projects/project-a", 200}, {"/assets/missing.js", 404}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("%s: got %d", tc.path, w.Code)
		}
	}
}

func TestSecureAbortHandler(t *testing.T) {
	h := secure(config.Config{PublicURL: "https://studio.example", DevMode: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial-download"))
		panic(http.ErrAbortHandler)
	}))
	w := httptest.NewRecorder()
	defer func() {
		if p := recover(); p != http.ErrAbortHandler {
			t.Fatalf("expected abort sentinel, got %v", p)
		}
		if w.Body.String() != "partial-download" {
			t.Fatalf("abort response was rewritten: %q", w.Body.String())
		}
	}()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/projects/p/export", nil))
}

func TestSecureRecoversUnexpectedPanic(t *testing.T) {
	h := secure(config.Config{PublicURL: "https://studio.example", DevMode: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("unexpected failure")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d want 500", w.Code)
	}
}
