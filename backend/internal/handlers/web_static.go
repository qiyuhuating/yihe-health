package handlers

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Vite 产物 /assets/* 文件名带内容哈希，内容变化即换名，可永久缓存。
	cacheImmutable = "public, max-age=31536000, immutable"
	// /static/* 文件名不带哈希（字体、品牌图），给较长但可被更新的缓存期。
	cacheStatic = "public, max-age=604800"
	// 入口 index.html 必须每次向服务器确认（配合 ETag 命中 304），才能及时拿到新版本。
	cacheRevalidate = "no-cache"

	// 小于该大小的文本压缩收益有限，直接走原始文件。
	gzipMinSize = 1024
)

// 只压缩文本类资源；woff2/png/jpg/webp 等本身已压缩，再压只会浪费 CPU。
var compressibleExt = map[string]bool{
	".html": true, ".js": true, ".mjs": true, ".css": true, ".json": true,
	".svg": true, ".txt": true, ".xml": true, ".webmanifest": true, ".map": true,
}

type encodedEntry struct {
	body        []byte
	etag        string
	contentType string
	encoding    string
	ok          bool // false 表示无预压缩文件或不值得压缩，回退其他编码或原始文件
}

// staticHandler 在 http.FileServer 之上增加缓存头、预压缩 Brotli (.br) 与按需 gzip。
// 前端产物编译进二进制后内容不可变，因此压缩结果按路径缓存在内存里，每个文件只处理一次。
type staticHandler struct {
	fsys    fs.FS
	files   http.Handler
	cache   sync.Map // 清洗后的文件路径 -> *encodedEntry (gzip)
	brCache sync.Map // 清洗后的文件路径 -> *encodedEntry (pre-compressed .br)
}

func newStaticHandler(fsys fs.FS) http.Handler {
	return &staticHandler{fsys: fsys, files: http.FileServer(http.FS(fsys))}
}

func cacheControlFor(urlPath string) string {
	switch {
	case strings.HasPrefix(urlPath, "/assets/"):
		return cacheImmutable
	case strings.HasPrefix(urlPath, "/static/"):
		return cacheStatic
	default:
		return cacheRevalidate
	}
}

func acceptsEncoding(r *http.Request, target string) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), target) {
			continue
		}
		// 显式 q=0 表示拒绝。
		if _, q, found := strings.Cut(strings.ReplaceAll(params, " ", ""), "q="); found {
			if v, err := strconv.ParseFloat(q, 64); err == nil && v == 0 {
				return false
			}
		}
		return true
	}
	return false
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.files.ServeHTTP(w, r)
		return
	}
	// 路径清洗：path.Clean 会折叠 ".."，FileServer 本身也会拒绝越界，这里只用于命中缓存。
	clean := path.Clean("/" + r.URL.Path)
	w.Header().Set("Cache-Control", cacheControlFor(clean))
	// 同一 URL 随 Accept-Encoding 变化，缓存层必须区分。
	w.Header().Add("Vary", "Accept-Encoding")

	name := strings.TrimPrefix(clean, "/")
	if r.URL.Path == "/" {
		name = "index.html"
	}
	if name == "" || !compressibleExt[strings.ToLower(path.Ext(name))] {
		h.files.ServeHTTP(w, r)
		return
	}
	if acceptsEncoding(r, "br") {
		if entry := h.brotli(name); entry.ok {
			h.serveEncoded(w, r, name, entry)
			return
		}
	}
	if acceptsEncoding(r, "gzip") {
		if entry := h.gzipped(name); entry.ok {
			h.serveEncoded(w, r, name, entry)
			return
		}
	}
	h.files.ServeHTTP(w, r)
}

func (h *staticHandler) serveEncoded(w http.ResponseWriter, r *http.Request, name string, entry *encodedEntry) {
	hd := w.Header()
	hd.Set("Content-Type", entry.contentType)
	hd.Set("Content-Encoding", entry.encoding)
	hd.Set("ETag", entry.etag)
	// ServeContent 负责 If-None-Match -> 304、HEAD 与 Range。
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(entry.body))
}

func contentTypeFor(name string) string {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func (h *staticHandler) brotli(name string) *encodedEntry {
	if v, ok := h.brCache.Load(name); ok {
		return v.(*encodedEntry)
	}
	brBytes, err := fs.ReadFile(h.fsys, name+".br")
	if err != nil || len(brBytes) == 0 {
		// 不存在的路径绝不写入缓存，避免恶意路径探测撑爆内存。
		return &encodedEntry{}
	}
	sum := sha256.Sum256(brBytes)
	entry := &encodedEntry{
		body:        brBytes,
		etag:        `"` + hex.EncodeToString(sum[:8]) + `-br"`,
		contentType: contentTypeFor(name),
		encoding:    "br",
		ok:          true,
	}
	actual, _ := h.brCache.LoadOrStore(name, entry)
	return actual.(*encodedEntry)
}

func (h *staticHandler) gzipped(name string) *encodedEntry {
	if v, ok := h.cache.Load(name); ok {
		return v.(*encodedEntry)
	}
	raw, err := fs.ReadFile(h.fsys, name)
	if err != nil {
		// 不存在的路径来自请求，绝不写入缓存，否则可被用来撑爆内存。
		return &encodedEntry{}
	}
	entry := compress(name, raw)
	actual, _ := h.cache.LoadOrStore(name, entry)
	return actual.(*encodedEntry)
}

func compress(name string, raw []byte) *encodedEntry {
	miss := &encodedEntry{}
	if len(raw) < gzipMinSize {
		return miss
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return miss
	}
	if _, err := zw.Write(raw); err != nil || zw.Close() != nil || buf.Len() >= len(raw) {
		return miss
	}
	sum := sha256.Sum256(buf.Bytes())
	return &encodedEntry{
		body:        buf.Bytes(),
		etag:        `"` + hex.EncodeToString(sum[:8]) + `-gz"`,
		contentType: contentTypeFor(name),
		encoding:    "gzip",
		ok:          true,
	}
}
