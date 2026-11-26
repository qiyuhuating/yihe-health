// Package opscli provides one bounded offline lifecycle for simple encryption backfills.
package opscli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/familyaddress"
	"yihe-care/internal/guardiancontact"
	"yihe-care/internal/residentprofile"
	"yihe-care/internal/securestore"
	"yihe-care/internal/socialrepo"
	"yihe-care/internal/userphone"
)

type backfillSpec[R any] struct {
	name, applyHelp, limitHelp, subject string
	requireKeyOnScan                    bool
	run                                 func(context.Context, *sql.DB, *securestore.Box, int, bool) (R, error)
}

func runBackfill[R any](spec backfillSpec[R], args []string, output io.Writer) error {
	flags := flag.NewFlagSet(spec.name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	apply := flags.Bool("apply", false, spec.applyHelp)
	limit := flags.Int("limit", 100, spec.limitHelp)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return nil
		}
		return fmt.Errorf("invalid %s arguments; use --help", spec.subject)
	}
	if flags.NArg() != 0 || *limit < 1 || *limit > 1000 {
		return errors.New("provide a limit between 1 and 1000 without extra arguments")
	}
	var box *securestore.Box
	if *apply || spec.requireKeyOnScan {
		var err error
		box, err = securestore.New(config.Secret("DATA_ENCRYPTION_KEY"))
		if err != nil {
			return fmt.Errorf("valid service encryption key required for %s", spec.subject)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, config.Secret("MYSQL_DSN"))
	if err != nil {
		return errors.New("cannot open explicitly configured MySQL")
	}
	defer pool.Close()
	report, operationErr := spec.run(ctx, pool.DB, box, *limit, *apply)
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return fmt.Errorf("cannot write %s aggregate report", spec.subject)
	}
	return operationErr
}

// RunTarget preserves each original executable's --apply / --limit flags and
// report schema. Long-lived worker service and exceptional migration commands
// intentionally remain separate.
func RunTarget(target string, args []string, output io.Writer) error {
	switch target {
	case "family-addresses":
		return runBackfill(backfillSpec[familyaddress.BackfillReport]{
			name: "protect-family-addresses", applyHelp: "apply with relevant writers stopped and a tested backup",
			limitHelp: "maximum addresses per invocation, 1..1000", subject: "family address backfill",
			requireKeyOnScan: true, run: familyaddress.Backfill,
		}, args, output)
	case "family-phones":
		return runBackfill(backfillSpec[userphone.FamilyBackfillReport]{
			name: "protect-family-phones", applyHelp: "apply with account writers stopped and a tested backup",
			limitHelp: "maximum rows per invocation, 1..1000", subject: "family backfill",
			run: userphone.BackfillFamily,
		}, args, output)
	case "guardian-contacts":
		return runBackfill(backfillSpec[guardiancontact.BackfillReport]{
			name: "protect-guardian-contacts", applyHelp: "apply with relevant writers stopped and a tested backup",
			limitHelp: "maximum contacts per invocation, 1..1000", subject: "guardian contact backfill",
			requireKeyOnScan: true, run: guardiancontact.Backfill,
		}, args, output)
	case "resident-birthdates":
		return runBackfill(backfillSpec[residentprofile.BackfillReport]{
			name: "protect-resident-birthdates", applyHelp: "apply with resident writers stopped and a tested backup",
			limitHelp: "maximum birthdates per invocation, 1..1000", subject: "resident birthdate backfill",
			requireKeyOnScan: true, run: residentprofile.BackfillBirthdates,
		}, args, output)
	case "resident-profiles":
		return runBackfill(backfillSpec[residentprofile.BackfillReport]{
			name: "protect-resident-profiles", applyHelp: "apply with relevant writers stopped and a tested backup",
			limitHelp: "maximum profiles per invocation, 1..1000", subject: "resident backfill",
			requireKeyOnScan: true, run: residentprofile.Backfill,
		}, args, output)
	case "social-phones":
		return runBackfill(backfillSpec[socialrepo.PhoneBackfillReport]{
			name: "protect-social-phones", applyHelp: "apply with social writers stopped and a tested backup",
			limitHelp: "maximum rows per invocation, 1..1000", subject: "social backfill",
			requireKeyOnScan: true, run: socialrepo.BackfillPhones,
		}, args, output)
	default:
		return fmt.Errorf("unknown protect target %q", target)
	}
}
