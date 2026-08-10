package cli

import (
	"fmt"
	"io"
)

// Defaults for a build that was not stamped — a plain `go build ./cmd/recap`. They must not
// look like a version: a binary claiming "0.2" when nobody said so is how you end up
// debugging the wrong code.
const (
	defaultVersion = "dev"
	defaultCommit  = "unknown"
	defaultDate    = "unknown"
)

// Build information, set at link time:
//
//	go build -ldflags "-X github.com/gortazar/recap/internal/cli.Version=0.2 ..."
//
// tools/release-build.sh and flake.nix both stamp these; see --version.
var (
	Version   = defaultVersion
	Commit    = defaultCommit
	BuildDate = defaultDate
)

// printVersion writes the one line `recap --version` prints. The installer's smoke test
// greps the version out of it, so the shape is fixed by a test.
func printVersion(w io.Writer) {
	fmt.Fprintf(w, "recap %s (commit %s, built %s)\n", Version, Commit, BuildDate)
}
