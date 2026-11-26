package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/blob"
	"yihe-care/internal/csrf"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func TestMySQLImageRotationPreservesHTTPDownload(t *testing.T) {
	db := testDatabase(t)
	seedCareAdminDuty(t, db, 1, "supervisor")
	key := func(v byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{v}, 32)) }
	ring := func(active string, keys map[string]string) *securestore.Box {
		t.Helper()
		box, err := securestore.NewKeyring(active, keys, key(9))
		if err != nil {
			t.Fatal(err)
		}
		return box
	}
	server := func(box *securestore.Box) (*httptest.Server, *session.Session) {
		t.Helper()
		handler := testHandler(t, db)
		handler.Box = box
		active := handler.Sessions.Create(session.Principal{Role: auth.RoleAdmin, ID: 1})
		if active == nil {
			t.Fatal("could not create synthetic test session")
		}
		mux := http.NewServeMux()
		handler.Routes(mux)
		testServer := httptest.NewServer(csrf.Middleware(handler.Sessions, auth.Middleware(handler.Sessions, handler.EnforceAdminDuty(mux))))
		t.Cleanup(testServer.Close)
		return testServer, active
	}
	oldServer, oldSession := server(ring("A", map[string]string{"A": key(1)}))
	var form bytes.Buffer
	multipartBody := multipart.NewWriter(&form)
	part, err := multipartBody.CreateFormFile("file", "synthetic.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(part, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := multipartBody.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, oldServer.URL+"/api/v1/image/saveImage", &form)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", multipartBody.FormDataContentType())
	request.Header.Set(csrf.HeaderName, oldSession.CSRFToken)
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: oldSession.ID})
	response, err := oldServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var upload struct {
		Filename string `json:"filename"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&upload)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || decodeErr != nil || upload.Filename == "" {
		t.Fatalf("valid image upload failed with HTTP %d: %v", response.StatusCode, decodeErr)
	}
	mustExec(t, db, "INSERT INTO family(family_id,family_tou_pic) VALUES(1,?)", upload.Filename)
	downloadPath := "/api/v1/image/download?name=" + url.QueryEscape(upload.Filename)
	download := func(testServer *httptest.Server, active *session.Session, status int) []byte {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, testServer.URL+downloadPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		if active != nil {
			req.AddCookie(&http.Cookie{Name: session.CookieName, Value: active.ID})
		}
		resp, err := testServer.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != status {
			t.Fatalf("expected image HTTP %d, got %d: %v", status, resp.StatusCode, err)
		}
		if status == http.StatusOK && (resp.Header.Get("Cache-Control") != "private, no-store" ||
			resp.Header.Get("Content-Type") != "image/png") {
			t.Fatal("image response headers changed")
		}
		return body
	}
	before := download(oldServer, oldSession, http.StatusOK)
	oldServer.Close()
	report, err := blob.RotateMySQLImages(context.Background(), db.DB,
		ring("B", map[string]string{"A": key(1), "B": key(2)}), 1, true)
	if err != nil || !report.Complete || report.Rewritten != 1 {
		t.Fatal("image rotation failed", report, err)
	}
	newServer, newSession := server(ring("B", map[string]string{"B": key(2)}))
	download(newServer, nil, http.StatusUnauthorized)
	if after := download(newServer, newSession, http.StatusOK); !bytes.Equal(after, before) {
		t.Fatal("original download URL returned different bytes after key rotation")
	}
}
