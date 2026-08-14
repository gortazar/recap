package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var now = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

// transcript writes the smallest Claude Code transcript recap can make sense of: a request
// and an answer, in a project directory, at a given age.
func transcript(t *testing.T, store, projectDir, id string, age time.Duration) {
	t.Helper()
	dir := filepath.Join(store, strings.ReplaceAll(projectDir, "/", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ts := now.Add(-age).UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"type":"user","sessionId":%q,"cwd":%q,"gitBranch":"main","version":"2.1.0","timestamp":%q,"message":{"role":"user","content":[{"type":"text","text":"Run the suite"}]}}
{"type":"assistant","sessionId":%q,"cwd":%q,"timestamp":%q,"message":{"role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"All green."}]}}
`, id, projectDir, ts, id, projectDir, ts)
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// testEnv is a machine with a Claude store, no readable process table and no root limit.
func testEnv(t *testing.T) Env {
	t.Helper()
	return Env{
		ClaudeProjects: t.TempDir(),
		ProcRoot:       filepath.Join(t.TempDir(), "no-proc-here"),
		Now:            func() time.Time { return now },
	}
}

func run(t *testing.T, env Env, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = RunWith(args, &out, &errb, env)
	return code, out.String(), errb.String()
}

func TestReportsOneLinePerProject(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)
	transcript(t, env.ClaudeProjects, "/home/user/git/beta", "s2", 30*time.Minute)

	// --no-report, because this is about the one line per project; the paragraph under it
	// has its own tests.
	code, stdout, stderr := run(t, env, "-no-report")
	if code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), stdout)
	}
	if !strings.Contains(lines[0], "beta") {
		t.Errorf("first line is not the most recent project:\n%s", stdout)
	}
	if !strings.Contains(lines[0], "(Claude Code)") {
		t.Errorf("line does not name the agent:\n%s", stdout)
	}
}

// Without a readable process table recap cannot claim a quiet session is merely idle.
func TestUnknownLivenessIsReportedAsUnclear(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	_, stdout, _ := run(t, env, "-no-icons")
	if !strings.HasPrefix(stdout, "unclear") {
		t.Errorf("want an unclear status without a process table, got:\n%s", stdout)
	}
}

func TestSinceWindowHidesOlderSessions(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", 40*time.Hour)

	_, stdout, stderr := run(t, env)
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing outside the default 24h window", stdout)
	}
	if !strings.Contains(stderr, "nothing to report") {
		t.Errorf("stderr = %q, want an explanation", stderr)
	}

	if _, stdout, _ = run(t, env, "-all"); !strings.Contains(stdout, "alpha") {
		t.Errorf("--all did not bring the old session back:\n%s", stdout)
	}
	if _, stdout, _ = run(t, env, "-since", "2d"); !strings.Contains(stdout, "alpha") {
		t.Errorf("--since 2d did not bring the old session back:\n%s", stdout)
	}
}

func TestRootsHideProjectsOutsideThem(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)
	transcript(t, env.ClaudeProjects, "/tmp/scratch", "s2", time.Hour)

	_, stdout, _ := run(t, env, "-root", "/home/user/git")
	if strings.Contains(stdout, "scratch") {
		t.Errorf("a session outside the root was reported:\n%s", stdout)
	}
	if !strings.Contains(stdout, "alpha") {
		t.Errorf("the session inside the root was not reported:\n%s", stdout)
	}
}

func TestProjectFilter(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)
	transcript(t, env.ClaudeProjects, "/home/user/git/beta", "s2", time.Hour)

	_, stdout, _ := run(t, env, "-project", "alpha")
	if strings.Contains(stdout, "beta") || !strings.Contains(stdout, "alpha") {
		t.Errorf("--project alpha printed the wrong thing:\n%s", stdout)
	}
}

// opencodeStore builds a store from the opencode package's own fixtures, so there is one
// copy of them and the CLI is exercised against the schema the real agent writes.
func opencodeStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, f := range []string{"schema.sql", "states.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "opencode", "testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("loading %s: %v", f, err)
		}
	}
	return path
}

func TestBothAgentsAreReportedTogether(t *testing.T) {
	env := testEnv(t)
	env.OpencodeStore = opencodeStore(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/claude-only", "s1", time.Hour)

	_, stdout, stderr := run(t, env, "-all")
	if !strings.Contains(stdout, "claude-only (Claude Code)") {
		t.Errorf("Claude session missing:\n%s\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "(opencode)") {
		t.Errorf("opencode sessions missing:\n%s\n%s", stdout, stderr)
	}

	_, stdout, _ = run(t, env, "-all", "-agent", "opencode")
	if strings.Contains(stdout, "claude-only") {
		t.Errorf("--agent opencode still printed a Claude session:\n%s", stdout)
	}
}

func TestJSONOutputIsAValidDocument(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	code, stdout, stderr := run(t, env, "-json")
	if code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	var doc struct {
		Version  int `json:"version"`
		Projects []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"projects"`
		Liveness string `json:"liveness"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if doc.Version == 0 {
		t.Error("document has no version")
	}
	if len(doc.Projects) != 1 || doc.Projects[0].Name != "alpha" {
		t.Errorf("projects = %v, want just alpha", doc.Projects)
	}
	// This machine has no readable process table in the test environment, and the document
	// says so rather than leaving the consumer to guess why the status is unclear.
	if doc.Liveness != "unavailable" {
		t.Errorf("liveness = %q, want %q", doc.Liveness, "unavailable")
	}

	// Nothing to report is still a document, not an empty stream.
	_, stdout, _ = run(t, env, "-json", "-project", "nothing-called-this")
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("empty report is not JSON: %v\n%s", err, stdout)
	}
}

func configFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigFileSuppliesDefaults(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", 40*time.Hour)
	transcript(t, env.ClaudeProjects, "/home/user/git/scratch", "s2", time.Hour)
	env.ConfigPath = configFile(t, `
since = "3d"
roots = ["/home/user/git"]
ignore = ["/home/user/git/scratch"]
icons = false
`)

	_, stdout, stderr := run(t, env)
	if !strings.Contains(stdout, "alpha") {
		t.Errorf("since = 3d from the config file did not take effect:\n%s\n%s", stdout, stderr)
	}
	if strings.Contains(stdout, "scratch") {
		t.Errorf("an ignored directory was reported:\n%s", stdout)
	}
	if !strings.Contains(stdout, "unclear ") {
		t.Errorf("icons = false from the config file did not take effect:\n%s", stdout)
	}
}

// The answered question in PLAN.md: flags take precedence over the config file.
func TestFlagsBeatTheConfigFile(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", 40*time.Hour)
	env.ConfigPath = configFile(t, "since = \"3d\"\n")

	_, stdout, _ := run(t, env, "-since", "1h")
	if strings.Contains(stdout, "alpha") {
		t.Errorf("--since 1h did not override the config file's 3d:\n%s", stdout)
	}
}

func TestConfigFileCanReplaceAnIcon(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)
	env.ConfigPath = configFile(t, "[icon]\nunclear = \"??\"\n")

	_, stdout, _ := run(t, env)
	if !strings.HasPrefix(stdout, "?? alpha") {
		t.Errorf("the configured icon was not used:\n%s", stdout)
	}
}

func TestABrokenConfigFileStopsRecapWithAnExplanation(t *testing.T) {
	env := testEnv(t)
	env.ConfigPath = configFile(t, "sicne = \"3d\"\n")

	code, _, stderr := run(t, env)
	if code == 0 {
		t.Error("exit 0 for a config file recap could not understand")
	}
	if !strings.Contains(stderr, "unknown setting") {
		t.Errorf("stderr = %q, want it to name the mistake", stderr)
	}
}

func TestSecondRunReadsTheCache(t *testing.T) {
	env := testEnv(t)
	env.CachePath = filepath.Join(t.TempDir(), "sessions.json")
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	_, first, _ := run(t, env)
	if _, err := os.Stat(env.CachePath); err != nil {
		t.Fatalf("no cache was written: %v", err)
	}

	// Same report, this time without re-reading the transcript.
	_, second, _ := run(t, env)
	if first != second {
		t.Errorf("the cached run printed something different:\n%s\n%s", first, second)
	}

	// And --no-cache still works when the cache is there.
	_, third, _ := run(t, env, "-no-cache")
	if third != first {
		t.Errorf("--no-cache printed something different:\n%s\n%s", first, third)
	}
}

// The cache must never be the reason a report is wrong: a transcript that has grown since
// it was cached is read again.
func TestAGrownTranscriptIsNotServedFromTheCache(t *testing.T) {
	env := testEnv(t)
	env.CachePath = filepath.Join(t.TempDir(), "sessions.json")
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", 2*time.Hour)
	run(t, env)

	// Rewrite it with a newer timestamp, as a live session would.
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Minute)

	_, stdout, _ := run(t, env, "-v")
	if !strings.Contains(stdout, "1m ago") {
		t.Errorf("stale cache entry was used; output:\n%s", stdout)
	}
}

func TestSmartReplacesTheSentenceAndTheParagraph(t *testing.T) {
	var sent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"content":[{"type":"text","text":"[{\"sentence\":\"Ran the suite and stopped for the night.\",\"report\":\"One request, then eleven Bash calls over ten minutes.\"}]"}]}`)
	}))
	defer srv.Close()

	env := testEnv(t)
	env.SmartEndpoint = srv.URL
	env.APIKey = "test-key"
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	_, stdout, stderr := run(t, env, "-smart")
	if !strings.Contains(stdout, "Ran the suite and stopped for the night.") {
		t.Errorf("the model's sentence was not used:\n%s\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "eleven Bash calls") {
		t.Errorf("the model's paragraph was not used:\n%s\n%s", stdout, stderr)
	}
	if !strings.Contains(string(sent), "alpha") {
		t.Errorf("the project facts were not sent: %s", sent)
	}
	if strings.Contains(string(sent), env.ClaudeProjects) {
		t.Errorf("a path into the store was sent: %s", sent)
	}
}

