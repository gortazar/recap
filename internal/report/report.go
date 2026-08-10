// Package report turns a pile of sessions into the thing recap prints: one entry per
// project, newest first, each led by its busiest session.
package report

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/gortazar/recap/internal/session"
)

// Liveness is the seam over process discovery, so the report can be built from a table in
// tests and from the real process table in production.
type Liveness interface {
	Liveness(agent session.Agent, dir string) session.Liveness
}

// Entry is one session with the two things recap computed about it.
type Entry struct {
	Session  *session.Session
	Status   session.Status
	Sentence string
}

// Project is one line of output: everything that happened in one working directory.
type Project struct {
	Name string
	Dir  string
	// Agents that ran here, most recently used first.
	Agents []session.Agent
	// Sessions, newest first.
	Sessions     []Entry
	Lead         Entry
	LastActivity time.Time
}

// Status of the project is the status of its busiest session.
func (p Project) Status() session.Status { return p.Lead.Status }

// busyness orders the status vocabulary by how much a project is demanding of you. It
// decides which session speaks for a project when several share one directory.
func busyness(s session.Status) int {
	switch s {
	case session.StatusRunning:
		return 5
	case session.StatusAwaitingInput:
		return 4
	case session.StatusInterrupted:
		return 3
	case session.StatusUnknown:
		return 2
	case session.StatusIdle:
		return 1
	default: // StatusFinished — nothing left to do here
		return 0
	}
}

// Build classifies every session, groups them by working directory and orders the result.
func Build(sessions []*session.Session, live Liveness, now time.Time) []Project {
	byDir := map[string][]Entry{}
	for _, s := range sessions {
		st := session.Classify(s, live.Liveness(s.Agent, s.Dir), now)
		key := groupKey(s)
		byDir[key] = append(byDir[key], Entry{
			Session:  s,
			Status:   st,
			Sentence: session.Sentence(s, st),
		})
	}

	projects := make([]Project, 0, len(byDir))
	for key, entries := range byDir {
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].Session.LastActivity.After(entries[j].Session.LastActivity)
		})
		p := Project{
			Name:         filepath.Base(key),
			Dir:          entries[0].Session.Dir,
			Sessions:     entries,
			Lead:         lead(entries),
			LastActivity: entries[0].Session.LastActivity,
			Agents:       agentsOf(entries),
		}
		projects = append(projects, p)
	}

	sort.SliceStable(projects, func(i, j int) bool {
		if projects[i].LastActivity.Equal(projects[j].LastActivity) {
			return projects[i].Name < projects[j].Name
		}
		return projects[i].LastActivity.After(projects[j].LastActivity)
	})
	return projects
}

// groupKey is the working directory, falling back to the store directory a session came
// from when the transcript never said where it ran. Grouping everything nameless together
// would merge unrelated projects into one line.
func groupKey(s *session.Session) string {
	if s.Dir != "" {
		return s.Dir
	}
	if s.Source != "" {
		return filepath.Dir(s.Source)
	}
	return "unknown"
}

// lead picks the session that speaks for the project: the busiest, and among equals the
// most recently active. Entries are already newest first, so a stable max does it.
func lead(entries []Entry) Entry {
	best := entries[0]
	for _, e := range entries[1:] {
		if busyness(e.Status) > busyness(best.Status) {
			best = e
		}
	}
	return best
}

func agentsOf(entries []Entry) []session.Agent {
	var agents []session.Agent
	seen := map[session.Agent]bool{}
	for _, e := range entries {
		a := e.Session.Agent
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		agents = append(agents, a)
	}
	return agents
}
