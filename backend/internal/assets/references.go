package assets

import (
	"encoding/json"
	"net/http"

	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

type AssetReference struct {
	ProjectID      string `json:"project_id"`
	ProjectName    string `json:"project_name"`
	ProjectStatus  string `json:"project_status"`
	ProjectDeleted bool   `json:"project_deleted"`
	OwnerType      string `json:"owner_type"`
	OwnerID        string `json:"owner_id"`
	OwnerLabel     string `json:"owner_label"`
}

func (s *Service) references(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	var owned bool
	var total int64
	var raw []byte
	err := s.db.QueryRow(r.Context(), assetReferencesSQL, r.PathValue("id"), core.UserFrom(r.Context()).ID, page.Limit, page.Offset).Scan(&owned, &total, &raw)
	if err != nil {
		httpx.Fail(w, 500, "asset_references_failed", "无法读取素材引用")
		return
	}
	if !owned {
		assetError(w, ErrNotFound)
		return
	}
	var items []AssetReference
	if err = json.Unmarshal(raw, &items); err != nil {
		httpx.Fail(w, 500, "asset_references_failed", "无法读取素材引用")
		return
	}
	httpx.JSON(w, 200, page.Result(items, total))
}

// Ownership, the total, and the page share one statement snapshot. Project and
// shot joins also check ownership so even an invalid reference cannot expose
// another account's project name or shot title.
const assetReferencesSQL = `WITH owned_asset AS (
 SELECT id FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL
), refs AS (
 SELECT p.id AS project_id,p.name AS project_name,p.status AS project_status,
  p.deleted_at IS NOT NULL AS project_deleted,ar.owner_type,ar.owner_id,
  CASE WHEN ar.owner_type='shot' THEN coalesce(sh.state->>'title','') ELSE '' END AS owner_label
 FROM asset_references ar JOIN owned_asset a ON a.id=ar.asset_id
 JOIN projects p ON p.id=ar.project_id AND p.user_id=$2
 LEFT JOIN shots sh ON ar.owner_type='shot' AND sh.id=ar.owner_id
  AND sh.project_id=p.id AND sh.user_id=$2
), page AS (
 SELECT * FROM refs ORDER BY project_id,owner_type,owner_id LIMIT $3 OFFSET $4
)
SELECT EXISTS(SELECT 1 FROM owned_asset),(SELECT count(*) FROM refs),
 coalesce((SELECT jsonb_agg(to_jsonb(page) ORDER BY project_id,owner_type,owner_id) FROM page),'[]'::jsonb)`
