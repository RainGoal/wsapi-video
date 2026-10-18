package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"wsapi-video/internal/httpx"
)

func workbench(directory string) http.Handler {
	root, _ := filepath.Abs(directory)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			httpx.Fail(w, 405, "method_not_allowed", "不支持此操作")
			return
		}
		clean := filepath.Clean(filepath.FromSlash(r.URL.Path))
		target := filepath.Join(root, strings.TrimLeft(clean, "/\\"))
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, ":") {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(target)
		if err != nil || info.IsDir() {
			if filepath.Ext(r.URL.Path) != "" {
				http.NotFound(w, r)
				return
			}
			target = filepath.Join(root, "index.html")
			if _, err = os.Stat(target); err != nil {
				httpx.Fail(w, 503, "workbench_not_built", "请先构建 web，或使用开发前端端口 5176")
				return
			}
		}
		if filepath.Base(target) == "index.html" {
			w.Header().Set("Cache-Control", "no-store")
		} else if strings.HasPrefix(rel, "assets"+string(filepath.Separator)) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, target)
	})
}
