package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gortazar/recap/internal/session"
)

func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, body)
	}
	return doc
}

func TestJSONDocumentShape(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sample(), Options{Now: now}, "process-table"); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	doc := decode(t, buf.Bytes())

	if got, want := doc["version"], float64(SchemaVersion); got != want {
		t.Errorf("version = %v, want %v", got, want)
	}
	if got, want := doc["liveness"], "process-table"; got != want {
		t.Errorf("liveness = %v, want %v", got, want)
	}

	projects, ok := doc["projects"].([]any)
	if !ok || len(projects) != 2 {
		t.Fatalf("projects = %v, want 2 entries", doc["projects"])
	}
	first := projects[0].(map[string]any)
	for _, key := range []string{"name", "dir", "status", "icon", "recap", "agents", "last_activity", "sessions"} {
		if _, ok := first[key]; !ok {
			t.Errorf("project is missing %q: %v", key, first)
		}
	}
	if got, want := first["status"], "running"; got != want {
		t.Errorf("status = %v, want %v (the word, not the number)", got, want)
	}
	sessions := first["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %v, want 1", sessions)
	}
	s := sessions[0].(map[string]any)
	if got, want := s["agent"], "Claude Code"; got != want {
		t.Errorf("session agent = %v, want %v", got, want)
	}
	if got, want := s["last_tool"], "Bash"; got != want {
		t.Errorf("session last_tool = %v, want %v", got, want)
	}
}

// A consumer iterating the list should not have to special-case an absent one.
func TestJSONWithNothingToReportIsStillADocument(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, nil, Options{Now: now}, "unavailable"); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	doc := decode(t, buf.Bytes())
	projects, ok := doc["projects"].([]any)
	if !ok {
		t.Fatalf("projects = %v, want an empty list rather than null", doc["projects"])
	}
	if len(projects) != 0 {
		t.Errorf("projects = %v, want empty", projects)
	}
}

func TestLivenessSource(t *testing.T) {
	if got, want := LivenessSource(true), "process-table"; got != want {
		t.Errorf("LivenessSource(true) = %q, want %q", got, want)
	}
	if got, want := LivenessSource(false), "unavailable"; got != want {
		t.Errorf("LivenessSource(false) = %q, want %q", got, want)
	}
}

// recap-gs checks the schema version and refuses one it does not know, so adding the
// paragraph must not bump it: every field a version-1 consumer reads has to still be there,
// unchanged.
func TestVersionOneConsumerSeesEverythingItSawBefore(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sampleWithReports(), Options{Now: now}, "process-table"); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	doc := decode(t, buf.Bytes())

	if got, want := doc["version"], float64(1); got != want {
		t.Errorf("version = %v, want %v: adding optional fields does not bump the schema", got, want)
	}
	for _, key := range []string{"version", "generated_at", "liveness", "projects"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("document lost the version-1 field %q", key)
		}
	}
	project := doc["projects"].([]any)[0].(map[string]any)
	for _, key := range []string{"name", "dir", "status", "icon", "recap", "agents", "last_activity", "sessions"} {
		if _, ok := project[key]; !ok {
			t.Errorf("project lost the version-1 field %q", key)
		}
	}
	s := project["sessions"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "agent", "status", "icon", "recap", "last_activity"} {
		if _, ok := s[key]; !ok {
			t.Errorf("session lost the version-1 field %q", key)
		}
	}
	// And the one-line sentence still means what it meant: the paragraph is a new field
	// beside it, not a replacement for it.
	if got := s["recap"].(string); !strings.HasPrefix(got, "Asked to ") {
		t.Errorf("recap = %q, want the one-line sentence unchanged", got)
	}
}

func TestJSONCarriesTheParagraphAndItsCounts(t *testing.T) {
	projects := sampleWithReports()
	projects[0].Sessions[0].Session.Activity = session.Activity{
		ToolCounts: map[string]int{"Bash": 12},
		Files:      []string{"scheduler.go"},
		Requests:   []string{"run the suite"},
		Turns:      6,
		Errors:     1,
		First:      ago(time.Hour),
		Last:       now,
		Truncated:  true,
	}

	var buf bytes.Buffer
	if err := JSON(&buf, projects, Options{Now: now}, "process-table"); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	doc := decode(t, buf.Bytes())
	project := doc["projects"].([]any)[0].(map[string]any)

	if got := project["report"]; got == nil || !strings.Contains(got.(string), "tool calls") {
		t.Errorf("project report = %v, want the paragraph", got)
	}
	s := project["sessions"].([]any)[0].(map[string]any)
	activity, ok := s["activity"].(map[string]any)
	if !ok {
		t.Fatalf("session has no activity object: %v", s)
	}
	if got := activity["tool_counts"].(map[string]any)["Bash"]; got != float64(12) {
		t.Errorf("tool_counts[Bash] = %v, want 12", got)
	}
	if got := activity["turns"]; got != float64(6) {
		t.Errorf("turns = %v, want 6", got)
	}
	if got := activity["truncated"]; got != true {
		t.Errorf("truncated = %v, want true", got)
	}
	for _, key := range []string{"files", "requests", "errors", "window_start"} {
		if _, ok := activity[key]; !ok {
			t.Errorf("activity is missing %q", key)
		}
	}
}

// A session that did nothing in the window has no counts worth an object, and omitting it is
// what omitempty is for.
func TestJSONOmitsAnEmptyActivity(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sample(), Options{Now: now}, "process-table"); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	doc := decode(t, buf.Bytes())
	s := doc["projects"].([]any)[0].(map[string]any)["sessions"].([]any)[0].(map[string]any)
	if _, present := s["activity"]; present {
		t.Errorf("activity present for a session with none: %v", s)
	}
}
