package proc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gortazar/recap/internal/session"
)

// fakeProc builds a /proc-shaped tree: one directory per pid, each with a NUL-separated
// cmdline and a cwd symlink, which is exactly what the real scanner reads.
func fakeProc(t *testing.T, entries map[string][]string, cwds map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, argv := range entries {
		dir := filepath.Join(root, pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		var buf []byte
		for _, a := range argv {
			buf = append(buf, []byte(a)...)
			buf = append(buf, 0)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), buf, 0o644); err != nil {
			t.Fatal(err)
		}
		if target, ok := cwds[pid]; ok {
			if err := os.Symlink(target, filepath.Join(dir, "cwd")); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Not a pid: the scanner must not trip over it.
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestScanFindsAgentProcesses(t *testing.T) {
	root := fakeProc(t,
		map[string][]string{
			"101": {"claude", "-r"},
			"102": {"opencode"},
			// An agent launched through a runtime rather than its own shim.
			"103": {"node", "/usr/lib/node_modules/@anthropic-ai/claude-code/cli.js"},
			// A shell the agent spawned. Its command line mentions claude, but it is not
			// an agent, and counting it would make every busy project look alive.
			"104": {"/usr/bin/zsh", "-c", "source /home/user/.claude/shell-snapshots/snap.sh"},
			// Something else entirely.
			"105": {"/usr/bin/firefox"},
			// No cwd symlink: unreadable, so it cannot be correlated with anything.
			"106": {"claude"},
		},
		map[string]string{
			"101": "/home/user/git/alpha",
			"102": "/home/user/git/beta",
			"103": "/home/user/git/gamma",
			"104": "/home/user/git/alpha",
			"105": "/home/user",
		},
	)

	found, ok := Scan(root)
	if !ok {
		t.Fatal("Scan reported the platform as unsupported for a well-formed tree")
	}

	got := map[string]session.Agent{}
	for _, p := range found {
		got[p.Dir] = p.Agent
	}
	want := map[string]session.Agent{
		"/home/user/git/alpha": session.AgentClaude,
		"/home/user/git/beta":  session.AgentOpencode,
		"/home/user/git/gamma": session.AgentClaude,
	}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for dir, agent := range want {
		if got[dir] != agent {
			t.Errorf("%s: agent = %q, want %q", dir, got[dir], agent)
		}
	}
}

func TestScanOfAMissingTreeIsUnsupportedNotFatal(t *testing.T) {
	if _, ok := Scan(filepath.Join(t.TempDir(), "no-such-proc")); ok {
		t.Error("Scan of a missing tree reported success")
	}
}

func TestLivenessCorrelatesByAgentAndDirectory(t *testing.T) {
	idx := NewIndex([]Process{
		{PID: 1, Agent: session.AgentClaude, Dir: "/home/user/git/alpha"},
		{PID: 2, Agent: session.AgentOpencode, Dir: "/home/user/git/beta"},
	}, true)

	cases := []struct {
		agent session.Agent
		dir   string
		want  session.Liveness
	}{
		{session.AgentClaude, "/home/user/git/alpha", session.Alive},
		{session.AgentClaude, "/home/user/git/alpha/", session.Alive},
		// Right directory, wrong agent: opencode is not what is running there.
		{session.AgentOpencode, "/home/user/git/alpha", session.Dead},
		{session.AgentClaude, "/home/user/git/beta", session.Dead},
		{session.AgentClaude, "/home/user/git/never-opened", session.Dead},
	}
	for _, c := range cases {
		if got := idx.Liveness(c.agent, c.dir); got != c.want {
			t.Errorf("Liveness(%q, %q) = %v, want %v", c.agent, c.dir, got, c.want)
		}
	}
}

// On a platform where recap cannot read the process table, every session must come back
// unknown rather than dead — "nothing is running" would be a claim recap cannot make.
func TestLivenessIsUnknownWhenTheProcessTableIsUnavailable(t *testing.T) {
	idx := NewIndex(nil, false)
	if got := idx.Liveness(session.AgentClaude, "/home/user/git/alpha"); got != session.LivenessUnknown {
		t.Errorf("Liveness = %v, want %v", got, session.LivenessUnknown)
	}
}
