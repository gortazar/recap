// Package opencode reads opencode's session store.
//
// Since 1.18 that store is a SQLite database rather than the JSON tree older releases used;
// see docs/session-formats.md. recap opens it strictly read-only and never checkpoints or
// writes, because the user may well be in the middle of a session in it.
package opencode

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gortazar/recap/internal/session"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, so recap stays a single static binary
)

const (
	// tailParts is how many of a session's most recent parts recap looks at. A session's
	// state is decided by its last few events, and the report window rarely needs more; it
	// is the same kind of cap as the Claude reader's byte limit, and it is what stops a
	// months-old session costing anything.
	tailParts = 400

	// maxFiles and maxRequests cap what one session contributes to the paragraph, and to
	// the cache, which stores the whole Activity.
	maxFiles    = 10
	maxRequests = 5
)

// DefaultStore is where opencode keeps its database.
func DefaultStore() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "opencode", "opencode.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

// Discover reads every top-level session from the store. A missing store is not an error:
// it just means opencode was never used here.
// since bounds the Activity: only parts at or after it count towards what the session did.
// A zero since means everything the cap allows. It does not affect the status, which always
// comes from the end of the conversation.
func Discover(dbPath string, since time.Time) ([]*session.Session, error) {
	if dbPath == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	db, err := sql.Open("sqlite", readOnlyDSN(dbPath))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	sessions, err := readSessions(db)
	if err != nil {
		return nil, err
	}
	for _, s := range sessions {
		// One session failing to yield its detail must not lose the others, so the tail is
		// best-effort: what it cannot read stays zero and the status rules cope.
		readTail(db, s, since)
		readTodos(db, s)
	}
	return sessions, nil
}

// readOnlyDSN opens the database strictly for reading. Writing to a live agent's store could
// corrupt a session in progress, so this is the only way recap ever opens it.
func readOnlyDSN(path string) string {
	return "file:" + url.PathEscape(path) + "?mode=ro"
}

