package render

import (
	"bytes"
	"encoding/json"
	"testing"
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
