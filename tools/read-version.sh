#!/usr/bin/env bash
# Print recap's version. flake.nix is the one place it is written down, so everything that
# needs it — the release build, the release workflow's version guard — reads it from here
# rather than keeping a second copy that can drift.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

version="$(sed -n 's/^[[:space:]]*version = "\([^"]*\)";[[:space:]]*$/\1/p' flake.nix | head -1)"

if [ -z "$version" ]; then
    echo "could not find a version in flake.nix (expected a line like: version = \"0.2\";)" >&2
    exit 1
fi

printf '%s\n' "$version"
