package session

import "testing"

func TestSentence(t *testing.T) {
	cases := []struct {
		name string
		s    Session
		st   Status
		want string
	}{
		{
			name: "interrupted mid tool names the tool and what was asked",
			s: Session{
				LastRequest: "Fix the flaky test in the scheduler",
				Tail:        TailPendingTool,
				PendingTool: "Bash",
			},
			st:   StatusInterrupted,
			want: `Asked to "Fix the flaky test in the scheduler" — interrupted mid-Bash.`,
		},
		{
			name: "a pending tool on a live session is a permission prompt",
			s: Session{
				LastRequest: "Draft the plan",
				Tail:        TailPendingTool,
				PendingTool: "Write",
			},
			st:   StatusAwaitingInput,
			want: `Asked to "Draft the plan" — waiting for you to approve Write.`,
		},
		{
			name: "waiting after prose is waiting for you, with no tool named",
			s:    Session{LastRequest: "Explain the analytics numbers", Tail: TailAssistantText},
			st:   StatusAwaitingInput,
			want: `Asked to "Explain the analytics numbers" — answered, waiting for you.`,
		},
		{
			name: "running says what tool it is on",
			s:    Session{LastRequest: "Run the suite", Tail: TailToolResult, LastTool: "Bash"},
			st:   StatusRunning,
			want: `Asked to "Run the suite" — working, last used Bash.`,
		},
		{
			name: "an explicit progress marker is worth more than the tool",
			s: Session{
				LastRequest: "Build the reader",
				Tail:        TailToolResult,
				LastTool:    "Edit",
				TodoDone:    3,
				TodoTotal:   7,
			},
			st:   StatusRunning,
			want: `Asked to "Build the reader" — working, 3 of 7 done, last used Edit.`,
		},
		{
			name: "you interrupted it yourself",
			s:    Session{LastRequest: "Rewrite the docs", Tail: TailInterrupted},
			st:   StatusInterrupted,
			want: `Asked to "Rewrite the docs" — you interrupted it.`,
		},
		{
			name: "a request that was never answered",
			s:    Session{LastRequest: "Add a --json flag", Tail: TailUserRequest},
			st:   StatusInterrupted,
			want: `Asked to "Add a --json flag" — stopped before answering.`,
		},
		{
			name: "an API error is reported as one",
			s:    Session{LastRequest: "Summarise the logs", Tail: TailError},
			st:   StatusInterrupted,
			want: `Asked to "Summarise the logs" — stopped on an error.`,
		},
		{
			name: "idle is an ordinary stopping point",
			s:    Session{LastRequest: "Review the diff", Tail: TailAssistantText},
			st:   StatusIdle,
			want: `Asked to "Review the diff" — done for now, nothing running.`,
		},
		{
			name: "finished",
			s:    Session{LastRequest: "Ship it", Completed: true},
			st:   StatusFinished,
			want: `Asked to "Ship it" — finished.`,
		},
		{
			name: "an unreadable transcript says so rather than guessing",
			s:    Session{Unreadable: "no recognisable records"},
			st:   StatusUnknown,
			want: `Unreadable transcript: no recognisable records.`,
		},
		{
			name: "unknown with nothing to go on",
			s:    Session{Tail: TailUnknown},
			st:   StatusUnknown,
			want: `Nothing recognisable in the transcript.`,
		},
		{
			name: "only the first sentence of a long request survives",
			s: Session{
				LastRequest: "Fix the parser. Then add tests, update the README and think about\nthe error messages.",
				Tail:        TailAssistantText,
			},
			st:   StatusIdle,
			want: `Asked to "Fix the parser" — done for now, nothing running.`,
		},
		{
			name: "a long single-clause request is cut on a word boundary",
			s: Session{
				LastRequest: "Rework the whole session discovery layer so that it reads the process table only once and caches",
				Tail:        TailAssistantText,
			},
			st:   StatusIdle,
			want: `Asked to "Rework the whole session discovery layer so that it reads the…" — done for now, nothing running.`,
		},
		{
			name: "with no request at all the now clause stands alone",
			s:    Session{Tail: TailPendingTool, PendingTool: "Bash"},
			st:   StatusInterrupted,
			want: `Interrupted mid-Bash.`,
		},
		{
			name: "a one-word nudge is not a useful recap, so the title stands in",
			s: Session{
				LastRequest: "Yes",
				Title:       "Rework the CI workflow",
				Tail:        TailAssistantText,
			},
			st:   StatusIdle,
			want: `Asked to "Rework the CI workflow" — done for now, nothing running.`,
		},
		{
			name: "a one-word nudge is still better than nothing",
			s:    Session{LastRequest: "Yes", Tail: TailAssistantText},
			st:   StatusIdle,
			want: `Asked to "Yes" — done for now, nothing running.`,
		},
		{
			name: "punctuation left dangling by the writer is trimmed",
			s:    Session{LastRequest: "Check the realtime overview ..", Tail: TailAssistantText},
			st:   StatusIdle,
			want: `Asked to "Check the realtime overview" — done for now, nothing running.`,
		},
		{
			name: "the agent's own title stands in for a missing request",
			s:    Session{Title: "Wire up the GNOME extension", Tail: TailAssistantText},
			st:   StatusIdle,
			want: `Asked to "Wire up the GNOME extension" — done for now, nothing running.`,
		},
		{
			name: "the first request stands in when there is no later one",
			s:    Session{FirstRequest: "Start the migration", Tail: TailAssistantText},
			st:   StatusIdle,
			want: `Asked to "Start the migration" — done for now, nothing running.`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sentence(&c.s, c.st); got != c.want {
				t.Errorf("Sentence =\n  %q\nwant\n  %q", got, c.want)
			}
		})
	}
}
