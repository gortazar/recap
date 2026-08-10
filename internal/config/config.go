// Package config reads recap's optional config file.
//
// The file is TOML, but only the subset a settings file needs: comments, `key = value` with
// strings, booleans and integers, arrays of strings, and one level of `[section]`. Anything
// else is reported as an error with its line number rather than quietly ignored — a setting
// that does not take effect and says nothing is worse than no config file at all.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the file's contents. Every field is optional; a field left out means "no opinion"
// and the built-in default applies. Command-line flags override whatever is here.
type Config struct {
	// Since is the default time window, as a duration string ("24h", "2d").
	Since string
	// Roots limits which projects are reported, e.g. ["~/git"]. Empty means your home
	// directory.
	Roots []string
	// Ignore hides projects under these directories even when they are inside a root.
	Ignore []string
	// Icons turns the status emoji on or off. Nil means "not set".
	Icons *bool
	// Icon overrides the glyph for individual statuses, keyed by status word
	// (running, waiting, idle, interrupted, finished, unclear).
	Icon map[string]string
	// SmartModel is the model --smart asks to rewrite the sentences.
	SmartModel string
}

// DefaultPath is where recap looks for the file.
func DefaultPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "recap", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "recap", "config.toml")
}

// Load reads the config file. A missing file is not an error: the config file is optional and
// recap works with none.
func Load(path string) (Config, error) {
	var cfg Config
	if path == "" {
		return cfg, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	return parse(string(body), path)
}

func parse(body, path string) (Config, error) {
	cfg := Config{}
	section := ""
	for i, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(stripComment(raw))
		if line == "" {
			continue
		}
		where := fmt.Sprintf("%s:%d", path, i+1)

		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return cfg, fmt.Errorf("%s: unterminated section header", where)
			}
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section != "icon" {
				return cfg, fmt.Errorf("%s: unknown section [%s]", where, section)
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return cfg, fmt.Errorf("%s: expected key = value", where)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if section == "icon" {
			glyph, err := parseString(value)
			if err != nil {
				return cfg, fmt.Errorf("%s: %s: %v", where, key, err)
			}
			if cfg.Icon == nil {
				cfg.Icon = map[string]string{}
			}
			cfg.Icon[key] = glyph
			continue
		}

		if err := assign(&cfg, key, value); err != nil {
			return cfg, fmt.Errorf("%s: %v", where, err)
		}
	}
	return cfg, nil
}

func assign(cfg *Config, key, value string) error {
	switch key {
	case "since":
		s, err := parseString(value)
		if err != nil {
			return fmt.Errorf("since: %v", err)
		}
		cfg.Since = s
	case "roots":
		list, err := parseStringList(value)
		if err != nil {
			return fmt.Errorf("roots: %v", err)
		}
		cfg.Roots = expandAll(list)
	case "ignore":
		list, err := parseStringList(value)
		if err != nil {
			return fmt.Errorf("ignore: %v", err)
		}
		cfg.Ignore = expandAll(list)
	case "smart_model":
		s, err := parseString(value)
		if err != nil {
			return fmt.Errorf("smart_model: %v", err)
		}
		cfg.SmartModel = s
	case "icons":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("icons: expected true or false, got %s", value)
		}
		cfg.Icons = &b
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	return nil
}

// stripComment removes a trailing # comment, leaving #s inside quotes alone.
func stripComment(line string) string {
	inQuotes := false
	for i, r := range line {
		switch r {
		case '"':
			inQuotes = !inQuotes
		case '#':
			if !inQuotes {
				return line[:i]
			}
		}
	}
	return line
}

func parseString(value string) (string, error) {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", fmt.Errorf("expected a quoted string, got %s", value)
	}
	return value[1 : len(value)-1], nil
}

func parseStringList(value string) ([]string, error) {
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil, fmt.Errorf("expected a list of quoted strings, got %s", value)
	}
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil, nil
	}
	var out []string
	for _, item := range strings.Split(inner, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		s, err := parseString(item)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// expandAll turns a leading ~ into the home directory, which is what anyone writing a path
// in a config file expects.
func expandAll(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, expand(p))
	}
	return out
}

func expand(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
}
