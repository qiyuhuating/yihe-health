package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"yihe-care/internal/observability"
	"yihe-care/internal/securestore"
)

const operationTimeout = 3 * time.Second

type databaseStore struct {
	db  *sql.DB
	box *securestore.Box
}

func idHash(id string) string { sum := sha256.Sum256([]byte(id)); return hex.EncodeToString(sum[:]) }

func (d *databaseStore) create(ctx context.Context, sess *Session) bool {
	b, err := json.Marshal(sess)
	if err != nil {
		return false
	}
	sealed, err := d.box.SealBytes(b, "session")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err = d.db.ExecContext(ctx, "INSERT INTO web_session(id_hash,role,actor_id,payload,expires_at) VALUES(?,?,?,?,?)", idHash(sess.ID), sess.Principal.Role, sess.Principal.ID, sealed, sess.ExpiresAt.UTC())
	return err == nil
}

func (d *databaseStore) get(ctx context.Context, id string) *Session {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	var sealed, role string
	var actorID int64
	var generation sql.NullString
	queryStart := time.Now()
	queryErr := d.db.QueryRowContext(ctx, "SELECT s.payload,s.role,s.actor_id,f.generation FROM web_session s LEFT JOIN auth_factor f ON f.role=s.role AND f.actor_id=s.actor_id AND f.enabled=true WHERE s.id_hash=? AND s.expires_at>UTC_TIMESTAMP(6)", idHash(id)).Scan(&sealed, &role, &actorID, &generation)
	observability.Default.Add("yihe_session_lookup_seconds_total", time.Since(queryStart).Seconds())
	observability.Default.Add("yihe_session_lookup_total", 1)
	if queryErr != nil {
		return nil
	}
	decodeStart := time.Now()
	plain, err := d.box.OpenBytes(sealed, "session")
	var sess Session
	decodeErr := err
	if decodeErr == nil {
		decodeErr = json.Unmarshal(plain, &sess)
	}
	observability.Default.Add("yihe_session_decode_seconds_total", time.Since(decodeStart).Seconds())
	if decodeErr != nil || sess.ID != id || time.Now().After(sess.ExpiresAt) || sess.Principal.Role != role || sess.Principal.ID != actorID || ctx.Err() != nil {
		return nil
	}
	if role != "" && generation.String != sess.Principal.FactorGeneration {
		return nil
	}
	return &sess
}

func (d *databaseStore) destroy(ctx context.Context, id string) bool {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err := d.db.ExecContext(ctx, "DELETE FROM web_session WHERE id_hash=?", idHash(id))
	return err == nil
}

func (d *databaseStore) revoke(ctx context.Context, role string, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err := d.db.ExecContext(ctx, "DELETE FROM web_session WHERE role=? AND actor_id=?", role, id)
	return err
}

func (d *databaseStore) deleteExpired(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err := d.db.ExecContext(ctx, "DELETE FROM web_session WHERE expires_at<=UTC_TIMESTAMP(6)")
	return err
}
