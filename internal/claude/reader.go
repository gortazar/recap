// Package claude reads Claude Code's per-session JSONL transcripts.
//
// The format is undocumented and changes between releases, so every field is optional and
// unrecognised records are skipped rather than treated as errors. See
// docs/session-formats.md for what was observed and when.
package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gortazar/recap/internal/session"
)

const (
	// headBytes is enough to find the session's first user request and its metadata.
	headBytes = 64 << 10
	// tailBytes bounds the work per session. Transcripts reach tens of megabytes, and recap
	// only ever needs the end of one; 512 KiB is many turns' worth.
	tailBytes = 512 << 10

	// maxFiles and maxRequests cap what a session contributes to the paragraph — and to the
	// cache, which stores the whole Activity. A busy day touches hundreds of files; naming
	// ten of them is already more than a paragraph can use.
	maxFiles    = 10
	maxRequests = 5
)

// ReadSession parses one transcript into a Session. It returns an error only if the file
// cannot be read at all; a file whose contents make no sense yields a Session with
// Unreadable set, so that one broken transcript cannot hide the others.
//
// since bounds the Activity: only records at or after it are counted towards what the
// session did. A zero since means the whole transcript. It does not affect the status, which
// always comes from the end of the transcript — "what is it doing now" is not a question
// about the last 24 hours.
func ReadSession(path string, since time.Time) (*session.Session, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	head, tail, err := readEnds(path, info.Size())
	if err != nil {
		return nil, err
	}

	s := &session.Session{
		Agent:  session.AgentClaude,
		Source: path,
		ID:     strings.TrimSuffix(filepath.Base(path), ".jsonl"),
	}

	recognised := 0
	// The head only contributes the opening of the conversation; everything else is taken
	// from the tail, which is the part that describes the current state.
	headRecords := parseLines(head)
	for i := range headRecords {
		rec := &headRecords[i]
		recognised++
		applyMetadata(s, rec)
		if s.FirstRequest == "" {
			if text, ok := requestText(rec); ok {
				s.FirstRequest = text
			}
		}
		if ts, ok := rec.time(); ok && (s.Started.IsZero() || ts.Before(s.Started)) {
			s.Started = ts
		}
	}

	records := parseLines(tail)
	recognised += len(records)
	if recognised == 0 {
		s.Unreadable = "no recognisable records"
		s.LastActivity = info.ModTime()
		return s, nil
	}

	// Tool calls waiting for a result, in the order they were made. A result removes its
	// call; whatever is left at the end was never answered.
	pending := map[string]string{}
	activity := newActivity()
	var lastConv *record
	for i := range records {
		rec := &records[i]
		applyMetadata(s, rec)
		ts, hasTime := rec.time()
		if hasTime {
			if ts.After(s.LastActivity) {
				s.LastActivity = ts
			}
			if s.Started.IsZero() {
				s.Started = ts
			}
		}
		if !rec.conversational() {
			continue
		}

		// Whether this record counts towards "what did it do". A record with no timestamp
		// cannot be placed in the window, so it is left out of the counts rather than
		// guessed into them.
		inWindow := hasTime && (since.IsZero() || !ts.Before(since))
		if inWindow {
			activity.saw(ts)
			if rec.isAssistant() {
				activity.turns++
				if rec.Message != nil && rec.Message.Model == syntheticModel {
					activity.errors++
				}
			}
		}

		for _, b := range rec.blocks() {
			switch b.Type {
			case "tool_use":
				pending[b.ID] = b.Name
				s.LastTool = b.Name
				if f := b.file(); f != "" {
					s.LastFile = f
				}
				if inWindow {
					activity.tool(b.Name, b.file())
				}
			case "tool_result":
				delete(pending, b.ToolUseID)
				if inWindow && b.failed() {
					activity.errors++
				}
			case "text":
				if rec.isAssistant() {
					s.LastText = strings.TrimSpace(b.Text)
				}
			}
		}
		if text, ok := requestText(rec); ok {
			s.LastRequest = text
			if s.FirstRequest == "" {
				s.FirstRequest = text
			}
			if inWindow {
				activity.request(text)
			}
		}
		lastConv = rec
	}
	s.Activity = activity.result()

	if s.LastActivity.IsZero() {
		// Some record types carry no timestamp at all, and a transcript can end on one.
		s.LastActivity = info.ModTime()
	}
	classifyTail(s, lastConv, pending)
	return s, nil
}

