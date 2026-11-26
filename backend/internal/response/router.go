package response

import (
	"net/http"
	"strings"
)

// Router keeps unmatched API paths and method failures in the JSON contract.
func Router(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, pattern := mux.Handler(r)
		if pattern != "" {
			handler.ServeHTTP(w, r)
			return
		}
		allowed := []string{}
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			probe := r.Clone(r.Context())
			probe.Method = method
			if _, match := mux.Handler(probe); match != "" {
				allowed = append(allowed, method)
			}
		}
		if len(allowed) != 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不受支持")
			return
		}
		Error(w, http.StatusNotFound, "NOT_FOUND", "接口不存在")
	})
}
