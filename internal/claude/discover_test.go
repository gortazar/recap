package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gortazar/recap/internal/session"
)

// projectsTree lays out a ~/.claude/projects-shaped directory from the committed fixtures.
func projectsTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for dest, fixture := range files {
		full := filepath.Join(root, dest)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDiscoverReadsEverySessionInEveryProject(t *testing.T) {
	root := projectsTree(t, map[string]string{
		"-home-user-git-alpha/e03ec97b-6d9f-4d9e-812e-40a808f2c76f.jsonl": "awaiting-input.jsonl",
		"-home-user-git-beta/b7bff73e-2961-40bf-b4be-6c9c73b00664.jsonl":  "tool-pending.jsonl",
		"-home-user-git-beta/58e4e9b2-48c5-4ddf-bb7a-4bce8e161845.jsonl":  "interrupted-by-user.jsonl",
		// Not a transcript: must be ignored rather than reported as broken.
		"-home-user-git-beta/notes.txt": "stub.jsonl",
	})

	sessions, err := Discover(root, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got, want := len(sessions), 3; got != want {
		ids := []string{}
		for _, s := range sessions {
			ids = append(ids, s.ID)
		}
		t.Fatalf("found %d sessions %v, want %d", got, ids, want)
	}
	for _, s := range sessions {
		if s.Dir == "" {
			t.Errorf("session %s has no working directory", s.ID)
		}
	}
}

// One session recap cannot parse must not hide the others: it comes back as a session that
// says so.
func TestDiscoverKeepsGoingPastABrokenSession(t *testing.T) {
	root := projectsTree(t, map[string]string{
		"-home-user-git-alpha/good.jsonl": "awaiting-input.jsonl",
		"-home-user-git-alpha/bad.jsonl":  "stub.jsonl",
	})

	sessions, err := Discover(root, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("found %d sessions, want 2", len(sessions))
	}
	var broken int
	for _, s := range sessions {
		if s.Unreadable != "" {
			broken++
		}
	}
	if broken != 1 {
		t.Errorf("%d unreadable sessions, want exactly 1", broken)
	}
}

func TestDiscoverOfAMissingDirectoryIsNotAnError(t *testing.T) {
	sessions, err := Discover(filepath.Join(t.TempDir(), "no-claude-here"), nil)
	if err != nil {
		t.Fatalf("Discover of a missing directory: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("found %d sessions in a missing directory", len(sessions))
	}
}

// countingCache proves Discover asks the cache before parsing, and tells it what it parsed.
type countingCache struct {
	sessions map[string]*session.Session
	lookups  int
	stores   int
}

func (c *countingCache) Lookup(path string, size int64, mod time.Time) (*session.Session, bool) {
	c.lookups++
	s, ok := c.sessions[path]
	return s, ok
}

func (c *countingCache) Store(path string, size int64, mod time.Time, s *session.Session) {
	c.stores++
	if c.sessions == nil {
		c.sessions = map[string]*session.Session{}
	}
	c.sessions[path] = s
}

func TestDiscoverUsesTheCacheAndFillsIt(t *testing.T) {
	root := projectsTree(t, map[string]string{
		"-home-user-git-alpha/one.jsonl": "awaiting-input.jsonl",
		"-home-user-git-beta/two.jsonl":  "tool-pending.jsonl",
	})

	c := &countingCache{}
	if _, err := Discover(root, c); err != nil {
		t.Fatal(err)
	}
	if c.stores != 2 {
		t.Errorf("stored %d sessions, want 2", c.stores)
	}

	// Second run: everything is a hit, and the sessions still come back.
	before := c.lookups
	sessions, err := Discover(root, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions from the cache, want 2", len(sessions))
	}
	if c.lookups-before != 2 {
		t.Errorf("%d lookups on the second run, want 2", c.lookups-before)
	}
	if c.stores != 2 {
		t.Errorf("%d stores after a fully cached run, want the original 2", c.stores)
	}
}
