package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gortazar/recap/internal/session"
)

var mod = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func sample() *session.Session {
	return &session.Session{
		ID:           "s1",
		Agent:        session.AgentClaude,
		Dir:          "/home/user/git/alpha",
		LastRequest:  "Run the suite",
		Tail:         session.TailAssistantText,
		LastActivity: mod,
	}
}

func TestAnUnchangedFileIsNotParsedAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	c := Open(path)
	if _, ok := c.Lookup("/transcript.jsonl", 100, mod); ok {
		t.Error("an empty cache returned a hit")
	}
	c.Store("/transcript.jsonl", 100, mod, sample())
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok := Open(path).Lookup("/transcript.jsonl", 100, mod)
	if !ok {
		t.Fatal("a file that has not changed was not found in the cache")
	}
	if got.ID != "s1" || got.LastRequest != "Run the suite" {
		t.Errorf("cached session came back wrong: %+v", got)
	}
}

func TestAChangedFileMisses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	c := Open(path)
	c.Store("/transcript.jsonl", 100, mod, sample())
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	reopened := Open(path)
	if _, ok := reopened.Lookup("/transcript.jsonl", 200, mod); ok {
		t.Error("a file that grew was served from the cache")
	}
	if _, ok := reopened.Lookup("/transcript.jsonl", 100, mod.Add(time.Second)); ok {
		t.Error("a file rewritten in place was served from the cache")
	}
}

// A cache written by an older recap may have been parsed by different rules, so it is
// ignored rather than believed.
func TestACacheFromAnotherVersionIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	body := `{"version":0,"entries":{"/transcript.jsonl":{"size":100,"mtime":"2026-08-09T12:00:00Z","session":{"ID":"s1"}}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Open(path).Lookup("/transcript.jsonl", 100, mod); ok {
		t.Error("an entry from another cache version was used")
	}
}

// The upgrade this version bump exists for. An entry in the shape recap 0.2 wrote parses
// perfectly well — it simply has no Activity — so believing it would mean no paragraph for
// every session already in the cache, silently, until it changed on disk.
func TestAnEntryFromTheVersionBeforeActivityIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	// Exactly what 0.2 wrote: version 1, and a session with no Activity field.
	body := `{"version":1,"entries":{"/transcript.jsonl":{"size":100,"mtime":"2026-08-09T12:00:00Z",` +
		`"session":{"ID":"s1","Agent":"Claude Code","Dir":"/home/user/git/alpha","LastRequest":"Run the suite"}}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Open(path).Lookup("/transcript.jsonl", 100, mod); ok {
		t.Error("a 0.2 cache entry was used, so the session would have no paragraph until its transcript changed")
	}
}

// And a round trip through the current version keeps the Activity, which is the other half
// of the same promise.
func TestActivitySurvivesTheCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	s := sample()
	s.Activity = session.Activity{
		ToolCounts: map[string]int{"Bash": 12, "Edit": 3},
		Files:      []string{"scheduler.go"},
		Requests:   []string{"run the suite"},
		Turns:      6,
		Errors:     1,
		First:      mod.Add(-time.Hour),
		Last:       mod,
		Truncated:  true,
	}

	c := Open(path)
	c.Store("/transcript.jsonl", 100, mod, s)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	got, ok := Open(path).Lookup("/transcript.jsonl", 100, mod)
	if !ok {
		t.Fatal("the entry just written was not found")
	}
	if got.Activity.ToolCounts["Bash"] != 12 || got.Activity.Turns != 6 || got.Activity.Errors != 1 {
		t.Errorf("Activity came back wrong: %+v", got.Activity)
	}
	if !got.Activity.Truncated {
		t.Error("Truncated was lost, so the paragraph would claim to cover the whole window")
	}
	if len(got.Activity.Files) != 1 || got.Activity.Files[0] != "scheduler.go" {
		t.Errorf("Files came back as %v", got.Activity.Files)
	}
}

func TestACorruptCacheIsJustAnEmptyOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte("this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Open(path)
	if _, ok := c.Lookup("/transcript.jsonl", 100, mod); ok {
		t.Error("a corrupt cache returned a hit")
	}
	c.Store("/transcript.jsonl", 100, mod, sample())
	if err := c.Save(); err != nil {
		t.Errorf("Save over a corrupt cache: %v", err)
	}
}

// Transcripts get deleted; the cache must not grow forever.
func TestEntriesNotTouchedThisRunAreDropped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	c := Open(path)
	c.Store("/gone.jsonl", 100, mod, sample())
	c.Store("/kept.jsonl", 100, mod, sample())
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	second := Open(path)
	if _, ok := second.Lookup("/kept.jsonl", 100, mod); !ok {
		t.Fatal("the entry that should have survived was missing")
	}
	if err := second.Save(); err != nil {
		t.Fatal(err)
	}

	third := Open(path)
	if _, ok := third.Lookup("/gone.jsonl", 100, mod); ok {
		t.Error("an entry untouched for a whole run was kept")
	}
	if _, ok := third.Lookup("/kept.jsonl", 100, mod); !ok {
		t.Error("the entry looked up last run was dropped")
	}
}

func TestANilCacheIsUsable(t *testing.T) {
	var c *Cache
	if _, ok := c.Lookup("/transcript.jsonl", 100, mod); ok {
		t.Error("a nil cache returned a hit")
	}
	c.Store("/transcript.jsonl", 100, mod, sample())
	if err := c.Save(); err != nil {
		t.Errorf("Save on a nil cache: %v", err)
	}
}
