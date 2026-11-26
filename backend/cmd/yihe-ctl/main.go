// Command yihe-ctl consolidates the simple offline encryption backfills.
// Existing protect-* commands remain available for operational scripts.
package main

import (
	"fmt"
	"io"
	"os"

	"yihe-care/internal/opscli"
)

func main() {
	if err := dispatch(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(args []string, out io.Writer) error {
	if len(args) < 2 || args[0] != "protect" {
		return fmt.Errorf("usage: yihe-ctl protect <family-addresses|family-phones|guardian-contacts|resident-birthdates|resident-profiles|social-phones> [--apply] [--limit N]")
	}
	return opscli.RunTarget(args[1], args[2:], out)
}
