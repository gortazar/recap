package opencode

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gortazar/recap/internal/session"

	_ "modernc.org/sqlite"
)

// store builds a database from the schema the real opencode writes plus the given fixtures.
func store(t *testing.T, fixtures ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, f := range append([]string{"schema.sql"}, fixtures...) {
		body, err := os.ReadFile(filepath.Join("testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("loading %s: %v", f, err)
		}
	}
	return path
}

func discover(t *testing.T, path string) map[string]*session.Session {
	t.Helper()
	sessions, err := Discover(path)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	byID := map[string]*session.Session{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	return byID
}

func TestReadsARealSession(t *testing.T) {
	got := discover(t, store(t, "real-store.sql"))
	if len(got) != 1 {
		t.Fatalf("found %d sessions, want 1", len(got))
	}
	s := got["ses_0243c909effe63ZZpnGRyPNoZE"]
	if s == nil {
		t.Fatal("the session from the real store is missing")
	}
	if got, want := s.Agent, session.AgentOpencode; got != want {
		t.Errorf("Agent = %q, want %q", got, want)
	}
	if got, want := s.Dir, "/home/user/git/models-benchmark"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got, want := s.Title, "Docker Compose for Ollama, OpenWeb UI, and Tailscale setup"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
	if got, want := s.Model, "qwen3-coder:30b"; got != want {
		t.Errorf("Model = %q, want %q", got, want)
	}
	if got, want := s.Version, "1.18.14"; got != want {
		t.Errorf("Version = %q, want %q", got, want)
	}
	if got, want := s.LastActivity.UnixMilli(), int64(1786098479394); got != want {
		t.Errorf("LastActivity = %d, want %d", got, want)
	}
	// It ended with the assistant's summary of what it had written.
	if got, want := s.Tail, session.TailAssistantText; got != want {
		t.Errorf("Tail = %v, want %v", got, want)
	}
	if s.LastText == "" {
		t.Error("LastText is empty")
	}
	if got, want := s.LastTool, "write"; got != want {
		t.Errorf("LastTool = %q, want %q", got, want)
	}
}

func TestSessionKilledMidToolIsPending(t *testing.T) {
	s := discover(t, store(t, "states.sql"))["ses_pending"]
	if s == nil {
		t.Fatal("ses_pending is missing")
	}
	if got, want := s.Tail, session.TailPendingTool; got != want {
		t.Errorf("Tail = %v, want %v", got, want)
	}
	if got, want := s.PendingTool, "bash"; got != want {
		t.Errorf("PendingTool = %q, want %q", got, want)
	}
	if got, want := s.LastRequest, "Make the failing scheduler test pass"; got != want {
		t.Errorf("LastRequest = %q, want %q", got, want)
	}
}

func TestTodoListIsAProgressMarker(t *testing.T) {
	s := discover(t, store(t, "states.sql"))["ses_todo"]
	if s == nil {
		t.Fatal("ses_todo is missing")
	}
	if s.TodoDone != 2 || s.TodoTotal != 3 {
		t.Errorf("progress = %d of %d, want 2 of 3", s.TodoDone, s.TodoTotal)
	}
	if got, want := s.LastFile, "/home/user/git/beta/reader.go"; got != want {
		t.Errorf("LastFile = %q, want %q", got, want)
	}
}

// Archiving a session is the only thing either agent records that means "this is over".
func TestArchivedSessionIsCompleted(t *testing.T) {
	s := discover(t, store(t, "states.sql"))["ses_archived"]
	if s == nil {
		t.Fatal("ses_archived is missing")
	}
	if !s.Completed {
		t.Error("Completed = false, want true for an archived session")
	}
	if got := session.Classify(s, session.Dead, time.Now()); got != session.StatusFinished {
		t.Errorf("status = %v, want %v", got, session.StatusFinished)
	}
}

func TestSubSessionsAreNotReported(t *testing.T) {
	if s := discover(t, store(t, "states.sql"))["ses_child"]; s != nil {
		t.Error("a sub-session was reported; v1 does not recurse into sub-agents")
	}
}

// Every field is optional and the formats drift: a column that stops being what recap
// expects must cost that one field, not the session.
func TestUnexpectedFieldShapesDoNotLoseTheSession(t *testing.T) {
	s := discover(t, store(t, "states.sql"))["ses_oddmodel"]
	if s == nil {
		t.Fatal("a session with an unparseable model column was dropped")
	}
	if got, want := s.Dir, "/home/user/git/gamma"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestMissingStoreIsNotAnError(t *testing.T) {
	sessions, err := Discover(filepath.Join(t.TempDir(), "no-opencode-here.db"))
	if err != nil {
		t.Fatalf("Discover of a missing store: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("found %d sessions in a missing store", len(sessions))
	}
}
