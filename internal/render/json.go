package render

import (
	"encoding/json"
	"io"
	"time"

	"github.com/gortazar/recap/internal/report"
	"github.com/gortazar/recap/internal/session"
)

// SchemaVersion is the version of the --json document. It is a public interface — the
// recap-gs GNOME Shell extension reads it — so it changes only when the shape does, and a
// consumer that does not recognise the version should say so rather than guess.
//
// Version 1: the document below.
//
// The version bumps when a field is removed, renamed, or changes meaning — never when an
// optional one is added. 0.3 added `report` and `activity` to every session and project and
// deliberately left the version at 1: every field a version-1 consumer reads is still there,
// unchanged, so bumping would have made recap-gs report an incompatible recap in exchange
// for nothing.
const SchemaVersion = 1

// Document is what --json prints.
type Document struct {
	Version     int       `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	// Liveness says how the statuses were arrived at: "process-table" when recap could see
	// what is running, "unavailable" when it could not and the statuses fall back to
	// recency. A consumer showing an "unclear" status can explain which it is.
	Liveness string        `json:"liveness"`
	Projects []jsonProject `json:"projects"`
}

type jsonProject struct {
	Name         string        `json:"name"`
	Dir          string        `json:"dir"`
	Status       string        `json:"status"`
	Icon         string        `json:"icon"`
	Recap        string        `json:"recap"`
	Report       string        `json:"report,omitempty"`
	Agents       []string      `json:"agents"`
	LastActivity time.Time     `json:"last_activity"`
	Sessions     []jsonSession `json:"sessions"`
}

type jsonSession struct {
	ID           string    `json:"id"`
	Agent        string    `json:"agent"`
	Status       string    `json:"status"`
	Icon         string    `json:"icon"`
	Recap        string    `json:"recap"`
	Title        string    `json:"title,omitempty"`
	Dir          string    `json:"dir,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	Model        string    `json:"model,omitempty"`
	AgentVersion string    `json:"agent_version,omitempty"`
	Started      time.Time `json:"started,omitempty"`
	LastActivity time.Time `json:"last_activity"`
	LastTool     string    `json:"last_tool,omitempty"`
	LastFile     string    `json:"last_file,omitempty"`
	TodoDone     int       `json:"todo_done,omitempty"`
	TodoTotal    int       `json:"todo_total,omitempty"`
	Source       string    `json:"source,omitempty"`
	Unreadable   string    `json:"unreadable,omitempty"`

	// Report is the paragraph, and Activity the counts it was built from, so a consumer can
	// render its own version rather than parsing prose back apart.
	Report   string        `json:"report,omitempty"`
	Activity *jsonActivity `json:"activity,omitempty"`
}

// jsonActivity is what a session did over the report window.
type jsonActivity struct {
	ToolCounts  map[string]int `json:"tool_counts,omitempty"`
	Files       []string       `json:"files,omitempty"`
	Requests    []string       `json:"requests,omitempty"`
	Turns       int            `json:"turns,omitempty"`
	Errors      int            `json:"errors,omitempty"`
	WindowStart time.Time      `json:"window_start,omitempty"`
	WindowEnd   time.Time      `json:"window_end,omitempty"`
	// Truncated says the reader hit its cap before reaching the start of the window, so
	// these counts cover from window_start onwards and no earlier.
	Truncated bool `json:"truncated,omitempty"`
}

func activityOf(a session.Activity) *jsonActivity {
	if a.Empty() {
		return nil
	}
	return &jsonActivity{
		ToolCounts:  a.ToolCounts,
		Files:       a.Files,
		Requests:    a.Requests,
		Turns:       a.Turns,
		Errors:      a.Errors,
		WindowStart: a.First,
		WindowEnd:   a.Last,
		Truncated:   a.Truncated,
	}
}

// LivenessSource describes where liveness came from, for the document's liveness field.
func LivenessSource(processTableAvailable bool) string {
	if processTableAvailable {
		return "process-table"
	}
	return "unavailable"
}

// JSON writes the machine-readable report.
func JSON(w io.Writer, projects []report.Project, opts Options, liveness string) error {
	doc := Document{
		Version:     SchemaVersion,
		GeneratedAt: opts.Now,
		Liveness:    liveness,
		// Never null: a consumer iterating the list should not have to special-case it.
		Projects: make([]jsonProject, 0, len(projects)),
	}
	for _, p := range projects {
		jp := jsonProject{
			Name:         p.Name,
			Dir:          p.Dir,
			Status:       p.Status().Word(),
			Icon:         opts.icon(p.Status()),
			Recap:        p.Lead.Sentence,
			Report:       p.Lead.Report,
			Agents:       agentStrings(p.Agents),
			LastActivity: p.LastActivity,
			Sessions:     make([]jsonSession, 0, len(p.Sessions)),
		}
		for _, e := range p.Sessions {
			jp.Sessions = append(jp.Sessions, jsonSessionOf(e, opts))
		}
		doc.Projects = append(doc.Projects, jp)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func jsonSessionOf(e report.Entry, opts Options) jsonSession {
	s := e.Session
	return jsonSession{
		ID:           s.ID,
		Agent:        string(s.Agent),
		Status:       e.Status.Word(),
		Icon:         opts.icon(e.Status),
		Recap:        e.Sentence,
		Title:        s.Title,
		Dir:          s.Dir,
		Branch:       s.Branch,
		Model:        s.Model,
		AgentVersion: s.Version,
		Started:      s.Started,
		LastActivity: s.LastActivity,
		LastTool:     s.LastTool,
		LastFile:     s.LastFile,
		TodoDone:     s.TodoDone,
		TodoTotal:    s.TodoTotal,
		Source:       s.Source,
		Unreadable:   s.Unreadable,
		Report:       e.Report,
		Activity:     activityOf(s.Activity),
	}
}

func agentStrings(agents []session.Agent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, string(a))
	}
	return out
}
