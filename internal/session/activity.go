package session

import "time"

// Activity is what a session did over the report window: the observable facts a reader can
// pull out of a transcript without interpreting anything.
//
// Every field is best-effort, like the rest of Session. A reader that cannot fill one
// leaves it zero and the paragraph degrades to what is known — it never invents.
type Activity struct {
	// Requests the user made in the window, oldest first, each clipped. Readers cap this
	// (at the last few) so the cache does not grow without bound.
	Requests []string
	// ToolCounts is how many times each tool was called.
	ToolCounts map[string]int
	// Files touched, most frequently first, capped by the reader.
	Files []string
	// Turns is the number of assistant turns seen, and Errors how many of them ended in an
	// error.
	Turns  int
	Errors int
	// First and Last are the timestamps actually seen, which is not the same as the window:
	// a session that stopped at noon has a Last of noon however wide the window is.
	First time.Time
	Last  time.Time
	// Truncated is set when the reader hit its cap before reaching the start of the window.
	// The paragraph then says what it really covers rather than implying it saw the lot.
	Truncated bool
}

// Empty reports whether anything at all happened in the window.
func (a Activity) Empty() bool {
	return a.Turns == 0 && len(a.ToolCounts) == 0 && len(a.Requests) == 0
}

// Tools is the total number of tool calls.
func (a Activity) Tools() int {
	n := 0
	for _, c := range a.ToolCounts {
		n += c
	}
	return n
}

// TopTools returns the most-used tools, most first, at most n of them. Ties break by name so
// the output is stable between runs.
func (a Activity) TopTools(n int) []ToolCount {
	out := make([]ToolCount, 0, len(a.ToolCounts))
	for name, count := range a.ToolCounts {
		out = append(out, ToolCount{Name: name, Count: count})
	}
	sortToolCounts(out)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// ToolCount is one entry of TopTools.
type ToolCount struct {
	Name  string
	Count int
}

func sortToolCounts(t []ToolCount) {
	// Insertion sort: these lists are a handful of entries long, and this keeps the
	// tie-breaking rule visible in one place.
	for i := 1; i < len(t); i++ {
		for j := i; j > 0; j-- {
			if t[j].Count > t[j-1].Count || (t[j].Count == t[j-1].Count && t[j].Name < t[j-1].Name) {
				t[j], t[j-1] = t[j-1], t[j]
				continue
			}
			break
		}
	}
}