// A model that cannot be reached must not cost you the report.
func TestSmartFallsBackToThePlainSentences(t *testing.T) {
	env := testEnv(t)
	env.SmartEndpoint = "http://127.0.0.1:1/never-listening"
	env.APIKey = "test-key"
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	code, stdout, stderr := run(t, env, "-smart")
	if code != 0 {
		t.Errorf("exit %d, want 0: the report is still worth printing", code)
	}
	if !strings.Contains(stdout, "Asked to") {
		t.Errorf("the plain sentence was not printed:\n%s", stdout)
	}
	if !strings.Contains(stdout, "no tool calls") {
		t.Errorf("the plain paragraph was not printed either:\n%s", stdout)
	}
	if !strings.Contains(stderr, "--smart") {
		t.Errorf("stderr = %q, want it to explain that --smart failed", stderr)
	}
}

func TestSmartWithoutAKeySaysSo(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	code, stdout, stderr := run(t, env, "-smart")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if !strings.Contains(stderr, "ANTHROPIC_API_KEY") {
		t.Errorf("stderr = %q, want it to name the variable to set", stderr)
	}
	if !strings.Contains(stdout, "alpha") {
		t.Errorf("the report was withheld:\n%s", stdout)
	}
}

// The answered question in PLAN.md chose option (b): the paragraph is the default, and
// --no-report is how you get the old one-line report back.
func TestParagraphIsOnByDefault(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	_, stdout, _ := run(t, env)
	if !strings.Contains(stdout, "no tool calls") {
		t.Errorf("no paragraph in the default output:\n%s", stdout)
	}

	_, plain, _ := run(t, env, "-no-report")
	if strings.Contains(plain, "no tool calls") {
		t.Errorf("--no-report still printed a paragraph:\n%s", plain)
	}
	if !strings.Contains(plain, "alpha") {
		t.Errorf("--no-report dropped the status line too:\n%s", plain)
	}
}

