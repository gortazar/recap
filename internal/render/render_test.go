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

// The one-line-per-project report, which --no-report restores now that paragraphs are the
// default.
func TestOneLinePerProject(t *testing.T) {
	got := render(t, sample(), Options{Now: now, NoReport: true})
	want := "" +
		"🟢 alpha (Claude Code) -> Asked to \"Run the suite\" — working, last used Bash.\n" +
		"🔴 beta (opencode) -> Asked to \"Fix the flaky test\" — interrupted mid-Bash.\n"
	if got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// Terminals and pipes that mangle emoji get words instead, aligned so the column still reads.
func TestNoIconsSubstitutesWords(t *testing.T) {
	got := render(t, sample(), Options{Now: now, NoIcons: true, NoReport: true})
	want := "" +
		"running     alpha (Claude Code) -> Asked to \"Run the suite\" — working, last used Bash.\n" +
		"interrupted beta (opencode) -> Asked to \"Fix the flaky test\" — interrupted mid-Bash.\n"
	if got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestVerboseAddsALinePerSession(t *testing.T) {
	got := render(t, sample(), Options{Now: now, Verbose: true, NoReport: true})
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

// sampleWithReports is the sample report with paragraphs attached, as the readers would
// leave it.
func sampleWithReports() []report.Project {
	projects := sample()
	for i := range projects {
		for j := range projects[i].Sessions {
			projects[i].Sessions[j].Report = "Over 1h: one request, \"run the suite\". " +
				"12 tool calls — all Bash — touching scheduler.go. Idle since 20:00."
		}
		projects[i].Lead.Report = projects[i].Sessions[0].Report
	}
	return projects
}

// The paragraph is what this whole feature is for, so it is on by default. --no-report is
// the way back to the one-line-per-project report.
func TestParagraphIsPrintedUnderTheProjectLine(t *testing.T) {
	// Wide enough that the paragraph is one line, so the blank line after it is lines[2].
	got := render(t, sampleWithReports(), Options{Now: now, Width: 200})
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	if !strings.HasPrefix(lines[0], "🟢 alpha") {
		t.Fatalf("first line is not the project line:\n%s", got)
	}
	if !strings.HasPrefix(lines[1], "    Over 1h:") {
		t.Errorf("paragraph is not indented one level under its project line:\n%s", got)
	}
	if lines[2] != "" {
		t.Errorf("no blank line after the paragraph; several stacked are unreadable:\n%s", got)
	}
}

func TestNoReportRestoresTheOneLineReport(t *testing.T) {
	got := render(t, sampleWithReports(), Options{Now: now, Width: 100, NoReport: true})
	want := "" +
		"🟢 alpha (Claude Code) -> Asked to \"Run the suite\" — working, last used Bash.\n" +
		"🔴 beta (opencode) -> Asked to \"Fix the flaky test\" — interrupted mid-Bash.\n"
	if got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// Under -v the paragraph belongs to its session line, so it indents one level deeper again.
func TestVerboseIndentsTheParagraphUnderItsSession(t *testing.T) {
	got := render(t, sampleWithReports(), Options{Now: now, Width: 100, Verbose: true})
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	if !strings.HasPrefix(lines[1], "    1  10s ago") {
		t.Fatalf("second line is not the session line:\n%s", got)
	}
	if !strings.HasPrefix(lines[2], "        Over 1h:") {
		t.Errorf("paragraph is not indented under its session line:\n%s", got)
	}
	// And the project line does not repeat it.
	if strings.Count(got, "Over 1h:") != 2 {
		t.Errorf("expected one paragraph per session and none on the project line:\n%s", got)
	}
}

func TestParagraphWrapsToTheGivenWidth(t *testing.T) {
	projects := sampleWithReports()
	got := render(t, projects[:1], Options{Now: now, Width: 60})
	// The status line is one line per project by design and is never wrapped; the paragraph
	// under it is what has to fit.
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if !strings.HasPrefix(line, indent) {
			continue
		}
		if len([]rune(line)) > 60 {
			t.Errorf("paragraph line is %d wide, want at most 60: %q", len([]rune(line)), line)
		}
	}
	// Wrapped, not truncated: every word survives.
	flat := strings.Join(strings.Fields(strings.ReplaceAll(got, "\n", " ")), " ")
	if !strings.Contains(flat, "touching scheduler.go. Idle since 20:00.") {
		t.Errorf("wrapping lost the end of the paragraph:\n%s", got)
	}
}

// A redirect must produce a stable file, and CI output must not depend on the runner's
// terminal. Width 0 means "not a terminal", which is 80 columns.
func TestWidthDefaultsTo80WhenNotATerminal(t *testing.T) {
	got := render(t, sampleWithReports()[:1], Options{Now: now})
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if !strings.HasPrefix(line, indent) {
			continue
		}
		if len([]rune(line)) > 80 {
			t.Errorf("line is %d wide, want at most 80: %q", len([]rune(line)), line)
		}
	}
}

// An unreadable transcript has no paragraph to write — the status line already says so — and
// must not leave a stray blank line or an empty indent behind.
func TestASessionWithoutAParagraphPrintsNothingExtra(t *testing.T) {
	sessions := []*session.Session{{
		ID:           "bad",
		Agent:        session.AgentClaude,
		Dir:          "/home/user/git/alpha",
		Unreadable:   "no recognisable records",
		LastActivity: ago(time.Hour),
	}}
	got := render(t, report.Build(sessions, fakeLive{}, now), Options{Now: now, Width: 100})
	if strings.Contains(got, "\n\n") {
		t.Errorf("blank line printed for a session with no paragraph:\n%q", got)
	}
	if lines := strings.Count(got, "\n"); lines != 1 {
		t.Errorf("got %d lines, want just the status line:\n%q", lines, got)
	}
}

func TestWrap(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"short text is one line", "hello there", 40, []string{"hello there"}},
		{"breaks on spaces", "aaa bbb ccc ddd", 7, []string{"aaa bbb", "ccc ddd"}},
		{
			name:  "a word longer than the width is not broken",
			text:  "see /a/very/long/path/that/will/not/fit here",
			width: 10,
			want:  []string{"see", "/a/very/long/path/that/will/not/fit", "here"},
		},
		{"empty text is no lines", "", 40, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrap(c.text, c.width)
			if len(got) != len(c.want) {
				t.Fatalf("wrap = %q, want %q", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("wrap = %q, want %q", got, c.want)
				}
			}
		})
	}
}
