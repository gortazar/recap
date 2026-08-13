package claude

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gortazar/recap/internal/session"
)

func read(t *testing.T, name string) *session.Session {
	t.Helper()
	s, err := ReadSession(filepath.Join("testdata", name), time.Time{})
	if err != nil {
		t.Fatalf("ReadSession(%s): %v", name, err)
	}
	return s
}

func TestReadsSessionMetadata(t *testing.T) {
	s := read(t, "awaiting-input.jsonl")

	if got, want := s.ID, "e03ec97b-6d9f-4d9e-812e-40a808f2c76f"; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
	if got, want := s.Agent, session.AgentClaude; got != want {
		t.Errorf("Agent = %q, want %q", got, want)
	}
	if got, want := s.Dir, "/home/user/git/blog-publication-automation"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got, want := s.Branch, "main"; got != want {
		t.Errorf("Branch = %q, want %q", got, want)
	}
	if got, want := s.Model, "claude-opus-5"; got != want {
		t.Errorf("Model = %q, want %q", got, want)
	}
	if got, want := s.Version, "2.1.224"; got != want {
		t.Errorf("Version = %q, want %q", got, want)
	}
}

func TestReadsTimestamps(t *testing.T) {
	s := read(t, "awaiting-input.jsonl")

	if got, want := s.Started.UTC().Format("2006-01-02T15:04:05Z"), "2026-08-09T15:15:16Z"; got != want {
		t.Errorf("Started = %q, want %q", got, want)
	}
	if got, want := s.LastActivity.UTC().Format("2006-01-02T15:04:05Z"), "2026-08-09T15:37:00Z"; got != want {
		t.Errorf("LastActivity = %q, want %q", got, want)
	}
}

func TestReadsRequestsAndLastText(t *testing.T) {
	s := read(t, "awaiting-input.jsonl")

	if !strings.HasPrefix(s.FirstRequest, `I set "First user source / medium"`) {
		t.Errorf("FirstRequest = %q", s.FirstRequest)
	}
	if !strings.HasPrefix(s.LastText, "You're right — Realtime only exposes") {
		t.Errorf("LastText = %q", s.LastText)
	}
}

func TestTailShapes(t *testing.T) {
	cases := []struct {
		fixture string
		want    session.Tail
	}{
		// Ends with assistant prose, followed only by bookkeeping records, which must be
		// skipped when looking for the last conversational event.
		{"awaiting-input.jsonl", session.TailAssistantText},
		// Ends with a tool_use whose result never arrived.
		{"tool-pending.jsonl", session.TailPendingTool},
		// Ends with the agent's own "[Request interrupted by user]" marker.
		{"interrupted-by-user.jsonl", session.TailInterrupted},
		// Ends with a tool result: the agent was mid-turn when the transcript stopped.
		{"tool-result-tail.jsonl", session.TailToolResult},
	}
	for _, c := range cases {
		if got := read(t, c.fixture).Tail; got != c.want {
			t.Errorf("%s: Tail = %v, want %v", c.fixture, got, c.want)
		}
	}
}

func TestPendingToolIsNamed(t *testing.T) {
	s := read(t, "tool-pending.jsonl")
	if got, want := s.PendingTool, "Write"; got != want {
		t.Errorf("PendingTool = %q, want %q", got, want)
	}
	if got, want := s.LastTool, "Write"; got != want {
		t.Errorf("LastTool = %q, want %q", got, want)
	}
	if got, want := s.LastFile, "/tmp/claude-1000/-home-patxi-git-aideas/040753cf-ea02-486e-9b73-5ae34a8957c1/scratchpad/nolimits/repo/ideas/recap-gs/PLAN.md"; got != want {
		t.Errorf("LastFile = %q, want %q", got, want)
	}
}

func TestTitleFromAgentsOwnTitleRecord(t *testing.T) {
	s := read(t, "interrupted-by-user.jsonl")
	if got, want := s.Title, "Continue implementing idea per CLAUDE.md"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
}

// Slash-command plumbing is written into the transcript as user turns. It is not something
// the user asked for in words, so it must not become the recap sentence.
func TestSyntheticUserTurnsAreNotRequests(t *testing.T) {
	s := read(t, "string-content.jsonl")
	if s.FirstRequest != "" {
		t.Errorf("FirstRequest = %q, want empty (all user turns are command plumbing)", s.FirstRequest)
	}
	if s.LastRequest != "" {
		t.Errorf("LastRequest = %q, want empty", s.LastRequest)
	}
}