func TestReportCanBeTurnedOffInTheConfigFileAndBackOnWithAFlag(t *testing.T) {
	env := testEnv(t)
	env.ConfigPath = configFile(t, "report = false\n")
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	_, stdout, _ := run(t, env)
	if strings.Contains(stdout, "no tool calls") {
		t.Errorf("report = false in the config file was ignored:\n%s", stdout)
	}

	_, stdout, _ = run(t, env, "-report")
	if !strings.Contains(stdout, "no tool calls") {
		t.Errorf("--report did not override report = false:\n%s", stdout)
	}
}

func TestBadFlagValuesFailWithAMessage(t *testing.T) {
	env := testEnv(t)
	for _, args := range [][]string{
		{"-since", "yesterday"},
		{"-agent", "cursor"},
		{"-nope"},
	} {
		code, _, stderr := run(t, env, args...)
		if code == 0 {
			t.Errorf("%v: exit 0, want non-zero", args)
		}
		if stderr == "" {
			t.Errorf("%v: no message on stderr", args)
		}
	}
}

func TestLegendExplainsTheVocabulary(t *testing.T) {
	code, stdout, _ := run(t, testEnv(t), "-legend")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, word := range []string{"running", "waiting", "idle", "interrupted", "finished", "unclear"} {
		if !strings.Contains(stdout, word) {
			t.Errorf("legend does not mention %q:\n%s", word, stdout)
		}
	}
}

func TestHelpGoesToStdout(t *testing.T) {
	code, stdout, stderr := run(t, testEnv(t), "--help")
	if stderr != "" {
		t.Errorf("--help wrote to stderr as well: %q", stderr)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "recap") || !strings.Contains(stdout, "-since") {
		t.Errorf("--help output is not the usage text: %q", stdout)
	}
}

func TestVerboseAddsSessionLines(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	_, stdout, _ := run(t, env, "-v")
	if !strings.Contains(stdout, "s1") || !strings.Contains(stdout, "1h ago") {
		t.Errorf("-v did not add a session line:\n%s", stdout)
	}
}

