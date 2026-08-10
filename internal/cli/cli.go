// Package cli parses recap's command line and drives the readers and renderer.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gortazar/recap/internal/cache"
	"github.com/gortazar/recap/internal/claude"
	"github.com/gortazar/recap/internal/config"
	"github.com/gortazar/recap/internal/opencode"
	"github.com/gortazar/recap/internal/proc"
	"github.com/gortazar/recap/internal/render"
	"github.com/gortazar/recap/internal/report"
	"github.com/gortazar/recap/internal/session"
	"github.com/gortazar/recap/internal/smart"
)

const usage = `recap — what were my coding agents doing?

Usage: recap [flags]

Prints one line per project with the status of its most recent agent session.

Flags:
`

// Env is everything recap reads from the machine, gathered in one place so the whole command
// can be run against a fixture tree in tests.
type Env struct {
	// ClaudeProjects is Claude Code's store, ~/.claude/projects by default.
	ClaudeProjects string
	// OpencodeStore is opencode's SQLite store.
	OpencodeStore string
	// ConfigPath is the optional config file, ~/.config/recap/config.toml by default.
	ConfigPath string
	// CachePath is where parsed sessions are remembered between runs.
	CachePath string
	// SmartEndpoint is the Messages API --smart calls. Empty means the real one.
	SmartEndpoint string
	// APIKey authenticates --smart. Empty means --smart cannot run.
	APIKey string
	// ProcRoot is the process table, /proc by default.
	ProcRoot string
	// Roots limits which projects are reported. Empty means the user's home directory.
	Roots []string
	// Now is the clock.
	Now func() time.Time
}

// DefaultEnv reads the real machine.
func DefaultEnv() Env {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = []string{home}
	}
	return Env{
		ClaudeProjects: claude.DefaultProjectsDir(),
		OpencodeStore:  opencode.DefaultStore(),
		ConfigPath:     config.DefaultPath(),
		CachePath:      cache.DefaultPath(),
		APIKey:         os.Getenv("ANTHROPIC_API_KEY"),
		ProcRoot:       proc.DefaultRoot,
		Roots:          roots,
		Now:            time.Now,
	}
}

// Run executes recap with the given arguments and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunWith(args, stdout, stderr, DefaultEnv())
}

