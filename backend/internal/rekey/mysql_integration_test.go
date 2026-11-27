package rekey

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/familyaddress"
	"yihe-care/internal/guardiancontact"
	"yihe-care/internal/health"
	"yihe-care/internal/residentprofile"
	"yihe-care/internal/userphone"
	"yihe-care/internal/vitalrepo"
)

// This uses a disposable, fully migrated InnoDB database. Node adapters and
// crypto-only tests cannot establish the pagination or CAS guarantees here.
func TestRekeyMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	database := pool.DB
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	old, current, retired, legacy := boxes(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	get := func(query string, args ...any) string {
		t.Helper()
		var value string
		if err := database.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	column := func(columns []Column, name string) Column {
		t.Helper()
		for _, value := range columns {
			if value.Name == name {
				return value
			}
		}
		t.Fatalf("column missing from report: %s", name)
		return Column{}
	}
	s := func(scope string) string { return sealed(t, old, "synthetic protected value", scope) }
	phone, err := userphone.Protect(old, 7, "13800000007")
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO users(user_id,user_name,user_phone_cipher,user_phone_lookup,user_phone_protected) VALUES(7,'synthetic',?,?,true)", phone.Ciphertext, phone.Lookup)
	phoneKey, err := userphone.KeyBinding(old)
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO account_phone_key_state(singleton,lookup_verifier) VALUES(1,?)", phoneKey)
	familyPhone, err := userphone.ProtectFamily(old, 7, "13800000017")
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO family(family_id,family_name,family_phone_cipher,family_phone_lookup,family_phone_protected) VALUES(7,'synthetic',?,?,true)", familyPhone.Ciphertext, familyPhone.Lookup)
	exec("INSERT INTO said(said_id,text_id,said_phone_cipher,said_phone_protected) VALUES(7,1,?,true)", sealed(t, old, "13800000027", "social-comment-phone:7"))
	socialLookup := old.Index("social-like-phone", "13800000027")
	socialBinding := old.Index("social-phone-key-binding", "v1")
	exec("INSERT INTO social_phone_key_state(singleton,lookup_verifier) VALUES(1,?)", socialBinding)
	exec("INSERT INTO nice_count(said_id,phone_lookup,phone_protected) VALUES(7,?,true)", socialLookup)
	exec("INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic')")
	exec("INSERT INTO oldperson(old_id,name) VALUES(7,'synthetic profile')")
	profileTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer profileTx.Rollback()
	if err := residentprofile.Store(ctx, profileTx, old, 7, residentprofile.Strings("13800000037", "301", "synthetic condition", "", "rice", "", "synthetic medication")); err != nil {
		t.Fatal(err)
	}
	if err := profileTx.Commit(); err != nil {
		t.Fatal(err)
	}
	profileLookup := get("SELECT phone_lookup FROM oldperson WHERE old_id=7")
	profileBinding := get("SELECT lookup_verifier FROM resident_profile_key_state WHERE singleton=1")
	exec("INSERT INTO customer(customer_id,customer_name) VALUES(7,'synthetic guardian')")
	contactTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer contactTx.Rollback()
	if err := guardiancontact.Store(ctx, contactTx, old, 7, guardiancontact.Strings("010-12345678", "synthetic contact address")); err != nil {
		t.Fatal(err)
	}
	if err := contactTx.Commit(); err != nil {
		t.Fatal(err)
	}
	contactBinding := get("SELECT lookup_verifier FROM guardian_contact_key_state WHERE singleton=1")
	address := sealed(t, old, "synthetic family address", familyaddress.Scope("7"))
	exec("UPDATE family SET family_address_cipher=?,family_address_protected=true WHERE family_id=7", address)
	birthdate := sealed(t, old, "1956-02-29", residentprofile.BirthdateScope("7"))
	exec("UPDATE oldperson SET birthdate_cipher=?,birthdate_protected=true WHERE old_id=7", birthdate)
	sampleID := strings.Repeat("a", 64)
	seriesKey := strings.Repeat("\x00\xff", 16)
	dailyKey := strings.Repeat("b", 64)
	exec("INSERT INTO health_change(revision,old_id,sample_id,payload_cipher) VALUES(8,7,UNHEX(?),?)",
		sampleID, sealed(t, old, "{}", health.ChangePayloadScope("8", "7", sampleID)))
	exec(`INSERT INTO health_sample(sample_id,old_id,epoch,version,revision,payload_cipher,source,source_device,metric,semantics,sample_start,sample_end,deleted)
 VALUES(UNHEX(?),7,1,1,8,?,'watch','device','heart_rate','instant',1,1,false)`,
		sampleID, sealed(t, old, "{}", health.SamplePayloadScope(sampleID, "7", "8")))
	exec("INSERT INTO health_sample_latest(old_id,series_key,sample_id,epoch,revision,payload_cipher) VALUES(7,?,UNHEX(?),1,8,?)",
		[]byte(seriesKey), sampleID, sealed(t, old, "{}", health.LatestPayloadScope("7", seriesKey, "8")))
	exec("INSERT INTO metric_daily(cache_key,old_id,source,source_device,metric,semantics,record_day,offset_minutes,computed_revision,result_cipher) VALUES(?,7,'watch','device','heart_rate','instant','1956-02-29',0,8,?)",
		dailyKey, sealed(t, old, "{}", health.DailyResultScope(dailyKey, "8")))
	exec("INSERT INTO device(device_id,device_no,secret,old_id,last_lng,last_lat) VALUES(1,'REKEY-LAB',?,1,?,?)", strings.Repeat("x", 48), s("location"), s("location"))
	exec("INSERT INTO ratepre(id,old_id,device_id) VALUES(7,7,1)")
	vitalTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer vitalTx.Rollback()
	owner, device, pulse := int64(7), int64(1), int64(72)
	if err := vitalrepo.StoreProtectedMeasurements(ctx, vitalTx, old, 7, &owner, &device, vitalrepo.Measurements{HeartRate: &pulse}); err != nil {
		t.Fatal(err)
	}
	if err := vitalTx.Commit(); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO care_incident(id,old_id,device_id,resolution_note) VALUES(1,1,1,?)", s("care-text"))
	for _, id := range []int{1, 2, 10, 100} {
		exec("INSERT INTO care_incident_event(id,incident_id,actor,action,note) VALUES(?,1,'synthetic','received',?)", id, s("care-text"))
	}
	exec("INSERT INTO alert_review(id,entity,record_id,kind,verdict,reason,actor_id) VALUES(1,'care',1,'sos','confirmed',?,1)", s("care-text"))
	lookup := old.Index("care-notification-recipient", "13800000001")
	exec("INSERT INTO care_notification_outbox(id,incident_id,channel,recipient,recipient_key) VALUES(1,1,'sms',?,?)", sealed(t, old, "13800000001", "care-notification-recipient"), lookup)
	exec("INSERT INTO message_outbox(id,topic,source_key,family_id,channel,recipient) VALUES(1,'care','1',1,'sms',?)", s("notification-recipient"))
	exec("INSERT INTO device_location(id,device_id,old_id,lng,lat,position_metadata) VALUES(1,1,1,?,?,?)", s("location"), s("location"), s("location"))
	// Historical outbox entries are retained for rekey, but the retired PG
	// projection triggers no longer populate them on new business writes.
	historicalLocation, err := json.Marshal(map[string]any{"lng": s("location"), "lat": s("location"), "source": "gps"})
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO analytics_outbox(entity,entity_id,payload) VALUES('device_location',1,?)", string(historicalLocation))
	exec("INSERT INTO alarm_precess(id,old_id,device_id,lng,lat,position_metadata) VALUES(1,1,1,?,?,?)", s("location"), s("location"), s("location"))
	exec("INSERT INTO alarm_process(id,old_id,device_id,lng,lat) VALUES(1,1,1,?,?)", s("location"), s("location"))
	for _, hash := range []string{strings.Repeat("1", 64), strings.Repeat("a", 64), strings.Repeat("z", 64)} {
		exec("INSERT INTO web_session(id_hash,role,actor_id,payload,expires_at) VALUES(?,'ADMIN',1,?,UTC_TIMESTAMP(6)+INTERVAL 1 HOUR)", hash, s("session"))
	}
	for _, role := range []string{"ADMIN", "FAMILY"} {
		for _, id := range []int{1, 10} {
			scope := target{table: "auth_factor"}.fieldScope([]any{role, id})
			exec("INSERT INTO auth_factor(role,actor_id,secret,generation,pending_until,recovery_hashes) VALUES(?,?,?,'00000000-0000-0000-0000-000000000000',UTC_TIMESTAMP(6),'[]')", role, id, s(scope))
		}
	}
	for _, kind := range []string{"heart_rate", "inactivity"} {
		wrapper, _ := json.Marshal(map[string]any{"encrypted": s("rule-evidence"), "future": 7})
		exec("INSERT INTO rule_state(device_id,epoch,kind,observation_key,streak,active,evidence,updated_at) VALUES(1,1,?,'synthetic',1,false,?,UTC_TIMESTAMP(6))", kind, string(wrapper))
	}
	exec("INSERT INTO telemetry_inbox(id,device_id,elder_id,binding_epoch,request_id,payload,next_retry_at,created_at) VALUES(1,1,1,1,'synthetic',?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", s("telemetry-inbox"))
	exec(`INSERT INTO analytics_outbox(entity,entity_id,payload) VALUES('care_incident',98765,'{"note":"sne1:v2:A:not-a-coordinate"}')`)
	unrelatedProjection := get("SELECT CAST(payload AS CHAR) FROM analytics_outbox WHERE entity='care_incident' AND entity_id=98765")
	options := Options{BatchSize: 1, MaxRowsPerColumn: 1000}
	beforeBusiness := get("SELECT CONCAT(health_epoch,':',old_id,':',device_no) FROM device WHERE device_id=1")
	beforeIncident := get("SELECT CONCAT(state,':',updated_at) FROM care_incident WHERE id=1")
	beforeNote := get("SELECT resolution_note FROM care_incident WHERE id=1")

	t.Run("scan is readonly and covers all actual columns", func(t *testing.T) {
		report, err := Run(ctx, database, current, old, options)
		if err != nil || len(report.Before) != 33 || report.Stage != "complete" || report.EncryptedColumnsClean || report.RetirementAuthorized {
			t.Fatalf("unexpected report: %+v err=%v", report, err)
		}
		for _, column := range report.Before {
			if !column.Complete || column.OldKey < 1 || column.Invalid != 0 || column.Plaintext != 0 {
				t.Fatalf("missing actual-column coverage: %+v", column)
			}
		}
		if get("SELECT resolution_note FROM care_incident WHERE id=1") != beforeNote {
			t.Fatal("scan mutated data")
		}
	})
	t.Run("bounded composite numeric and string pagination with retirement read", func(t *testing.T) {
		options.Apply = true
		report, err := Run(ctx, database, current, old, options)
		if err != nil || !report.EncryptedColumnsClean || report.Stage != "complete" || report.RetirementAuthorized {
			t.Fatalf("apply failed: %+v err=%v", report, err)
		}
		var rewrites int64
		for _, column := range report.Applied {
			rewrites += column.Rewritten
			if column.Conflicts != 0 || !column.Complete {
				t.Fatal("incomplete apply")
			}
		}
		if rewrites != 42 {
			t.Fatalf("pagination lost rows: rewrites=%d", rewrites)
		}
		options.Apply = false
		check, err := Run(ctx, database, retired, old, options)
		if err != nil || !check.EncryptedColumnsClean {
			t.Fatal("old key still needed for current fields", err)
		}
		if get("SELECT recipient_key FROM care_notification_outbox WHERE id=1") != lookup || current.Index("care-notification-recipient", "13800000001") != lookup {
			t.Fatal("HMAC index changed")
		}
		if value, err := retired.Open(get("SELECT said_phone_cipher FROM said WHERE said_id=7"), "social-comment-phone:7"); err != nil || value != "13800000027" {
			t.Fatal("comment phone still requires retired reader", err)
		}
		if get("SELECT phone_lookup FROM nice_count WHERE said_id=7") != socialLookup || get("SELECT lookup_verifier FROM social_phone_key_state WHERE singleton=1") != socialBinding {
			t.Fatal("AEAD rotation changed social HMAC identity or binding")
		}
		if _, err := retired.Open(get("SELECT care_profile_cipher FROM oldperson WHERE old_id=7"), "resident-care-profile:7"); err != nil || get("SELECT phone_lookup FROM oldperson WHERE old_id=7") != profileLookup || get("SELECT lookup_verifier FROM resident_profile_key_state WHERE singleton=1") != profileBinding {
			t.Fatal("resident profile rotation changed its scope, lookup or binding", err)
		}
		if _, err := retired.Open(get("SELECT contact_cipher FROM customer WHERE customer_id=7"), "guardian-contact:7"); err != nil || get("SELECT lookup_verifier FROM guardian_contact_key_state WHERE singleton=1") != contactBinding {
			t.Fatal("guardian contact rotation changed its scope or binding", err)
		}
		if err := guardiancontact.VerifyKey(ctx, database, retired); err != nil {
			t.Fatal("guardian reader marker still requires the retired key", err)
		}
		address := get("SELECT family_address_cipher FROM family WHERE family_id=7")
		plain, err := (familyaddress.Record{Cipher: &address, Protected: true}).Open(retired, 7)
		if err != nil || plain == nil || *plain != "synthetic family address" {
			t.Fatal("family address could not be read with only the new key", err)
		}
		storedVital := get("SELECT measurement_cipher FROM ratepre WHERE id=7")
		vital, err := (vitalrepo.StoredMeasurements{Cipher: &storedVital}).Open(retired, 7, &owner, &device)
		if err != nil || vital.HeartRate == nil || *vital.HeartRate != pulse || vital.Pressure != nil {
			t.Fatal("legacy vital measurements still require the previous reader", err)
		}
		birthdate := get("SELECT birthdate_cipher FROM oldperson WHERE old_id=7")
		date, err := (residentprofile.BirthdateRecord{Cipher: &birthdate, Protected: true}).Open(retired, 7)
		if err != nil || date == nil || date.Format("2006-01-02") != "1956-02-29" {
			t.Fatal("resident birthdate could not be read with only the new key", err)
		}
		if get("SELECT CONCAT(health_epoch,':',old_id,':',device_no) FROM device WHERE device_id=1") != beforeBusiness || get("SELECT CONCAT(state,':',updated_at) FROM care_incident WHERE id=1") != beforeIncident {
			t.Fatal("business state changed")
		}
		if get("SELECT JSON_EXTRACT(evidence,'$.future') FROM rule_state LIMIT 1") != "7" {
			t.Fatal("JSON wrapper metadata lost")
		}
		if get("SELECT COUNT(*) FROM analytics_outbox WHERE entity='device_location' AND CAST(payload AS CHAR) LIKE '%sne1:v2:A:%'") != "0" || report.RetirementAuthorized {
			t.Fatal("historical coordinate events retain A or retirement was authorized")
		}
		if get("SELECT CAST(payload AS CHAR) FROM analytics_outbox WHERE entity='care_incident' AND entity_id=98765") != unrelatedProjection {
			t.Fatal("unrelated projection was changed")
		}
		// The preserved old ciphertext is backup-like historical input, not a
		// claim of a coordinated backup restore. Only an A reader can open it.
		if _, err := retired.Open(beforeNote, "care-text"); err == nil {
			t.Fatal("retired reader opened A")
		}
		if got, err := old.Open(beforeNote, "care-text"); err != nil || got != "synthetic protected value" {
			t.Fatal("historical ciphertext cannot be restored with retained key")
		}
	})
	t.Run("authentication failure prevents every write", func(t *testing.T) {
		valid := sealed(t, old, "valid old note", "care-text")
		goodQueue := get("SELECT payload FROM telemetry_inbox WHERE id=1")
		exec("UPDATE care_incident SET resolution_note=? WHERE id=1", valid)
		exec("UPDATE telemetry_inbox SET payload='sne1:v2:MISSING:AAAA' WHERE id=1")
		options.Apply = true
		report, err := Run(ctx, database, current, old, options)
		if err == nil || report.Stage != "preflight" || len(report.Applied) != 0 || column(report.Before, "telemetry_inbox.payload").Invalid != 1 {
			t.Fatal("invalid late column did not fail before mutations")
		}
		if get("SELECT resolution_note FROM care_incident WHERE id=1") != valid {
			t.Fatal("valid earlier column was modified before preflight finished")
		}
		exec("UPDATE telemetry_inbox SET payload=? WHERE id=1", goodQueue)
	})
	t.Run("legacy envelope resumability and idempotent rerun", func(t *testing.T) {
		value := sealed(t, legacy, "legacy synthetic note", "care-text")
		exec("UPDATE care_incident SET resolution_note=? WHERE id=1", value)
		options.Apply = true
		report, err := Run(ctx, database, current, old, options)
		if err != nil || report.Before[0].Legacy != 1 || report.Applied[0].Rewritten != 1 || !report.EncryptedColumnsClean {
			t.Fatal("legacy resume failed", err)
		}
		report, err = Run(ctx, database, current, old, options)
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range report.Applied {
			if column.Rewritten != 0 {
				t.Fatal("idempotent rerun rewrote active data")
			}
		}
	})
	t.Run("historical projection authentication fails before direct field writes", func(t *testing.T) {
		note := sealed(t, old, "valid earlier field", "care-text")
		exec("UPDATE care_incident SET resolution_note=? WHERE id=1", note)
		payload, _ := json.Marshal(map[string]any{"lng": sealed(t, old, "113.5", "location"), "lat": "sne1:v2:MISSING:AAAA", "source": "gps", "old_id": 1})
		exec("INSERT INTO analytics_outbox(entity,entity_id,payload) VALUES('device_location',98766,?)", string(payload))
		options.Apply = true
		report, err := Run(ctx, database, current, old, options)
		if err == nil || report.Stage != "preflight" || len(report.Applied) != 0 || column(report.Before, "analytics_outbox.payload").Invalid != 1 {
			t.Fatal("bad nested coordinate did not block every write")
		}
		if get("SELECT resolution_note FROM care_incident WHERE id=1") != note {
			t.Fatal("earlier direct field changed before projection authentication")
		}
		exec("UPDATE analytics_outbox SET payload=JSON_SET(payload,'$.lat',?) WHERE entity='device_location' AND entity_id=98766", sealed(t, current, "28.5", "location"))
		report, err = Run(ctx, database, current, old, options)
		if err != nil || !report.EncryptedColumnsClean || column(report.Applied, "analytics_outbox.payload").Rewritten != 1 {
			t.Fatal("repaired mixed-version projection failed", err)
		}
		if get("SELECT JSON_UNQUOTE(JSON_EXTRACT(payload,'$.source')) FROM analytics_outbox WHERE entity='device_location' AND entity_id=98766") != "gps" {
			t.Fatal("projection metadata changed")
		}
	})
	t.Run("row budget fails incomplete before writes", func(t *testing.T) {
		limited := options
		limited.MaxRowsPerColumn = 1
		report, err := Run(ctx, database, current, old, limited)
		if err == nil || report.Stage != "preflight" || len(report.Applied) != 0 || column(report.Before, "care_incident_event.note").Complete {
			t.Fatal("truncated inventory accepted")
		}
	})
	t.Run("plaintext is reported without silent migration", func(t *testing.T) {
		original := get("SELECT reason FROM alert_review WHERE id=1")
		exec("UPDATE alert_review SET reason='synthetic legacy plaintext' WHERE id=1")
		report, err := Run(ctx, database, current, old, options)
		if err == nil || report.EncryptedColumnsClean || report.After[2].Plaintext != 1 || get("SELECT reason FROM alert_review WHERE id=1") != "synthetic legacy plaintext" {
			t.Fatal("plaintext was silently migrated or accepted")
		}
		encoded, _ := json.Marshal(report)
		if strings.Contains(string(encoded), "synthetic legacy plaintext") || strings.Contains(string(encoded), beforeNote) {
			t.Fatal("report leaked payload")
		}
		exec("UPDATE alert_review SET reason=? WHERE id=1", original)
	})
	t.Run("oversize value is rejected without loading it", func(t *testing.T) {
		original := get("SELECT payload FROM telemetry_inbox WHERE id=1")
		exec("UPDATE telemetry_inbox SET payload=REPEAT('x',?) WHERE id=1", maxFieldBytes+1)
		report, err := Run(ctx, database, current, old, options)
		if err == nil || len(report.Applied) != 0 || column(report.Before, "telemetry_inbox.payload").Invalid != 1 {
			t.Fatal("oversize value accepted")
		}
		exec("UPDATE telemetry_inbox SET payload=? WHERE id=1", original)
	})
	t.Run("binary CAS preserves concurrent writer including case and spaces", func(t *testing.T) {
		exec("ALTER TABLE alert_review MODIFY reason TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL")
		target := targets()[2]
		for _, concurrent := range []string{"SAME", "same "} {
			exec("UPDATE alert_review SET reason='same' WHERE id=1")
			stale := get("SELECT reason FROM alert_review WHERE id=1")
			done := make(chan error, 1)
			go func() {
				_, err := database.ExecContext(ctx, "UPDATE alert_review SET reason=? WHERE id=1", concurrent)
				done <- err
			}()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			changed, err := compareSwap(ctx, database, target, []any{int64(1)}, stale, "must not win")
			if err != nil || changed || get("SELECT reason FROM alert_review WHERE id=1") != concurrent {
				t.Fatal("CAS overwrote concurrent update", err)
			}
		}
		// Deletion cannot be resurrected by a stale candidate.
		exec("DELETE FROM alert_review WHERE id=1")
		changed, err := compareSwap(ctx, database, target, []any{int64(1)}, "same ", "must not resurrect")
		if err != nil || changed {
			t.Fatal("deleted row was rewritten", err)
		}
	})
	t.Run("cancelled operation is incomplete", func(t *testing.T) {
		cancelled, stop := context.WithCancel(ctx)
		stop()
		report, err := Run(cancelled, database, current, old, Options{BatchSize: 1, MaxRowsPerColumn: 1000})
		if err == nil || report.EncryptedColumnsClean || report.Stage == "complete" {
			t.Fatal("cancelled scan was successful")
		}
	})
	t.Run("projection CAS preserves consumer deletion and exact entity identity", func(t *testing.T) {
		target := targets()[19]
		payload, _ := json.Marshal(map[string]any{"lng": sealed(t, old, "113.5", "location"), "lat": nil})
		exec("INSERT INTO analytics_outbox(entity,entity_id,payload) VALUES('device_location',98767,?)", string(payload))
		id := get("SELECT CAST(id AS CHAR) FROM analytics_outbox WHERE entity='device_location' AND entity_id=98767")
		stale := get("SELECT CAST(payload AS CHAR) FROM analytics_outbox WHERE id=?", id)
		exec("DELETE FROM analytics_outbox WHERE id=?", id)
		if changed, err := compareSwap(ctx, database, target, []any{id}, stale, stale); err != nil || changed {
			t.Fatal("consumer-deleted event was rewritten", err)
		}
		exec("ALTER TABLE analytics_outbox MODIFY entity VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL")
		exec("INSERT INTO analytics_outbox(entity,entity_id,payload) VALUES('DEVICE_LOCATION',98768,?)", string(payload))
		id = get("SELECT CAST(id AS CHAR) FROM analytics_outbox WHERE entity_id=98768")
		stale = get("SELECT CAST(payload AS CHAR) FROM analytics_outbox WHERE id=?", id)
		if changed, err := compareSwap(ctx, database, target, []any{id}, stale, stale); err != nil || changed {
			t.Fatal("out-of-scope entity was rewritten", err)
		}
		options.Apply = false
		if report, err := Run(ctx, database, current, old, options); err != nil || !report.EncryptedColumnsClean {
			t.Fatal("out-of-scope entity was treated as a location event", err)
		}
		if get("SELECT CAST(payload AS CHAR) FROM analytics_outbox WHERE id=?", id) != stale {
			t.Fatal("out-of-scope entity payload changed")
		}
	})
}
