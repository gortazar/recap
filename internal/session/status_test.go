package session

import (
	"testing"
	"time"
)

var now = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return now.Add(-d) }

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		s    Session
		live Liveness
		want Status
	}{
		{
			name: "live process and a transcript that just grew is running",
			s:    Session{Tail: TailToolResult, LastActivity: at(5 * time.Second)},
			live: Alive,
			want: StatusRunning,
		},
		{
			name: "live process, quiet for a while, last word was the agent's: waiting for you",
			s:    Session{Tail: TailAssistantText, LastActivity: at(10 * time.Minute)},
			live: Alive,
			want: StatusAwaitingInput,
		},
		{
			name: "live process sitting on an unanswered tool call is asking for permission",
			s:    Session{Tail: TailPendingTool, LastActivity: at(10 * time.Minute)},
			live: Alive,
			want: StatusAwaitingInput,
		},
		{
			name: "live process on a long tool call is still running",
			s:    Session{Tail: TailToolResult, LastActivity: at(10 * time.Minute)},
			live: Alive,
			want: StatusRunning,
		},
		{
			name: "live process after you interrupted it is back at the prompt",
			s:    Session{Tail: TailInterrupted, LastActivity: at(10 * time.Minute)},
			live: Alive,
			want: StatusAwaitingInput,
		},
		{
			name: "no process, stopped after answering: an ordinary stopping point",
			s:    Session{Tail: TailAssistantText, LastActivity: at(3 * time.Hour)},
			live: Dead,
			want: StatusIdle,
		},
		{
			name: "no process, stopped with a tool call unanswered: killed mid-work",
			s:    Session{Tail: TailPendingTool, LastActivity: at(3 * time.Hour)},
			live: Dead,
			want: StatusInterrupted,
		},
		{
			name: "no process, stopped just after a tool returned: killed mid-turn",
			s:    Session{Tail: TailToolResult, LastActivity: at(3 * time.Hour)},
			live: Dead,
			want: StatusInterrupted,
		},
		{
			name: "no process, your last request was never answered",
			s:    Session{Tail: TailUserRequest, LastActivity: at(3 * time.Hour)},
			live: Dead,
			want: StatusInterrupted,
		},
		{
			name: "no process, ended on an explicit interrupt marker",
			s:    Session{Tail: TailInterrupted, LastActivity: at(3 * time.Hour)},
			live: Dead,
			want: StatusInterrupted,
		},
		{
			name: "no process, ended on an API error",
			s:    Session{Tail: TailError, LastActivity: at(3 * time.Hour)},
			live: Dead,
			want: StatusInterrupted,
		},
		{
			name: "nothing recognisable in the transcript is not idle, it is unknown",
			s:    Session{Tail: TailUnknown, LastActivity: at(3 * time.Hour)},
			live: Dead,
			want: StatusUnknown,
		},
		{
			name: "a session recap could not parse is unknown",
			s:    Session{Unreadable: "no recognisable records", LastActivity: at(time.Minute)},
			live: Alive,
			want: StatusUnknown,
		},
		{
			name: "an explicitly completed session is finished",
			s:    Session{Completed: true, Tail: TailAssistantText, LastActivity: at(3 * time.Hour)},
			live: Dead,
			want: StatusFinished,
		},
		// Without process discovery recap must not guess: it degrades to recency, which is
		// honest about "was active a moment ago" and refuses to claim idleness otherwise.
		{
			name: "liveness unknown but active moments ago reads as running",
			s:    Session{Tail: TailToolResult, LastActivity: at(5 * time.Second)},
			live: LivenessUnknown,
			want: StatusRunning,
		},
		{
			name: "liveness unknown and long quiet after the agent spoke is unknown, not idle",
			s:    Session{Tail: TailAssistantText, LastActivity: at(3 * time.Hour)},
			live: LivenessUnknown,
			want: StatusUnknown,
		},
		{
			name: "liveness unknown but the transcript ends mid-work is still interrupted",
			s:    Session{Tail: TailPendingTool, LastActivity: at(3 * time.Hour)},
			live: LivenessUnknown,
			want: StatusInterrupted,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(&c.s, c.live, now); got != c.want {
				t.Errorf("Classify = %v, want %v", got, c.want)
			}
		})
	}
}

func TestStatusHasAnIconAndAWord(t *testing.T) {
	all := []Status{
		StatusRunning, StatusAwaitingInput, StatusIdle,
		StatusInterrupted, StatusFinished, StatusUnknown,
	}
	seenIcon := map[string]bool{}
	seenWord := map[string]bool{}
	for _, s := range all {
		if s.Icon() == "" || s.Word() == "" {
			t.Errorf("%v: icon %q word %q, want both non-empty", s, s.Icon(), s.Word())
		}
		if seenIcon[s.Icon()] {
			t.Errorf("%v: icon %q is not unique", s, s.Icon())
		}
		if seenWord[s.Word()] {
			t.Errorf("%v: word %q is not unique", s, s.Word())
		}
		seenIcon[s.Icon()] = true
		seenWord[s.Word()] = true
	}
}
