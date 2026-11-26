// 前端静态资源：将 Vue 构建产物（frontend/dist）编译进 Go 二进制。
//
// 构建前由脚本把 frontend/dist 复制到本目录（web/）；若目录为空（仅 .gitkeep），
// 则不挂载静态路由，后端退化为纯 API 模式。
package handlers

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:web
var webFS embed.FS

// MountWeb 把内嵌前端挂到 mux 的 "/"（API 路由更具体，优先匹配，不受影响）。
func MountWeb(mux *http.ServeMux) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return // 无前端产物：纯 API 模式
	}
	files := newStaticHandler(sub)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://tile.openstreetmap.org; font-src 'self'; connect-src 'self' ipc: http://ipc.localhost; object-src 'none'; frame-src https://www.amap.com; base-uri 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	}))
}
