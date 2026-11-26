package securityworkflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/authrepo"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

type FactorAction string

const (
	Setup   FactorAction = "setup"
	Confirm FactorAction = "confirm"
	Disable FactorAction = "disable"
)

type FactorFailure string

const (
	WrongPassword  FactorFailure = "wrong_password"
	AlreadyEnabled FactorFailure = "already_enabled"
	InvalidState   FactorFailure = "invalid_state"
	InvalidOTP     FactorFailure = "invalid_otp"
	InvalidAction  FactorFailure = "invalid_action"
)

type FactorInput struct {
	Role             string
	ActorID          int64
	Action           FactorAction
	Password         string
	OTP              string
	DatabaseSessions bool
}

func ChangeFactor(ctx context.Context, store *db.Database, box *securestore.Box, input FactorInput) (map[string]any, FactorFailure, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	password, passwordErr := authrepo.LockPassword(ctx, tx, input.Role, input.ActorID)
	if passwordErr != nil || !auth.Matches(input.Password, password) {
		return nil, WrongPassword, nil
	}
	scope := "totp:" + input.Role + ":" + strconv.FormatInt(input.ActorID, 10)
	factor, factorErr := authrepo.LockFactor(ctx, tx, input.Role, input.ActorID)
	if factorErr != nil && !errors.Is(factorErr, sql.ErrNoRows) {
		return nil, "", factorErr
	}
	result := map[string]any{}
	now := time.Now()
	switch input.Action {
	case Setup:
		if factor.Enabled {
			return nil, AlreadyEnabled, nil
		}
		result, err = authrepo.CreateFactorSetup(ctx, tx, box, input.Role, input.ActorID, scope, now)
	case Confirm, Disable:
		if factorErr != nil || (input.Action == Confirm && (factor.Enabled || now.After(factor.PendingUntil))) || (input.Action == Disable && !factor.Enabled) {
			return nil, InvalidState, nil
		}
		secret, openErr := box.Open(factor.Sealed, scope)
		if openErr != nil {
			return nil, "", openErr
		}
		counter, valid := securestore.VerifyTOTP(secret, input.OTP, now, factor.LastCounter)
		if !valid {
			return nil, InvalidOTP, nil
		}
		if input.Action == Disable {
			err = authrepo.DeleteFactor(ctx, tx, input.Role, input.ActorID)
		} else {
			var codes []string
			var hashes []string
			codes, hashes, err = authrepo.GenerateRecoveryCodes()
			if err != nil {
				return nil, "", err
			}
			var encoded []byte
			encoded, err = json.Marshal(hashes)
			if err != nil {
				return nil, "", err
			}
			err = authrepo.EnableFactor(ctx, tx, input.Role, input.ActorID, counter, encoded)
			result["recoveryCodes"] = codes
		}
		result["reloginRequired"] = true
	default:
		return nil, InvalidAction, nil
	}
	if err != nil {
		return nil, "", err
	}
	if input.Action != Setup && input.DatabaseSessions {
		if err := authrepo.RevokeDatabaseSessions(ctx, tx, input.Role, input.ActorID); err != nil {
			return nil, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	return result, "", nil
}
