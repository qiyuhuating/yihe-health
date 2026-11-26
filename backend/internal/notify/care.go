// 照护通知（care_notification_outbox）的收件人加密入队与读取。
// 收件人手机号与 message_outbox 一样走 securestore AEAD 密文存储；
// 去重使用收件人的确定性摘要 recipient_key（配置密钥时为 HMAC-SHA256），
// 因为密文每次密封都是随机的，不能再用明文列做唯一约束。
package notify

import (
	"context"
	"database/sql"
	"strings"
	"yihe-care/internal/config"

	"yihe-care/internal/auth"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

const recipientScope = "care-notification-recipient"

// SealNotificationRecipient 返回收件人的存储形态与去重键。
// 未配置密钥（config.Validate 已在生产环境阻断该组合）时回退明文 +
// SHA-256 摘要，与迁移回填的历史行保持一致，保证去重语义不变。
func SealNotificationRecipient(box *securestore.Box, phone string) (string, string, error) {
	if box != nil {
		sealed, err := box.Seal(phone, recipientScope)
		if err != nil {
			return "", "", err
		}
		return sealed, box.Index(recipientScope, phone), nil
	}
	return phone, box.Index(recipientScope, phone), nil
}

// OpenNotificationRecipient 还原收件人明文；无密文前缀的历史明文行原样返回。
func OpenNotificationRecipient(box *securestore.Box, stored string) (string, error) {
	if !strings.HasPrefix(stored, securestore.Prefix) {
		return stored, nil
	}
	return box.Open(stored, recipientScope)
}

// QueueCareSMS 为长者的每个有效家属手机号入队一条 SMS 通知，
// 返回实际插入的行数（0 表示无可通知的家属）。
func QueueCareSMS(ctx context.Context, tx *sql.Tx, box *securestore.Box, incidentID, oldID int64) (int64, error) {
	phones := []string{}
	err := db.QueryEach(ctx, tx, func(rows *sql.Rows) error {
		var id int64
		var record userphone.Record
		if err := rows.Scan(&id, &record.Legacy, &record.Cipher, &record.Lookup, &record.Protected); err != nil {
			return err
		}
		phone, err := record.OpenFamilyStored(box, id)
		if err != nil {
			return err
		}
		if phone != nil && auth.ValidPhone(*phone) {
			phones = append(phones, *phone)
		}
		return nil
	}, `SELECT f.family_id,f.family_phone,f.family_phone_cipher,f.family_phone_lookup,f.family_phone_protected
 FROM oldperson o JOIN family f ON f.family_id=o.family_id WHERE o.old_id=?`, oldID)
	if err != nil {
		return 0, err
	}
	if len(phones) == 0 {
		return 0, nil
	}
	var values strings.Builder
	args := make([]any, 0, len(phones)*3)
	for i, phone := range phones {
		sealed, key, err := SealNotificationRecipient(box, phone)
		if err != nil {
			return 0, err
		}
		if i > 0 {
			values.WriteByte(',')
		}
		values.WriteString("(?,'sms',?,?)")
		args = append(args, incidentID, sealed, key)
	}
	ct, err := tx.ExecContext(ctx, `INSERT INTO care_notification_outbox(incident_id,channel,recipient,recipient_key)
		VALUES `+values.String()+` ON DUPLICATE KEY UPDATE id=id`, args...)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected()
}

// EscalationPhone returns the configured, valid fallback recipient.
func EscalationPhone() string {
	phone := strings.TrimSpace(config.Setting("CARE_ESCALATION_PHONE"))
	if !auth.ValidPhone(phone) {
		return ""
	}
	return phone
}

// QueueEscalationSMS writes the fallback to the caller's incident transaction.
func QueueEscalationSMS(ctx context.Context, tx *sql.Tx, box *securestore.Box, incidentID int64, phone string) error {
	if phone == "" {
		return nil
	}
	sealed, key, err := SealNotificationRecipient(box, phone)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO care_notification_outbox(incident_id,channel,recipient,recipient_key)
		VALUES(?,'sms',?,?) ON DUPLICATE KEY UPDATE id=id`, incidentID, sealed, key)
	return err
}
