package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"yihe-care/internal/config"
	"yihe-care/internal/securestore"
)

func (d *Worker) deliver(ctx context.Context, id int64, topic, key, channel, phone string) error {
	if channel == "sms" && config.Setting("TENCENT_SMS_APPID") != "" {
		return TencentSMS(ctx, d.Client, config.Setting("TENCENT_SMS_APPID"), config.Secret("TENCENT_SMS_APPKEY"), phone)
	}
	if d.URL == "" || d.Token == "" {
		return fmt.Errorf("gateway unavailable")
	}
	if !validGatewayURL(d.URL) {
		return fmt.Errorf("gateway requires HTTPS")
	}
	b, _ := json.Marshal(map[string]any{"type": topic, "sourceId": key, "channel": channel, "recipient": phone, "notificationId": id})
	r, err := d.gatewayRequest(ctx, d.URL, b)
	if err != nil {
		return err
	}
	r.Header.Set("Idempotency-Key", fmt.Sprintf("message-%d", id))
	resp, err := d.Client.Do(r)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gateway not accepted")
	}
	return nil
}

func validGatewayURL(url string) bool {
	return strings.HasPrefix(url, "https://") || config.Enabled("NOTIFY_ALLOW_HTTP_TEST")
}

func (d *Worker) gatewayRequest(ctx context.Context, url string, body []byte) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+d.Token)
	r.Header.Set("Content-Type", "application/json")
	return r, nil
}

func (d *Worker) openRecipient(phone string) (string, error) {
	if strings.HasPrefix(phone, securestore.Prefix) {
		return d.Box.Open(phone, "notification-recipient")
	}
	return phone, nil
}
