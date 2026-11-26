// Command rekey-images rotates the ciphertext of stable MySQL image names.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"yihe-care/internal/blob"
	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	apply := flag.Bool("apply", false, "switch authenticated images to the configured active key")
	maxImages := flag.Int64("max-images", 1000000, "maximum images per inventory pass")
	timeout := flag.Duration("timeout", 15*time.Minute, "overall operation deadline, at most 24h")
	flag.Parse()
	if flag.NArg() != 0 || *maxImages < 1 || *timeout <= 0 || *timeout > 24*time.Hour {
		return errors.New("provide a positive image limit and a timeout up to 24h without positional arguments")
	}
	box, err := securestore.New(config.Secret("DATA_ENCRYPTION_KEY"))
	if err != nil || box.ActiveKeyID() == "" {
		return errors.New("a valid versioned application keyring is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, config.Secret("MYSQL_DSN"))
	if err != nil {
		return errors.New("cannot open explicitly configured MySQL")
	}
	defer pool.Close()
	report, operationErr := blob.RotateMySQLImages(ctx, pool.DB, box, *maxImages, *apply)
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return errors.New("cannot write aggregate image rotation report")
	}
	switch {
	case operationErr == nil:
		return nil
	case errors.Is(operationErr, blob.ErrIntegrity), errors.Is(operationErr, blob.ErrBusy),
		errors.Is(operationErr, blob.ErrImageRotationChanged), errors.Is(operationErr, blob.ErrImageRotationLimit):
		return operationErr
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		return errors.New("image rotation could not finish; verify the database schema and inspect aggregate progress before retrying")
	}
}
