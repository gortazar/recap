// Package render prints a report. The default is one line per project, which is the whole
// point of recap: a few lines, no interaction.
package render

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gortazar/recap/internal/report"
	"github.com/gortazar/recap/internal/session"
)

// Options are the presentation choices the command line makes.
type Options struct {
	// Now is the clock, injected so output is reproducible in tests.
	Now time.Time
	// NoIcons substitutes words for the status emoji, for terminals and pipes that mangle
	// them.
	NoIcons bool
	// Verbose adds a line per session under each project.
	Verbose bool
	// NoReport suppresses the paragraph under each line, leaving the one-line report.
	NoReport bool
	// Width is the column to wrap paragraphs at. Zero means stdout is not a terminal, which
	// is a fixed 80: a redirect should produce a stable file, and CI output should not
	// depend on the runner's terminal.
	Width int
	// Icons overrides the glyph for individual statuses. Anything not named here keeps its
	// built-in icon.
	Icons map[session.Status]string
}

// icon is the glyph for a status, after any configured override.
func (o Options) icon(s session.Status) string {
	if glyph, ok := o.Icons[s]; ok && glyph != "" {
		return glyph
	}
	return s.Icon()
}

// Text writes the human-readable report.
func Text(w io.Writer, projects []report.Project, opts Options) error {
	width := wordWidth()
	for _, p := range projects {
		mark := opts.icon(p.Status())
		if opts.NoIcons {
			mark = fmt.Sprintf("%-*s", width, p.Status().Word())
		}
		if _, err := fmt.Fprintf(w, "%s %s (%s) -> %s\n", mark, shortName(p.Name), agentList(p.Agents), p.Lead.Sentence); err != nil {
			return err
		}

		// The paragraph belongs to the line above it, one indent deeper. Without -v that is
		// the project line, and the lead session speaks for the project as it does
		// everywhere else.
		if !opts.Verbose {
			if err := writeReport(w, p.Lead.Report, indent, opts); err != nil {
				return err
			}
			continue
		}
		for _, e := range p.Sessions {
			if _, err := fmt.Fprintf(w, "%s%s\n", indent, sessionLine(e, opts.Now)); err != nil {
				return err
			}
			if err := writeReport(w, e.Report, indent+indent, opts); err != nil {
				return err
			}
		}
	}
	return nil
}

// indent is one level. Session lines sit at one, their paragraphs at two.
const indent = "    "

// defaultWidth is what a paragraph wraps at when stdout is not a terminal.
const defaultWidth = 80

// minWidth stops a very narrow terminal from producing one word per line.
const minWidth = 40

// writeReport prints a paragraph under the line it belongs to: indented, wrapped, and
// followed by a blank line, because several three-line paragraphs stacked without one are
// unreadable.
func writeReport(w io.Writer, text, prefix string, opts Options) error {
	if opts.NoReport || text == "" {
		return nil
	}
	width := opts.Width
	if width <= 0 {
		width = defaultWidth
	}
	if width < minWidth {
		width = minWidth
	}
	for _, line := range wrap(text, width-len(prefix)) {
		if _, err := fmt.Fprintf(w, "%s%s\n", prefix, line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// wrap breaks text on spaces at the given width. A word longer than the width — a path,
// usually — is left to overflow rather than broken somewhere meaningless.
func wrap(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var (
		lines []string
		line  string
	)
	for _, word := range words {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	return append(lines, line)
}

// Legend prints the status vocabulary and the rule behind each icon.
func Legend(w io.Writer, opts Options) error {
	width := wordWidth()
	for _, s := range session.Statuses() {
		if _, err := fmt.Fprintf(w, "%s  %-*s  %s\n", opts.icon(s), width, s.Word(), s.Describe()); err != nil {
			return err
		}
	}
	return nil
}

// sessionLine is the per-session detail under a project: enough to tell two sessions in the
// same directory apart and to know where to pick one up.
func sessionLine(e report.Entry, now time.Time) string {
	s := e.Session
	parts := []string{s.ID, Age(now.Sub(s.LastActivity))}
	if s.Model != "" {
		parts = append(parts, s.Model)
	}
	if s.LastTool != "" {
		parts = append(parts, "last tool "+s.LastTool)
	}
	if s.LastFile != "" {
		parts = append(parts, filepath.Base(s.LastFile))
	}
	if s.Branch != "" {
		parts = append(parts, "on "+s.Branch)
	}
	return strings.Join(parts, "  ")
}

// Age renders a duration the way you would say it out loud.
func Age(d time.Duration) string {
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// maxName bounds the project column. Real project names are short; the long ones are the
// escaped store directory a session that never said where it ran fell back to, and their
// tail is the informative end.
const maxName = 40

func shortName(name string) string {
	r := []rune(name)
	if len(r) <= maxName {
		return name
	}
	return "…" + string(r[len(r)-maxName+1:])
}

func agentList(agents []session.Agent) string {
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, string(a))
	}
	if len(names) == 0 {
		return "unknown agent"
	}
	return strings.Join(names, ", ")
}

// wordWidth keeps the word column aligned however the vocabulary grows.
func wordWidth() int {
	width := 0
	for _, s := range session.Statuses() {
		if n := len(s.Word()); n > width {
			width = n
		}
	}
	return width
}
