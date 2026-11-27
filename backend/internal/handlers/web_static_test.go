package handlers

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newTestStatic() (*staticHandler, string) {
	js := strings.Repeat("console.log('yihe');\n", 200)
	fsys := fstest.MapFS{
		"index.html":            {Data: []byte("<!doctype html>" + strings.Repeat("<div>颐和</div>", 200))},
		"assets/app-abc123.js":  {Data: []byte(js)},
		"assets/br-asset.js":    {Data: []byte(js)},
		"assets/br-asset.js.br": {Data: []byte("fake-brotli-bytes")},
		"assets/tiny.js":        {Data: []byte("x")},
		"static/fonts/a.woff2":  {Data: []byte(strings.Repeat("\x00\x01\x02", 800))},
	}
	return newStaticHandler(fsys).(*staticHandler), js
}

func get(h http.Handler, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestStaticGzipRoundTrip(t *testing.T) {
	h, js := newTestStatic()
	rec := get(h, "/assets/app-abc123.js", map[string]string{"Accept-Encoding": "gzip, br"})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("want gzipped 200, got %d enc=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if got := rec.Header().Get("Cache-Control"); got != cacheImmutable {
		t.Fatalf("hashed asset cache-control = %q", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("missing Vary: Accept-Encoding")
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") &&
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if string(body) != js {
		t.Fatalf("decompressed body differs from original")
	}
	if rec.Body.Len() >= len(js) {
		t.Fatalf("gzip did not shrink the payload")
	}
}

func TestStaticBrotliPrecompressed(t *testing.T) {
	h, _ := newTestStatic()
	rec := get(h, "/assets/br-asset.js", map[string]string{"Accept-Encoding": "gzip, br"})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("want brotli 200, got %d enc=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if rec.Body.String() != "fake-brotli-bytes" {
		t.Fatalf("unexpected brotli body: %q", rec.Body.String())
	}
	etag := rec.Header().Get("ETag")
	if !strings.HasSuffix(etag, `-br"`) {
		t.Fatalf("unexpected brotli etag: %q", etag)
	}
	notMod := get(h, "/assets/br-asset.js", map[string]string{"Accept-Encoding": "br", "If-None-Match": etag})
	if notMod.Code != http.StatusNotModified {
		t.Fatalf("want 304 for brotli etag, got %d", notMod.Code)
	}
	fallback := get(h, "/assets/br-asset.js", map[string]string{"Accept-Encoding": "br;q=0, gzip"})
	if fallback.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("want gzip fallback when br;q=0, got %q", fallback.Header().Get("Content-Encoding"))
	}
}

func TestStaticConditionalRequestReturns304(t *testing.T) {
	h, _ := newTestStatic()
	first := get(h, "/", map[string]string{"Accept-Encoding": "gzip"})
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != cacheRevalidate {
		t.Fatalf("index.html must carry ETag and no-cache, got etag=%q cc=%q", etag, first.Header().Get("Cache-Control"))
	}
	second := get(h, "/", map[string]string{"Accept-Encoding": "gzip", "If-None-Match": etag})
	if second.Code != http.StatusNotModified {
		t.Fatalf("want 304, got %d", second.Code)
	}
}

func TestStaticWithoutGzipFallsBackToOriginal(t *testing.T) {
	h, js := newTestStatic()
	for _, hdr := range []map[string]string{nil, {"Accept-Encoding": "br"}, {"Accept-Encoding": "gzip;q=0"}} {
		rec := get(h, "/assets/app-abc123.js", hdr)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != js {
			t.Fatalf("hdr %v: expected original body without Content-Encoding", hdr)
		}
	}
}

func TestStaticSkipsIncompressibleAndTinyFiles(t *testing.T) {
	h, _ := newTestStatic()
	for _, target := range []string{"/static/fonts/a.woff2", "/assets/tiny.js"} {
		rec := get(h, target, map[string]string{"Accept-Encoding": "gzip"})
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%s should be served as-is, code=%d enc=%q", target, rec.Code, rec.Header().Get("Content-Encoding"))
		}
	}
	if got := get(h, "/static/fonts/a.woff2", nil).Header().Get("Cache-Control"); got != cacheStatic {
		t.Fatalf("static cache-control = %q", got)
	}
}

func TestStaticMissingPathsAreNotCached(t *testing.T) {
	h, _ := newTestStatic()
	for _, target := range []string{"/nope.js", "/a/b/c.css", "/x.html"} {
		if rec := get(h, target, map[string]string{"Accept-Encoding": "gzip, br"}); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: want 404, got %d", target, rec.Code)
		}
	}
	n := 0
	h.cache.Range(func(_, _ any) bool { n++; return true })
	h.brCache.Range(func(_, _ any) bool { n++; return true })
	if n != 0 {
		t.Fatalf("missing paths leaked %d cache entries", n)
	}
}

func TestStaticHeadAndPostStillWork(t *testing.T) {
	h, _ := newTestStatic()
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/assets/app-abc123.js", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD: code=%d bodyLen=%d", head.Code, head.Body.Len())
	}
	// 非 GET/HEAD 直接透传给 FileServer，与改动前行为一致，且不会被压缩。
	post := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, "/assets/app-abc123.js", nil)
	postReq.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(post, postReq)
	if post.Header().Get("Content-Encoding") != "" || post.Header().Get("Cache-Control") != "" {
		t.Fatalf("POST must pass through untouched, got enc=%q cc=%q", post.Header().Get("Content-Encoding"), post.Header().Get("Cache-Control"))
	}
}
