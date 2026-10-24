package projects

import (
	"net/http"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

func (s *Service) pagedUserProjects(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	f, ok := httpx.QueryFilters(w, r, map[string]int{"q": 200, "status": 20})
	if !ok {
		return
	}
	status := f["status"]
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "archived" && status != "all" {
		httpx.Fail(w, 422, "invalid_status", "无效项目状态")
		return
	}
	const where = ` WHERE user_id=$1 AND workspace_kind='' AND deleted_at IS NULL AND ($2='all' OR status=$2) AND ($3='' OR position(lower($3) IN lower(name||' '||description))>0)`
	args := []any{core.UserFrom(r.Context()).ID, status, f["q"]}
	var total int64
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM projects"+where, args...).Scan(&total); err != nil {
		failDB(w)
		return
	}
	rows, err := s.db.Query(r.Context(), "SELECT "+projectFields+" FROM projects"+where+" ORDER BY updated_at DESC,id DESC LIMIT $4 OFFSET $5", append(args, page.Limit, page.Offset)...)
	if err != nil {
		failDB(w)
		return
	}
	defer rows.Close()
	items := []Project{}
	for rows.Next() {
		p, e := scanProject(rows)
		if e != nil {
			failDB(w)
			return
		}
		items = append(items, p)
	}
	if rows.Err() != nil {
		failDB(w)
		return
	}
	httpx.JSON(w, 200, page.Result(items, total))
}
