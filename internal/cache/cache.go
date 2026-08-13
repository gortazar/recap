// Package cache remembers what recap parsed out of a transcript, so a session that has not
// changed is not read twice.
//
// The key is the file's size and modification time. A transcript is append-only while a
// session is alive and never rewritten afterwards, so a file whose size and mtime both match
// has the same tail it had last time. Nothing here is authoritative: a miss, a corrupt cache
// or an unwritable cache directory all just mean recap does the parsing again.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/gortazar/recap/internal/session"
)

// version is baked into the cache file. Bump it whenever a reader changes what it extracts,
// so that entries written by an older recap are ignored rather than believed.
//
// Version 2: 0.3 added session.Activity, which readers fill and 0.2 did not. Without this
// bump an upgraded recap would print no paragraph for every session it had already seen —
// and the tests would not have caught it, because they build a fresh cache every time.
const version = 2

// DefaultPath is where recap keeps its cache.
func DefaultPath() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "recap", "sessions.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "recap", "sessions.json")
}

type entry struct {
	Size    int64            `json:"size"`
	ModTime time.Time        `json:"mtime"`
	Session *session.Session `json:"session"`
}

type file struct {
	Version int              `json:"version"`
	Entries map[string]entry `json:"entries"`
}

// Cache is a set of parsed sessions keyed by transcript path. It is not safe for concurrent
// use; recap reads sessions one at a time.
type Cache struct {
	path  string
	old   map[string]entry
	fresh map[string]entry
	hits  int
}

// Open reads the cache at path. A missing, unreadable or stale cache is simply an empty one.
func Open(path string) *Cache {
	c := &Cache{path: path, old: map[string]entry{}, fresh: map[string]entry{}}
	if path == "" {
		return c
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var f file
	if err := json.Unmarshal(body, &f); err != nil || f.Version != version {
		return c
	}
	for k, v := range f.Entries {
		if v.Session != nil {
			c.old[k] = v
		}
	}
	return c
}

// Lookup returns the session recap parsed from this file last time, if the file has not
// changed since.
func (c *Cache) Lookup(path string, size int64, mod time.Time) (*session.Session, bool) {
	if c == nil {
		return nil, false
	}
	e, ok := c.old[path]
	if !ok || e.Size != size || !e.ModTime.Equal(mod) {
		return nil, false
	}
	c.fresh[path] = e
	c.hits++
	return e.Session, true
}

// Store records what a file parsed to.
func (c *Cache) Store(path string, size int64, mod time.Time, s *session.Session) {
	if c == nil || s == nil {
		return
	}
	c.fresh[path] = entry{Size: size, ModTime: mod, Session: s}
}

// Hits is how many sessions came from the cache, for --verbose diagnostics.
func (c *Cache) Hits() int {
	if c == nil {
		return 0
	}
	return c.hits
}

// Save writes the cache back. Only the entries touched during this run are kept, so
// transcripts that have been deleted drop out instead of accumulating forever.
//
// Failing to save is not an error worth stopping for: the cache is an optimisation, and a
// read-only or full cache directory should not stop recap from printing its report.
func (c *Cache) Save() error {
	if c == nil || c.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(file{Version: version, Entries: c.fresh})
	if err != nil {
		return err
	}
	// Write and rename, so a recap interrupted mid-write leaves the old cache intact rather
	// than a truncated one.
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".sessions-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}
