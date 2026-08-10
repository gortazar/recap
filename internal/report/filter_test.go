package report

import (
	"testing"
	"time"

	"github.com/gortazar/recap/internal/session"
)

func ids(sessions []*session.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.ID)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFilterSessions(t *testing.T) {
	all := []*session.Session{
		{ID: "recent-claude", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", LastActivity: ago(time.Hour)},
		{ID: "old-claude", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", LastActivity: ago(72 * time.Hour)},
		{ID: "recent-opencode", Agent: session.AgentOpencode, Dir: "/home/user/git/beta", LastActivity: ago(2 * time.Hour)},
		{ID: "scratch", Agent: session.AgentClaude, Dir: "/tmp/scratch", LastActivity: ago(time.Hour)},
	}

	cases := []struct {
		name string
		f    Filters
		want []string
	}{
		{
			name: "no filters keeps everything",
			f:    Filters{},
			want: []string{"recent-claude", "old-claude", "recent-opencode", "scratch"},
		},
		{
			name: "a time window hides what has not been touched since",
			f:    Filters{Since: 24 * time.Hour},
			want: []string{"recent-claude", "recent-opencode", "scratch"},
		},
		{
			name: "one agent only",
			f:    Filters{Agent: session.AgentOpencode},
			want: []string{"recent-opencode"},
		},
		{
			name: "roots hide throwaway sessions from outside them",
			f:    Filters{Roots: []string{"/home/user/git"}},
			want: []string{"recent-claude", "old-claude", "recent-opencode"},
		},
		{
			name: "a root matches the directory itself, not just its children",
			f:    Filters{Roots: []string{"/home/user/git/alpha"}},
			want: []string{"recent-claude", "old-claude"},
		},
		// /home/user/gitlab must not match a /home/user/git root.
		{
			name: "roots match whole path components",
			f:    Filters{Roots: []string{"/home/user/gi"}},
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ids(FilterSessions(all, c.f, now))
			if !equal(got, c.want) {
				t.Errorf("kept %v, want %v", got, c.want)
			}
		})
	}
}

func TestFilterProjects(t *testing.T) {
	sessions := []*session.Session{
		{ID: "a", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", Tail: session.TailToolResult, LastActivity: ago(10 * time.Second)},
		{ID: "b", Agent: session.AgentClaude, Dir: "/home/user/git/beta", Tail: session.TailAssistantText, LastActivity: ago(5 * time.Hour)},
	}
	projects := Build(sessions, fakeLive{"/home/user/git/alpha": session.Alive}, now)

	t.Run("by name", func(t *testing.T) {
		got := FilterProjects(projects, Filters{Project: "beta"})
		if len(got) != 1 || got[0].Name != "beta" {
			t.Errorf("got %d projects, want just beta", len(got))
		}
	})

	t.Run("only what is running", func(t *testing.T) {
		got := FilterProjects(projects, Filters{RunningOnly: true})
		if len(got) != 1 || got[0].Name != "alpha" {
			t.Errorf("got %v, want just alpha", got)
		}
	})
}
