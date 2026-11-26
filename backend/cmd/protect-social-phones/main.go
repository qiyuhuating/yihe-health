// Command protect-social-phones runs the bounded offline encryption backfill.
package main

import (
	"fmt"
	"io"
	"os"

	"yihe-care/internal/opscli"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	return opscli.RunTarget("social-phones", args, output)
}
