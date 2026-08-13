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
	// tailBytes is the minimum read: enough of the end of a transcript to decide the status,
	// whatever the report window is.
	tailBytes = 512 << 10
	// chunkBytes is how much more is read at a time when the window needs more than the
	// minimum.
	chunkBytes = 512 << 10
	// maxTailBytes is the hard cap on how much of one transcript is read. Reading a day
	// instead of a tail multiplies the work by the number of transcripts, and a pathological
	// one would otherwise turn a 150 ms command into a slow one. When the cap bites, the
	// Activity is marked Truncated and the paragraph says what it really covers rather than
	// implying it saw the whole window.
	//
	// 1 MiB, not the 4 MiB first tried: measured over this machine's 25 projects, 4 MiB cost
	// nothing on the default 24h window (most sessions are covered long before the cap) but
	// made --all take 0.93s against 0.33s. A megabyte is still around a thousand records.
	maxTailBytes = 1 << 20

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
	head, tail, truncated, err := readEnds(path, info.Size(), since)
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
	activity.truncated = truncated
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

// readEnds returns the first headBytes of the file and as much of its end as the window
// needs, each trimmed to whole lines. For a file small enough to be read whole, head is
// empty and tail is everything, so no record is counted twice.
//
// truncated reports that the cap was reached before the window was covered.
func readEnds(path string, size int64, since time.Time) (head, tail []byte, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, false, err
	}
	defer f.Close()

	if size <= tailBytes {
		all, err := io.ReadAll(f)
		return nil, all, false, err
	}

	head = make([]byte, headBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, nil, false, err
	}
	head = head[:n]
	// Drop the trailing partial line.
	if i := bytes.LastIndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	}

	tail, truncated, err = readBackwards(f, size, since)
	return head, tail, truncated, err
}

// readBackwards reads the end of the file in chunks until it has covered the window, run out
// of file, or hit the cap.
//
// Records are written in order, so the oldest record read so far is the first one in the
// buffer: checking that alone is enough to know whether the window is covered, which keeps
// this O(chunks) rather than re-parsing everything each round.
func readBackwards(f *os.File, size int64, since time.Time) (tail []byte, truncated bool, err error) {
	offset := size
	var buf []byte

	for {
		want := int64(chunkBytes)
		if offset < want {
			want = offset
		}
		offset -= want

		chunk := make([]byte, want)
		if _, err := f.ReadAt(chunk, offset); err != nil && err != io.EOF {
			return nil, false, err
		}
		buf = append(chunk, buf...)

		if offset == 0 {
			// The whole file: nothing was missed, whatever the window asked for.
			return buf, false, nil
		}
		// Everything from here on is judged on whole lines only; the first line of the
		// buffer is a fragment of a record that starts earlier in the file.
		trimmed := buf
		if i := bytes.IndexByte(trimmed, '\n'); i >= 0 {
			trimmed = trimmed[i+1:]
		}

		if len(trimmed) >= tailBytes && coversWindow(trimmed, since) {
			return trimmed, false, nil
		}
		if len(buf) >= maxTailBytes {
			return trimmed, true, nil
		}
	}
}

// coversWindow reports whether the buffer reaches back past the start of the window. A zero
// since means there is no window to cover, so the cap is what stops the read.
func coversWindow(buf []byte, since time.Time) bool {
	if since.IsZero() {
		return false
	}
	ts, ok := firstTimestamp(buf)
	return ok && ts.Before(since)
}

// firstTimestamp is the timestamp of the earliest record in the buffer. Bookkeeping records
// carry none, so it scans forward until it finds one — over a bounded number of lines, since
// a run of untimestamped records is short.
func firstTimestamp(buf []byte) (time.Time, bool) {
	const maxLines = 50
	for i, line := range bytes.SplitN(buf, []byte{'\n'}, maxLines+1) {
		if i == maxLines {
			break
		}
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if ts, ok := rec.time(); ok {
			return ts, true
		}
	}
	return time.Time{}, false
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

	// Filled by blocks() the first time it is called.
	cachedBlocks []block
	parsed       bool
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

// blocks parses the record's content, once. Several callers want it — the activity counts,
// the request extraction, the tail classification — and unmarshalling a tool result twice is
// pure cost: with a day's window rather than a tail, this is the hot path.
func (r *record) blocks() []block {
	if r.parsed {
		return r.cachedBlocks
	}
	r.parsed = true
	if r.Message == nil || len(r.Message.Content) == 0 {
		return nil
	}
	var bs []block
	if err := json.Unmarshal(r.Message.Content, &bs); err == nil {
		r.cachedBlocks = bs
		return bs
	}
	var s string
	if err := json.Unmarshal(r.Message.Content, &s); err == nil {
		r.cachedBlocks = []block{{Type: "text", Text: s}}
	}
	return r.cachedBlocks
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
	truncated  bool
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
		Truncated:  b.truncated,
	}
}
