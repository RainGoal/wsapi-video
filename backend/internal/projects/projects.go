package projects

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/storage"
)

type Service struct {
	db           *pgxpool.Pool
	blobs        storage.Store
	exportSecret []byte
}

func New(db *pgxpool.Pool, cfg config.Config, blobs storage.Store) *Service {
	return &Service{db: db, blobs: blobs, exportSecret: []byte(cfg.AppSecret)}
}
func (s *Service) Register(mux *http.ServeMux, protect func(http.Handler) http.Handler, _ func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/projects", protect(http.HandlerFunc(s.list)))
	mux.Handle("POST /api/v1/projects", protect(http.HandlerFunc(s.create)))
	mux.Handle("GET /api/v1/projects/{id}", protect(http.HandlerFunc(s.get)))
	mux.Handle("PATCH /api/v1/projects/{id}", protect(http.HandlerFunc(s.update)))
	mux.Handle("DELETE /api/v1/projects/{id}", protect(http.HandlerFunc(s.remove)))
	mux.Handle("GET /api/v1/projects/{id}/canvas", protect(http.HandlerFunc(s.getCanvas)))
	mux.Handle("PUT /api/v1/projects/{id}/canvas", protect(http.HandlerFunc(s.putCanvas)))
	mux.Handle("GET /api/v1/projects/{id}/shots", protect(http.HandlerFunc(s.getShots)))
	mux.Handle("PUT /api/v1/projects/{id}/shots", protect(http.HandlerFunc(s.putShots)))
	mux.Handle("POST /api/v1/projects/{id}/export", protect(http.HandlerFunc(s.export)))
	mux.Handle("POST /api/v1/projects/{id}/export/prepare", protect(http.HandlerFunc(s.prepareExport)))
	mux.Handle("GET /api/v1/projects/{id}/export/download", protect(http.HandlerFunc(s.downloadExport)))
	mux.Handle("POST /api/v1/projects/{id}/duplicate", protect(http.HandlerFunc(s.duplicate)))
}

