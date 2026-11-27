package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"yihe-care/internal/config"
)

// Bulk is explicitly configured and never silently replaces Tencent's adapter.
func (d *Worker) bulkEnabled() bool {
	return d.BulkURL != "" && config.Setting("TENCENT_SMS_APPID") == ""
}

type bulkNotification struct {
	Topic          string `json:"type"`
	SourceID       string `json:"sourceId"`
	Channel        string `json:"channel"`
	Recipient      string `json:"recipient"`
	NotificationID int64  `json:"notificationId"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// deliverBulk requires an explicit acceptance for each ID. A successful HTTP
// status alone is never a per-notification acceptance or delivery receipt.
func (d *Worker) deliverBulk(ctx context.Context, jobs []messageJob) []error {
	notAccepted := fmt.Errorf("bulk gateway not accepted")
	results := make([]error, len(jobs))
	for i := range results {
		results[i] = notAccepted
	}
	if d.Token == "" || !validGatewayURL(d.BulkURL) {
		return results
	}
	request := struct {
		Notifications []bulkNotification `json:"notifications"`
	}{Notifications: make([]bulkNotification, 0, len(jobs))}
	indices := make(map[int64]int, len(jobs))
	for i, job := range jobs {
		phone, err := d.openRecipient(job.recipient)
		if err != nil {
			continue
		}
		indices[job.id] = i
		request.Notifications = append(request.Notifications, bulkNotification{
			Topic: job.topic, SourceID: job.key, Channel: job.channel, Recipient: phone,
			NotificationID: job.id, IdempotencyKey: fmt.Sprintf("message-%d", job.id),
		})
	}
	if len(request.Notifications) == 0 {
		return results
	}
	body, err := json.Marshal(request)
	if err != nil {
		return results
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := d.gatewayRequest(call, d.BulkURL, body)
	if err != nil {
		return results
	}
	resp, err := d.Client.Do(r)
	if err != nil {
		return results
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return results
	}
	// Bound provider input independently of its declared Content-Length.
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return results
	}
	var response struct {
		Results []struct {
			ID       int64 `json:"notificationId"`
			Accepted bool  `json:"accepted"`
		} `json:"results"`
	}
	if json.Unmarshal(data, &response) != nil {
		return results
	}
	seen := make(map[int64]bool, len(response.Results))
	for _, result := range response.Results {
		if _, exists := indices[result.ID]; !exists || seen[result.ID] {
			// Reject the ambiguous response as a whole before applying any ACK.
			return results
		}
		seen[result.ID] = true
	}
	for _, result := range response.Results {
		if result.Accepted {
			results[indices[result.ID]] = nil
		}
	}
	return results
}
