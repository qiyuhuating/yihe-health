package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"yihe-care/internal/auth"
	"yihe-care/internal/blob"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func TestMySQLCoordinatedBackupRestore(t *testing.T) {
	source := testDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedDevice(t, source)
	seedCareAdminDuty(t, source, 1, "supervisor")
	mustExec(t, source, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'synthetic','13800000001')")
	mustExec(t, source, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	mustExec(t, source, "INSERT INTO ratepre(id,device_id,old_id,hr,create_time) VALUES(1,1,1,72,CURRENT_TIMESTAMP(6))")
	key := func(v byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{v}, 32)) }
	ring := func(active string, keys map[string]string) *securestore.Box {
		t.Helper()
		box, err := securestore.NewKeyring(active, keys, key(3))
		if err != nil {
			t.Fatal(err)
		}
		return box
	}
	oldKey := ring("A", map[string]string{"A": key(1)})
	dual := ring("B", map[string]string{"A": key(1), "B": key(2)})
	active := ring("B", map[string]string{"B": key(2)})
	handler := testHandler(t, source)
	handler.Box = dual
	handler.Sessions.UseDatabase(source.DB, dual)
	identity := session.Principal{Role: auth.RoleAdmin, ID: 1}
	originalSession := handler.Sessions.Create(identity)
	if originalSession == nil {
		t.Fatal("could not persist the synthetic session")
	}
	payload := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "restore-same-request", "bindingVersion": 1}
	out := api(t, handler.deviceSos, payload)
	if out["returnCode"] != "1111" || out["notificationState"] != "pending" {
		t.Fatal("SOS did not create a durable pending notification")
	}
	api(t, withHealthPrincipal(handler, identity, handler.careIncidents), map[string]any{})
	manager := func(pool *dbstore.Database, box *securestore.Box) *blob.Manager {
		t.Helper()
		store, err := blob.NewMySQL(pool.DB)
		if err != nil {
			t.Fatal(err)
		}
		return &blob.Manager{DB: pool, Store: &blob.Encrypted{Store: store, Box: box}}
	}
	content := []byte("synthetic coordinated backup payload")
	const name = "coordinated.png"
	if err := manager(source, oldKey).Put(ctx, name, content); err != nil {
		t.Fatal(err)
	}
	mustExec(t, source, "UPDATE family SET family_tou_pic=? WHERE family_id=1", name)
	if report, err := blob.RotateMySQLImages(ctx, source.DB, dual, 1, true); err != nil || report.Rewritten != 1 || !report.Complete {
		t.Fatal("source rotation failed", err)
	}
	history := mysqlRecoveryDigests(t, ctx, source)
	cfg, err := mysql.ParseDSN(os.Getenv("TEST_MYSQL_DSN"))
	if err != nil || cfg.Net != "tcp" {
		t.Fatal("restore drill needs a dedicated TCP MySQL test DSN")
	}
	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	client := func(tool string, args []string, input []byte) []byte {
		t.Helper()
		program := tool
		invocation := []string{"--no-defaults", "--protocol=TCP", "--host=" + host, "--port=" + port, "--user=" + cfg.User}
		if container := os.Getenv("TEST_MYSQL_CONTAINER_ID"); container != "" {
			if !regexp.MustCompile("^[0-9a-f]{12,64}$").MatchString(container) {
				t.Fatal("expected an isolated MySQL fixture container ID")
			}
			program = "docker"
			invocation = []string{"exec", "-i", "-e", "MYSQL_PWD", container, tool,
				"--no-defaults", "--protocol=TCP", "--host=127.0.0.1", "--port=3306", "--user=" + cfg.User}
		}
		binary, err := exec.LookPath(program)
		if err != nil {
			t.Fatal("restore drill requires MySQL client tools or the isolated fixture container")
		}
		cli := exec.CommandContext(ctx, binary, append(invocation, args...)...)
		cli.Env = append(os.Environ(), "MYSQL_PWD="+cfg.Passwd)
		cli.Stdin = bytes.NewReader(input)
		output, err := cli.Output()
		if err != nil {
			t.Fatalf("%s failed with SQL output withheld: %v", tool, err)
		}
		return output
	}
	var database, serverID string
	if err := source.QueryRowContext(ctx, "SELECT DATABASE(),@@server_uuid").Scan(&database, &serverID); err != nil {
		t.Fatal(err)
	}
	if cliID := client("mysql", []string{"--batch", "--skip-column-names", "-e", "SELECT @@server_uuid"}, nil); strings.TrimSpace(string(cliID)) != serverID {
		t.Fatal("MySQL restore tools are not connected to the fixture server")
	}
	archive := client("mysqldump", []string{"--single-transaction", "--quick", "--routines", "--triggers", "--hex-blob",
		"--no-tablespaces", "--set-gtid-purged=OFF", "--column-statistics=0", database}, nil)
	if len(archive) == 0 {
		t.Fatal("backup did not capture SQL")
	}
	mustExec(t, source, "DELETE FROM image_payload_generation WHERE name=?", name)
	if _, err := manager(source, active).Get(ctx, name); !errors.Is(err, blob.ErrIntegrity) {
		t.Fatal("deliberate source loss did not remove the current ciphertext", err)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	target := "yihe_restore_" + uuid.NewString()[:8]
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+target+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE `" + target + "`"); err != nil {
			t.Error("could not clean up the disposable restored database")
		}
	}()
	client("mysql", []string{target}, archive)
	cfg.DBName = target
	restored, err := dbstore.OpenMySQL(ctx, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := dbstore.MigrateMySQL(ctx, restored); err != nil {
		t.Fatal("schema and migration validation failed on restored database", err)
	}
	if got := mysqlRecoveryDigests(t, ctx, restored); !reflect.DeepEqual(got, history) {
		t.Fatal("restored care, health, audit, queue, image, session, or migration data differed")
	}
	var parentID, recipientID int64
	if err := restored.QueryRowContext(ctx, `SELECT i.id,o.id FROM care_incident i
 JOIN care_notification_outbox o ON o.incident_id=i.id
 JOIN care_incident_event ev ON ev.incident_id=i.id AND ev.action='received'
 JOIN care_access_audit a ON a.admin_id=1 AND a.action='incident_list'
 JOIN oldperson p ON p.old_id=i.old_id
 JOIN device d ON d.device_id=i.device_id AND d.old_id=p.old_id
 JOIN family f ON f.family_id=p.family_id WHERE o.status='pending' AND f.family_tou_pic=?`, name).
		Scan(&parentID, &recipientID); err != nil || parentID < 1 || recipientID < 1 {
		t.Fatal("restored incident, event, outbox, audit, resident, device and image did not join", err)
	}
	if _, err := restored.ExecContext(ctx, "UPDATE care_notification_outbox SET incident_id=987654321 WHERE id=?", recipientID); err != nil {
		var mysqlError *mysql.MySQLError
		if !errors.As(err, &mysqlError) || mysqlError.Number != 1452 {
			t.Fatal("restored outbox foreign key check returned a different error", err)
		}
	} else {
		t.Fatal("restored outbox foreign key accepted an orphan row")
	}
	reopened := testHandler(t, restored)
	reopened.Box = active
	reopened.Sessions.UseDatabase(restored.DB, active)
	if entry := reopened.Sessions.Get(originalSession.ID); entry == nil || entry.Principal.ID != identity.ID {
		t.Fatal("restored durable session could not authenticate with the matching key")
	}
	if data, err := manager(restored, active).Get(ctx, name); err != nil || !bytes.Equal(data, content) {
		t.Fatal("restored image could not authenticate with active key", err)
	}
	if report, err := blob.RotateMySQLImages(ctx, restored.DB, active, 1, false); err != nil || report.Before.MissingReferences != 0 ||
		report.Before.DetachedCiphertext != 0 || report.Before.Pending != 0 || report.Before.Checked != 1 {
		t.Fatal("restored image inventory had missing references, extra versions or older envelopes", report, err)
	}
	duplicate := api(t, reopened.deviceSos, payload)
	if duplicate["incidentId"] != out["incidentId"] {
		t.Fatal("retry of restored SOS returned a different incident")
	}
	after := mysqlRecoveryDigests(t, ctx, restored)
	for _, group := range []string{"incidents", "events", "queue", "audit"} {
		if after[group] != history[group] {
			t.Fatalf("retry modified restored %s", group)
		}
	}
	t.Log("Coordinated SQL restore authenticated active images and session, preserved care records and refused orphan/repeated work")
}

func mysqlRecoveryDigests(t *testing.T, ctx context.Context, pool *dbstore.Database) map[string][32]byte {
	t.Helper()
	queries := map[string]string{
		"migrations": "SELECT * FROM schema_migrations ORDER BY name",
		"health":     "SELECT * FROM ratepre ORDER BY id",
		"incidents":  "SELECT * FROM care_incident ORDER BY id",
		"events":     "SELECT * FROM care_incident_event ORDER BY id",
		"audit":      "SELECT * FROM care_access_audit ORDER BY id",
		"queue":      "SELECT * FROM care_notification_outbox ORDER BY id",
		"sessions":   "SELECT * FROM web_session ORDER BY id_hash",
		"catalog":    "SELECT * FROM image_blob ORDER BY name",
		"images":     "SELECT * FROM image_payload ORDER BY name",
		"versions":   "SELECT * FROM image_payload_generation ORDER BY name,generation",
	}
	result := make(map[string][32]byte, len(queries))
	for group, query := range queries {
		rows, err := pool.QueryContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		hash := sha256.New()
		enc := json.NewEncoder(hash)
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				break
			}
			if err = enc.Encode(values); err != nil {
				break
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		result[group] = [32]byte(hash.Sum(nil))
	}
	return result
}
