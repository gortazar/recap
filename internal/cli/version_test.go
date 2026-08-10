package cli

import (
	"strings"
	"testing"
)

// The version line is how "did the installer install what it claimed?" is answered, so its
// shape is pinned here and the release smoke test greps for the version in it.
func TestVersionLine(t *testing.T) {
	defer restoreBuildInfo(Version, Commit, BuildDate)()
	Version, Commit, BuildDate = "0.2", "abc1234", "2026-08-10T10:00:00Z"

	code, stdout, stderr := run(t, testEnv(t), "--version")
	if code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("--version wrote to stderr: %q", stderr)
	}
	want := "recap 0.2 (commit abc1234, built 2026-08-10T10:00:00Z)\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// A local `go build ./cmd/recap` stamps nothing, and must still say something honest.
func TestUnstampedBuildSaysDev(t *testing.T) {
	defer restoreBuildInfo(Version, Commit, BuildDate)()
	Version, Commit, BuildDate = "dev", "unknown", "unknown"

	_, stdout, _ := run(t, testEnv(t), "--version")
	if !strings.HasPrefix(stdout, "recap dev ") {
		t.Errorf("stdout = %q, want it to start with %q", stdout, "recap dev ")
	}
}

// The defaults compiled into the binary, so an unstamped build never claims a version.
func TestDefaultsAreNotAVersionNumber(t *testing.T) {
	if defaultVersion != "dev" {
		t.Errorf("default version = %q, want %q", defaultVersion, "dev")
	}
}

func restoreBuildInfo(version, commit, date string) func() {
	return func() { Version, Commit, BuildDate = version, commit, date }
}
