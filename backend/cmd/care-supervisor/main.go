// care-supervisor is a local, explicitly audited first-supervisor recovery tool.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"time"

	"yihe-care/internal/careworkflow"
	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/securityaudit"
)

func main() {
	if err := run(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("care-supervisor", flag.ContinueOnError)
	adminID := flags.Int64("admin-id", 0, "existing active administrator user_id to recover")
	reason := flags.String("reason", "", "required incident/ticket reason; missing reasons are audited and denied")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: care-supervisor --admin-id=<ID> --reason=<ticket reason>")
	}
	operator, err := user.Current()
	if err != nil {
		return fmt.Errorf("read OS identity: %w", err)
	}
	host, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := db.OpenMySQL(ctx, config.Secret("MYSQL_DSN"))
	if err != nil {
		return errors.New("MySQL connection failed: check MYSQL_DSN or MYSQL_DSN_FILE")
	}
	defer store.Close()
	result, err := careworkflow.RecoverSupervisor(ctx, store, *adminID, securityaudit.Actor{
		User: operator.Username, UID: operator.Uid, Host: host,
	}, *reason)
	if errors.Is(err, careworkflow.ErrRecoveryDenied) {
		return fmt.Errorf("recovery denied (%s); audit=%s; high-priority alert queued", result.Decision, result.AuditID)
	}
	if err != nil {
		return err
	}
	fmt.Printf("first supervisor assigned to administrator %d; audit=%s; high-priority alert queued\n", *adminID, result.AuditID)
	return nil
}
