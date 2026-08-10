package session

import (
	"fmt"
	"strings"
	"unicode"
)

// maxRequest is how much of a request survives into the sentence. Long enough to identify
// the task, short enough that a dozen projects still fit on one screen.
const maxRequest = 64

// Sentence is the one-line recap: what the session was asked to do, and where it stands.
// It is assembled from the transcript alone — no model, no network — so recap stays instant
// and works offline. Everything it says is traceable to a field a reader extracted.
func Sentence(s *Session, st Status) string {
	if s.Unreadable != "" {
		return "Unreadable transcript: " + s.Unreadable + "."
	}

	now := nowClause(s, st)
	was := requestClause(s)
	if was == "" {
		if st == StatusUnknown {
			return "Nothing recognisable in the transcript."
		}
		return capitalise(now) + "."
	}
	return fmt.Sprintf("Asked to %q — %s.", was, now)
}

// minRequest is the length below which a request says nothing useful. "Yes", "go on" and
// "continue" are real requests, but as a recap line they are worse than the session's title.
const minRequest = 12

// requestClause picks what the session was asked to do. The most recent request is the most
// informative; the agent's own title and the opening request are the fallbacks, and they
// also stand in when the last request was a one-word nudge.
func requestClause(s *Session) string {
	candidates := []string{s.LastRequest, s.Title, s.FirstRequest}
	var first string
	for _, candidate := range candidates {
		c := trimToClause(candidate)
		if c == "" {
			continue
		}
		if len([]rune(c)) >= minRequest {
			return c
		}
		if first == "" {
			first = c
		}
	}
	return first
}

func nowClause(s *Session, st Status) string {
	switch st {
	case StatusRunning:
		parts := []string{"working"}
		if p := progress(s); p != "" {
			parts = append(parts, p)
		}
		if s.LastTool != "" {
			parts = append(parts, "last used "+s.LastTool)
		}
		return strings.Join(parts, ", ")

	case StatusAwaitingInput:
		if s.Tail == TailPendingTool && s.PendingTool != "" {
			return "waiting for you to approve " + s.PendingTool
		}
		if s.Tail == TailInterrupted {
			return "you interrupted it, waiting for you"
		}
		return "answered, waiting for you"

	case StatusIdle:
		return "done for now, nothing running"

	case StatusInterrupted:
		base := interruptedClause(s)
		if p := progress(s); p != "" {
			return base + ", " + p
		}
		return base

	case StatusFinished:
		return "finished"

	default:
		return "state unclear"
	}
}

func interruptedClause(s *Session) string {
	switch s.Tail {
	case TailPendingTool:
		if s.PendingTool != "" {
			return "interrupted mid-" + s.PendingTool
		}
		return "interrupted mid-work"
	case TailInterrupted:
		return "you interrupted it"
	case TailUserRequest:
		return "stopped before answering"
	case TailError:
		return "stopped on an error"
	default:
		return "interrupted mid-turn"
	}
}

// progress reports an explicit marker the agent left, never an estimate of its own.
func progress(s *Session) string {
	if s.TodoTotal <= 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d done", s.TodoDone, s.TodoTotal)
}

// trimToClause reduces a request to its first sentence, cut on a word boundary if it is
// still too long. Requests run to paragraphs; the sentence has room for a clause.
func trimToClause(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// First line, then first sentence within it.
	if i := strings.IndexAny(text, "\n\r"); i >= 0 {
		text = text[:i]
	}
	for i, r := range text {
		if r == '.' || r == '?' || r == '!' {
			// Only a sentence end if what follows is a space or nothing, so that
			// "ideas/recap/PLAN.md" and "3.5" survive intact.
			rest := text[i+len(string(r)):]
			if rest == "" || unicode.IsSpace(rune(rest[0])) {
				text = text[:i]
				break
			}
		}
	}
	text = strings.TrimSpace(text)

	// Trailing punctuation left by the cut, or by the way the request was written, reads as
	// a typo in the middle of the sentence recap builds around it.
	text = strings.TrimRight(text, " .,;:-")

	runes := []rune(text)
	if len(runes) <= maxRequest {
		return text
	}
	cut := string(runes[:maxRequest])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
