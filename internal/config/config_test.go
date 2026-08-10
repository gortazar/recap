package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadsEverySetting(t *testing.T) {
	cfg, err := Load(write(t, `
# what counts as recent
since = "12h"
roots = ["/home/user/git", "/home/user/work"]
ignore = ["/home/user/git/scratch"]
icons = false
smart_model = "claude-haiku-4-5-20251001"

[icon]
running = "▶"
waiting = "?"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Since, "12h"; got != want {
		t.Errorf("Since = %q, want %q", got, want)
	}
	if got, want := strings.Join(cfg.Roots, ","), "/home/user/git,/home/user/work"; got != want {
		t.Errorf("Roots = %q, want %q", got, want)
	}
	if got, want := strings.Join(cfg.Ignore, ","), "/home/user/git/scratch"; got != want {
		t.Errorf("Ignore = %q, want %q", got, want)
	}
	if got, want := cfg.SmartModel, "claude-haiku-4-5-20251001"; got != want {
		t.Errorf("SmartModel = %q, want %q", got, want)
	}
	if cfg.Icons == nil || *cfg.Icons {
		t.Errorf("Icons = %v, want false", cfg.Icons)
	}
	if got, want := cfg.Icon["running"], "▶"; got != want {
		t.Errorf("Icon[running] = %q, want %q", got, want)
	}
}

// A setting left out means "no opinion", which is different from "off".
func TestUnsetSettingsAreDistinguishableFromFalse(t *testing.T) {
	cfg, err := Load(write(t, `since = "1h"`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Icons != nil {
		t.Errorf("Icons = %v, want nil when the file does not mention it", *cfg.Icons)
	}
}

func TestTildeInPathsIsExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	cfg, err := Load(write(t, `roots = ["~/git"]`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Roots[0], filepath.Join(home, "git"); got != want {
		t.Errorf("Roots[0] = %q, want %q", got, want)
	}
}

// A setting that does not take effect and says nothing is worse than no config file.
func TestMistakesAreReportedWithTheirLine(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown setting", "sicne = \"1h\"", "unknown setting"},
		{"unknown section", "[colours]\nrunning = \"x\"", "unknown section"},
		{"not a key/value", "just some words", "expected key = value"},
		{"unquoted string", "since = 1h", "quoted string"},
		{"list of the wrong thing", "roots = \"/home/user\"", "list of quoted strings"},
		{"icons is not a bool", "icons = \"yes\"", "true or false"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(write(t, c.body))
			if err == nil {
				t.Fatalf("no error for %q", c.body)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
			if !strings.Contains(err.Error(), "config.toml:") {
				t.Errorf("error = %q, want it to name the file and line", err)
			}
		})
	}
}

func TestAMissingConfigFileIsFine(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nothing-here.toml"))
	if err != nil {
		t.Fatalf("Load of a missing file: %v", err)
	}
	if cfg.Since != "" || cfg.Roots != nil {
		t.Errorf("missing file produced settings: %+v", cfg)
	}
}

func TestCommentsAndBlankLinesAreIgnored(t *testing.T) {
	cfg, err := Load(write(t, "\n# a comment\n\nsince = \"3h\"  # trailing comment\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Since, "3h"; got != want {
		t.Errorf("Since = %q, want %q", got, want)
	}
}
