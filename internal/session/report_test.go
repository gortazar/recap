package session

import (
	"strings"
	"testing"
	"time"
)

func clock(h, m int) time.Time {
	return time.Date(2026, 8, 13, h, m, 0, 0, time.UTC)
}

var reportNow = clock(21, 0)

func TestReport(t *testing.T) {
	cases := []struct {
		name string
		s    Session
		st   Status
		want string
	}{
		{
			name: "a full day's work, the shape the whole feature exists for",
			s: Session{
				LastActivity: clock(20, 41),
				Activity: Activity{
					Requests: []string{
						"start on the release workflow",
						"make the release workflow verify the checksum",
					},
					ToolCounts: map[string]int{"Bash": 61, "Edit": 28, "Read": 21, "Write": 8},
					Files:      []string{"release-build.sh", "install.sh", "a.go", "b.go", "c.go"},
					Turns:      44,
					Errors:     2,
					First:      clock(13, 41),
					Last:       clock(20, 41),
				},
			},
			st: StatusIdle,
			want: `Over 7h: 2 requests, ending "make the release workflow verify the checksum". ` +
				`118 tool calls — mostly Bash (61), Edit (28), Read (21) — touching ` +
				`release-build.sh, install.sh and 3 other files. 2 turns ended in an error. ` +
				`Idle since 20:41.`,
		},
		{
			name: "one request reads as one request",
			s: Session{
				LastActivity: clock(20, 0),
				Activity: Activity{
					Requests:   []string{"fix the flaky test"},
					ToolCounts: map[string]int{"Bash": 3},
					Files:      []string{"scheduler.go"},
					Turns:      2,
					First:      clock(19, 30),
					Last:       clock(20, 0),
				},
			},
			st: StatusIdle,
			want: `Over 30m: one request, "fix the flaky test". 3 tool calls — all Bash — ` +
				`touching scheduler.go. Idle since 20:00.`,
		},
		{
			name: "no request in the window, just work",
			s: Session{
				LastActivity: clock(20, 0),
				Activity: Activity{
					ToolCounts: map[string]int{"Bash": 2, "Read": 2},
					Turns:      3,
					First:      clock(19, 0),
					Last:       clock(20, 0),
				},
			},
			st:   StatusIdle,
			want: `Over 1h: 4 tool calls — Bash (2), Read (2). Idle since 20:00.`,
		},
		{
			name: "a session that only talked, with no tools at all",
			s: Session{
				LastActivity: clock(20, 0),
				Activity: Activity{
					Requests: []string{"explain the analytics numbers"},
					Turns:    2,
					First:    clock(19, 50),
					Last:     clock(20, 0),
				},
			},
			st:   StatusAwaitingInput,
			want: `Over 10m: one request, "explain the analytics numbers". 2 turns, no tool calls. Waiting since 20:00.`,
		},
		{
			name: "still running says so instead of naming a time",
			s: Session{
				LastActivity: clock(20, 59),
				Activity: Activity{
					Requests:   []string{"run the benchmark suite"},
					ToolCounts: map[string]int{"Bash": 12},
					Turns:      6,
					First:      clock(20, 0),
					Last:       clock(20, 59),
				},
			},
			st:   StatusRunning,
			want: `Over 59m: one request, "run the benchmark suite". 12 tool calls — all Bash. Still going.`,
		},
		{
			name: "an interrupted session says where it stopped",
			s: Session{
				LastActivity: clock(18, 5),
				Activity: Activity{
					Requests:   []string{"deploy to staging"},
					ToolCounts: map[string]int{"Bash": 4},
					Turns:      3,
					First:      clock(18, 0),
					Last:       clock(18, 5),
				},
			},
			st:   StatusInterrupted,
			want: `Over 5m: one request, "deploy to staging". 4 tool calls — all Bash. Stopped mid-work at 18:05.`,
		},
		{
			name: "a truncated read says what it actually covers",
			s: Session{
				LastActivity: clock(20, 41),
				Activity: Activity{
					Requests:   []string{"keep going"},
					ToolCounts: map[string]int{"Bash": 200},
					Turns:      90,
					First:      clock(6, 10),
					Last:       clock(20, 41),
					Truncated:  true,
				},
			},
			st: StatusIdle,
			want: `Since 06:10 (earlier activity not read): one request, "keep going". ` +
				`200 tool calls — all Bash. Idle since 20:41.`,
		},
		{
			name: "nothing in the window is one honest short sentence",
			s: Session{
				LastActivity: reportNow.Add(-72 * time.Hour),
				Activity:     Activity{},
			},
			st:   StatusIdle,
			want: `Nothing in the window; last active 3d ago.`,
		},
		{
			name: "an unreadable transcript has no paragraph to write",
			s:    Session{Unreadable: "no recognisable records"},
			st:   StatusUnknown,
			want: "",
		},
		{
			name: "a long request is clipped in the paragraph too",
			s: Session{
				LastActivity: clock(20, 0),
				Activity: Activity{
					Requests: []string{
						"Rework the whole session discovery layer so that it reads the process table only once and caches the result",
					},
					Turns: 1,
					First: clock(19, 0),
					Last:  clock(20, 0),
				},
			},
			st: StatusIdle,
			want: `Over 1h: one request, "Rework the whole session discovery layer so that it reads the…". ` +
				`1 turn, no tool calls. Idle since 20:00.`,
		},
		{
			name: "yesterday's session gets an age, not an ambiguous clock time",
			s: Session{
				LastActivity: reportNow.Add(-30 * time.Hour),
				Activity: Activity{
					Requests:   []string{"tidy the docs"},
					ToolCounts: map[string]int{"Edit": 2},
					Turns:      1,
					First:      reportNow.Add(-31 * time.Hour),
					Last:       reportNow.Add(-30 * time.Hour),
				},
			},
			st:   StatusIdle,
			want: `Over 1h: one request, "tidy the docs". 2 tool calls — all Edit. Idle since 30h ago.`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Report(&c.s, c.st, reportNow); got != c.want {
				t.Errorf("Report =\n  %q\nwant\n  %q", got, c.want)
			}
		})
	}
}

// The paragraph must never claim something the transcript cannot show. This is the rule the
// whole feature turns on, so it gets its own test rather than living in a comment.
func TestReportNeverClaimsAResult(t *testing.T) {
	s := Session{
		LastActivity: clock(20, 0),
		Activity: Activity{
			Requests:   []string{"make the tests pass"},
			ToolCounts: map[string]int{"Bash": 11},
			Turns:      5,
			First:      clock(19, 0),
			Last:       clock(20, 0),
		},
	}
	got := Report(&s, StatusIdle, reportNow)
	for _, forbidden := range []string{
		"passed", "failed", "fixed", "succeeded", "working now", "green",
	} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Errorf("paragraph claims a result recap cannot see (%q): %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "11 tool calls") {
		t.Errorf("paragraph does not report the calls it can see: %s", got)
	}
}

func TestTopToolsIsStableAndOrdered(t *testing.T) {
	a := Activity{ToolCounts: map[string]int{"Read": 5, "Bash": 5, "Edit": 9, "Write": 1}}
	got := a.TopTools(3)
	want := []ToolCount{{"Edit", 9}, {"Bash", 5}, {"Read", 5}}
	if len(got) != len(want) {
		t.Fatalf("TopTools = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TopTools = %v, want %v (ties break by name, so runs agree)", got, want)
		}
	}
}
