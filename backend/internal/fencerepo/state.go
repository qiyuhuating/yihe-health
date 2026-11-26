package fencerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"yihe-care/internal/fence"
)

// LoadState locks the persisted state so observations for a fence serialize.
// A different revision starts with a fresh state after configuration changes.
func LoadState(ctx context.Context, tx *sql.Tx, fenceID, revision int64) (fence.State, error) {
	var storedRevision int64
	var payload []byte
	err := tx.QueryRowContext(ctx, "SELECT revision,payload FROM fence_state WHERE fence_id=? FOR UPDATE", fenceID).Scan(&storedRevision, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return fence.State{}, nil
	}
	if err != nil {
		return fence.State{}, err
	}
	if storedRevision != revision {
		return fence.State{}, nil
	}
	var state fence.State
	if err := json.Unmarshal(payload, &state); err != nil {
		return fence.State{}, err
	}
	return state, nil
}

// SaveState writes the observation in the transaction that owns the fence lock.
func SaveState(ctx context.Context, tx *sql.Tx, fenceID, revision int64, state fence.State) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO fence_state(fence_id,revision,payload) VALUES(?,?,?)
		 AS new ON DUPLICATE KEY UPDATE revision=new.revision,payload=new.payload`,
		fenceID, revision, json.RawMessage(payload))
	return err
}