// RunWith is Run against a supplied environment.
func RunWith(args []string, stdout, stderr io.Writer, env Env) int {
	fs := flag.NewFlagSet("recap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// Silent: the flag package calls this both for -h and after a bad flag, and it has
	// already written the error message itself. Help is answered on stdout below, where a
	// user piping `recap --help` into a pager expects it.
	fs.Usage = func() {}

	var (
		since    = fs.String("since", "24h", "hide sessions untouched for longer than this (e.g. 90m, 2d)")
		all      = fs.Bool("all", false, "ignore the time window")
		agent    = fs.String("agent", "", "only this agent: claude or opencode")
		project  = fs.String("project", "", "only this project, by name")
		running  = fs.Bool("running", false, "only projects with something running right now")
		root     = newRepeatable(fs, "root", "only report projects under this directory (repeatable)")
		noIcons  = fs.Bool("no-icons", false, "print status words instead of emoji")
		legend   = fs.Bool("legend", false, "explain the status vocabulary and exit")
		asJSON   = fs.Bool("json", false, "print the report as JSON (a versioned public interface)")
		confPath = fs.String("config", "", "read this config file instead of ~/.config/recap/config.toml")
		noCache  = fs.Bool("no-cache", false, "re-read every transcript instead of using ~/.cache/recap")
		useSmart = fs.Bool("smart", false, "have a model write the sentences; sends a short summary of each project to the Anthropic API (needs ANTHROPIC_API_KEY)")
		verbose  = fs.Bool("v", false, "add a line per session under each project")
		verbose2 = fs.Bool("verbose", false, "add a line per session under each project")
	)

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(stdout, usage)
			fs.SetOutput(stdout)
			fs.PrintDefaults()
			return 0
		}
		fmt.Fprintf(stderr, "recap: try --help\n")
		return 2
	}

	if *confPath != "" {
		env.ConfigPath = *confPath
	}
	cfg, err := config.Load(env.ConfigPath)
	if err != nil {
		fmt.Fprintln(stderr, "recap:", err)
		return 2
	}

	// Flags beat the config file, which beats the built-in defaults. `set` is how we tell a
	// flag left at its default from one the user actually typed.
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	opts := render.Options{
		Now:     env.Now(),
		NoIcons: *noIcons,
		Verbose: *verbose || *verbose2,
		Icons:   iconOverrides(cfg.Icon, stderr),
	}
	if !set["no-icons"] && cfg.Icons != nil {
		opts.NoIcons = !*cfg.Icons
	}

	if *legend {
		if err := render.Legend(stdout, opts); err != nil {
			fmt.Fprintln(stderr, "recap:", err)
			return 1
		}
		return 0
	}

	filters := report.Filters{
		Project:     *project,
		RunningOnly: *running,
		Roots:       env.Roots,
		Ignore:      cfg.Ignore,
	}
	if len(cfg.Roots) > 0 {
		filters.Roots = cfg.Roots
	}
	if len(*root) > 0 {
		filters.Roots = *root
	}
	if !*all {
		window := *since
		if !set["since"] && cfg.Since != "" {
			window = cfg.Since
		}
		d, err := parseDuration(window)
		if err != nil {
			fmt.Fprintf(stderr, "recap: --since %q: %v\n", window, err)
			return 2
		}
		filters.Since = d
	}
	if *agent != "" {
		a, err := parseAgent(*agent)
		if err != nil {
			fmt.Fprintf(stderr, "recap: %v\n", err)
			return 2
		}
		filters.Agent = a
	}

	// One agent's store being unreadable must not cost you the other's sessions, so each
	// failure is reported and the report is built from whatever was readable.
	var store *cache.Cache
	if !*noCache {
		store = cache.Open(env.CachePath)
	}

	var sessions []*session.Session
	claudeSessions, err := claude.Discover(env.ClaudeProjects, store)
	if err != nil {
		fmt.Fprintln(stderr, "recap: reading Claude Code sessions:", err)
	}
	sessions = append(sessions, claudeSessions...)

	opencodeSessions, err := opencode.Discover(env.OpencodeStore)
	if err != nil {
		fmt.Fprintln(stderr, "recap: reading opencode sessions:", err)
	}
	sessions = append(sessions, opencodeSessions...)

	// Saving the cache is an optimisation for next time, so a cache directory that cannot
	// be written is worth a word on stderr and nothing more.
	if err := store.Save(); err != nil {
		fmt.Fprintln(stderr, "recap: could not save the cache:", err)
	}

	procs, supported := proc.Scan(env.ProcRoot)
	live := proc.NewIndex(procs, supported)

	projects := report.Build(report.FilterSessions(sessions, filters, opts.Now), live, opts.Now)
	projects = report.FilterProjects(projects, filters)

	if *useSmart {
		// A model being unreachable is no reason to withhold the report, so a failure is
		// explained on stderr and the heuristic sentences stand.
		if err := rewrite(projects, cfg.SmartModel, env, opts.Now); err != nil {
			fmt.Fprintln(stderr, "recap: --smart:", err, "— keeping the plain sentences")
		}
	}

	if *asJSON {
		// Always a document, even with nothing to report: a consumer should not have to
		// tell "no sessions" apart from "recap failed" by parsing stderr.
		if err := render.JSON(stdout, projects, opts, render.LivenessSource(live.Supported())); err != nil {
			fmt.Fprintln(stderr, "recap:", err)
			return 1
		}
		return 0
	}

	if len(projects) == 0 {
		fmt.Fprintln(stderr, "recap: nothing to report")
		return 0
	}
	if err := render.Text(stdout, projects, opts); err != nil {
		fmt.Fprintln(stderr, "recap:", err)
		return 1
	}
	return 0
}

