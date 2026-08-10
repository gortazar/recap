# On-disk session formats (M0 spike)

Everything here was observed on this machine on 2026-08-09 by reading the real state
directories. Both formats are undocumented and drift between releases, so this file records
*what was seen*, with the version it was seen at. Readers must treat every field as optional.

Versions observed:

| Agent       | Version                          | Store                                  |
|-------------|----------------------------------|----------------------------------------|
| Claude Code | `version` field per record, e.g. `2.x` | `~/.claude/projects/<escaped-cwd>/<session-id>.jsonl` |
| opencode    | `1.18.14`                        | `~/.local/share/opencode/opencode.db` (SQLite) |

## Claude Code

### Layout

```
~/.claude/projects/
  -home-patxi-git-vacations/                       # cwd with / and . replaced by -
    89a6d812-...-20fc005dcbbc.jsonl                # one session
    89a6d812-...-20fc005dcbbc/                     # side files (not needed by recap)
```

The directory name is the session's working directory with every `/` (and `.`) turned into
`-`, which is **not** reversible by string substitution: `-home-patxi-git-blog-publication-automation`
could be `/home/patxi/git/blog-publication-automation` or `/home/patxi/git/blog/publication/automation`.
Do not try to invert it — every record inside carries the real `cwd`, so read the path from
the transcript instead and use the directory name only for grouping/globbing.

Transcripts get large: 20 MB and 3600 lines was the biggest seen. Tail-reading is mandatory.

### Records

One JSON object per line. `type` seen, in one 104-line session:

`assistant`, `user`, `system`, `attachment`, `queue-operation`, `custom-title`, `agent-name`,
`mode`, `permission-mode`, `file-history-snapshot`, `file-history-delta`, `last-prompt`.

Only `user` and `assistant` are conversational. The rest is bookkeeping, and crucially
**bookkeeping records are appended after the last conversational turn** — a session that
stopped normally typically ends with `system/stop_hook_summary`, `system/turn_duration` and
later `system/away_summary`; others end with `mode` / `permission-mode` / `last-prompt`.
So "what was the last event" means "the last record whose type is `user` or `assistant`".

Fields present on most conversational records: `type`, `uuid`, `parentUuid`, `timestamp`
(ISO-8601 UTC), `sessionId`, `cwd`, `version`, `gitBranch`, `isSidechain`, `message`.
`message.content` is either a string or a list of blocks with `type` in
`text` / `thinking` / `tool_use` / `tool_result`.

Not every record has a `timestamp` (`mode`, `last-prompt`, `permission-mode` had none), so
recency must fall back to the file mtime.

`isSidechain: true` marks sub-agent (Task tool) turns. Per PLAN.md there is no sub-agent
recursion in v1, so sidechain records are ignored when picking the last event.

### Signals recap uses

| Signal | How it appears |
|---|---|
| interrupted by the user | a `user` record whose text block starts `[Request interrupted` (34 seen across the store) |
| tool denied / blocked | `tool_result` content containing `<tool_use_error>` (e.g. `Blocked: ...`) |
| API failure | `assistant` record with `message.model == "<synthetic>"` and text containing `API Error` |
| outstanding tool call | trailing `assistant` record with a `tool_use` block whose `tool_use_id` has no matching `tool_result` later in the file |
| last user request | last non-sidechain `user` record with a `text` block that is not a `tool_result` and not an `[Request interrupted...]` marker |
| model | `message.model` on `assistant` records |
| branch | `gitBranch` |

### Fixtures

`tools/scrub-claude-fixture.py` derives the fixtures in `internal/claude/testdata/` from real
transcripts. It is **not** reproducible: the source sessions are live and keep growing, so
re-running it produces different content and will break the expectations pinned in the tests.
It is there to record *how* a fixture was made and to make new ones, not to regenerate the
existing ones. `tool-result-tail.jsonl` was additionally truncated at its last `tool_result`,
because no live session happened to end on one.

No session on this machine has ever used `TodoWrite`, so there is no recorded fixture for the
"3 of 7 done" progress marker on the Claude side; opencode's `todo` table is the source that
does exist.

### Empty / stub sessions

A file with a single unparseable or typeless line exists in the store (a stub written by
another tool). One bad session must not hide the rest.

## opencode 1.18.14

### Layout

```
~/.local/share/opencode/
  opencode.db, opencode.db-wal, opencode.db-shm    # SQLite, WAL mode
  log/  repos/  snapshot/<project-id>/
```

Older releases used a JSON file tree under `storage/`; that is **gone** in 1.18.14. There is
no `storage/` directory on this machine.

Opening with `file:...?mode=ro` returns the same rows as a copy of db+wal+shm read
read-write, so a plain read-only connection sees committed WAL content. recap opens
read-only and never writes (no checkpointing, no `-shm` creation), as required.

### Tables that matter

`session`:

`id`, `project_id`, `workspace_id`, `parent_id`, `slug`, `directory`, `path`, `title`,
`version`, `summary_*`, `metadata`, `cost`, `tokens_*`, `revert`, `permission`, `agent`,
`model` (JSON: `{"id":..., "providerID":...}`), `time_created`, `time_updated`,
`time_compacting`, `time_archived` — epoch **milliseconds**.

Much of what recap needs is already columnar here: `directory` (the cwd), `title` (a human
summary of the session), `agent`, `model`, `version`, `time_updated`. `parent_id` marks
sub-sessions (the sidechain equivalent) and is skipped in v1.

`message`: `id`, `session_id`, `time_created`, `time_updated`, `data` (JSON). `data` holds
`role`, `agent`, `modelID`, `providerID`, `path.cwd`, `tokens`, `time.created`/`time.completed`,
and `finish` — `stop` for a completed turn, `tool-calls` for a step that ended in tools.
A message with no `time.completed` is a turn that never finished.

`part`: `id`, `message_id`, `session_id`, `time_created`, `time_updated`, `data` (JSON) with
`type` in `text`, `tool`, `patch`, `step-start`, `step-finish`, … A `tool` part carries
`tool` (name), `callID` and `state.status` (`completed` seen; `pending`/`running`/`error`
exist in opencode's schema). `patch` parts list the files touched.

`todo`: per-session todo list (`content`, `status`, `priority`, `position`) — the closest
thing either agent has to an explicit "3 of 7 units done" marker.

`permission`: pending permission requests, keyed by `project_id` + `action` + `resource`.

`session_message` exists but is empty at this version; `message`/`part` are the live tables.

## Consequences for the status rules

- "Last event" must skip bookkeeping (Claude) and sub-sessions (both).
- ⚪ vs 🔴 hinges on the outstanding-`tool_use` and `[Request interrupted` signals for Claude,
  and on `finish`/`state.status` for opencode.
- ✅ needs an explicit completion marker; neither format has one that means "the task was
  done", so it stays reserved and unknown states report `❓` rather than ⚪ (per PLAN.md).
- Timestamps are ISO-8601 strings (Claude) vs epoch ms (opencode); the domain model uses
  `time.Time` and readers convert.