// A transcript recap cannot make sense of must still produce a session, so one bad file
// cannot hide the good ones.
// The set of tags Claude Code wraps synthetic turns in keeps growing between releases, so
// recap recognises the shape rather than a list of names.
func TestAnyXMLTagAtTheStartOfAUserTurnIsPlumbing(t *testing.T) {
	for _, text := range []string{
		"<command-name>/usage</command-name>",
		"<task-notification>idea 7 stopped</task-notification>",
		"<some-future-tag>whatever</some-future-tag>",
	} {
		if !isPlumbing(text) {
			t.Errorf("%q was not recognised as plumbing", text)
		}
	}
	for _, text := range []string{
		"<- this arrow starts a real sentence",
		"Compare a < b in the parser",
		"3 < 4 is true",
	} {
		if isPlumbing(text) {
			t.Errorf("%q was wrongly treated as plumbing", text)
		}
	}
}

func TestUnparseableSessionDegradesGracefully(t *testing.T) {
	s := read(t, "stub.jsonl")
	if s.Unreadable == "" {
		t.Errorf("Unreadable = %q, want an explanation", s.Unreadable)
	}
	if s.ID == "" {
		t.Errorf("ID = %q, want the filename to stand in for the session id", s.ID)
	}
	if s.LastActivity.IsZero() {
		t.Errorf("LastActivity is zero, want the file mtime as a fallback")
	}
}

func TestMissingFileIsAnError(t *testing.T) {
	if _, err := ReadSession(filepath.Join("testdata", "no-such-session.jsonl"), time.Time{}); err == nil {
		t.Fatal("ReadSession on a missing file returned no error")
	}
}

// The paragraph is only as good as what the reader collected, so the counts are pinned
// against a real (scrubbed) transcript rather than a hand-built one.
func TestReadsActivityOverTheWindow(t *testing.T) {
	s := read(t, "tool-result-tail.jsonl")
	a := s.Activity

	if got, want := a.Turns, 9; got != want {
		t.Errorf("Turns = %d, want %d", got, want)
	}
	if got, want := a.Errors, 0; got != want {
		t.Errorf("Errors = %d, want %d", got, want)
	}
	want := map[string]int{"Bash": 1, "Edit": 3, "Write": 2}
	if len(a.ToolCounts) != len(want) {
		t.Fatalf("ToolCounts = %v, want %v", a.ToolCounts, want)
	}
	for tool, count := range want {
		if a.ToolCounts[tool] != count {
			t.Errorf("ToolCounts[%s] = %d, want %d", tool, a.ToolCounts[tool], count)
		}
	}
	if got, want := a.Tools(), 6; got != want {
		t.Errorf("Tools() = %d, want %d", got, want)
	}

	// Files come back most-touched first, as basenames: the full path is in LastFile, and a
	// paragraph full of absolute paths is unreadable.
	if len(a.Files) == 0 || a.Files[0] != "scrub-claude-fixture.py" {
		t.Errorf("Files = %v, want the most-touched file first", a.Files)
	}

	if got, want := len(a.Requests), 1; got != want {
		t.Errorf("Requests = %v, want %d", a.Requests, want)
	}
	if got, want := a.First.UTC().Format("15:04:05"), "15:20:20"; got != want {
		t.Errorf("First = %s, want %s", got, want)
	}
	if got, want := a.Last.UTC().Format("15:04:05"), "15:37:00"; got != want {
		t.Errorf("Last = %s, want %s", got, want)
	}
	if a.Truncated {
		t.Error("Truncated is set for a transcript read whole")
	}
}

// The window is the report window: a session that did nothing in it has an empty Activity,
// and gets the short honest paragraph rather than a summary of last week.
func TestActivityIsLimitedToTheWindow(t *testing.T) {
	s, err := ReadSession(filepath.Join("testdata", "tool-result-tail.jsonl"), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Activity.Empty() {
		t.Errorf("Activity = %+v, want empty for a window after everything happened", s.Activity)
	}
	// The status still comes from the tail, whatever the window: "what is it doing now" is
	// not a question about the last 24 hours.
	if s.Tail != session.TailToolResult {
		t.Errorf("Tail = %v, want the tail shape to survive a window that excludes everything", s.Tail)
	}
}

func TestActivityCountsToolErrors(t *testing.T) {
	s := read(t, "interrupted-by-user.jsonl")
	if s.Activity.Turns == 0 {
		t.Errorf("Turns = 0, want the assistant turns in the window")
	}
}

// A busy day would otherwise put a hundred files and fifty requests into the cache and the
// paragraph. Both are capped in the reader, where the cost is.
func TestActivityListsAreCapped(t *testing.T) {
	s := read(t, "tool-result-tail.jsonl")
	if len(s.Activity.Files) > maxFiles {
		t.Errorf("Files has %d entries, want at most %d", len(s.Activity.Files), maxFiles)
	}
	if len(s.Activity.Requests) > maxRequests {
		t.Errorf("Requests has %d entries, want at most %d", len(s.Activity.Requests), maxRequests)
	}
}
