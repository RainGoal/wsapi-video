package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/storage"
)

var ErrNotFound = errors.New("asset not found")
var ErrInvalidMedia = errors.New("unsupported or invalid media")
var ErrTooLarge = errors.New("media size exceeds limit")

type Asset struct {
	ID              string    `json:"id"`
	UserID          string    `json:"-"`
	ProjectID       string    `json:"project_id"`
	TaskID          string    `json:"task_id,omitempty"`
	Kind            string    `json:"kind"`
	FileName        string    `json:"file_name"`
	ContentType     string    `json:"content_type"`
	ObjectKey       string    `json:"-"`
	SizeBytes       int64     `json:"size_bytes"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	DurationSeconds float64   `json:"duration_seconds"`
	SHA256          string    `json:"sha256"`
	CreatedAt       time.Time `json:"created_at"`
	URL             string    `json:"url"`
	PreviewURL      string    `json:"preview_url"`
	DownloadURL     string    `json:"download_url"`
}

const assetFields = `id,user_id,project_id,coalesce(task_id,''),kind,file_name,content_type,object_key,size_bytes,width,height,duration_seconds,sha256,created_at`

func scanAsset(row pgx.Row) (Asset, error) {
	var a Asset
	err := row.Scan(&a.ID, &a.UserID, &a.ProjectID, &a.TaskID, &a.Kind, &a.FileName, &a.ContentType, &a.ObjectKey, &a.SizeBytes, &a.Width, &a.Height, &a.DurationSeconds, &a.SHA256, &a.CreatedAt)
	a.PreviewURL = "/api/v1/assets/" + a.ID + "/download"
	a.URL = a.PreviewURL
	a.DownloadURL = a.PreviewURL + "?disposition=attachment"
	return a, err
}

type Service struct {
	db    *pgxpool.Pool
	cfg   config.Config
	blobs storage.Store
}

func New(db *pgxpool.Pool, cfg config.Config, blobs storage.Store) *Service {
	return &Service{db: db, cfg: cfg, blobs: blobs}
}
func (s *Service) Register(mux *http.ServeMux, protect func(http.Handler) http.Handler, _ func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/assets", protect(http.HandlerFunc(s.list)))
	mux.Handle("POST /api/v1/assets", protect(http.HandlerFunc(s.upload)))
	mux.Handle("GET /api/v1/assets/{id}", protect(http.HandlerFunc(s.get)))
	mux.Handle("GET /api/v1/assets/{id}/references", protect(http.HandlerFunc(s.references)))
	mux.Handle("GET /api/v1/assets/{id}/content", protect(http.HandlerFunc(s.download)))
	mux.Handle("GET /api/v1/assets/{id}/download", protect(http.HandlerFunc(s.download)))
	mux.Handle("DELETE /api/v1/assets/{id}", protect(http.HandlerFunc(s.remove)))
}
func (s *Service) Resolve(ctx context.Context, userID, assetID string) (Asset, error) {
	a, err := scanAsset(s.db.QueryRow(ctx, `SELECT `+assetFields+` FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, assetID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return a, err
}
func (s *Service) ReferenceURL(ctx context.Context, userID, assetID string) (string, error) {
	a, err := s.Resolve(ctx, userID, assetID)
	if err != nil {
		return "", err
	}
	return s.blobs.SignedURL(ctx, a.ObjectKey, 6*time.Hour)
}

// Archived resolves a previously committed task archive only after verifying
// its stored bytes, so crash recovery never settles a missing or partial object.
func (s *Service) Archived(ctx context.Context, userID, projectID, taskID string) (Asset, error) {
	a, err := scanAsset(s.db.QueryRow(ctx, `SELECT `+assetFields+` FROM assets WHERE task_id=$1 AND user_id=$2 AND project_id=$3`, taskID, userID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, err
	}
	return s.verifyStored(ctx, a)
}

func (s *Service) verifyStored(ctx context.Context, a Asset) (Asset, error) {
	body, err := s.blobs.Open(ctx, a.ObjectKey)
	if err != nil {
		return Asset{}, err
	}
	defer body.Close()
	temporary, err := os.CreateTemp("", "studio-verify-*")
	if err != nil {
		return Asset{}, err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(hash, temporary), io.LimitReader(body, a.SizeBytes+1))
	if err != nil {
		return Asset{}, err
	}
	if n != a.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
		return Asset{}, errors.New("archived object does not match recorded bytes")
	}
	metadata, err := inspectMedia(temporary, n, s.cfg.FFprobePath)
	if err != nil {
		return Asset{}, err
	}
	if metadata.kind != a.Kind || metadata.contentType != a.ContentType || metadata.width != a.Width || metadata.height != a.Height {
		return Asset{}, ErrInvalidMedia
	}
	return a, nil
}

