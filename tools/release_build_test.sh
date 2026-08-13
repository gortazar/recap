#!/usr/bin/env bash
# Test tools/release-build.sh: the artefacts a release is made of, checked before any
# release exists. Cross-compiles four targets, so it is slower than the Go suite and runs as
# its own CI step rather than inside the flake sandbox, which has no module cache for four
# GOOS/GOARCH pairs.
#
#   nix develop -c tools/release_build_test.sh
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail=0
check() {
    if [ "$2" = "$3" ]; then
        echo "ok   - $1"
    else
        echo "FAIL - $1: got '$2', want '$3'"
        fail=1
    fi
}

version="9.9-test"
if ! tools/release-build.sh --version "$version" --out "$work/dist" > "$work/build.log" 2>&1; then
    cat "$work/build.log"
    echo "FAIL - release-build.sh exited non-zero"
    exit 1
fi

check "four tarballs, one per supported platform" \
    "$(find "$work/dist" -name '*.tar.gz' | wc -l)" "4"

for platform in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
    archive="$work/dist/recap_${version}_${platform}.tar.gz"
    check "$platform archive exists" "$([ -f "$archive" ] && echo yes || echo no)" "yes"
    [ -f "$archive" ] || continue

    # The tarball unpacks into a single named directory holding the binary and the README.
    # install.sh relies on exactly this layout.
    contents="$(tar -tzf "$archive" | sed "s|^recap_${version}_${platform}/||" \
        | grep -v '^$' | sort | tr '\n' ' ')"
    check "$platform archive holds the binary and the README" "$contents" "README.md recap "
done

check "SHA256SUMS covers every archive" \
    "$(wc -l < "$work/dist/SHA256SUMS")" "4"
check "SHA256SUMS verifies" \
    "$(cd "$work/dist" && sha256sum -c SHA256SUMS >/dev/null 2>&1 && echo yes || echo no)" "yes"

# The one artefact that can run here proves the stamping works end to end: an installer that
# cannot answer "did I install what I claimed?" is not much of an installer.
host_os="$(go env GOOS)"
host_arch="$(go env GOARCH)"
host_archive="$work/dist/recap_${version}_${host_os}_${host_arch}.tar.gz"
if [ -f "$host_archive" ]; then
    tar -xzf "$host_archive" -C "$work"
    reported="$("$work/recap_${version}_${host_os}_${host_arch}/recap" --version)"
    check "the built binary reports the version it was built with" \
        "$(echo "$reported" | cut -d' ' -f2)" "$version"
else
    echo "skip - no artefact for this host ($host_os/$host_arch)"
fi

# Reproducibility is the whole reason for the tar flags, so assert it rather than hope:
# same commit, same bytes.
tools/release-build.sh --version "$version" --out "$work/dist2" > /dev/null 2>&1
check "two runs of the same commit produce identical archives" \
    "$(cd "$work/dist2" && sha256sum -c "$work/dist/SHA256SUMS" >/dev/null 2>&1 && echo yes || echo no)" "yes"

# The version a release is built with, when nobody passes one, comes from flake.nix.
check "the default version is the one in flake.nix" \
    "$(tools/read-version.sh)" \
    "$(sed -n 's/^[[:space:]]*version = "\([^"]*\)";[[:space:]]*$/\1/p' flake.nix | head -1)"

[ "$fail" -eq 0 ] || { echo "release-build tests failed"; exit 1; }
echo "all release-build tests passed"
