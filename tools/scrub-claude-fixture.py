#!/usr/bin/env python3
"""Turn a real Claude Code transcript into a committable test fixture.

Transcripts contain source code, absolute paths and occasionally secrets, so nothing goes
into testdata/ unscrubbed. What survives is the *structure* recap keys on — record types,
field names, block types, timestamps, ids — plus short human text, which is what the recap
sentence is assembled from.

Dropped or replaced:
  - tool_use inputs and tool_result bodies -> placeholders (this is where file contents and
    command output live)
  - thinking blocks -> a fixed short string
  - any text block longer than --max-text -> truncated with an explicit marker
  - home directory in cwd/gitBranch/paths -> /home/user
  - anything that looks like a token or key -> REDACTED

Usage:
  tools/scrub-claude-fixture.py SOURCE.jsonl DEST.jsonl [--head N] [--tail N]
"""

import argparse
import json
import os
import re
import sys

SECRET_RE = re.compile(
    r"(sk-[A-Za-z0-9_\-]{8,}|gh[pousr]_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9\-]{8,}"
    r"|eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,})"
)
HOME = os.path.expanduser("~")


def scrub_text(s, max_text):
    if not isinstance(s, str):
        return s
    s = s.replace(HOME, "/home/user")
    s = SECRET_RE.sub("REDACTED", s)
    if len(s) > max_text:
        s = s[:max_text] + " …[scrubbed]"
    return s


# Tool inputs are where file contents and shell commands live, so the default is to drop
# them. These two are kept because recap reports them: the file a session last touched, and
# the progress marker a TodoWrite leaves behind.
KEPT_INPUT_KEYS = ("file_path", "filePath", "notebook_path", "path")


def scrub_tool_input(inp, max_text):
    if not isinstance(inp, dict):
        return {"scrubbed": True}
    out = {"scrubbed": True}
    for k in KEPT_INPUT_KEYS:
        if isinstance(inp.get(k), str):
            out[k] = scrub_text(inp[k], max_text)
    todos = inp.get("todos")
    if isinstance(todos, list):
        out["todos"] = [
            {
                "content": scrub_text(t.get("content", ""), 60),
                "status": t.get("status"),
                "activeForm": scrub_text(t.get("activeForm", ""), 60),
            }
            for t in todos
            if isinstance(t, dict)
        ]
    return out


def scrub_block(b, max_text):
    if not isinstance(b, dict):
        return b
    t = b.get("type")
    if t == "text":
        return {"type": "text", "text": scrub_text(b.get("text", ""), max_text)}
    if t == "thinking":
        return {"type": "thinking", "thinking": "[scrubbed]", "signature": "[scrubbed]"}
    if t == "tool_use":
        return {
            "type": "tool_use",
            "id": b.get("id"),
            "name": b.get("name"),
            "input": scrub_tool_input(b.get("input"), max_text),
        }
    if t == "tool_result":
        content = b.get("content")
        # <tool_use_error> markers are a status signal, so keep the marker itself.
        if isinstance(content, str) and "<tool_use_error>" in content:
            content = "<tool_use_error>[scrubbed]</tool_use_error>"
        else:
            content = "[scrubbed]"
        out = {"type": "tool_result", "tool_use_id": b.get("tool_use_id"), "content": content}
        if "is_error" in b:
            out["is_error"] = b["is_error"]
        return out
    return {"type": t}


def scrub_record(o, max_text):
    if not isinstance(o, dict):
        return o
    out = {}
    for k, v in o.items():
        if k == "message" and isinstance(v, dict):
            m = {}
            for mk, mv in v.items():
                if mk == "content":
                    if isinstance(mv, list):
                        m[mk] = [scrub_block(b, max_text) for b in mv]
                    else:
                        m[mk] = scrub_text(mv, max_text)
                elif mk in ("role", "model", "id", "stop_reason", "type"):
                    m[mk] = mv
                elif mk == "usage":
                    m[mk] = mv
            out[k] = m
        elif k in ("toolUseResult", "attachment", "snapshot", "hookInfos", "backup"):
            out[k] = "[scrubbed]"
        elif k in ("content", "lastPrompt", "customTitle"):
            out[k] = scrub_text(v, max_text) if isinstance(v, str) else "[scrubbed]"
        elif isinstance(v, str):
            out[k] = scrub_text(v, max_text)
        else:
            out[k] = v
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("source")
    ap.add_argument("dest")
    ap.add_argument("--head", type=int, default=6, help="records to keep from the start")
    ap.add_argument("--tail", type=int, default=24, help="records to keep from the end")
    ap.add_argument("--max-text", type=int, default=400)
    args = ap.parse_args()

    lines = [l for l in open(args.source, errors="replace").read().splitlines() if l.strip()]
    if len(lines) > args.head + args.tail:
        kept = lines[: args.head] + lines[-args.tail :]
        elided = len(lines) - args.head - args.tail
        print(f"{args.source}: elided {elided} middle records", file=sys.stderr)
    else:
        kept = lines

    with open(args.dest, "w") as f:
        for l in kept:
            try:
                o = json.loads(l)
            except json.JSONDecodeError:
                f.write(l + "\n")  # keep malformed lines: recap must survive them
                continue
            f.write(json.dumps(scrub_record(o, args.max_text), ensure_ascii=False) + "\n")
    print(f"wrote {args.dest} ({len(kept)} records)", file=sys.stderr)


if __name__ == "__main__":
    main()
