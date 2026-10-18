package httpx

import (
	"net/http"
	"strconv"
)

type Page struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Pagination bounds database work and rejects malformed values instead of
// silently changing which records the operator sees.
func Pagination(w http.ResponseWriter, r *http.Request) (Page, bool) {
	p := Page{Limit: 25}
	for _, item := range []struct {
		name             string
		value            *int
		minimum, maximum int
	}{{"limit", &p.Limit, 1, 100}, {"offset", &p.Offset, 0, 10000000}} {
		values, present := r.URL.Query()[item.name]
		if !present {
			continue
		}
		if len(values) != 1 {
			Fail(w, 422, "invalid_pagination", "分页参数无效")
			return p, false
		}
		v, err := strconv.Atoi(values[0])
		if err != nil || v < item.minimum || v > item.maximum {
			Fail(w, 422, "invalid_pagination", "分页参数无效")
			return p, false
		}
		*item.value = v
	}
	return p, true
}

func (p Page) Result(items any, total int64) map[string]any {
	return map[string]any{"items": items, "total": total, "limit": p.Limit, "offset": p.Offset}
}
