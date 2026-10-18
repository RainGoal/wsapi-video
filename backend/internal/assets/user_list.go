package assets

import (
	"net/http"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

func (s *Service) pagedUserAssets(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	f, ok := httpx.QueryFilters(w, r, map[string]int{"q": 200, "project_id": 100, "kind": 20})
	if !ok {
		return
	}
	kind := f["kind"]
	if kind != "" && kind != "image" && kind != "video" && kind != "audio" {
		httpx.Fail(w, 422, "invalid_kind", "素材类型无效")
		return
	}
	const where = ` WHERE user_id=$1 AND deleted_at IS NULL AND ($2='' OR project_id=$2 OR EXISTS(SELECT 1 FROM asset_references ar WHERE ar.asset_id=assets.id AND ar.project_id=$2)) AND ($3='' OR kind=$3) AND ($4='' OR position(lower($4) IN lower(file_name))>0)`
	args := []any{core.UserFrom(r.Context()).ID, f["project_id"], kind, f["q"]}
	var total int64
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM assets"+where, args...).Scan(&total); err != nil {
		httpx.Fail(w, 500, "assets_failed", "无法读取素材")
		return
	}
	rows, err := s.db.Query(r.Context(), "SELECT "+assetFields+" FROM assets"+where+" ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6", append(args, page.Limit, page.Offset)...)
	if err != nil {
		httpx.Fail(w, 500, "assets_failed", "无法读取素材")
		return
	}
	defer rows.Close()
	items := []Asset{}
	for rows.Next() {
		a, e := scanAsset(rows)
		if e != nil {
			httpx.Fail(w, 500, "assets_failed", "无法读取素材")
			return
		}
		items = append(items, a)
	}
	if rows.Err() != nil {
		httpx.Fail(w, 500, "assets_failed", "无法读取素材")
		return
	}
	httpx.JSON(w, 200, page.Result(items, total))
}
