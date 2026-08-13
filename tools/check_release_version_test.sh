#!/usr/bin/env bash
# Test the release workflow's version guard, without pushing a tag.
#
#   nix develop -c tools/check_release_version_test.sh
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

fail=0
check() {
    if [ "$2" = "$3" ]; then
        echo "ok   - $1"
    else
        echo "FAIL - $1: got '$2', want '$3'"
        fail=1
    fi
}

version="$(tools/read-version.sh)"

status() {
    tools/check-release-version.sh "$1" >/dev/null 2>&1 && echo pass || echo fail
}

check "the tag for the recorded version passes" "$(status "v$version")" "pass"
check "a tag for some other version fails" "$(status "v99.99")" "fail"
check "a tag with no v fails" "$(status "$version")" "fail"
check "an empty tag fails" "$(status "")" "fail"
check "a tag that is only a v fails" "$(status "v")" "fail"

# The message has to say which of the two is wrong, or whoever is looking at a red CI job
# has to go and look both of them up.
message="$(tools/check-release-version.sh v99.99 2>&1 || true)"
case "$message" in
    *"v99.99 says version 99.99"*"flake.nix says $version"*)
        echo "ok   - the failure names both versions" ;;
    *)
        echo "FAIL - the failure does not name both versions: $message"; fail=1 ;;
esac

check "no argument is a usage error" \
    "$(tools/check-release-version.sh >/dev/null 2>&1 && echo pass || echo fail)" "fail"

[ "$fail" -eq 0 ] || { echo "version guard tests failed"; exit 1; }
echo "all version guard tests passed"
