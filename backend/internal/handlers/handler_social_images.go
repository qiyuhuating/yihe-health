package handlers

import (
	"bytes"
	"errors"
	"github.com/google/uuid"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"yihe-care/internal/blob"
	"yihe-care/internal/imagecodec"
	"yihe-care/internal/response"
)

// ImagesApi —— 图片上传下载（PhotoUtil 等价实现，仅标准库）
// ---------------------------------------------------------------------------

const imageFailMessage = "请选择不超过5MB的有效 JPG、PNG 或 BMP 图片"
const maxImageBytes = 5 * 1024 * 1024

// POST /api/v1/image/saveImage —— 图片上传（multipart/form-data）。
func (h *Handler) imageSave(w http.ResponseWriter, r *http.Request) {
	fail := func() {
		response.JSON(w, http.StatusBadRequest, map[string]any{"message": imageFailMessage})
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+(1<<20))
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		fail()
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		fail()
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxImageBytes {
		fail()
		return
	}
	format, img, width, height, ok := imagecodec.Sniff(data)
	if !ok || !imagecodec.ValidDims(width, height) {
		fail()
		return
	}
	ext := "png"
	if format == "jpeg" {
		ext = "jpg"
	}
	name := uuid.NewString() + "." + ext
	var encoded bytes.Buffer
	if ext == "jpg" {
		err = jpeg.Encode(&encoded, img, nil)
	} else {
		err = png.Encode(&encoded, img)
	}
	if err != nil || encoded.Len() > maxImageBytes {
		fail()
		return
	}
	manager, err := h.images()
	if err != nil {
		imageUnavailable(w)
		return
	}
	if err = manager.Put(r.Context(), name, encoded.Bytes()); err != nil {
		imageUnavailable(w)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"filename": name})
}

var imageNameRe = regexp.MustCompile(`(?i)^[^\x00]+\.(png|jpe?g|bmp)$`)

func validImageName(name string) bool {
	if name == "" || strings.ContainsAny(name, "/\\:") {
		return false
	}
	return imageNameRe.MatchString(name)
}

// GET /api/v1/image/download?name=xxx —— 图片下载（多层路径穿越防护）。
func (h *Handler) imageDownload(w http.ResponseWriter, r *http.Request) {
	notFound := func() { w.WriteHeader(http.StatusNotFound) }
	name := r.URL.Query().Get("name")
	if !validImageName(name) {
		notFound()
		return
	}
	manager, err := h.images()
	if err != nil {
		imageUnavailable(w)
		return
	}
	data, err := manager.Get(r.Context(), name)
	if errors.Is(err, blob.ErrNotFound) {
		notFound()
		return
	}
	if err != nil {
		imageUnavailable(w)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// ---------------------------------------------------------------------------
