// Package session holds recap's agent-independent view of a coding session: what a reader
// extracts from a transcript, and the rules that turn that into a status and a sentence.
//
// Nothing here knows about JSONL or SQLite. Readers translate into these types, so adding a
// third agent means adding a reader and nothing else.
package session

import "time"

// Agent is the display name of the tool that produced a session.
type Agent string

const (
	AgentClaude   Agent = "Claude Code"
	AgentOpencode Agent = "opencode"
)

// Tail describes the last thing that happened in a transcript. It is deliberately about the
// *shape* of the transcript, not about the status: two agents can produce the same tail and
// the status rules are applied once, in one place.
type Tail int

const (
	// TailUnknown — nothing recognisable at the end of the transcript.
	TailUnknown Tail = iota
	// TailAssistantText — the agent finished its turn with prose. Either it answered and is
	// waiting, or it asked a question.
	TailAssistantText
	// TailPendingTool — the agent asked for a tool and no result ever arrived: the classic
	// killed-mid-work shape.
	TailPendingTool
	// TailToolResult — a tool result is the last event, so the agent was mid-turn.
	TailToolResult
	// TailInterrupted — an explicit interrupt marker written by the agent.
	TailInterrupted
	// TailUserRequest — the user asked for something the agent never answered.
	TailUserRequest
	// TailError — the turn ended in an API or tool error.
	TailError
)

var tailNames = map[Tail]string{
	TailUnknown:       "unknown",
	TailAssistantText: "assistant-text",
	TailPendingTool:   "pending-tool",
	TailToolResult:    "tool-result",
	TailInterrupted:   "interrupted",
	TailUserRequest:   "user-request",
	TailError:         "error",
}

func (t Tail) String() string {
	if s, ok := tailNames[t]; ok {
		return s
	}
	return "unknown"
}

// Session is one agent session as recap understands it. Every field is best-effort: the
// on-disk formats are undocumented and drift, so a reader that cannot find something leaves
// it zero rather than failing.
type Session struct {
	ID     string
	Agent  Agent
	Dir    string // the session's working directory
	Branch string
	Model  string
	// Version of the agent that wrote the transcript, when recorded. Useful when a format
	// change is suspected.
	Version string
	// Title is the agent's own name for the session, when it keeps one.
	Title string

	Started      time.Time
	LastActivity time.Time

	FirstRequest string
	LastRequest  string
	LastText     string // the agent's last prose

	Tail Tail
	// PendingTool is the name of the tool call left without a result, when Tail is
	// TailPendingTool.
	PendingTool string
	// LastTool is the last tool the agent used, and LastFile the file it last touched.
	LastTool string
	LastFile string

	// TodoDone/TodoTotal come from an explicit progress marker the agent left, when there is
	// one. Zero total means "no marker", not "no work".
	TodoDone  int
	TodoTotal int

	// Completed is set only when the agent left an explicit marker that the session ended
	// having finished what it was asked. recap never infers it: without a model there is no
	// reliable "the task was done" signal, and claiming success wrongly is worse than
	// saying nothing.
	Completed bool

	// Source is where this came from, for --verbose and for error messages.
	Source string
	// Unreadable explains why a session could not be parsed. A session with this set is
	// still reported — as a line saying so — rather than dropped.
	Unreadable string
}
