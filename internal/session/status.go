package session

import "time"

// Status is what recap reports about a session. Each value has a rule behind it, so the icon
// is a fact rather than a vibe; the rules are all in Classify.
type Status int

const (
	// StatusUnknown — recap cannot tell. Deliberately its own state: reporting an
	// unreadable or unrecognisable session as "idle" would be a claim recap cannot back up.
	StatusUnknown Status = iota
	// StatusRunning — an agent is at work right now.
	StatusRunning
	// StatusAwaitingInput — the agent stopped and the next move is yours: it answered, asked
	// a question, or is sitting on a permission prompt.
	StatusAwaitingInput
	// StatusIdle — nothing is running and the session stopped at an ordinary point.
	StatusIdle
	// StatusInterrupted — nothing is running and the transcript ends mid-work. This is the
	// closed-the-laptop case.
	StatusInterrupted
	// StatusFinished — the session ended after explicitly completing what it was asked.
	// Reserved for an explicit marker: recap has no way to judge "the task was done", so it
	// never infers this.
	StatusFinished
)

// ActiveWindow is how recently a transcript must have grown for recap to call a session
// active. Longer than a slow model turn, shorter than a coffee break.
const ActiveWindow = 90 * time.Second

// Liveness is what process discovery could tell us about a session.
type Liveness int

const (
	// LivenessUnknown — process discovery is unavailable or inconclusive on this platform.
	LivenessUnknown Liveness = iota
	// Alive — an agent process is attached to this session's working directory.
	Alive
	// Dead — no agent process is attached.
	Dead
)

var statusInfo = map[Status]struct{ icon, word, describe string }{
	StatusRunning:       {"🟢", "running", "an agent is working on it right now"},
	StatusAwaitingInput: {"🟡", "waiting", "stopped and waiting for you: a question, an answer, or a permission prompt"},
	StatusIdle:          {"⚪", "idle", "not running; it stopped at an ordinary point"},
	StatusInterrupted:   {"🔴", "interrupted", "not running and the transcript ends mid-work"},
	StatusFinished:      {"✅", "finished", "ended after explicitly completing what it was asked"},
	StatusUnknown:       {"❓", "unclear", "recap could not tell — an unreadable transcript, or no way to check what is running"},
}

func (s Status) Icon() string     { return statusInfo[s].icon }
func (s Status) Word() string     { return statusInfo[s].word }
func (s Status) Describe() string { return statusInfo[s].describe }
func (s Status) String() string   { return s.Word() }

// Statuses lists every status in the order the legend prints them.
func Statuses() []Status {
	return []Status{
		StatusRunning, StatusAwaitingInput, StatusIdle,
		StatusInterrupted, StatusFinished, StatusUnknown,
	}
}

// Classify applies the status rules to one session. It is a pure function of the transcript
// tail, what process discovery found, and the clock, so the whole vocabulary is testable
// without any live agents.
func Classify(s *Session, live Liveness, now time.Time) Status {
	if s.Unreadable != "" {
		return StatusUnknown
	}
	if s.Completed {
		return StatusFinished
	}

	active := !s.LastActivity.IsZero() && now.Sub(s.LastActivity) < ActiveWindow

	switch live {
	case Alive:
		// A transcript that grew moments ago settles it, whatever shape the tail has: the
		// agent is mid-flight and the tail is just wherever it happens to be.
		if active {
			return StatusRunning
		}
		switch s.Tail {
		case TailAssistantText, TailInterrupted, TailError:
			// The agent has had its say and the process is still up: it is at the prompt.
			return StatusAwaitingInput
		case TailPendingTool:
			// A live process with an unanswered tool call is almost always a permission
			// prompt waiting on the user.
			return StatusAwaitingInput
		case TailToolResult, TailUserRequest:
			// Mid-turn with the process alive: a long tool call or a long think.
			return StatusRunning
		default:
			return StatusUnknown
		}

	case Dead:
		switch s.Tail {
		case TailAssistantText:
			return StatusIdle
		case TailPendingTool, TailToolResult, TailUserRequest, TailInterrupted, TailError:
			return StatusInterrupted
		default:
			return StatusUnknown
		}

	default:
		// No process discovery. Fall back to recency, which is the honest degradation: recap
		// can say "this was moving a moment ago" and it can still recognise a transcript
		// that ends mid-work, but it must not claim a quiet session is merely idle.
		if active {
			return StatusRunning
		}
		switch s.Tail {
		case TailPendingTool, TailToolResult, TailUserRequest, TailInterrupted, TailError:
			return StatusInterrupted
		default:
			return StatusUnknown
		}
	}
}
