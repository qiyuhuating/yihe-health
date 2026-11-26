package rules

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/securestore"
	"yihe-care/internal/vitalrepo"
)

func TestProtectedLegacyVitalsRuleFallbackMySQL(t *testing.T) {
	database, now := seed(t)
	ctx := context.Background()
	box, err := securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("V", 32))))
	if err != nil {
		t.Fatal(err)
	}
	engine := Engine{DB: database, Box: box}
	owner, device, high := int64(1), int64(1), int64(150)
	for i := range 3 {
		at := now.Add(time.Duration(i) * time.Second)
		func() {
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			id := int64(i + 1)
			if _, err := tx.ExecContext(ctx, "INSERT INTO ratepre(id,old_id,device_id,create_time) VALUES(?,?,?,?)", id, owner, device, at); err != nil {
				t.Fatal(err)
			}
			if err := vitalrepo.StoreProtectedMeasurements(ctx, tx, box, id, &owner, &device, vitalrepo.Measurements{HeartRate: &high}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}()
		if err := engine.Device(ctx, device, at.Add(time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	var alerts, pressure int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_incident WHERE kind='rule_heart_rate'").Scan(&alerts); err != nil || alerts != 1 {
		t.Fatalf("legacy fallback missed distinct protected readings: %d, %v", alerts, err)
	}
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM rule_state WHERE kind='blood_pressure'").Scan(&pressure); err != nil || pressure != 0 {
		t.Fatalf("missing pressure created state: %d, %v", pressure, err)
	}
	missingReader := Engine{DB: database}
	if err := missingReader.Device(ctx, device, now.Add(3*time.Second)); err == nil {
		t.Fatal("protected legacy readings were ignored when the key was missing")
	}
}
