#!/usr/bin/env python3
"""Build a throwaway agent store with one session in each state, for the screenshot.

The screenshot in the README is real output from the real binary, but the *data* is made up
here rather than taken from the machine it was shot on: a screenshot of someone's actual
sessions publishes their project names and their requests.

Prints a directory to use as HOME. Everything recap reads lives under it, so the screenshot
script needs no flags and no override hooks:

    <root>/.claude/projects/   the transcripts
    <root>/projects/<name>     the working directories the sessions claim to run in
"""

import json
import os
import tempfile
from datetime import datetime, timedelta, timezone

NOW = datetime.now(timezone.utc)


def ts(seconds_ago):
    return (NOW - timedelta(seconds=seconds_ago)).strftime("%Y-%m-%dT%H:%M:%S.000Z")


def record(kind, cwd, age, **extra):
    base = {
        "type": kind,
        "sessionId": extra.pop("sid"),
        "cwd": cwd,
        "gitBranch": extra.pop("branch", "main"),
        "version": "2.1.226",
        "isSidechain": False,
        "timestamp": ts(age),
    }
    base.update(extra)
    return base


def user_text(cwd, sid, age, text, branch="main"):
    return record("user", cwd, age, sid=sid, branch=branch,
                  message={"role": "user", "content": [{"type": "text", "text": text}]})


def assistant(cwd, sid, age, blocks, branch="main"):
    return record("assistant", cwd, age, sid=sid, branch=branch,
                  message={"role": "assistant", "model": "claude-opus-5", "content": blocks})


def tool_result(cwd, sid, age, call_id, branch="main"):
    return record("user", cwd, age, sid=sid, branch=branch,
                  message={"role": "user", "content": [
                      {"type": "tool_result", "tool_use_id": call_id, "content": "ok"}]})


def work(cwd, sid, start_age, step, calls):
    """A run of tool calls and their results, oldest first, for the paragraph to count."""
    records = []
    age = start_age
    for i, (tool, path) in enumerate(calls):
        call_id = f"{sid}-w{i}"
        inp = {"command": "make check"} if path is None else {"file_path": f"{cwd}/{path}"}
        records.append(assistant(cwd, sid, age, [
            {"type": "tool_use", "id": call_id, "name": tool, "input": inp}]))
        age -= step // 2
        records.append(tool_result(cwd, sid, age, call_id))
        age -= step // 2
    return records


def write_session(store, project_dir, sid, records):
    escaped = project_dir.replace("/", "-")
    dest = os.path.join(store, escaped)
    os.makedirs(dest, exist_ok=True)
    with open(os.path.join(dest, sid + ".jsonl"), "w") as f:
        for r in records:
            f.write(json.dumps(r) + "\n")


def main():
    root = tempfile.mkdtemp(prefix="recap-demo-")
    store = os.path.join(root, ".claude", "projects")
    projects = os.path.join(root, "projects")
    os.makedirs(store)

    def project(name):
        path = os.path.join(projects, name)
        os.makedirs(path, exist_ok=True)
        return path

    # Running: a live agent lives in this directory (tools/screenshot.sh starts one) and the
    # transcript grew seconds ago. It also has a day's worth of work behind it, so the
    # paragraph under it has something to count.
    d = project("orchestrator")
    write_session(store, d, "aaaa1111", [
        user_text(d, "aaaa1111", 14400, "Start on the release workflow"),
        *work(d, "aaaa1111", 14000, 200, [
            ("Bash", None), ("Read", "release-build.sh"), ("Bash", None),
            ("Edit", "release-build.sh"), ("Bash", None), ("Edit", "install.sh"),
            ("Bash", None), ("Read", "flake.nix"), ("Bash", None), ("Edit", "install.sh"),
            ("Bash", None), ("Write", "release.yml"), ("Bash", None), ("Edit", "ci.yml"),
        ]),
        user_text(d, "aaaa1111", 900, "Make the release workflow verify the checksum"),
        assistant(d, "aaaa1111", 20, [{"type": "tool_use", "id": "t1", "name": "Bash",
                                       "input": {"command": "make bench"}}]),
        tool_result(d, "aaaa1111", 5, "t1"),
    ])

    # Waiting: the agent asked a question and stopped.
    d = project("blog-pipeline")
    write_session(store, d, "bbbb2222", [
        user_text(d, "bbbb2222", 5400, "Work out why first-user source is coming back as direct"),
        *work(d, "bbbb2222", 5200, 400, [
            ("Read", "tags.js"), ("Bash", None), ("Read", "tags.js"), ("Edit", "tags.js"),
            ("Bash", None), ("Read", "gtm.json"),
        ]),
        assistant(d, "bbbb2222", 1200, [{"type": "text",
                                         "text": "Two of the three tags are wrong. Do you want me to fix the "
                                                 "template or the tag manager trigger?"}]),
        {"type": "system", "subtype": "turn_duration", "sessionId": "bbbb2222",
         "timestamp": ts(1200), "durationMs": 34000},
    ])

    # Interrupted: killed with a tool call outstanding.
    d = project("ansible-ascent")
    write_session(store, d, "cccc3333", [
        user_text(d, "cccc3333", 40000, "Start the container on the remote host over tailscale",
                  branch="deploy"),
        assistant(d, "cccc3333", 39000, [{"type": "tool_use", "id": "t9", "name": "Bash",
                                          "input": {"command": "ssh ..."}}]),
    ])

    # Interrupted by you.
    d = project("gnome-tasks")
    write_session(store, d, "dddd4444", [
        user_text(d, "dddd4444", 90000, "Rework the preferences window to use libadwaita",
                  branch="agent/gnome-tasks"),
        assistant(d, "dddd4444", 89000, [{"type": "text", "text": "Starting with the schema."}]),
        user_text(d, "dddd4444", 88000, "[Request interrupted by user]", branch="agent/gnome-tasks"),
    ])

    # Idle: stopped at an ordinary point.
    d = project("vacations")
    write_session(store, d, "eeee5555", [
        user_text(d, "eeee5555", 120000, "Write the accrual rules down as a markdown table"),
        assistant(d, "eeee5555", 119000, [{"type": "text", "text": "Done — the table is in docs/accrual.md."}]),
    ])

    # There is deliberately no unreadable session here. recap groups a transcript that never
    # said where it ran under its escaped store directory, which is a very long name, and a
    # screenshot is not the place to show it off; `recap --legend` documents ❓ instead.

    print(root)


if __name__ == "__main__":
    main()
