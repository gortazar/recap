package session

import (
	"fmt"
	"strings"
	"time"
)

// maxFilesNamed is how many files the paragraph lists before summarising the rest as a
// count. Three keeps the sentence readable; the count keeps it honest.
const maxFilesNamed = 2

// maxToolsNamed is how many tools get a name and a number.
const maxToolsNamed = 3

// Report is the paragraph printed under a session line: two or three sentences about what
// the session did over the report window.
//
// It is a pure function of the Activity a reader collected plus the session's status, and it
// only ever says what a transcript can show — requests, tool calls, files, turns, errors,
// timings. It never claims a result. "Ran the test suite" is not something a transcript
// proves; "ran go test eleven times" is.
//
// A session recap could not parse gets no paragraph at all: the status line already says so,
// and repeating it in prose would be noise.
func Report(s *Session, st Status, now time.Time) string {
	if s.Unreadable != "" {
		return ""
	}
	a := s.Activity
	if a.Empty() {
		return "Nothing in the window; last active " + ageWords(now.Sub(s.LastActivity)) + "."
	}

	// The span is a fragment — "Over 7h" — so it leads whichever clause comes first and
	// lets that clause finish the sentence. Without one it would be a sentence of its own,
	// which reads like a stub.
	var bodies []string
	if requests := requestsPhrase(a); requests != "" {
		bodies = append(bodies, requests)
	}
	// With no tool calls, the error sentence below already accounts for the turns, and
	// "1 turn, no tool calls. 1 turn ended in an error." says turn twice.
	if work := workBody(a); work != "" && !(a.Tools() == 0 && a.Errors > 0) {
		bodies = append(bodies, work)
	}

	var sentences []string
	switch span := spanPhrase(a); {
	case span != "" && len(bodies) > 0:
		sentences = append(sentences, span+": "+bodies[0]+".")
		bodies = bodies[1:]
	case span != "":
		sentences = append(sentences, span+".")
	}
	for _, body := range bodies {
		sentences = append(sentences, capitalise(body)+".")
	}

	if a.Errors > 0 {
		sentences = append(sentences, fmt.Sprintf("%s ended in an error.", plural(a.Errors, "turn")))
	}
	sentences = append(sentences, endSentence(s, st, now))
	return strings.Join(sentences, " ")
}

func spanPhrase(a Activity) string {
	if a.Truncated && !a.First.IsZero() {
		// Say what was really covered rather than implying the whole window was read.
		return "Since " + a.First.Format("15:04") + " (earlier activity not read)"
	}
	if a.First.IsZero() || a.Last.IsZero() || !a.Last.After(a.First) {
		return ""
	}
	return "Over " + shortDuration(a.Last.Sub(a.First))
}

func requestsPhrase(a Activity) string {
	switch len(a.Requests) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("one request, %q", trimToClause(a.Requests[0]))
	default:
		return fmt.Sprintf("%d requests, ending %q", len(a.Requests), trimToClause(a.Requests[len(a.Requests)-1]))
	}
}

// workBody is what the session actually did: tool calls, which tools, which files. It is a
// clause rather than a sentence, so the caller can either lead with the span or capitalise
// it and stand it alone.
func workBody(a Activity) string {
	total := a.Tools()
	if total == 0 {
		if a.Turns == 0 {
			return ""
		}
		// Worth saying explicitly: a session that only talked looks identical to a broken
		// reader otherwise.
		return fmt.Sprintf("%s, no tool calls", plural(a.Turns, "turn"))
	}

	body := fmt.Sprintf("%s — %s", plural(total, "tool call"), toolsPhrase(a))
	if files := filesPhrase(a); files != "" {
		body += " — touching " + files
	}
	return body
}

func toolsPhrase(a Activity) string {
	top := a.TopTools(maxToolsNamed)
	if len(top) == 1 {
		// "1 tool call — all Bash" is odd; there is nothing for "all" to contrast with.
		if top[0].Count == 1 {
			return top[0].Name
		}
		return "all " + top[0].Name
	}
	parts := make([]string, 0, len(top))
	for _, t := range top {
		parts = append(parts, fmt.Sprintf("%s (%d)", t.Name, t.Count))
	}
	joined := strings.Join(parts, ", ")
	if len(a.ToolCounts) > len(top) {
		// "mostly" is only true when something was left out.
		return "mostly " + joined
	}
	return joined
}

func filesPhrase(a Activity) string {
	switch {
	case len(a.Files) == 0:
		return ""
	case len(a.Files) <= maxFilesNamed:
		return joinWords(a.Files)
	default:
		named := a.Files[:maxFilesNamed]
		rest := len(a.Files) - maxFilesNamed
		return strings.Join(named, ", ") + fmt.Sprintf(" and %d other file%s", rest, suffix(rest))
	}
}

// endSentence says how the session stands now, in the same vocabulary as the status line.
func endSentence(s *Session, st Status, now time.Time) string {
	when, isClock := whenWords(s.LastActivity, now)
	// "at 20:41" but "24h ago", never "at 24h ago".
	at := when
	if isClock {
		at = "at " + when
	}
	switch st {
	case StatusRunning:
		return "Still going."
	case StatusAwaitingInput:
		return "Waiting since " + when + "."
	case StatusIdle:
		return "Idle since " + when + "."
	case StatusInterrupted:
		return "Stopped mid-work " + at + "."
	case StatusFinished:
		return "Finished " + at + "."
	default:
		return "Last seen " + when + "."
	}
}

// whenWords is a clock time for something that happened today, and an age for anything
// older: "20:41" three days later would be a lie by omission. The bool says which it is, so
// the caller can put "at" in front of a time but not in front of an age.
func whenWords(t, now time.Time) (string, bool) {
	if t.IsZero() {
		return "an unknown time", false
	}
	if now.Sub(t) < 24*time.Hour {
		return t.Format("15:04"), true
	}
	return ageWords(now.Sub(t)), false
}

func ageWords(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments ago"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// shortDuration is the length of a span, in the largest unit that stays honest.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func plural(n int, noun string) string {
	return fmt.Sprintf("%d %s%s", n, noun, suffix(n))
}

func suffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func joinWords(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}
