package care

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"yihe-care/internal/notify"
	"yihe-care/internal/observability"
	"time"
)

type delivery struct {
	id         int64
	incidentID int64
	recipient  string
	channel    string
	attempts   int
	kind       string
}

type careDeliveryClaim struct {
	job            delivery
	recipientError error
}

func (d *dispatcher) dispatchNext(ctx context.Context) (bool, error) {
	claim, found, err := d.claimCareDelivery(ctx)
	if err != nil || !found {
		return found, err
	}
	err = d.sendCareDelivery(ctx, &claim.job, claim.recipientError)
	return true, d.finishCareDelivery(ctx, &claim.job, err)
}

func (d *dispatcher) claimCareDelivery(ctx context.Context) (careDeliveryClaim, bool, error) {
	// Atomic database claim; SKIP LOCKED supports multiple service replicas.
	var job delivery
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return careDeliveryClaim{}, true, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT id,incident_id,recipient,channel,attempts
        FROM care_notification_outbox
		WHERE exhausted=false AND ((status IN ('pending','failed') AND retry_at <= CURRENT_TIMESTAMP(6))
           OR (status='sending' AND last_attempt_at < CURRENT_TIMESTAMP(6) - INTERVAL 2 MINUTE))
        ORDER BY retry_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&job.id, &job.incidentID, &job.recipient, &job.channel, &job.attempts)
	if err != nil {
		// No row is normal; connection and SQL errors are not.
		if errors.Is(err, sql.ErrNoRows) {
			return careDeliveryClaim{}, false, nil
		}
		return careDeliveryClaim{}, true, err
	}
	// 收件人在入库时是密文；解密失败（密钥错误或已轮换）按投递失败走退避路径，
	// 不把密文原样交给网关。
	recipient, openErr := notify.OpenNotificationRecipient(d.box, job.recipient)
	job.recipient = recipient
	job.attempts++
	observability.Default.Add(`yihe_notification_attempts_total{queue="care"}`, 1)
	if err := tx.QueryRowContext(ctx, "SELECT kind FROM care_incident WHERE id=?", job.incidentID).Scan(&job.kind); err != nil {
		return careDeliveryClaim{}, true, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE care_notification_outbox SET status='sending',
        attempts=?,last_attempt_at=CURRENT_TIMESTAMP(6) WHERE id=?`, job.attempts, job.id); err != nil {
		return careDeliveryClaim{}, true, err
	}
	if err := tx.Commit(); err != nil {
		return careDeliveryClaim{}, true, err
	}
	return careDeliveryClaim{job: job, recipientError: openErr}, true, nil
}

func (d *dispatcher) sendCareDelivery(ctx context.Context, job *delivery, openErr error) error {
	err := openErr
	if openErr != nil {
		err = &gatewayFailure{reason: "decrypt"}
	}
	var payload []byte
	if err == nil {
		payload, err = json.Marshal(map[string]any{
			"type": "care." + job.kind, "incidentId": job.incidentID,
			"notificationId": job.id, "channel": job.channel, "recipient": job.recipient,
		})
	}
	if err == nil {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(payload))
		if reqErr != nil {
			err = &gatewayFailure{reason: "request"}
		} else {
			req.Header.Set("Authorization", "Bearer "+d.webhookToken)
			req.Header.Set("Idempotency-Key", "care-outbox-"+strconv.FormatInt(job.id, 10))
			req.Header.Set("Content-Type", "application/json")
			resp, callErr := d.httpClient.Do(req)
			if callErr != nil {
				err = transportFailure(callErr)
			} else {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					reason := "http_4xx"
					if resp.StatusCode == 429 {
						reason = "http_429"
					} else if resp.StatusCode >= 500 {
						reason = "http_5xx"
					} else if resp.StatusCode < 400 {
						reason = "http_redirect"
					}
					err = &gatewayFailure{reason: reason, status: resp.StatusCode, retryAfter: providerRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
				}
			}
		}
	}
	return err
}

func (d *dispatcher) finishCareDelivery(ctx context.Context, job *delivery, err error) error {
	if err == nil {
		_, updateErr := d.db.ExecContext(ctx, `
			UPDATE care_notification_outbox
			SET status='sent', sent_at=CURRENT_TIMESTAMP(6),last_error=NULL
			WHERE id=? AND status='sending' AND attempts=?`, job.id, job.attempts)
		// A newer claim or an authenticated receipt must never be overwritten.
		// "sent" only means the gateway accepted the request.
		return updateErr
	}
	wait := retryDelay(job.attempts)
	var failure *gatewayFailure
	if errors.As(err, &failure) {
		wait = max(wait, failure.retryAfter)
		observability.Default.Add(`yihe_care_gateway_failures_total{reason="`+failure.reason+`"}`, 1)
	}
	finish, beginErr := d.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if beginErr != nil {
		return beginErr
	}
	defer finish.Rollback()
	changed, updateErr := finish.ExecContext(ctx, `
		UPDATE care_notification_outbox
		SET status='failed', last_error=?,
		    retry_at=TIMESTAMPADD(SECOND,?,CURRENT_TIMESTAMP(6)),
		    exhausted=(?>=5)
		WHERE id=? AND status='sending' AND attempts=?`, truncate(err.Error(), 400), int(wait.Seconds()), job.attempts, job.id, job.attempts)
	if updateErr != nil {
		return updateErr
	}
	affected, err := changed.RowsAffected()
	if err != nil {
		return err
	}
	// A missing or undecryptable recipient must not be converted into a
	// permanently undeliverable voice task when SMS retries are exhausted.
	if affected > 0 && job.attempts >= 5 && job.channel == "sms" && job.recipient != "" &&
		(failure == nil || failure.reason != "decrypt") {
		voiceRecipient, voiceKey, sealErr := notify.SealNotificationRecipient(d.box, job.recipient)
		if sealErr != nil {
			return sealErr
		}
		_, err = finish.ExecContext(ctx, `INSERT INTO care_notification_outbox(incident_id,channel,recipient,recipient_key) VALUES(?,'voice',?,?) ON DUPLICATE KEY UPDATE id=id`, job.incidentID, voiceRecipient, voiceKey)
		if err != nil {
			return err
		}
	}
	return finish.Commit()
}

// retryDelay is based on the atomic claim's persisted attempt counter.
// The delay grows from 30 seconds to at most 32 minutes.
func retryDelay(attempts int) time.Duration {
	return time.Duration(30*(1<<min(max(attempts-1, 0), 6))) * time.Second
}

func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes]
}
