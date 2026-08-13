package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gortazar/recap/internal/session"
)

// writeLargeTranscript generates a transcript of roughly the given size covering the given
// span, ending now. Real transcripts reach tens of megabytes; this is the shape that would
// make recap slow if the read were not capped.
func writeLargeTranscript(t *testing.T, sizeBytes int, span time.Duration, end time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "large.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Padding stands in for what actually makes transcripts huge: tool output and file
	// contents echoed back into the conversation.
	padding := strings.Repeat("x", 900)

	written := 0
	turn := 0
	for written < sizeBytes {
		// Spread the records evenly across the span, oldest first.
		at := end.Add(-span + time.Duration(float64(span)*float64(written)/float64(sizeBytes)))
		stamp := at.UTC().Format(time.RFC3339Nano)

		var line string
		switch turn % 3 {
		case 0:
			line = fmt.Sprintf(`{"type":"user","sessionId":"big","cwd":"/home/user/git/big","gitBranch":"main","version":"2.1.0","timestamp":%q,"message":{"role":"user","content":[{"type":"text","text":"request %d"}]}}`, stamp, turn)
		case 1:
			line = fmt.Sprintf(`{"type":"assistant","sessionId":"big","cwd":"/home/user/git/big","timestamp":%q,"message":{"role":"assistant","model":"claude-opus-5","content":[{"type":"tool_use","id":"t%d","name":"Bash","input":{"file_path":"/home/user/git/big/f%d.go","padding":%q}}]}}`, stamp, turn, turn%7, padding)
		default:
			line = fmt.Sprintf(`{"type":"user","sessionId":"big","cwd":"/home/user/git/big","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t%d","content":%q}]}}`, stamp, turn-1, padding)
		}
		n, err := f.WriteString(line + "\n")
		if err != nil {
			t.Fatal(err)
		}
		written += n
		turn++
	}
	return path
}

// recap's identity is "an answer now". Reading a day instead of a tail multiplies the work
// per session by the number of transcripts, so the cap gets an assertion rather than a hope.
func TestLargeTranscriptStaysWithinTheCap(t *testing.T) {
	now := time.Now()
	path := writeLargeTranscript(t, 8<<20, 24*time.Hour, now)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 7<<20 {
		t.Fatalf("generated transcript is %d bytes, want a realistic ~8 MiB", info.Size())
	}

	start := time.Now()
	s, err := ReadSession(path, now.Add(-24*time.Hour))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}

	// The whole point of the cap: a 24h window over an 8 MiB transcript does not read 8 MiB.
	if !s.Activity.Truncated {
		t.Error("Truncated is not set, so the read covered the whole window — the cap did not bite")
	}
	// A bound with room for a slow shared runner, but far below "read and parse it all".
	if elapsed > 500*time.Millisecond {
		t.Errorf("reading one 8 MiB transcript took %v, want under 500ms", elapsed)
	}
	t.Logf("8 MiB transcript, 24h window: %v", elapsed)

	// Truncated must not mean broken: the status and the counts are still there.
	if s.Activity.Tools() == 0 {
		t.Error("no tool calls counted in a transcript that is mostly tool calls")
	}
	if s.Dir != "/home/user/git/big" {
		t.Errorf("Dir = %q, want the session's directory", s.Dir)
	}
	if s.Activity.First.IsZero() {
		t.Error("First is zero, so the paragraph cannot say what it covers")
	}
}

// A window that the transcript does not fill is covered without reading to the cap, which is
// the common case: most sessions are far smaller than a day of work.
func TestASmallWindowStopsEarly(t *testing.T) {
	now := time.Now()
	path := writeLargeTranscript(t, 8<<20, 24*time.Hour, now)

	s, err := ReadSession(path, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if s.Activity.Truncated {
		t.Error("Truncated is set for a window the read covered comfortably")
	}
	if s.Activity.First.Before(now.Add(-90 * time.Minute)) {
		t.Errorf("First = %v, want activity limited to roughly the last 30 minutes", s.Activity.First)
	}
}

// --all over a huge transcript is a legitimate request and a very expensive one. The cap has
// to bound it even though the window does not.
func TestNoWindowIsStillCapped(t *testing.T) {
	now := time.Now()
	path := writeLargeTranscript(t, 8<<20, 24*time.Hour, now)

	start := time.Now()
	s, err := ReadSession(path, time.Time{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Activity.Truncated {
		t.Error("Truncated is not set, so --all read the whole 8 MiB")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("--all over an 8 MiB transcript took %v, want under 500ms", elapsed)
	}
}

// The status rules walk every record the reader returns, and they must not change because
// the window got wider. Same transcript, three windows, same answer.
func TestTheWindowDoesNotChangeTheStatus(t *testing.T) {
	now := time.Now()
	path := writeLargeTranscript(t, 2<<20, 6*time.Hour, now)

	var first *struct {
		tail session.Tail
		dir  string
	}
	for _, since := range []time.Time{{}, now.Add(-time.Hour), now.Add(-24 * time.Hour)} {
		s, err := ReadSession(path, since)
		if err != nil {
			t.Fatal(err)
		}
		got := struct {
			tail session.Tail
			dir  string
		}{s.Tail, s.Dir}
		if first == nil {
			first = &got
			continue
		}
		if got != *first {
			t.Errorf("window %v changed the status: %+v, want %+v", since, got, *first)
		}
	}
}