type Project struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	CoverAssetID  string    `json:"cover_asset_id"`
	Status        string    `json:"status"`
	Revision      int64     `json:"revision"`
	ShotsRevision int64     `json:"shots_revision"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

const projectFields = `id,name,description,coalesce(cover_asset_id,''),status,revision,shots_revision,created_at,updated_at`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.CoverAssetID, &p.Status, &p.Revision, &p.ShotsRevision, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	s.pagedUserProjects(w, r)
}
func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 120 || len([]rune(in.Description)) > 4000 {
		httpx.Fail(w, 422, "invalid_project", "项目名需为 1–120 个字符，描述不能超过 4000 个字符")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		failDB(w)
		return
	}
	defer tx.Rollback(r.Context())
	id := core.ID("prj")
	p, err := scanProject(tx.QueryRow(r.Context(), `INSERT INTO projects(id,user_id,name,description) VALUES($1,$2,$3,$4) RETURNING `+projectFields, id, core.UserFrom(r.Context()).ID, in.Name, in.Description))
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO canvases(project_id) VALUES($1)`, id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 201, p)
}
func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	p, err := scanProject(s.db.QueryRow(r.Context(), `SELECT `+projectFields+` FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, r.PathValue("id"), core.UserFrom(r.Context()).ID))
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(w)
		return
	}
	if err != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 200, p)
}

func (s *Service) update(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision     int64   `json:"revision"`
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		CoverAssetID *string `json:"cover_asset_id"`
		Status       *string `json:"status"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if *in.Name == "" || len([]rune(*in.Name)) > 120 {
			httpx.Fail(w, 422, "invalid_name", "项目名需为 1–120 个字符")
			return
		}
	}
	if in.Description != nil && len([]rune(*in.Description)) > 4000 {
		httpx.Fail(w, 422, "invalid_description", "描述过长")
		return
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "archived" {
		httpx.Fail(w, 422, "invalid_status", "无效项目状态")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		failDB(w)
		return
	}
	defer tx.Rollback(r.Context())
	p, err := lockedProject(r.Context(), tx, r.PathValue("id"), core.UserFrom(r.Context()).ID)
	if projectError(w, err) {
		return
	}
	if p.Revision != in.Revision {
		conflict(w)
		return
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
	if in.CoverAssetID != nil {
		p.CoverAssetID = *in.CoverAssetID
		if p.CoverAssetID != "" {
			if err = validateAssets(r.Context(), tx, core.UserFrom(r.Context()).ID, map[string]string{p.CoverAssetID: "image"}); err != nil {
				invalidAsset(w)
				return
			}
		}
	}
	p, err = scanProject(tx.QueryRow(r.Context(), `UPDATE projects SET name=$2,description=$3,cover_asset_id=nullif($4,''),status=$5,revision=revision+1,updated_at=now() WHERE id=$1 RETURNING `+projectFields, p.ID, p.Name, p.Description, p.CoverAssetID, p.Status))
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM asset_references WHERE owner_type='cover' AND owner_id=$1`, p.ID)
	}
	if err == nil && p.CoverAssetID != "" {
		_, err = tx.Exec(r.Context(), `INSERT INTO asset_references(asset_id,project_id,owner_type,owner_id) VALUES($1,$2,'cover',$2)`, p.CoverAssetID, p.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 200, p)
}
func (s *Service) remove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision int64 `json:"revision"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		failDB(w)
		return
	}
	defer tx.Rollback(r.Context())
	p, err := lockedProject(r.Context(), tx, r.PathValue("id"), core.UserFrom(r.Context()).ID)
	if projectError(w, err) {
		return
	}
	if p.Revision != in.Revision {
		conflict(w)
		return
	}
	if p.Status != "archived" {
		httpx.Fail(w, 422, "archive_required", "请先归档项目，再删除")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE projects SET deleted_at=now(),revision=revision+1,updated_at=now() WHERE id=$1`, p.ID)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}
func lockedProject(ctx context.Context, tx pgx.Tx, id, userID string) (Project, error) {
	return scanProject(tx.QueryRow(ctx, `SELECT `+projectFields+` FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, userID))
}
func projectError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(w)
		return true
	}
	if err != nil {
		failDB(w)
		return true
	}
	return false
}
func failDB(w http.ResponseWriter) {
	httpx.Fail(w, 500, "database_error", "暂时无法保存或读取数据，请重试")
}
func notFound(w http.ResponseWriter) { httpx.Fail(w, 404, "not_found", "项目不存在") }
func conflict(w http.ResponseWriter) {
	httpx.Fail(w, 409, "revision_conflict", "其他设备已更新此项目，请保留草稿并重新加载")
}
func invalidAsset(w http.ResponseWriter) {
	httpx.Fail(w, 422, "invalid_asset", "素材不存在、类型不符或不属于当前账号")
}

// Lock referenced assets while updating references. Deletion acquires the same
// row lock, preventing a check-then-delete race with concurrent saves.
func validateAssets(ctx context.Context, tx pgx.Tx, userID string, ids map[string]string) error {
	for id, expected := range ids {
		var kind string
		err := tx.QueryRow(ctx, `SELECT kind FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR SHARE`, id, userID).Scan(&kind)
		if err != nil {
			return err
		}
		if expected != "" && kind != expected {
			return errors.New("asset kind mismatch")
		}
	}
	return nil
}
func replaceRefs(ctx context.Context, tx pgx.Tx, projectID, ownerType, ownerID string, ids map[string]string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM asset_references WHERE owner_type=$1 AND owner_id=$2`, ownerType, ownerID); err != nil {
		return err
	}
	for id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO asset_references(asset_id,project_id,owner_type,owner_id) VALUES($1,$2,$3,$4)`, id, projectID, ownerType, ownerID); err != nil {
			return err
		}
	}
	return nil
}

