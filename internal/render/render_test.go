package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/gortazar/recap/internal/report"
	"github.com/gortazar/recap/internal/session"
)

var now = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return now.Add(-d) }

type fakeLive map[string]session.Liveness

func (f fakeLive) Liveness(agent session.Agent, dir string) session.Liveness {
	if l, ok := f[dir]; ok {
		return l
	}
	return session.Dead
}

func sample() []report.Project {
	sessions := []*session.Session{
		{
			ID: "1", Agent: session.AgentClaude, Dir: "/home/user/git/alpha",
			LastRequest: "Run the suite", Tail: session.TailToolResult, LastTool: "Bash",
			LastActivity: ago(10 * time.Second),
		},
		{
			ID: "2", Agent: session.AgentOpencode, Dir: "/home/user/git/beta",
			LastRequest: "Fix the flaky test", Tail: session.TailPendingTool, PendingTool: "Bash",
			LastActivity: ago(4 * time.Hour),
		},
	}
	return report.Build(sessions, fakeLive{"/home/user/git/alpha": session.Alive}, now)
}

func render(t *testing.T, projects []report.Project, opts Options) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Text(&buf, projects, opts); err != nil {
		t.Fatalf("Text: %v", err)
	}
	return buf.String()
}

func TestOneLinePerProject(t *testing.T) {
	got := render(t, sample(), Options{Now: now})
	want := "" +
		"🟢 alpha (Claude Code) -> Asked to \"Run the suite\" — working, last used Bash.\n" +
		"🔴 beta (opencode) -> Asked to \"Fix the flaky test\" — interrupted mid-Bash.\n"
	if got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// Terminals and pipes that mangle emoji get words instead, aligned so the column still reads.
func TestNoIconsSubstitutesWords(t *testing.T) {
	got := render(t, sample(), Options{Now: now, NoIcons: true})
	want := "" +
		"running     alpha (Claude Code) -> Asked to \"Run the suite\" — working, last used Bash.\n" +
		"interrupted beta (opencode) -> Asked to \"Fix the flaky test\" — interrupted mid-Bash.\n"
	if got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestVerboseAddsALinePerSession(t *testing.T) {
	got := render(t, sample(), Options{Now: now, Verbose: true})
	if !strings.Contains(got, "    1  10s ago") {
		t.Errorf("verbose output has no session line with an id and an age:\n%s", got)
	}
	if !strings.Contains(got, "last tool Bash") {
		t.Errorf("verbose output does not name the last tool:\n%s", got)
	}
}

// A session that never said where it ran is grouped under the escaped store directory it
// came from, which is far too long for a column. Its tail is the part worth keeping.
func TestOverlongProjectNamesAreCutFromTheFront(t *testing.T) {
	sessions := []*session.Session{{
		ID:           "bad",
		Agent:        session.AgentClaude,
		Source:       "/home/user/.claude/projects/-tmp-claude-scratchpad-repo-orchestrator-worktrees-beta-ideas-beta/bad.jsonl",
		Unreadable:   "no recognisable records",
		LastActivity: ago(time.Hour),
	}}
	got := render(t, report.Build(sessions, fakeLive{}, now), Options{Now: now})
	if !strings.Contains(got, "…-orchestrator-worktrees-beta-ideas-beta (Claude Code)") {
		t.Errorf("long project name was not cut from the front:\n%s", got)
	}
}

func TestEmptyReportSaysSoOnStderrNotStdout(t *testing.T) {
	got := render(t, nil, Options{Now: now})
	if got != "" {
		t.Errorf("output = %q, want nothing at all for an empty report", got)
	}
}

func TestLegendListsEveryStatus(t *testing.T) {
	var buf bytes.Buffer
	if err := Legend(&buf, Options{}); err != nil {
		t.Fatalf("Legend: %v", err)
	}
	got := buf.String()
	for _, s := range session.Statuses() {
		if !strings.Contains(got, s.Icon()) || !strings.Contains(got, s.Word()) {
			t.Errorf("legend is missing %v (%s):\n%s", s, s.Icon(), got)
		}
	}
}

func TestAgeIsHumanReadable(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s ago"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{50 * time.Hour, "2d ago"},
	}
	for _, c := range cases {
		if got := Age(c.d); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