// Every rejection has to show what would have been accepted, and blame the place the value
// was actually written.
func TestSinceMistakesAreExplained(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"-since", "yesterday"}, []string{`--since "yesterday"`, "6h, 90m or 7d", "s, m, h, d, w"}},
		{[]string{"-since", "6"}, []string{`--since "6"`, "6h, 90m or 7d"}},
		{[]string{"-since", "09:00"}, []string{`--since "09:00"`, "6h, 90m or 7d"}},
	} {
		code, stdout, stderr := run(t, env, c.args...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2", c.args, code)
		}
		if stdout != "" {
			t.Errorf("%v: printed a report despite a bad window:\n%s", c.args, stdout)
		}
		for _, want := range c.want {
			if !strings.Contains(stderr, want) {
				t.Errorf("%v: stderr = %q, want it to mention %q", c.args, stderr, want)
			}
		}
	}
}

// A bad value in the config file is not the flag's fault, and saying "--since" would send
// the reader looking in the wrong place.
func TestABadSinceInTheConfigFileNamesTheFileAndTheKey(t *testing.T) {
	env := testEnv(t)
	env.ConfigPath = configFile(t, "since = \"yesterday\"\n")
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	code, _, stderr := run(t, env)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, env.ConfigPath) {
		t.Errorf("stderr = %q, want it to name the config file", stderr)
	}
	if !strings.Contains(stderr, `since "yesterday"`) {
		t.Errorf("stderr = %q, want it to name the key and the value", stderr)
	}
	if strings.Contains(stderr, "--since") {
		t.Errorf("stderr = %q, want it not to blame the flag for a line in a file", stderr)
	}
}

// The one behaviour change in 0.4: a zero or negative window used to be a silent --all,
// because the filter simply skipped a window that was not positive.
func TestAZeroOrNegativeWindowIsAnError(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", 40*time.Hour)

	for _, args := range [][]string{
		{"-since", "0h"},
		{"-since", "-6h"},
		{"-since=-6h"},
		{"-since", "0s"},
	} {
		code, stdout, stderr := run(t, env, args...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2 — a non-positive window used to be a silent --all", args, code)
		}
		if stdout != "" {
			t.Errorf("%v: printed a report:\n%s", args, stdout)
		}
		if !strings.Contains(stderr, "must be positive") || !strings.Contains(stderr, "--all") {
			t.Errorf("%v: stderr = %q, want it to say the window must be positive and name --all", args, stderr)
		}
	}

	// And the flag that does what they probably meant still works.
	code, stdout, _ := run(t, env, "-all")
	if code != 0 || !strings.Contains(stdout, "alpha") {
		t.Errorf("--all after all that: exit %d, output:\n%s", code, stdout)
	}
}

func TestASincePinnedInTheConfigFileMustAlsoBePositive(t *testing.T) {
	env := testEnv(t)
	env.ConfigPath = configFile(t, "since = \"0\"\n")
	transcript(t, env.ClaudeProjects, "/home/user/git/alpha", "s1", time.Hour)

	code, _, stderr := run(t, env)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, env.ConfigPath) {
		t.Errorf("stderr = %q, want it to name the config file", stderr)
	}
}

// The forms the idea actually asked for, end to end.
func TestSinceHoursAndDays(t *testing.T) {
	env := testEnv(t)
	transcript(t, env.ClaudeProjects, "/home/user/git/recent", "s1", 8*time.Hour)
	transcript(t, env.ClaudeProjects, "/home/user/git/older", "s2", 3*24*time.Hour)

	_, stdout, _ := run(t, env, "-since", "6h")
	if strings.Contains(stdout, "recent") || strings.Contains(stdout, "older") {
		t.Errorf("--since 6h showed a session touched 8 hours ago:\n%s", stdout)
	}

	_, stdout, _ = run(t, env, "-since", "12h")
	if !strings.Contains(stdout, "recent") {
		t.Errorf("--since 12h hid a session touched 8 hours ago:\n%s", stdout)
	}
	if strings.Contains(stdout, "older") {
		t.Errorf("--since 12h showed a session touched 3 days ago:\n%s", stdout)
	}

	_, stdout, _ = run(t, env, "-since", "7d")
	for _, want := range []string{"recent", "older"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--since 7d hid %s:\n%s", want, stdout)
		}
	}

	// The same window, spelled the ways the new grammar allows.
	for _, spelling := range []string{"7d", "1w", "168h", "6d24h", "7D"} {
		_, spelled, _ := run(t, env, "-since", spelling)
		if !strings.Contains(spelled, "older") {
			t.Errorf("--since %s did not cover 3 days:\n%s", spelling, spelled)
		}
	}
}

// The help text is where someone looks after a rejected --since, so it has to name the same
// vocabulary the error message does.
func TestHelpNamesTheDurationUnitsAndShowsAnExample(t *testing.T) {
	_, stdout, _ := run(t, testEnv(t), "--help")
	for _, want := range []string{"s, m, h, d, w", "--since 6h", "--since 7d", "2d12h"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--help does not mention %q:\n%s", want, stdout)
		}
	}
}
