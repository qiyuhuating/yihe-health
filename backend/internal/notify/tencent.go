package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

var (
	tencentAppIDPattern = regexp.MustCompile(`^\d+$`)
	tencentPhonePattern = regexp.MustCompile(`^1\d{10}$`)
)

func TencentSMS(ctx context.Context, client *http.Client, appID, key, phone string) error {
	if !tencentAppIDPattern.MatchString(appID) || key == "" || !tencentPhonePattern.MatchString(phone) {
		return fmt.Errorf("SMS configuration or recipient invalid")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	nonce := binary.BigEndian.Uint64(random[:])%900000 + 100000
	ts := time.Now().Unix()
	material := "appkey=" + key + "&random=" + strconv.FormatUint(nonce, 10) + "&time=" + strconv.FormatInt(ts, 10) + "&mobile=" + phone
	sum := sha256.Sum256([]byte(material))
	body := map[string]any{"tel": map[string]any{"nationcode": "86", "mobile": phone}, "type": 0, "msg": "颐和有新的照护公告，请登录查看详情", "sig": hex.EncodeToString(sum[:]), "time": ts, "extend": "", "ext": ""}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	u := "https://yun.tim.qq.com/v5/tlssmssvr/sendsms?sdkappid=" + appID + "&random=" + strconv.FormatUint(nonce, 10)
	r, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("SMS provider not accepted")
	}
	// Read the complete bounded response. A missing result field is not success.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return fmt.Errorf("SMS provider not accepted")
	}
	var out struct {
		Result *int `json:"result"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Result == nil || *out.Result != 0 {
		return fmt.Errorf("SMS provider not accepted")
	}
	return nil
}
