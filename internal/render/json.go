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
	}
}

func agentStrings(agents []session.Agent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, string(a))
	}
	return out
}
