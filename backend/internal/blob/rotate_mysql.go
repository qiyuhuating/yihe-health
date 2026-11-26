package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

var (
	ErrImageRotationLimit   = errors.New("image inventory exceeds the row limit")
	ErrImageRotationChanged = errors.New("image storage changed during rotation; rescan and retry")
)

type ImageRotationReport struct {
	Mode                 string         `json:"mode"`
	ActiveKey            string         `json:"active_key_id"`
	Before               ImageInventory `json:"before"`
	After                ImageInventory `json:"after"`
	Rewritten            int64          `json:"rewritten"`
	Complete             bool           `json:"complete"`
	RetirementAuthorized bool           `json:"retirement_authorized"`
}

type ImageInventory struct {
	Checked            int64 `json:"checked"`
	Pending            int64 `json:"pending"`
	Uncatalogued       int64 `json:"uncatalogued"`
	Invalid            int64 `json:"invalid"`
	DetachedCiphertext int64 `json:"detached_ciphertext"`
	MissingReferences  int64 `json:"missing_references"`
}

type imageSnapshot struct {
	generation, ciphertext []byte
	digest, state          *string
	size                   *int64
}

func readImageSnapshot(ctx context.Context, reader payloadReader, name string) (imageSnapshot, error) {
	var snapshot imageSnapshot
	err := reader.QueryRowContext(ctx, "SELECT p.active_generation,"+boundedActivePayload+",c.content_sha256,c.state,c.size_bytes "+
		"FROM image_payload p"+activeGenerationJoin+" LEFT JOIN image_blob c ON c.name=p.name WHERE p.name=?", MaxStoredBytes, name).
		Scan(&snapshot.generation, &snapshot.ciphertext, &snapshot.digest, &snapshot.state, &snapshot.size)
	return snapshot, err
}

func (snapshot imageSnapshot) authenticate(box *securestore.Box, name string) ([]byte, error) {
	if !ValidKey(name) || len(snapshot.ciphertext) == 0 {
		return nil, ErrIntegrity
	}
	data, err := (&Encrypted{Box: box}).decode(name, snapshot.ciphertext)
	if err != nil {
		return nil, err
	}
	if snapshot.state != nil {
		if *snapshot.state != "ready" {
			return nil, ErrImageRotationChanged
		}
		if snapshot.size == nil || snapshot.digest == nil || int64(len(data)) != *snapshot.size || Digest(data) != *snapshot.digest {
			return nil, ErrIntegrity
		}
	}
	return data, nil
}

// RotateMySQLImages first authenticates the entire bounded inventory. Apply
// processes each logical image in its own transaction and rescans afterward.
// Callers must deploy the same active writer key everywhere before apply.
func RotateMySQLImages(ctx context.Context, database *sql.DB, box *securestore.Box, maxRows int64, apply bool) (ImageRotationReport, error) {
	report := ImageRotationReport{Mode: "scan", ActiveKey: box.ActiveKeyID()}
	if apply {
		report.Mode = "apply"
	}
	if database == nil || report.ActiveKey == "" || maxRows < 1 {
		return report, errors.New("image rotation requires MySQL, a versioned keyring and a positive row limit")
	}
	activePrefix := securestore.Prefix + "v2:" + report.ActiveKey + ":"
	store := &MySQL{db: database}
	scan := func(inventory *ImageInventory, replace bool) error {
		return store.Walk(ctx, func(entry Entry) error {
			if inventory.Checked == maxRows {
				return ErrImageRotationLimit
			}
			name := entry.Key
			snapshot, err := readImageSnapshot(ctx, database, name)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrImageRotationChanged
			}
			if err != nil {
				return err
			}
			inventory.Checked++
			if snapshot.state == nil {
				inventory.Uncatalogued++
			}
			plain, err := snapshot.authenticate(box, name)
			if err != nil {
				inventory.Invalid++
				return err
			}
			if bytes.HasPrefix(snapshot.ciphertext, []byte(activePrefix)) {
				return nil
			}
			inventory.Pending++
			if !replace {
				return nil
			}
			sealed, err := box.Seal(string(plain), "image:"+name)
			if err != nil {
				return err
			}
			if len(sealed) > MaxStoredBytes {
				return ErrIntegrity
			}
			if err := replaceImageGeneration(ctx, database, name, snapshot, []byte(sealed)); err != nil {
				return err
			}
			report.Rewritten++
			return nil
		})
	}
	if err := verifyImageInventory(ctx, database, &report.Before); err != nil {
		return report, err
	}
	if err := scan(&report.Before, false); err != nil {
		return report, err
	}
	if !apply {
		report.Complete = true
		return report, nil
	}
	if report.Before.Pending != 0 {
		if err := scan(new(ImageInventory), true); err != nil {
			return report, err
		}
	}
	if err := scan(&report.After, false); err != nil {
		return report, err
	}
	if err := verifyImageInventory(ctx, database, &report.After); err != nil {
		return report, err
	}
	report.Complete = report.After.Pending == 0
	if !report.Complete {
		return report, ErrImageRotationChanged
	}
	return report, nil
}

func verifyImageInventory(ctx context.Context, database *sql.DB, inventory *ImageInventory) error {
	var err error
	inventory.MissingReferences, err = MissingMySQLReferences(ctx, database)
	if err != nil {
		return err
	}
	err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_payload_generation g
 JOIN image_payload p ON p.name=g.name WHERE NOT(p.active_generation <=> g.generation)`).Scan(&inventory.DetachedCiphertext)
	if err != nil {
		return err
	}
	if inventory.DetachedCiphertext != 0 || inventory.MissingReferences != 0 {
		return ErrIntegrity
	}
	return nil
}

func replaceImageGeneration(ctx context.Context, database *sql.DB, name string, expected imageSnapshot, sealed []byte) error {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var backupActive bool
	if err = tx.QueryRowContext(ctx, "SELECT backup_until>CURRENT_TIMESTAMP(6) FROM image_gc_guard WHERE id=1 FOR UPDATE").Scan(&backupActive); err != nil {
		return err
	}
	if backupActive {
		return ErrBusy
	}
	var present string
	// Use the same guard/catalog/payload lock order as MySQL GC and imports.
	err = tx.QueryRowContext(ctx, "SELECT name FROM image_blob WHERE name=? FOR UPDATE", name).Scan(&present)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = tx.QueryRowContext(ctx, "SELECT name FROM image_payload WHERE name=? FOR UPDATE", name).Scan(&present); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrImageRotationChanged
		}
		return err
	}
	snapshot, err := readImageSnapshot(ctx, tx, name)
	if err != nil {
		return err
	}
	if !bytes.Equal(snapshot.generation, expected.generation) || !bytes.Equal(snapshot.ciphertext, expected.ciphertext) ||
		!sameCatalog(snapshot, expected) {
		return ErrImageRotationChanged
	}
	var generation [16]byte
	if _, err := rand.Read(generation[:]); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO image_payload_generation(name,generation,payload) VALUES(?,?,?)", name, generation[:], sealed); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE image_payload SET active_generation=?,payload=NULL WHERE name=?", generation[:], name); err != nil {
		return err
	}
	if snapshot.generation != nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM image_payload_generation WHERE name=? AND generation=?", name, snapshot.generation); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func sameCatalog(current, expected imageSnapshot) bool {
	if current.state == nil || expected.state == nil {
		return current.state == nil && expected.state == nil
	}
	return *current.state == *expected.state && current.digest != nil && expected.digest != nil &&
		*current.digest == *expected.digest && current.size != nil && expected.size != nil && *current.size == *expected.size
}