// classifyTail decides what shape the end of the transcript has. It only reports the shape;
// turning that into a status is the job of the status rules, which also know about liveness.
func classifyTail(s *session.Session, last *record, pending map[string]string) {
	if last == nil {
		s.Tail = session.TailUnknown
		return
	}
	blocks := last.blocks()
	if last.isAssistant() {
		for _, b := range blocks {
			if b.Type == "tool_use" {
				if name, still := pending[b.ID]; still {
					s.Tail = session.TailPendingTool
					s.PendingTool = name
					return
				}
			}
		}
		if last.Message != nil && last.Message.Model == syntheticModel {
			s.Tail = session.TailError
			return
		}
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				s.Tail = session.TailAssistantText
				return
			}
		}
		// An assistant record with nothing but thinking in it is a turn that stopped
		// before it said or did anything: mid-turn, like a tool result.
		s.Tail = session.TailToolResult
		return
	}

	for _, b := range blocks {
		if b.Type == "tool_result" {
			s.Tail = session.TailToolResult
			return
		}
	}
	if text, _ := last.text(); isInterruptMarker(text) {
		s.Tail = session.TailInterrupted
		return
	}
	if _, ok := requestText(last); ok {
		s.Tail = session.TailUserRequest
		return
	}
	s.Tail = session.TailUnknown
}

func applyMetadata(s *session.Session, rec *record) {
	if rec.SessionID != "" {
		s.ID = rec.SessionID
	}
	if rec.CWD != "" {
		s.Dir = rec.CWD
	}
	if rec.GitBranch != "" {
		s.Branch = rec.GitBranch
	}
	if rec.Version != "" {
		s.Version = rec.Version
	}
	if rec.Message != nil && rec.Message.Model != "" && rec.Message.Model != syntheticModel {
		s.Model = rec.Message.Model
	}
	// The agent names sessions itself; a user-set title wins over a generated one.
	if rec.AITitle != "" && s.Title == "" {
		s.Title = rec.AITitle
	}
	if rec.CustomTitle != "" {
		s.Title = rec.CustomTitle
	}
}

// readEnds returns the first headBytes and last tailBytes of the file, each trimmed to whole
// lines. For a file small enough to be covered by the tail window, head is empty and tail is
// everything, so no record is counted twice.
func readEnds(path string, size int64) (head, tail []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	if size <= tailBytes {
		all, err := io.ReadAll(f)
		return nil, all, err
	}

	head = make([]byte, headBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, nil, err
	}
	head = head[:n]
	// Drop the trailing partial line.
	if i := bytes.LastIndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	}

	tail = make([]byte, tailBytes)
	if _, err := f.ReadAt(tail, size-tailBytes); err != nil && err != io.EOF {
		return nil, nil, err
	}
	// Drop the leading partial line.
	if i := bytes.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	return head, tail, nil
}

func parseLines(b []byte) []record {
	var out []record
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // a torn or unknown line is skipped, never fatal
		}
		if rec.Type == "" {
			continue
		}
		out = append(out, rec)
	}
	return out
}

const syntheticModel = "<synthetic>"

type record struct {
	Type        string   `json:"type"`
	Subtype     string   `json:"subtype"`
	SessionID   string   `json:"sessionId"`
	CWD         string   `json:"cwd"`
	Version     string   `json:"version"`
	GitBranch   string   `json:"gitBranch"`
	IsSidechain bool     `json:"isSidechain"`
	IsMeta      bool     `json:"isMeta"`
	Timestamp   string   `json:"timestamp"`
	Message     *message `json:"message"`
	AITitle     string   `json:"aiTitle"`
	CustomTitle string   `json:"customTitle"`
}

type message struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// failed reports whether a tool result carries an error. Claude Code marks these two ways
// depending on where the failure came from, and both mean the same thing here.
func (b block) failed() bool {
	if b.IsError {
		return true
	}
	var text string
	if err := json.Unmarshal(b.Content, &text); err == nil {
		return strings.Contains(text, "<tool_use_error>")
	}
	return false
}

func (b block) file() string {
	if len(b.Input) == 0 {
		return ""
	}
	var in struct {
		FilePath     string `json:"file_path"`
		FilePathAlt  string `json:"filePath"`
		NotebookPath string `json:"notebook_path"`
		Path         string `json:"path"`
	}
	if err := json.Unmarshal(b.Input, &in); err != nil {
		return ""
	}
	for _, p := range []string{in.FilePath, in.FilePathAlt, in.NotebookPath, in.Path} {
		if p != "" {
			return p
		}
	}
	return ""
}

