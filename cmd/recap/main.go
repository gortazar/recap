// Command recap prints what each local coding agent session was doing.
package main

import (
	"os"

	"github.com/gortazar/recap/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