func (s *Service) Archive(ctx context.Context, userID, projectID, taskID, fileName, contentType string, r io.Reader) (Asset, error) {
	if taskID == "" {
		return Asset{}, errors.New("archive requires task identity")
	}
	existing, err := s.Archived(ctx, userID, projectID, taskID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Asset{}, err
	}
	return s.save(ctx, userID, projectID, taskID, fileName, contentType, r, true)
}
func (s *Service) save(ctx context.Context, userID, projectID, taskID, fileName, declaredType string, r io.Reader, archive bool) (Asset, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL AND status='active')`
	if archive {
		query = `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND user_id=$2)`
	}
	if err := s.db.QueryRow(ctx, query, projectID, userID).Scan(&exists); err != nil {
		return Asset{}, err
	}
	if !exists {
		return Asset{}, ErrNotFound
	}
	maximum := s.cfg.MaxUploadBytes
	if archive && s.cfg.MaxMediaBytes > 0 {
		maximum = s.cfg.MaxMediaBytes
	}
	if maximum <= 0 {
		maximum = 256 << 20
	}
	temporary, err := os.CreateTemp("", "studio-media-*")
	if err != nil {
		return Asset{}, err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(r, maximum+1))
	if err != nil {
		return Asset{}, err
	}
	if size == 0 {
		return Asset{}, ErrInvalidMedia
	}
	if size > maximum {
		return Asset{}, ErrTooLarge
	}
	if _, err = temporary.Seek(0, io.SeekStart); err != nil {
		return Asset{}, err
	}
	metadata, err := inspectMedia(temporary, size, s.cfg.FFprobePath)
	if err != nil {
		return Asset{}, err
	}
	// The detected bytes decide content type. A declared generic MIME is allowed
	// for file uploads; a contradictory specific type is rejected.
	declaredType, _, _ = mime.ParseMediaType(declaredType)
	if declaredType == "application/ogg" {
		declaredType = "audio/ogg"
	} else if declaredType == "audio/x-flac" {
		declaredType = "audio/flac"
	}
	if declaredType != "" && declaredType != "application/octet-stream" && declaredType != metadata.contentType {
		return Asset{}, ErrInvalidMedia
	}
	id := core.ID("ast")
	fileName = safeName(fileName)
	if fileName == "" {
		fileName = "media" + metadata.extension
	}
	key := userID + "/" + projectID + "/" + id + metadata.extension
	if _, err = temporary.Seek(0, io.SeekStart); err != nil {
		return Asset{}, err
	}
	if err = s.blobs.Put(ctx, key, temporary, metadata.contentType); err != nil {
		return Asset{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		_ = s.blobs.Delete(ctx, key)
		return Asset{}, err
	}
	defer tx.Rollback(ctx)
	a, err := scanAsset(tx.QueryRow(ctx, `INSERT INTO assets(id,user_id,project_id,task_id,kind,file_name,content_type,object_key,size_bytes,width,height,duration_seconds,sha256) VALUES($1,$2,$3,nullif($4,''),$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+assetFields, id, userID, projectID, taskID, metadata.kind, fileName, metadata.contentType, key, size, metadata.width, metadata.height, metadata.duration, hex.EncodeToString(hasher.Sum(nil))))
	if err == nil && taskID != "" {
		_, err = tx.Exec(ctx, `INSERT INTO asset_references(asset_id,project_id,owner_type,owner_id) VALUES($1,$2,'task',$3) ON CONFLICT DO NOTHING`, a.ID, projectID, taskID)
	}
	commitAttempted := err == nil
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		var rejected *pgconn.PgError
		definiteFailure := !commitAttempted || errors.Is(err, pgx.ErrTxCommitRollback) || errors.As(err, &rejected)
		if definiteFailure {
			_ = s.blobs.Delete(ctx, key)
		}
		// A lost COMMIT response can still mean success. Retain the object and
		// recheck with a fresh bounded context; unresolved objects may be orphans
		// and need an offline database/object comparison before any cleanup.
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if !definiteFailure {
			if existing, lookupErr := s.Resolve(recoveryCtx, userID, id); lookupErr == nil {
				return s.verifyStored(recoveryCtx, existing)
			}
		}
		if taskID != "" {
			existing, lookupErr := s.Archived(recoveryCtx, userID, projectID, taskID)
			if lookupErr == nil {
				return existing, nil
			}
		}
		return Asset{}, err
	}
	return a, nil
}
func safeName(value string) string {
	value = path.Base(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Map(func(r rune) rune {
		if r < ' ' || r == 127 || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, value)
	if value == "." || value == ".." {
		return ""
	}
	runes := []rune(value)
	if len(runes) > 200 {
		value = string(runes[:200])
	}
	return value
}
func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	s.pagedUserAssets(w, r)
}
func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	a, err := s.Resolve(r.Context(), core.UserFrom(r.Context()).ID, r.PathValue("id"))
	if assetError(w, err) {
		return
	}
	httpx.JSON(w, 200, a)
}
func assetError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.Fail(w, 404, "not_found", "素材或项目不存在")
	case errors.Is(err, ErrInvalidMedia):
		httpx.Fail(w, 422, "invalid_media", "文件格式、尺寸或媒体元数据无效；支持 PNG、JPEG、GIF、WebP 和 MP4")
	case errors.Is(err, ErrTooLarge):
		httpx.Fail(w, 413, "file_too_large", "文件超出上传限制")
	default:
		httpx.Fail(w, 500, "media_failed", "素材处理失败，请稍后重试")
	}
	return true
}
func (s *Service) upload(w http.ResponseWriter, r *http.Request) {
	maximum := s.cfg.MaxUploadBytes
	if maximum <= 0 {
		maximum = 256 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximum+(1<<20))
	err := r.ParseMultipartForm(1 << 20)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			assetError(w, ErrTooLarge)
		} else {
			httpx.Fail(w, 422, "invalid_upload", "请通过表单上传一个文件")
		}
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 {
		httpx.Fail(w, 422, "invalid_upload", "每次请上传一个文件")
		return
	}
	if files[0].Size > maximum {
		assetError(w, ErrTooLarge)
		return
	}
	reader, err := files[0].Open()
	if err != nil {
		httpx.Fail(w, 422, "invalid_upload", "无法读取文件")
		return
	}
	defer reader.Close()
	a, err := s.save(r.Context(), core.UserFrom(r.Context()).ID, r.FormValue("project_id"), "", files[0].Filename, files[0].Header.Get("Content-Type"), reader, false)
	if assetError(w, err) {
		return
	}
	httpx.JSON(w, 201, a)
}
func (s *Service) download(w http.ResponseWriter, r *http.Request) {
	a, err := s.Resolve(r.Context(), core.UserFrom(r.Context()).ID, r.PathValue("id"))
	if assetError(w, err) {
		return
	}
	// An attachment is streamed through the authenticated endpoint because the
	// generic object-store interface deliberately cannot alter signed dispositions.
	if r.URL.Query().Get("disposition") == "attachment" {
		reader, err := s.blobs.Open(r.Context(), a.ObjectKey)
		if err != nil {
			httpx.Fail(w, 503, "media_unavailable", "素材暂时无法下载")
			return
		}
		defer reader.Close()
		w.Header().Set("Content-Type", a.ContentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.FileName}))
		w.Header().Set("Content-Length", fmt.Sprint(a.SizeBytes))
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = io.Copy(w, reader)
		return
	}
	url, err := s.blobs.SignedURL(r.Context(), a.ObjectKey, 15*time.Minute)
	if err != nil {
		httpx.Fail(w, 503, "media_unavailable", "素材暂时无法预览")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, url, http.StatusFound)
}
func (s *Service) remove(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, 500, "database_error", "暂时无法删除素材")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `SELECT id FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, r.PathValue("id"), core.UserFrom(r.Context()).ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		assetError(w, ErrNotFound)
		return
	}
	if err != nil {
		assetError(w, err)
		return
	}
	var referenced bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM asset_references WHERE asset_id=$1)`, id).Scan(&referenced); err != nil {
		assetError(w, err)
		return
	}
	if referenced {
		httpx.Fail(w, 409, "asset_in_use", "素材仍被画布、分镜、封面或任务引用")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE assets SET deleted_at=now() WHERE id=$1`, id)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if assetError(w, err) {
		return
	}
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}
