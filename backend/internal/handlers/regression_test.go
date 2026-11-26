package handlers

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yihe-care/internal/imagecodec"
)

func TestCoordinateJSONCompatibility(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"string_control", `{"lng":"121.4737","lat":"31.2304"}`},
		{"numeric_coordinates", `{"lng":121.4737,"lat":31.2304}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/v1/device/report", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			body, ok := bodyMap(response, request)
			if !ok {
				t.Fatal("JSON body rejected")
			}
			lng, _ := vstr(body, "lng")
			lat, _ := vstr(body, "lat")
			if message := checkPosition(lng, lat); message != "" {
				t.Fatalf("valid JSON coordinates rejected: lng=%q lat=%q error=%s", lng, lat, message)
			}
		})
	}
}

func TestOversizedPNGGuard(t *testing.T) {
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewGray(image.Rect(0, 0, 4000, 4000))); err != nil {
		t.Fatal(err)
	}
	if input.Len() > maxImageBytes {
		t.Fatal("fixture exceeds byte limit")
	}
	_, decoded, width, height, ok := imagecodec.Sniff(input.Bytes())
	if ok || decoded != nil {
		t.Fatalf("over-limit %dx%d image already fully decoded (%d compressed bytes) before dimension rejection", width, height, input.Len())
	}
}

func TestImageUploadGuards(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			var input bytes.Buffer
			img := image.NewRGBA(image.Rect(0, 0, 2, 2))
			var err error
			if format == "png" {
				err = png.Encode(&input, img)
			} else {
				err = jpeg.Encode(&input, img, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			kind, decoded, width, height, ok := imagecodec.Sniff(input.Bytes())
			if !ok || kind != format || decoded == nil || width != 2 || height != 2 {
				t.Fatal("valid image rejected")
			}
		})
	}
	h := &Handler{}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	field, err := writer.CreateFormField("oversized-field")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = field.Write(bytes.Repeat([]byte("x"), maxImageBytes+(1<<20)+1)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/v1/image/saveImage", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	h.imageSave(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized request accepted: HTTP %d", response.Code)
	}
	if h.imageManager != nil {
		t.Fatal("rejected upload initialized image storage")
	}
}

func TestFractionalMetricsRemainInvalid(t *testing.T) {
	for _, key := range []string{"hr", "step", "sleep"} {
		if _, msg := parseMetric(map[string]any{key: 12.5}, key, 0, 200000); msg == "" {
			t.Fatalf("fractional integer metric %s accepted", key)
		}
	}
}
