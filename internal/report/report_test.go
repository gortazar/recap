package report

import (
	"testing"
	"time"

	"github.com/gortazar/recap/internal/session"
)

var now = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return now.Add(-d) }

// fakeLive answers liveness from a table instead of the process table.
type fakeLive map[string]session.Liveness

func (f fakeLive) Liveness(agent session.Agent, dir string) session.Liveness {
	if l, ok := f[dir]; ok {
		return l
	}
	return session.Dead
}

func TestOneLinePerProjectNewestFirst(t *testing.T) {
	sessions := []*session.Session{
		{ID: "a", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", Tail: session.TailAssistantText, LastActivity: ago(2 * time.Hour)},
		{ID: "b", Agent: session.AgentClaude, Dir: "/home/user/git/beta", Tail: session.TailAssistantText, LastActivity: ago(10 * time.Minute)},
		{ID: "c", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", Tail: session.TailAssistantText, LastActivity: ago(3 * time.Hour)},
	}

	projects := Build(sessions, fakeLive{}, now)

	if got, want := len(projects), 2; got != want {
		t.Fatalf("%d projects, want %d", got, want)
	}
	if got, want := projects[0].Name, "beta"; got != want {
		t.Errorf("first project = %q, want %q (most recent activity first)", got, want)
	}
	if got, want := len(projects[1].Sessions), 2; got != want {
		t.Errorf("alpha has %d sessions, want %d", got, want)
	}
	if got, want := projects[1].LastActivity, ago(2*time.Hour); !got.Equal(want) {
		t.Errorf("alpha LastActivity = %v, want the newest of its sessions %v", got, want)
	}
}

// Several sessions in one project collapse to one line, and the busiest of them decides
// what that line says: a project with something still running is not "idle".
func TestBusiestSessionLeadsTheProject(t *testing.T) {
	sessions := []*session.Session{
		{ID: "idle", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", Tail: session.TailAssistantText, LastActivity: ago(time.Minute)},
		{ID: "busy", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", Tail: session.TailToolResult, LastActivity: ago(30 * time.Second)},
	}
	live := fakeLive{"/home/user/git/alpha": session.Alive}

	projects := Build(sessions, live, now)

	if got, want := projects[0].Status(), session.StatusRunning; got != want {
		t.Errorf("project status = %v, want %v", got, want)
	}
	if got, want := projects[0].Lead.Session.ID, "busy"; got != want {
		t.Errorf("lead session = %q, want %q", got, want)
	}
}

func TestSessionsWithinAProjectAreNewestFirst(t *testing.T) {
	sessions := []*session.Session{
		{ID: "old", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", LastActivity: ago(5 * time.Hour)},
		{ID: "new", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", LastActivity: ago(time.Hour)},
	}
	projects := Build(sessions, fakeLive{}, now)
	if got, want := projects[0].Sessions[0].Session.ID, "new"; got != want {
		t.Errorf("first session = %q, want %q", got, want)
	}
}

func TestProjectNamesTheAgentsThatRanInIt(t *testing.T) {
	sessions := []*session.Session{
		{ID: "a", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", LastActivity: ago(time.Hour)},
		{ID: "b", Agent: session.AgentOpencode, Dir: "/home/user/git/alpha", LastActivity: ago(2 * time.Hour)},
		{ID: "c", Agent: session.AgentClaude, Dir: "/home/user/git/alpha", LastActivity: ago(3 * time.Hour)},
	}
	got := Build(sessions, fakeLive{}, now)[0].Agents
	want := []session.Agent{session.AgentClaude, session.AgentOpencode}
	if len(got) != len(want) {
		t.Fatalf("agents = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("agents = %v, want %v", got, want)
		}
	}
}

// A session too broken to say where it ran still has to appear somewhere, under a name the
// user can recognise: the store directory it came from.
func TestUnreadableSessionsAreGroupedByTheirStoreDirectory(t *testing.T) {
	sessions := []*session.Session{{
		ID:           "bad",
		Agent:        session.AgentClaude,
		Source:       "/home/user/.claude/projects/-home-user-git-alpha/bad.jsonl",
		Unreadable:   "no recognisable records",
		LastActivity: ago(time.Hour),
	}}
	projects := Build(sessions, fakeLive{}, now)
	if got, want := projects[0].Name, "-home-user-git-alpha"; got != want {
		t.Errorf("project name = %q, want %q", got, want)
	}
	if got, want := projects[0].Status(), session.StatusUnknown; got != want {
		t.Errorf("status = %v, want %v", got, want)
	}
}

func TestEachSessionCarriesItsOwnStatusAndSentence(t *testing.T) {
	sessions := []*session.Session{{
		ID:           "a",
		Agent:        session.AgentClaude,
		Dir:          "/home/user/git/alpha",
		LastRequest:  "Fix the flaky test",
		Tail:         session.TailPendingTool,
		PendingTool:  "Bash",
		LastActivity: ago(3 * time.Hour),
	}}
	entry := Build(sessions, fakeLive{}, now)[0].Lead
	if got, want := entry.Status, session.StatusInterrupted; got != want {
		t.Errorf("status = %v, want %v", got, want)
	}
	if got, want := entry.Sentence, `Asked to "Fix the flaky test" — interrupted mid-Bash.`; got != want {
		t.Errorf("sentence = %q, want %q", got, want)
	}
}
