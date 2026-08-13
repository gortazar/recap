#!/usr/bin/env bash
# Static checks over every shell script here, plus a parse.
#
# install.sh gets piped straight into whatever /bin/sh a stranger happens to have, so it is
# checked as POSIX sh specifically: a bashism that works on the machine it was written on is
# exactly the bug that would strand them.
#
# Note when editing these comments: a line whose first word is the linter's own name is read
# as a directive for it, and fails the run.
#
#   nix develop -c tools/lint-shell.sh
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

shellcheck --shell=sh install.sh
sh -n install.sh
echo "ok   - install.sh is clean POSIX sh"

for script in tools/*.sh; do
    shellcheck "$script"
    bash -n "$script"
    echo "ok   - $script"
done
