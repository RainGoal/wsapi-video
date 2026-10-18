package httpx

import (
	"net/http"
	"strings"
)

// QueryFilters rejects ambiguous single-valued filters and bounds search work.
// Limits are bytes, matching the platform's existing API input limits.
func QueryFilters(w http.ResponseWriter, r *http.Request, limits map[string]int) (map[string]string, bool) {
	out := make(map[string]string, len(limits))
	for name, max := range limits {
		values, present := r.URL.Query()[name]
		if !present {
			continue
		}
		if len(values) != 1 || len(values[0]) > max || strings.ContainsRune(values[0], 0) {
			Fail(w, 422, "invalid_filter", "搜索或筛选参数无效")
			return nil, false
		}
		out[name] = strings.TrimSpace(values[0])
	}
	return out, true
}
