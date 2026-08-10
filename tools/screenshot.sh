#!/usr/bin/env bash
# Take the README screenshot: real output from the real binary, against the made-up store
# tools/demo-store.py builds, so nobody's actual project names end up in the repo.
#
# The 🟢 line is genuine liveness detection, not a mock: this starts a process whose argv[0]
# is "claude" in the demo project's directory, which is exactly what recap looks for.
#
#   nix develop -c tools/screenshot.sh
set -euo pipefail

cd "$(dirname "$0")/.."
# SVG rather than PNG: charm-freeze 0.2.2 crashes rasterizing this output (a Go stack
# overflow in its text layout), and SVG renders sharp on GitHub anyway.
out=screenshots/recap.svg

command -v freeze >/dev/null || { echo "freeze not found: run inside nix develop" >&2; exit 1; }
# charmbracelet/freeze, packaged as charm-freeze in nixpkgs — not the shellcode loader that
# also calls itself freeze.

go build -o /tmp/recap-screenshot ./cmd/recap

# The demo store is laid out as a home directory, so pointing HOME at it is all it takes to
# make recap read it instead of yours.
root="$(python3 tools/demo-store.py)"
trap 'rm -rf "$root"' EXIT

# Stand-ins for running agents: same argv[0] and same working directory as the real thing,
# which is all recap's liveness check looks at. One project is mid-work (🟢) and one has
# stopped and is waiting for an answer (🟡) — the difference is the transcript, not the
# process.
agents=()
for p in orchestrator blog-pipeline; do
  # Detached from this script's streams, so nothing downstream waits on them.
  ( cd "$root/projects/$p" && exec -a claude sleep 30 ) </dev/null >/dev/null 2>&1 &
  agents+=($!)
done
trap 'kill "${agents[@]}" 2>/dev/null || true; rm -rf "$root"' EXIT
sleep 0.2

mkdir -p screenshots
HOME="$root" /tmp/recap-screenshot --all > "$root/out.txt"

# Take them down as soon as the report is captured: the rest of the script has no use for
# them, and a stray sleep outliving the run is a nuisance.
kill "${agents[@]}" 2>/dev/null || true
wait "${agents[@]}" 2>/dev/null || true
trap 'rm -rf "$root"' EXIT

cat "$root/out.txt"
freeze "$root/out.txt" --output "$out" --window --theme dracula --font.size 15 --padding 24

echo "wrote $out"