// rewrite replaces each project's sentence with one written by a model. Only the project's
// own line changes and only in memory: nothing is written back to any store.
func rewrite(projects []report.Project, model string, env Env, now time.Time) error {
	if len(projects) == 0 {
		return nil
	}
	client := smart.New(env.APIKey, model)
	if env.SmartEndpoint != "" {
		client.Endpoint = env.SmartEndpoint
	}

	facts := make([]smart.Facts, 0, len(projects))
	for _, p := range projects {
		facts = append(facts, factsOf(p, now))
	}

	ctx, cancel := context.WithTimeout(context.Background(), smart.Timeout)
	defer cancel()
	sentences, err := client.Sentences(ctx, facts)
	if err != nil {
		return err
	}
	for i := range projects {
		projects[i].Lead.Sentence = sentences[i]
		// Keep the session entry the project line came from in step, so --json does not
		// contradict itself.
		for j := range projects[i].Sessions {
			if projects[i].Sessions[j].Session == projects[i].Lead.Session {
				projects[i].Sessions[j].Sentence = sentences[i]
			}
		}
	}
	return nil
}

// factsOf is the entire set of things --smart sends about a project. Requests are truncated:
// the model needs the gist, not the paragraph.
func factsOf(p report.Project, now time.Time) smart.Facts {
	s := p.Lead.Session
	f := smart.Facts{
		Project:     p.Name,
		Status:      p.Status().Word(),
		Request:     clip(s.LastRequest, 300),
		AgentSaid:   clip(s.LastText, 300),
		LastTool:    s.LastTool,
		PendingTool: s.PendingTool,
		Age:         render.Age(now.Sub(s.LastActivity)),
		Heuristic:   p.Lead.Sentence,
	}
	if f.Request == "" {
		f.Request = clip(s.Title, 300)
	}
	if len(p.Agents) > 0 {
		f.Agent = string(p.Agents[0])
	}
	if s.TodoTotal > 0 {
		f.Progress = fmt.Sprintf("%d of %d done", s.TodoDone, s.TodoTotal)
	}
	return f
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// iconOverrides turns the config file's status words into statuses. A word recap does not
// know is worth a warning: the user typed it meaning to change something.
func iconOverrides(icons map[string]string, stderr io.Writer) map[session.Status]string {
	if len(icons) == 0 {
		return nil
	}
	byWord := map[string]session.Status{}
	for _, s := range session.Statuses() {
		byWord[s.Word()] = s
	}
	out := map[session.Status]string{}
	for word, glyph := range icons {
		st, ok := byWord[word]
		if !ok {
			fmt.Fprintf(stderr, "recap: config: no status called %q, ignoring its icon\n", word)
			continue
		}
		out[st] = glyph
	}
	return out
}

func parseAgent(name string) (session.Agent, error) {
	switch strings.ToLower(name) {
	case "claude", "claude-code", "claudecode":
		return session.AgentClaude, nil
	case "opencode":
		return session.AgentOpencode, nil
	default:
		return "", fmt.Errorf("--agent %q: expected claude or opencode", name)
	}
}

// parseDuration is time.ParseDuration plus days, which is the unit you actually reach for
// when asking what happened while you were away.
func parseDuration(s string) (time.Duration, error) {
	if rest, ok := strings.CutSuffix(strings.TrimSpace(s), "d"); ok {
		days, err := strconv.ParseFloat(rest, 64)
		if err != nil {
			return 0, fmt.Errorf("not a duration")
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("not a duration")
	}
	return d, nil
}

// repeatable collects a flag that may be given more than once.
type repeatable []string

func (r *repeatable) String() string { return strings.Join(*r, ", ") }

func (r *repeatable) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func newRepeatable(fs *flag.FlagSet, name, help string) *repeatable {
	var r repeatable
	fs.Var(&r, name, help)
	return &r
}
