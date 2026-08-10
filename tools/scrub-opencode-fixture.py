#!/usr/bin/env python3
"""Dump the tables recap reads from an opencode store into a scrubbed SQL fixture.

The store is a SQLite database holding whole conversations, so nothing goes into testdata/
verbatim. The schema and the record *shapes* are what the tests need; the prose, the tool
inputs and the file contents are replaced.

The result is plain SQL rather than a binary database, so the fixture is reviewable in a
diff and the tests build the database from it.

Usage:
  tools/scrub-opencode-fixture.py [~/.local/share/opencode/opencode.db] DEST.sql
"""

import argparse
import json
import os
import re
import sqlite3
import sys

SECRET_RE = re.compile(
    r"(sk-[A-Za-z0-9_\-]{8,}|gh[pousr]_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9\-]{8,}"
    r"|eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,})"
)
HOME = os.path.expanduser("~")
MAX_TEXT = 300

# The columns recap reads, so the fixture pins exactly the schema the reader depends on.
TABLES = {
    "session": [
        "id", "project_id", "parent_id", "slug", "directory", "title", "version",
        "agent", "model", "time_created", "time_updated", "time_archived",
    ],
    "message": ["id", "session_id", "time_created", "time_updated", "data"],
    "part": ["id", "message_id", "session_id", "time_created", "time_updated", "data"],
    "todo": ["session_id", "content", "status", "priority", "position",
             "time_created", "time_updated"],
}


def scrub_text(s):
    s = s.replace(HOME, "/home/user")
    s = SECRET_RE.sub("REDACTED", s)
    if len(s) > MAX_TEXT:
        s = s[:MAX_TEXT] + " …[scrubbed]"
    return s


def scrub_json(value):
    """Scrub a JSON blob, keeping every key but blunting the free text inside it."""
    if isinstance(value, dict):
        out = {}
        for k, v in value.items():
            if k in ("content", "output", "stdout", "stderr", "diff", "patch", "prompt"):
                out[k] = "[scrubbed]"
            else:
                out[k] = scrub_json(v)
        return out
    if isinstance(value, list):
        return [scrub_json(v) for v in value]
    if isinstance(value, str):
        return scrub_text(value)
    return value


def scrub_cell(column, value):
    if value is None or not isinstance(value, str):
        return value
    if column == "data":
        try:
            return json.dumps(scrub_json(json.loads(value)), ensure_ascii=False)
        except json.JSONDecodeError:
            return "[scrubbed]"
    return scrub_text(value)


def literal(v):
    if v is None:
        return "NULL"
    if isinstance(v, (int, float)):
        return repr(v)
    return "'" + str(v).replace("'", "''") + "'"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("source", nargs="?",
                    default=os.path.expanduser("~/.local/share/opencode/opencode.db"))
    ap.add_argument("dest")
    args = ap.parse_args()

    con = sqlite3.connect(f"file:{args.source}?mode=ro", uri=True)
    con.row_factory = sqlite3.Row

    with open(args.dest, "w") as f:
        f.write("-- Scrubbed from a real opencode store by tools/scrub-opencode-fixture.py.\n")
        f.write("-- Only the tables and columns recap reads; prose and tool payloads replaced.\n\n")
        for table, columns in TABLES.items():
            rows = list(con.execute(f"SELECT {', '.join(columns)} FROM {table}"))
            print(f"{table}: {len(rows)} rows", file=sys.stderr)
            for r in rows:
                values = ", ".join(literal(scrub_cell(c, r[c])) for c in columns)
                f.write(f"INSERT INTO {table} ({', '.join(columns)}) VALUES ({values});\n")
            f.write("\n")


if __name__ == "__main__":
    main()