type Canvas struct {
	GraphSchemaVersion int             `json:"graph_schema_version"`
	Revision           int64           `json:"revision"`
	Graph              json.RawMessage `json:"graph"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

func (s *Service) getCanvas(w http.ResponseWriter, r *http.Request) {
	var canvas Canvas
	err := s.db.QueryRow(r.Context(), `SELECT c.graph_schema_version,c.revision,c.graph,c.updated_at FROM canvases c JOIN projects p ON p.id=c.project_id WHERE p.id=$1 AND p.user_id=$2 AND p.deleted_at IS NULL`, r.PathValue("id"), core.UserFrom(r.Context()).ID).Scan(&canvas.GraphSchemaVersion, &canvas.Revision, &canvas.Graph, &canvas.UpdatedAt)
	if projectError(w, err) {
		return
	}
	httpx.JSON(w, 200, canvas)
}
func (s *Service) putCanvas(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GraphSchemaVersion int             `json:"graph_schema_version"`
		Revision           int64           `json:"revision"`
		Graph              json.RawMessage `json:"graph"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	ids, err := graphReferences(in.Graph)
	if err != nil || in.GraphSchemaVersion != 1 {
		httpx.Fail(w, 422, "invalid_graph", "画布结构或版本不受支持")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		failDB(w)
		return
	}
	defer tx.Rollback(r.Context())
	p, err := lockedProject(r.Context(), tx, r.PathValue("id"), core.UserFrom(r.Context()).ID)
	if projectError(w, err) {
		return
	}
	if p.Status != "active" {
		httpx.Fail(w, 422, "project_archived", "请先恢复已归档项目")
		return
	}
	if err = validateAssets(r.Context(), tx, core.UserFrom(r.Context()).ID, ids); err != nil {
		invalidAsset(w)
		return
	}
	var canvas Canvas
	err = tx.QueryRow(r.Context(), `UPDATE canvases SET graph_schema_version=$2,graph=$3,revision=revision+1,updated_at=now() WHERE project_id=$1 AND revision=$4 RETURNING graph_schema_version,revision,graph,updated_at`, p.ID, in.GraphSchemaVersion, in.Graph, in.Revision).Scan(&canvas.GraphSchemaVersion, &canvas.Revision, &canvas.Graph, &canvas.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		conflict(w)
		return
	}
	if err == nil {
		err = replaceRefs(r.Context(), tx, p.ID, "canvas", p.ID, ids)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE projects SET updated_at=now() WHERE id=$1`, p.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 200, canvas)
}

func graphReferences(raw json.RawMessage) (map[string]string, error) {
	if len(raw) > 4<<20 {
		return nil, errors.New("graph too large")
	}
	var graph map[string]any
	if err := json.Unmarshal(raw, &graph); err != nil {
		return nil, err
	}
	nodes, ok := graph["nodes"].([]any)
	if !ok || len(nodes) > 2000 {
		return nil, errors.New("invalid nodes")
	}
	if _, ok = graph["edges"].([]any); !ok {
		return nil, errors.New("invalid edges")
	}
	ids := map[string]string{}
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		if depth > 30 {
			return errors.New("graph too deeply nested")
		}
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "asset_id" || key == "assetId" || key == "selected_image_id" || key == "selected_video_id" || strings.HasSuffix(key, "_asset_id") {
					if id, ok := child.(string); ok && id != "" {
						ids[id] = ""
					}
				}
				if key == "asset_ids" || strings.HasSuffix(key, "_asset_ids") || key == "image_candidate_ids" || key == "video_candidate_ids" {
					if list, ok := child.([]any); ok {
						for _, item := range list {
							if id, ok := item.(string); ok && id != "" {
								ids[id] = ""
							}
						}
					}
				}
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range value {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(graph, 0); err != nil {
		return nil, err
	}
	if len(ids) > 3000 {
		return nil, errors.New("too many assets")
	}
	return ids, nil
}
