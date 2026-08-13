// Package smart rewrites recap's sentences with a model, for when the heuristic ones read
// too blunt.
//
// This is the one part of recap that is not offline, so it is opt-in (--smart) and it is
// explicit about what leaves the machine: the short facts below and nothing else — no file
// contents, no tool output, no transcript. If anything goes wrong, the caller keeps the
// heuristic sentences; a recap that fails because a model was unreachable would be worse
// than a blunt one.
package smart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultModel is what --smart uses unless the config file names another.
const DefaultModel = "claude-sonnet-5"

// DefaultEndpoint is the Anthropic Messages API.
const DefaultEndpoint = "https://api.anthropic.com/v1/messages"

// Timeout bounds the whole call. recap is a command you run to get an answer now; it is
// better to fall back to the heuristic sentence than to sit there.
const Timeout = 15 * time.Second

// Facts is what recap knows about one project, and all it ever sends. Fields are omitted
// when empty so the request stays small and says nothing it does not know.
type Facts struct {
	Project     string `json:"project"`
	Agent       string `json:"agent,omitempty"`
	Status      string `json:"status"`
	Request     string `json:"request,omitempty"`
	AgentSaid   string `json:"agent_said,omitempty"`
	LastTool    string `json:"last_tool,omitempty"`
	PendingTool string `json:"pending_tool,omitempty"`
	Progress    string `json:"progress,omitempty"`
	Age         string `json:"age,omitempty"`
	Heuristic   string `json:"heuristic_sentence"`

	// What the session did over the report window, for the paragraph. Counts and names
	// only — still nothing from tool output or file contents.
	Span            string   `json:"span,omitempty"`
	Tools           string   `json:"tools,omitempty"`
	Files           []string `json:"files,omitempty"`
	Turns           int      `json:"turns,omitempty"`
	Errors          int      `json:"errors,omitempty"`
	HeuristicReport string   `json:"heuristic_report,omitempty"`
}

// Rewritten is what the model gives back for one project: the status line and the paragraph
// under it.
type Rewritten struct {
	Sentence string `json:"sentence"`
	Report   string `json:"report"`
}

const systemPrompt = `You rewrite status summaries for "recap", a command that tells a
developer what their coding agents were doing.

You are given a JSON array of facts, one entry per project, each with the blunt sentence and
paragraph recap generated itself. For each, write:

- "sentence": one natural sentence of at most 120 characters — what it was asked to do and
  where it stands.
- "report": two or three sentences about what the session did over the window, from the
  span, tools, files, turns and errors given.

Rules:
- Reply with a JSON array of objects, each with exactly "sentence" and "report", and nothing
  else. Same length as the input, same order.
- Say only what the facts support. Never invent a cause, a file, an error or a result. If a
  session was interrupted, you do not know why. Counting tool calls is a fact; concluding
  the tests now pass is not.
- Plain past/present tense, no marketing, no exclamation marks, no emoji.`

// Client talks to the Messages API. The zero value is not usable; use New.
type Client struct {
	HTTP     *http.Client
	Key      string
	Model    string
	Endpoint string
}

// New builds a client with recap's defaults. Model may be empty for DefaultModel.
func New(key, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		HTTP:     &http.Client{Timeout: Timeout},
		Key:      key,
		Model:    model,
		Endpoint: DefaultEndpoint,
	}
}

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type response struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Rewrite returns one rewritten sentence and paragraph per set of facts, in the same order.
func (c *Client) Rewrite(ctx context.Context, facts []Facts) ([]Rewritten, error) {
	if len(facts) == 0 {
		return nil, nil
	}
	if c.Key == "" {
		return nil, fmt.Errorf("no API key: set ANTHROPIC_API_KEY")
	}

	payload, err := json.Marshal(facts)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(request{
		Model: c.Model,
		// Roughly 40 tokens for the sentence and 120 for the paragraph, plus the JSON
		// around them.
		MaxTokens: 220*len(facts) + 200,
		System:    systemPrompt,
		Messages:  []message{{Role: "user", Content: string(payload)}},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var parsed response
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("unexpected reply (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, parsed.Error.Message)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	out, err := parseRewrites(text.String())
	if err != nil {
		return nil, err
	}
	if len(out) != len(facts) {
		return nil, fmt.Errorf("model returned %d entries for %d projects", len(out), len(facts))
	}
	return out, nil
}

// parseRewrites pulls the JSON array out of the reply. Models sometimes wrap it in a code
// fence or a line of preamble, which is not worth failing over — but the shape inside is
// checked strictly, because a half-parsed reply would put the model's idea of a paragraph
// somewhere recap promises only facts.
func parseRewrites(text string) ([]Rewritten, error) {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end < start {
		return nil, fmt.Errorf("model did not return a JSON array")
	}
	var out []Rewritten
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("model did not return a JSON array of {sentence, report} objects")
	}
	for i := range out {
		out[i].Sentence = strings.TrimSpace(out[i].Sentence)
		out[i].Report = strings.TrimSpace(out[i].Report)
		if out[i].Sentence == "" {
			return nil, fmt.Errorf("model returned an empty sentence")
		}
		if out[i].Report == "" {
			return nil, fmt.Errorf("model returned an empty report")
		}
	}
	return out, nil
}
