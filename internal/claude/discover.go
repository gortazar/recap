package claude

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gortazar/recap/internal/session"
)

// Cache is the seam over recap's parsed-session cache. Discover works with a nil Cache; the
// cache is an optimisation, never a source of truth.
type Cache interface {
	Lookup(path string, size int64, mod time.Time) (*session.Session, bool)
	Store(path string, size int64, mod time.Time, s *session.Session)
}

// DefaultProjectsDir is where Claude Code keeps one directory per project.
func DefaultProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// Discover reads every session under a ~/.claude/projects-shaped directory.
//
// The directory names there are the project paths with the separators replaced, which is not
// reversible — `-home-user-a-b` could be a/b or a-b — so recap never inverts them. Each
// session's real working directory is read from the transcript instead.
//
// A missing directory is not an error: it just means this agent was never used here.
//
// A transcript whose size and modification time match a cached entry is taken from the
// cache instead of being parsed again; pass a nil Cache to always parse.
func Discover(projectsDir string, c Cache) ([]*session.Session, error) {
	if projectsDir == "" {
		return nil, nil
	}
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*session.Session
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, project.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue // a project directory we cannot read tells us nothing; skip it
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dir, f.Name())
			if s, ok := cached(c, path, f); ok {
				sessions = append(sessions, s)
				continue
			}
			s, err := ReadSession(path)
			if err == nil && c != nil {
				if info, statErr := f.Info(); statErr == nil {
					c.Store(path, info.Size(), info.ModTime(), s)
				}
			}
			if err != nil {
				// Report it rather than dropping it: a session recap cannot open is
				// exactly the kind of thing the user wants to hear about.
				s = &session.Session{
					Agent:      session.AgentClaude,
					ID:         strings.TrimSuffix(f.Name(), ".jsonl"),
					Source:     path,
					Unreadable: err.Error(),
				}
			}
			sessions = append(sessions, s)
		}
	}
	return sessions, nil
}

// cached looks a transcript up without making the caller deal with a nil cache or a file
// whose metadata cannot be read.
func cached(c Cache, path string, f os.DirEntry) (*session.Session, bool) {
	if c == nil {
		return nil, false
	}
	info, err := f.Info()
	if err != nil {
		return nil, false
	}
	return c.Lookup(path, info.Size(), info.ModTime())
}
