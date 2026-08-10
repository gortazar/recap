// Package proc answers the only question a transcript cannot: is an agent running right now?
//
// A transcript looks the same whether the agent is thinking or was killed by a suspend, so
// liveness comes from the process table instead. The correlation is by working directory:
// Claude Code's command line does not carry the session id, but its cwd is the same
// directory the transcript records.
package proc

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gortazar/recap/internal/session"
)

// DefaultRoot is Linux's process table. Passing a different root is what makes the scanner
// testable without live agents.
const DefaultRoot = "/proc"

// Process is a running agent, reduced to what correlation needs.
type Process struct {
	PID   int
	Agent session.Agent
	Dir   string
}

// Scan reads the process table under root and returns the agent processes it recognises.
// The bool reports whether the process table could be read at all: false means recap has no
// way to tell what is running, which is different from "nothing is running".
func Scan(root string) ([]Process, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false
	}

	var found []Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a process directory
		}
		raw, err := os.ReadFile(filepath.Join(root, e.Name(), "cmdline"))
		if err != nil {
			continue // the process exited between the listing and the read, or is not ours
		}
		agent, ok := agentOf(splitArgv(raw))
		if !ok {
			continue
		}
		dir, err := os.Readlink(filepath.Join(root, e.Name(), "cwd"))
		if err != nil {
			continue // cwd is only readable for our own processes
		}
		found = append(found, Process{PID: pid, Agent: agent, Dir: filepath.Clean(dir)})
	}
	return found, true
}

func splitArgv(raw []byte) []string {
	var argv []string
	for _, a := range bytes.Split(raw, []byte{0}) {
		if len(a) > 0 {
			argv = append(argv, string(a))
		}
	}
	return argv
}

// runtimes launch an agent from a script rather than its own shim, so the agent's name is in
// argv[1] instead of argv[0].
var runtimes = map[string]bool{"node": true, "bun": true, "deno": true, "python": true, "python3": true}

// agentOf recognises an agent from its command line. It looks at argv[0] only (plus argv[1]
// for a runtime), never at the whole command line: the shells an agent spawns carry its name
// in their arguments, and counting those would make every busy project look alive.
func agentOf(argv []string) (session.Agent, bool) {
	if len(argv) == 0 {
		return "", false
	}
	if a, ok := agentByName(filepath.Base(argv[0])); ok {
		return a, true
	}
	if runtimes[filepath.Base(argv[0])] && len(argv) > 1 {
		if a, ok := agentByName(filepath.Base(argv[1])); ok {
			return a, true
		}
		// A script path such as .../claude-code/cli.js: the agent names the directory.
		for _, part := range strings.Split(filepath.ToSlash(argv[1]), "/") {
			if a, ok := agentByName(part); ok {
				return a, true
			}
		}
	}
	return "", false
}

func agentByName(name string) (session.Agent, bool) {
	switch strings.TrimSuffix(name, ".js") {
	case "claude", "claude-code":
		return session.AgentClaude, true
	case "opencode":
		return session.AgentOpencode, true
	}
	return "", false
}

// Index answers liveness questions about sessions.
type Index struct {
	// One directory can host more than one agent, so this is a set per directory rather
	// than a single agent.
	byDir     map[string]map[session.Agent]bool
	supported bool
}

// NewIndex builds an index over the processes found. supported is false when the process
// table could not be read, which makes every answer LivenessUnknown.
func NewIndex(procs []Process, supported bool) *Index {
	byDir := make(map[string]map[session.Agent]bool, len(procs))
	for _, p := range procs {
		dir := filepath.Clean(p.Dir)
		if byDir[dir] == nil {
			byDir[dir] = map[session.Agent]bool{}
		}
		byDir[dir][p.Agent] = true
	}
	return &Index{byDir: byDir, supported: supported}
}

// Current scans the real process table.
func Current() *Index {
	procs, ok := Scan(DefaultRoot)
	return NewIndex(procs, ok)
}

// Liveness reports whether an agent of this kind is running in this directory.
func (i *Index) Liveness(agent session.Agent, dir string) session.Liveness {
	if !i.supported {
		return session.LivenessUnknown
	}
	if i.byDir[filepath.Clean(dir)][agent] {
		return session.Alive
	}
	return session.Dead
}

// Supported reports whether recap could read the process table at all.
func (i *Index) Supported() bool { return i.supported }
