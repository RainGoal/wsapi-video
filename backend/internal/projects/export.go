package projects

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

const (
	maxExportAssets             = 200
	maxExportBytes        int64 = 2 << 30
	exportLinkTTL               = 5 * time.Minute
	maxExportTokenLength        = 12 << 10
	maxExportClaimsLength       = 32 << 10
)

type exportFile struct {
	id, key, name string
	size          int64
}
type exportManifest struct {
	files      []exportFile
	totalBytes int64
}
type exportInput struct {
	AssetIDs []string `json:"asset_ids"`
}
type exportClaims struct {
	Version   int      `json:"v"`
	UserID    string   `json:"u"`
	ProjectID string   `json:"p"`
	AssetIDs  []string `json:"a"`
	ExpiresAt int64    `json:"e"`
}

var errExportToken = errors.New("invalid or expired export link")

func exportPrivateHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func (s *Service) exportMAC(body string) []byte {
	mac := hmac.New(sha256.New, s.exportSecret)
	_, _ = mac.Write([]byte("studio-project-export:v1\n" + body))
	return mac.Sum(nil)
}

// Compress only the small ID snapshot, never media. This keeps 200 normal asset
// IDs within common proxy request-line limits without storing export tickets.
func (s *Service) newExportToken(userID, projectID string, assetIDs []string, now time.Time) (string, time.Time, error) {
	if len(s.exportSecret) < 32 || len(assetIDs) == 0 || len(assetIDs) > maxExportAssets {
		return "", time.Time{}, errExportToken
	}
	expires := time.Unix(now.Add(exportLinkTTL).Unix(), 0).UTC()
	claims := exportClaims{Version: 1, UserID: userID, ProjectID: projectID, AssetIDs: assetIDs, ExpiresAt: expires.Unix()}
	raw, err := json.Marshal(claims)
	if err != nil || len(raw) > maxExportClaimsLength {
		return "", time.Time{}, errExportToken
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err = writer.Write(raw); err != nil {
		_ = writer.Close()
		return "", time.Time{}, err
	}
	if err = writer.Close(); err != nil {
		return "", time.Time{}, err
	}
	body := base64.RawURLEncoding.EncodeToString(compressed.Bytes())
	token := body + "." + base64.RawURLEncoding.EncodeToString(s.exportMAC(body))
	if len(token) > maxExportTokenLength {
		return "", time.Time{}, errExportToken
	}
	return token, expires, nil
}

func (s *Service) parseExportToken(token, userID, projectID string, now time.Time) ([]string, error) {
	if len(s.exportSecret) < 32 || len(token) > maxExportTokenLength {
		return nil, errExportToken
	}
	body, signature, ok := strings.Cut(token, ".")
	sig, err := base64.RawURLEncoding.Strict().DecodeString(signature)
	if !ok || err != nil || !hmac.Equal(sig, s.exportMAC(body)) {
		return nil, errExportToken
	}
	compressed, err := base64.RawURLEncoding.Strict().DecodeString(body)
	if err != nil {
		return nil, errExportToken
	}
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, errExportToken
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, maxExportClaimsLength+1))
	if err != nil || len(raw) > maxExportClaimsLength {
		return nil, errExportToken
	}
	var claims exportClaims
	if json.Unmarshal(raw, &claims) != nil || claims.Version != 1 || claims.UserID == "" || claims.UserID != userID || claims.ProjectID == "" || claims.ProjectID != projectID || claims.ExpiresAt <= now.Unix() || claims.ExpiresAt > now.Add(exportLinkTTL).Unix() || len(claims.AssetIDs) == 0 || len(claims.AssetIDs) > maxExportAssets {
		return nil, errExportToken
	}
	seen := map[string]bool{}
	for _, id := range claims.AssetIDs {
		if id == "" || len(id) > 100 || seen[id] {
			return nil, errExportToken
		}
		seen[id] = true
	}
	return claims.AssetIDs, nil
}

