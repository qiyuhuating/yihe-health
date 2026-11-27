package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func BenchmarkSignedDeviceGatePreparation(b *testing.B) {
	for _, size := range []int{256, 64 << 10} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			raw, err := json.Marshal(map[string]string{"deviceNo": "WATCH001", "secret": "benchmark-secret", "extra": strings.Repeat("a", size)})
			if err != nil {
				b.Fatal(err)
			}
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			nonce := "benchmark-nonce-0123456789"
			sum := sha256.Sum256(raw)
			mac := hmac.New(sha256.New, []byte("benchmark-secret"))
			mac.Write([]byte("WATCH001:" + ts + ":" + nonce + ":" + hex.EncodeToString(sum[:])))
			header := http.Header{}
			header.Set("X-Device-Signature", hex.EncodeToString(mac.Sum(nil)))
			header.Set("X-Device-Timestamp", ts)
			header.Set("X-Device-Nonce", nonce)
			h := &Handler{}
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/device/heartbeat", bytes.NewReader(raw))
				req.Header = header
				_, credentials, ok := h.prepareSignedDeviceRequest(httptest.NewRecorder(), req)
				if !ok {
					b.Fatal("signature rejected")
				}
				if credentials.DeviceNo != "WATCH001" || credentials.Secret != "benchmark-secret" {
					b.Fatal("incorrect credentials")
				}
			}
		})
	}
}