func readSessions(db *sql.DB) ([]*session.Session, error) {
	// parent_id IS NULL: sub-agent sessions are opencode's sidechains, and v1 does not
	// report on sub-agents.
	rows, err := db.Query(`
		SELECT id, directory, title, version, model, time_created, time_updated, time_archived
		FROM session
		WHERE parent_id IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*session.Session
	for rows.Next() {
		var (
			id, dir, title, version, model sql.NullString
			created, updated, archived     sql.NullInt64
		)
		if err := rows.Scan(&id, &dir, &title, &version, &model, &created, &updated, &archived); err != nil {
			// A row shaped differently from what recap expects costs that row, not the run.
			continue
		}
		s := &session.Session{
			ID:           id.String,
			Agent:        session.AgentOpencode,
			Dir:          dir.String,
			Title:        title.String,
			Version:      version.String,
			Model:        modelName(model.String),
			Started:      msTime(created),
			LastActivity: msTime(updated),
			Completed:    archived.Valid,
			Source:       "opencode",
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// modelName pulls the model out of the JSON the model column holds. Older and newer releases
// may put a bare string there instead, which is just as usable.
func modelName(raw string) string {
	if raw == "" {
		return ""
	}
	var m struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err == nil && m.ID != "" {
		return m.ID
	}
	if raw[0] == '{' || raw[0] == '[' {
		return "" // JSON recap does not recognise: better to say nothing than to print it
	}
	return raw
}

// part is one event in a session, with the message that produced it.
type part struct {
	role string
	kind string
	data []byte
	at   time.Time
}

// readTail reads the last few parts of a session and works out what shape its end has.
func readTail(db *sql.DB, s *session.Session, since time.Time) {
	rows, err := db.Query(`
		SELECT message.data, part.data, part.time_created
		FROM part JOIN message ON message.id = part.message_id
		WHERE part.session_id = ?
		ORDER BY part.time_created DESC, part.id DESC
		LIMIT ?
	`, s.ID, tailParts)
	if err != nil {
		return
	}
	defer rows.Close()

	// Collected newest first, then reversed, so the loop below reads in conversation order.
	var parts []part
	for rows.Next() {
		var messageData, partData []byte
		var created sql.NullInt64
		if err := rows.Scan(&messageData, &partData, &created); err != nil {
			continue
		}
		var msg struct {
			Role string `json:"role"`
		}
		_ = json.Unmarshal(messageData, &msg)
		var p struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(partData, &p)
		parts = append(parts, part{role: msg.Role, kind: p.Type, data: partData, at: msTime(created)})
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}

	activity := newActivity()
	var last *part
	for i := range parts {
		p := &parts[i]
		// Whether this part counts towards "what did it do". A part with no timestamp
		// cannot be placed in the window, so it is left out rather than guessed in.
		inWindow := !p.at.IsZero() && (since.IsZero() || !p.at.Before(since))
		if inWindow {
			activity.saw(p.at)
		}

		switch p.kind {
		case "text":
			text := textOf(p.data)
			if text == "" {
				continue
			}
			if p.role == "user" {
				s.LastRequest = text
				if s.FirstRequest == "" {
					s.FirstRequest = text
				}
				if inWindow {
					activity.request(text)
				}
			} else {
				s.LastText = text
			}
		case "tool":
			t := toolOf(p.data)
			if t.Tool != "" {
				s.LastTool = t.Tool
			}
			if f := t.file(); f != "" {
				s.LastFile = f
			}
			if inWindow {
				activity.tool(t.Tool, t.file())
				if t.State.Status == "error" {
					activity.errors++
				}
			}
		case "patch":
			if f := firstPatchFile(p.data); f != "" {
				s.LastFile = f
			}
			if inWindow {
				activity.file(firstPatchFile(p.data))
			}
		case "step-finish":
			// opencode records a step per model turn, which is the closest thing it has to
			// the Claude reader's assistant turns.
			if inWindow {
				activity.turns++
				if reasonOf(p.data) == "error" {
					activity.errors++
				}
			}
		}
		last = p
	}
	// The cap is a fixed number of parts, so a session busier than that has activity older
	// than what was read — the same honesty the Claude reader's byte cap needs.
	activity.truncated = len(parts) == tailParts && !since.IsZero()
	s.Activity = activity.result()

	s.Tail = classifyTail(last)
	if s.Tail == session.TailPendingTool && last != nil {
		s.PendingTool = toolOf(last.data).Tool
	}
}

func classifyTail(last *part) session.Tail {
	if last == nil {
		return session.TailUnknown
	}
	switch last.kind {
	case "tool":
		switch toolOf(last.data).State.Status {
		case "completed":
			// A finished tool with nothing after it means the turn stopped there.
			return session.TailToolResult
		case "error":
			return session.TailError
		default: // pending, running, or a status recap does not know
			return session.TailPendingTool
		}
	case "text":
		if last.role == "user" {
			return session.TailUserRequest
		}
		return session.TailAssistantText
	case "step-finish":
		// The model stopped for a reason it recorded; "stop" means it had said its piece.
		if reasonOf(last.data) == "stop" {
			return session.TailAssistantText
		}
		return session.TailToolResult
	case "step-start", "patch", "snapshot":
		return session.TailToolResult
	default:
		return session.TailUnknown
	}
}

func readTodos(db *sql.DB, s *session.Session) {
	rows, err := db.Query(`SELECT status FROM todo WHERE session_id = ?`, s.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var status sql.NullString
		if err := rows.Scan(&status); err != nil {
			continue
		}
		s.TodoTotal++
		if status.String == "completed" {
			s.TodoDone++
		}
	}
}

func textOf(data []byte) string {
	var p struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(data, &p)
	return p.Text
}

func reasonOf(data []byte) string {
	var p struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(data, &p)
	return p.Reason
}

type toolPart struct {
	Tool  string `json:"tool"`
	State struct {
		Status string `json:"status"`
		Input  struct {
			FilePath string `json:"filePath"`
			Path     string `json:"path"`
		} `json:"input"`
	} `json:"state"`
}

func (t toolPart) file() string {
	if t.State.Input.FilePath != "" {
		return t.State.Input.FilePath
	}
	return t.State.Input.Path
}

func toolOf(data []byte) toolPart {
	var t toolPart
	_ = json.Unmarshal(data, &t)
	return t
}

func firstPatchFile(data []byte) string {
	var p struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(data, &p); err != nil || len(p.Files) == 0 {
		return ""
	}
	return p.Files[0]
}

func msTime(v sql.NullInt64) time.Time {
	if !v.Valid || v.Int64 == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64)
}

// activityBuilder accumulates what a session did over the window, capping the lists so a
// long conversation cannot grow the cache without bound.
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

func (b *activityBuilder) tool(name, path string) {
	if name != "" {
		b.tools[name]++
	}
	b.file(path)
}

// file records a touch, by basename: a paragraph of absolute paths is unreadable, and the
// full path is already on the session as LastFile.
func (b *activityBuilder) file(path string) {
	if path == "" {
		return
	}
	name := filepath.Base(path)
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
	// the same store produce the same paragraph.
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