func safeFileName(value string) string {
	value = filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Map(func(r rune) rune {
		if r < ' ' || r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, value)
	if value == "" || value == "." || value == ".." {
		return "media"
	}
	return value
}

// Export streams existing media. A retry never submits or charges generation.
func (s *Service) export(w http.ResponseWriter, r *http.Request) {
	var in exportInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	manifest, ok := s.resolveExport(w, r, in.AssetIDs, true)
	if ok {
		s.streamExport(w, r, manifest)
	}
}

// Prepare is read-only: it signs authorized IDs without opening object streams,
// creating generation tasks, writing ticket rows, or changing wallet events.
func (s *Service) prepareExport(w http.ResponseWriter, r *http.Request) {
	exportPrivateHeaders(w)
	var in exportInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	manifest, ok := s.resolveExport(w, r, in.AssetIDs, true)
	if !ok {
		return
	}
	ids := make([]string, 0, len(manifest.files))
	for _, file := range manifest.files {
		ids = append(ids, file.id)
	}
	token, expires, err := s.newExportToken(core.UserFrom(r.Context()).ID, r.PathValue("id"), ids, time.Now())
	if err != nil {
		httpx.Fail(w, 503, "export_unavailable", "下载暂时不可用，请稍后重试")
		return
	}
	query := url.Values{"token": {token}}
	httpx.JSON(w, 200, struct {
		DownloadURL string    `json:"download_url"`
		ExpiresAt   time.Time `json:"expires_at"`
		AssetCount  int       `json:"asset_count"`
		TotalBytes  int64     `json:"total_bytes"`
	}{DownloadURL: "/api/v1/projects/" + url.PathEscape(r.PathValue("id")) + "/export/download?" + query.Encode(), ExpiresAt: expires, AssetCount: len(ids), TotalBytes: manifest.totalBytes})
}

func (s *Service) downloadExport(w http.ResponseWriter, r *http.Request) {
	exportPrivateHeaders(w)
	ids, err := s.parseExportToken(r.URL.Query().Get("token"), core.UserFrom(r.Context()).ID, r.PathValue("id"), time.Now())
	if err != nil {
		httpx.Fail(w, 404, "invalid_export_link", "下载链接已过期或不可用，请重新发起下载")
		return
	}
	manifest, ok := s.resolveExport(w, r, ids, false)
	if ok {
		s.streamExport(w, r, manifest)
	}
}

// resolveExport authorizes the current project and every asset again. Download
// links pass a non-empty snapshot and never resolve a later selected version.
func (s *Service) resolveExport(w http.ResponseWriter, r *http.Request, assetIDs []string, selected bool) (exportManifest, bool) {
	var manifest exportManifest
	if len(assetIDs) > maxExportAssets {
		httpx.Fail(w, 422, "too_many_assets", "一次最多导出 200 个素材")
		return manifest, false
	}
	u := core.UserFrom(r.Context())
	var exists bool
	if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)`, r.PathValue("id"), u.ID).Scan(&exists); err != nil {
		failDB(w)
		return manifest, false
	}
	if !exists {
		notFound(w)
		return manifest, false
	}
	if selected && len(assetIDs) == 0 {
		rows, err := s.db.Query(r.Context(), `SELECT state->>'selected_image_id',state->>'selected_video_id' FROM shots WHERE project_id=$1 ORDER BY position`, r.PathValue("id"))
		if err != nil {
			failDB(w)
			return manifest, false
		}
		for rows.Next() {
			var imageID, videoID *string
			if err = rows.Scan(&imageID, &videoID); err != nil {
				break
			}
			if imageID != nil && *imageID != "" {
				assetIDs = append(assetIDs, *imageID)
			}
			if videoID != nil && *videoID != "" {
				assetIDs = append(assetIDs, *videoID)
			}
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil || rowErr != nil {
			failDB(w)
			return manifest, false
		}
	}
	if len(assetIDs) == 0 {
		httpx.Fail(w, 422, "empty_export", "请先选择成品或需要下载的素材")
		return manifest, false
	}
	if len(assetIDs) > maxExportAssets {
		httpx.Fail(w, 422, "too_many_assets", "一次最多导出 200 个素材")
		return manifest, false
	}
	seen := map[string]bool{}
	for _, id := range assetIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var key, name string
		var size int64
		err := s.db.QueryRow(r.Context(), `SELECT a.object_key,a.file_name,a.size_bytes FROM assets a WHERE a.id=$1 AND a.user_id=$2 AND a.deleted_at IS NULL AND (a.project_id=$3 OR EXISTS(SELECT 1 FROM asset_references ar WHERE ar.asset_id=a.id AND ar.project_id=$3))`, id, u.ID, r.PathValue("id")).Scan(&key, &name, &size)
		if err != nil {
			invalidAsset(w)
			return manifest, false
		}
		if size < 0 || size > maxExportBytes-manifest.totalBytes {
			httpx.Fail(w, 422, "export_too_large", "导出总大小上限为 2 GiB，请分批下载")
			return manifest, false
		}
		manifest.totalBytes += size
		manifest.files = append(manifest.files, exportFile{id: id, key: key, name: name, size: size})
	}
	return manifest, true
}

func (s *Service) streamExport(w http.ResponseWriter, r *http.Request, manifest exportManifest) {
	// Open all authorized objects before emitting the ZIP header so a missing
	// object returns a normal error instead of a deceptively complete ZIP file.
	readers := []io.ReadCloser{}
	var closeOnce sync.Once
	closeReaders := func() {
		closeOnce.Do(func() {
			for _, reader := range readers {
				_ = reader.Close()
			}
		})
	}
	defer closeReaders()
	for _, file := range manifest.files {
		reader, err := s.blobs.Open(r.Context(), file.key)
		if err != nil {
			httpx.Fail(w, 503, "media_unavailable", "部分素材暂时无法读取，请稍后重试")
			return
		}
		readers = append(readers, reader)
	}
	stopClose := context.AfterFunc(r.Context(), closeReaders)
	defer stopClose()
	if r.Context().Err() != nil {
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="project-export.zip"`)
	exportPrivateHeaders(w)
	writer := zip.NewWriter(w)
	buffer := make([]byte, 64<<10)
	for index, file := range manifest.files {
		header := &zip.FileHeader{Name: fmt.Sprintf("%03d-%s", index+1, safeFileName(file.name)), Method: zip.Store}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		body := io.LimitReader(exportContextReader{ctx: r.Context(), reader: readers[index]}, file.size+1)
		if written, err := io.CopyBuffer(entry, body, buffer); err != nil || written != file.size {
			panic(http.ErrAbortHandler)
		}
	}
	if err := writer.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}

type exportContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r exportContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
