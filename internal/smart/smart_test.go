package smart

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func facts() []Facts {
	return []Facts{
		{Project: "alpha", Status: "running", Request: "Run the suite", Heuristic: `Asked to "Run the suite" — working.`},
		{Project: "beta", Status: "interrupted", PendingTool: "Bash", Heuristic: "Interrupted mid-Bash."},
	}
}

// serve stands in for the Messages API and hands the request body back for inspection.
func serve(t *testing.T, status int, body string) (*Client, *[]byte, *http.Header) {
	t.Helper()
	var seen []byte
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = io.ReadAll(r.Body)
		headers = r.Header.Clone()
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New("test-key", "")
	c.Endpoint = srv.URL
	c.HTTP = srv.Client()
	return c, &seen, &headers
}

func TestRewritesEverySentenceAndParagraph(t *testing.T) {
	c, seen, headers := serve(t, http.StatusOK,
		`{"content":[{"type":"text","text":"[{\"sentence\":\"Ran the suite; still going.\",\"report\":\"Twelve Bash calls over an hour.\"},{\"sentence\":\"Was deploying; the shell command never finished.\",\"report\":\"Four Bash calls, then nothing.\"}]"}]}`)

	got, err := c.Rewrite(context.Background(), facts())
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if len(got) != 2 || got[0].Sentence != "Ran the suite; still going." {
		t.Errorf("rewrites = %+v", got)
	}
	if got[0].Report != "Twelve Bash calls over an hour." {
		t.Errorf("report = %q, want the paragraph the model wrote", got[0].Report)
	}

	if headers.Get("x-api-key") != "test-key" {
		t.Errorf("x-api-key = %q", headers.Get("x-api-key"))
	}
	if headers.Get("anthropic-version") == "" {
		t.Error("no anthropic-version header")
	}

	var req struct {
		Model    string `json:"model"`
		System   string `json:"system"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*seen, &req); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if req.Model != DefaultModel {
		t.Errorf("model = %q, want %q", req.Model, DefaultModel)
	}
	if !strings.Contains(req.Messages[0].Content, "alpha") {
		t.Errorf("facts were not sent: %s", req.Messages[0].Content)
	}
}

// The prompt asks for bare JSON, but models wrap things; that is not worth failing over.
func TestAcceptsAFencedOrChattyReply(t *testing.T) {
	c, _, _ := serve(t, http.StatusOK,
		"{\"content\":[{\"type\":\"text\",\"text\":\"Here you go:\\n```json\\n[{\\\"sentence\\\":\\\"one\\\",\\\"report\\\":\\\"first\\\"}, {\\\"sentence\\\":\\\"two\\\",\\\"report\\\":\\\"second\\\"}]\\n```\"}]}")
	got, err := c.Rewrite(context.Background(), facts())
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if len(got) != 2 || got[1].Sentence != "two" || got[1].Report != "second" {
		t.Errorf("rewrites = %+v", got)
	}
}

func TestFailuresAreReportedNotPaperedOver(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"api error", http.StatusUnauthorized, `{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`, "invalid x-api-key"},
		{"not json at all", http.StatusOK, `<html>gateway</html>`, "unexpected reply"},
		{"no array in the reply", http.StatusOK, `{"content":[{"type":"text","text":"I would rather not"}]}`, "JSON array"},
		{"wrong number of entries", http.StatusOK, `{"content":[{"type":"text","text":"[{\"sentence\":\"only one\",\"report\":\"r\"}]"}]}`, "1 entries for 2 projects"},
		{"an empty sentence", http.StatusOK, `{"content":[{"type":"text","text":"[{\"sentence\":\"fine\",\"report\":\"r\"},{\"sentence\":\"\",\"report\":\"r\"}]"}]}`, "empty sentence"},
		{"an empty report", http.StatusOK, `{"content":[{"type":"text","text":"[{\"sentence\":\"a\",\"report\":\"r\"},{\"sentence\":\"b\",\"report\":\"\"}]"}]}`, "empty report"},
		// The old reply shape, which a differently-prompted model might still produce.
		{"an array of bare strings", http.StatusOK, `{"content":[{"type":"text","text":"[\"one\",\"two\"]"}]}`, "{sentence, report} objects"},
		{"objects missing the report", http.StatusOK, `{"content":[{"type":"text","text":"[{\"sentence\":\"a\"},{\"sentence\":\"b\"}]"}]}`, "empty report"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := serve(t, tc.status, tc.body)
			_, err := c.Rewrite(context.Background(), facts())
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestNoKeySaysWhichVariableToSet(t *testing.T) {
	c := New("", "")
	_, err := c.Rewrite(context.Background(), facts())
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error = %v, want it to name the environment variable", err)
	}
}

func TestNothingToRewriteMakesNoRequest(t *testing.T) {
	c, seen, _ := serve(t, http.StatusOK, `{"content":[{"type":"text","text":"[]"}]}`)
	got, err := c.Rewrite(context.Background(), nil)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("rewrites = %+v, want none", got)
	}
	if len(*seen) != 0 {
		t.Errorf("a request was made for an empty report: %s", *seen)
	}
}

// Whatever else changes, this must not start sending transcripts.
func TestOnlyTheDeclaredFactsAreSent(t *testing.T) {
	c, seen, _ := serve(t, http.StatusOK, `{"content":[{"type":"text","text":"[{\"sentence\":\"a\",\"report\":\"r\"},{\"sentence\":\"b\",\"report\":\"r\"}]"}]}`)
	if _, err := c.Rewrite(context.Background(), facts()); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*seen, &req); err != nil {
		t.Fatal(err)
	}
	var sent []map[string]any
	if err := json.Unmarshal([]byte(req.Messages[0].Content), &sent); err != nil {
		t.Fatalf("facts are not the JSON array they claim to be: %v", err)
	}
	allowed := map[string]bool{
		"project": true, "agent": true, "status": true, "request": true, "agent_said": true,
		"last_tool": true, "pending_tool": true, "progress": true, "age": true,
		"heuristic_sentence": true,
		// The paragraph's facts: counts and names, still nothing from tool output or file
		// contents.
		"span": true, "tools": true, "files": true, "turns": true, "errors": true,
		"heuristic_report": true,
	}
	for _, entry := range sent {
		for key := range entry {
			if !allowed[key] {
				t.Errorf("unexpected field %q left the machine", key)
			}
		}
	}
}
