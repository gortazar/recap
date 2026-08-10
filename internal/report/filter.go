package report

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/gortazar/recap/internal/session"
)

// Filters is what the user asked to see. The zero value shows everything.
type Filters struct {
	// Since hides sessions untouched for longer than this. Zero means no window.
	Since time.Duration
	// Agent, when set, keeps only that agent's sessions.
	Agent session.Agent
	// Roots, when set, keep only sessions whose working directory is inside one of them.
	// This is what stops throwaway sessions from /tmp filling the report.
	Roots []string
	// Ignore hides sessions under these directories even when they are inside a root.
	Ignore []string
	// Project keeps only the project with this name.
	Project string
	// RunningOnly keeps only projects with something running right now.
	RunningOnly bool
}

// FilterSessions applies the filters that are properties of a session. It runs before
// grouping, so a project is not shown on the strength of a session the user filtered out.
func FilterSessions(sessions []*session.Session, f Filters, now time.Time) []*session.Session {
	var kept []*session.Session
	for _, s := range sessions {
		if f.Since > 0 && !s.LastActivity.IsZero() && now.Sub(s.LastActivity) > f.Since {
			continue
		}
		if f.Agent != "" && s.Agent != f.Agent {
			continue
		}
		if len(f.Roots) > 0 && s.Dir != "" && !underAnyRoot(s.Dir, f.Roots) {
			continue
		}
		if s.Dir != "" && underAnyRoot(s.Dir, f.Ignore) {
			continue
		}
		kept = append(kept, s)
	}
	return kept
}

// FilterProjects applies the filters that are properties of a project, which can only be
// known once sessions have been grouped and classified.
func FilterProjects(projects []Project, f Filters) []Project {
	var kept []Project
	for _, p := range projects {
		if f.Project != "" && !strings.EqualFold(p.Name, f.Project) {
			continue
		}
		if f.RunningOnly && p.Status() != session.StatusRunning {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// underAnyRoot compares whole path components, so /home/user/gitlab is not inside
// /home/user/git.
func underAnyRoot(dir string, roots []string) bool {
	dir = filepath.Clean(dir)
	for _, root := range roots {
		root = filepath.Clean(root)
		if dir == root {
			return true
		}
		if strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