// conversational reports whether this record is part of the conversation rather than
// bookkeeping. Sidechain records belong to sub-agents, which recap does not report on.
func (r *record) conversational() bool {
	return (r.Type == "user" || r.Type == "assistant") && !r.IsSidechain
}

func (r *record) isAssistant() bool { return r.Type == "assistant" }

func (r *record) time() (time.Time, bool) {
	if r.Timestamp == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, r.Timestamp)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

func (r *record) blocks() []block {
	if r.Message == nil || len(r.Message.Content) == 0 {
		return nil
	}
	var bs []block
	if err := json.Unmarshal(r.Message.Content, &bs); err == nil {
		return bs
	}
	var s string
	if err := json.Unmarshal(r.Message.Content, &s); err == nil {
		return []block{{Type: "text", Text: s}}
	}
	return nil
}

// text joins the record's prose. The bool reports whether there was any.
func (r *record) text() (string, bool) {
	var sb strings.Builder
	for _, b := range r.blocks() {
		if b.Type == "text" && b.Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(b.Text)
		}
	}
	s := strings.TrimSpace(sb.String())
	return s, s != ""
}

// requestText returns what the user actually asked for, if this record is such a request.
// Tool results, interrupt markers and the XML plumbing Claude Code writes for slash commands
// and hooks all arrive as "user" records but are not things a person typed.
func requestText(r *record) (string, bool) {
	if r.Type != "user" || r.IsSidechain || r.IsMeta {
		return "", false
	}
	for _, b := range r.blocks() {
		if b.Type == "tool_result" {
			return "", false
		}
	}
	text, ok := r.text()
	if !ok || isInterruptMarker(text) || isPlumbing(text) {
		return "", false
	}
	return text, true
}

func isInterruptMarker(text string) bool {
	return strings.HasPrefix(text, "[Request interrupted")
}

// plumbingTag matches the XML-ish opening tag that Claude Code wraps its synthetic user
// turns in: slash commands, hook output, caveats, task notifications. The list of tags keeps
// growing between releases, so recap recognises the shape rather than the names — no
// request a person types starts with a tag.
var plumbingTag = regexp.MustCompile(`^<[a-zA-Z][a-zA-Z0-9_-]*>`)

func isPlumbing(text string) bool {
	return plumbingTag.MatchString(text)
}

// activityBuilder accumulates what a session did, keeping counts the paragraph needs and
// capping the lists so a busy day cannot grow the cache without bound.
type activityBuilder struct {
	tools      map[string]int
	fileCounts map[string]int
	fileOrder  []string
	requests   []string
	turns      int
	errors     int
	first      time.Time
	last       time.Time
}

func newActivity() *activityBuilder {
	return &activityBuilder{tools: map[string]int{}, fileCounts: map[string]int{}}
}

func (b *activityBuilder) saw(ts time.Time) {
	if b.first.IsZero() || ts.Before(b.first) {
		b.first = ts
	}
	if ts.After(b.last) {
		b.last = ts
	}
}

func (b *activityBuilder) tool(name, file string) {
	if name != "" {
		b.tools[name]++
	}
	if file == "" {
		return
	}
	// Basenames: a paragraph of absolute paths is unreadable, and the full path is already
	// on the session as LastFile.
	name = filepath.Base(file)
	if _, seen := b.fileCounts[name]; !seen {
		b.fileOrder = append(b.fileOrder, name)
	}
	b.fileCounts[name]++
}

func (b *activityBuilder) request(text string) {
	b.requests = append(b.requests, text)
	if len(b.requests) > maxRequests {
		b.requests = b.requests[len(b.requests)-maxRequests:]
	}
}

func (b *activityBuilder) result() session.Activity {
	// Most-touched first, ties broken by the order they were first seen, so two runs over
	// the same transcript produce the same paragraph.
	files := append([]string(nil), b.fileOrder...)
	sort.SliceStable(files, func(i, j int) bool {
		return b.fileCounts[files[i]] > b.fileCounts[files[j]]
	})
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	return session.Activity{
		Requests:   b.requests,
		ToolCounts: b.tools,
		Files:      files,
		Turns:      b.turns,
		Errors:     b.errors,
		First:      b.first,
		Last:       b.last,
	}
}
