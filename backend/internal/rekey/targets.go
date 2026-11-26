// Package rekey rotates the application's existing MySQL encrypted fields.
// It does not migrate plaintext, indexes, objects, projections or backups.
package rekey

import (
	"fmt"

	"yihe-care/internal/familyaddress"
	"yihe-care/internal/health"
	"yihe-care/internal/residentprofile"
	"yihe-care/internal/vitalrepo"
)

type target struct {
	table, column string
	keys          []string
	scope         string
	format        envelopeFormat
}

type envelopeFormat uint8

const (
	directEnvelope envelopeFormat = iota
	ruleEnvelope
	coordinateProjection
)

func (t target) condition() string {
	if t.format == coordinateProjection {
		return "CAST(entity AS BINARY)=CAST('device_location' AS BINARY)"
	}
	return "true"
}

func (t target) name() string { return t.table + "." + t.column }
func (t target) fieldScope(keys []any) string {
	switch t.table {
	case "ratepre":
		return vitalrepo.MeasurementScope(fmt.Sprint(keys[0]))
	case "health_sample":
		return health.SamplePayloadScope(fmt.Sprint(keys[0]), fmt.Sprint(keys[1]), fmt.Sprint(keys[2]))
	case "health_change":
		return health.ChangePayloadScope(fmt.Sprint(keys[0]), fmt.Sprint(keys[1]), fmt.Sprint(keys[2]))
	case "health_sample_latest":
		return health.LatestPayloadScope(fmt.Sprint(keys[0]), fmt.Sprint(keys[1]), fmt.Sprint(keys[2]))
	case "metric_daily":
		return health.DailyResultScope(fmt.Sprint(keys[0]), fmt.Sprint(keys[1]))
	}
	if t.table == "auth_factor" {
		return "totp:" + fmt.Sprint(keys[0]) + ":" + fmt.Sprint(keys[1])
	}
	if t.table == "users" && t.column == "user_phone_cipher" {
		return "account-phone:admin:" + fmt.Sprint(keys[0])
	}
	if t.table == "family" && t.column == "family_phone_cipher" {
		return "account-phone:family:" + fmt.Sprint(keys[0])
	}
	if t.table == "family" && t.column == "family_address_cipher" {
		return familyaddress.Scope(fmt.Sprint(keys[0]))
	}
	if t.table == "said" && t.column == "said_phone_cipher" {
		return "social-comment-phone:" + fmt.Sprint(keys[0])
	}
	if t.table == "oldperson" && t.column == "care_profile_cipher" {
		return "resident-care-profile:" + fmt.Sprint(keys[0])
	}
	if t.table == "oldperson" && t.column == "birthdate_cipher" {
		return residentprofile.BirthdateScope(fmt.Sprint(keys[0]))
	}
	if t.table == "customer" && t.column == "contact_cipher" {
		return "guardian-contact:" + fmt.Sprint(keys[0])
	}
	return t.scope
}

// Identifiers are compile-time literals, never CLI parameters. Keep scopes in
// sync with production writers; the integration test covers the actual schema.
func targets() []target {
	return []target{
		{"care_incident", "resolution_note", []string{"id"}, "care-text", directEnvelope},
		{"care_incident_event", "note", []string{"id"}, "care-text", directEnvelope},
		{"alert_review", "reason", []string{"id"}, "care-text", directEnvelope},
		{"care_notification_outbox", "recipient", []string{"id"}, "care-notification-recipient", directEnvelope},
		{"message_outbox", "recipient", []string{"id"}, "notification-recipient", directEnvelope},
		{"device", "last_lng", []string{"device_id"}, "location", directEnvelope},
		{"device", "last_lat", []string{"device_id"}, "location", directEnvelope},
		{"device_location", "lng", []string{"id"}, "location", directEnvelope},
		{"device_location", "lat", []string{"id"}, "location", directEnvelope},
		{"device_location", "position_metadata", []string{"id"}, "location", directEnvelope},
		{"alarm_precess", "lng", []string{"id"}, "location", directEnvelope},
		{"alarm_precess", "lat", []string{"id"}, "location", directEnvelope},
		{"alarm_precess", "position_metadata", []string{"id"}, "location", directEnvelope},
		{"alarm_process", "lng", []string{"id"}, "location", directEnvelope},
		{"alarm_process", "lat", []string{"id"}, "location", directEnvelope},
		{"web_session", "payload", []string{"id_hash"}, "session", directEnvelope},
		{"auth_factor", "secret", []string{"role", "actor_id"}, "", directEnvelope},
		{"rule_state", "evidence", []string{"device_id", "epoch", "kind"}, "rule-evidence", ruleEnvelope},
		{"telemetry_inbox", "payload", []string{"id"}, "telemetry-inbox", directEnvelope},
		{"analytics_outbox", "payload", []string{"id"}, "location", coordinateProjection},
		{"users", "user_phone_cipher", []string{"user_id"}, "", directEnvelope},
		{"family", "family_phone_cipher", []string{"family_id"}, "", directEnvelope},
		{"said", "said_phone_cipher", []string{"said_id"}, "", directEnvelope},
		{"oldperson", "care_profile_cipher", []string{"old_id"}, "", directEnvelope},
		{"customer", "contact_cipher", []string{"customer_id"}, "", directEnvelope},
		{"guardian_contact_key_state", "key_verifier", []string{"singleton"}, "guardian-contact-key-state", directEnvelope},
		{"family", "family_address_cipher", []string{"family_id"}, "", directEnvelope},
		{"oldperson", "birthdate_cipher", []string{"old_id"}, "", directEnvelope},
		{"health_change", "payload_cipher", []string{"revision", "old_id", "sample_id"}, "", directEnvelope},
		{"health_sample_latest", "payload_cipher", []string{"old_id", "series_key", "revision"}, "", directEnvelope},
		{"metric_daily", "result_cipher", []string{"cache_key", "computed_revision"}, "", directEnvelope},
		{"health_sample", "payload_cipher", []string{"sample_id", "old_id", "revision"}, "", directEnvelope},
		{"ratepre", "measurement_cipher", []string{"id"}, "", directEnvelope},
	}
}
