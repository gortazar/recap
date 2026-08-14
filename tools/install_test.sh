#!/usr/bin/env bash
# Test install.sh against a fake release, over file:// URLs, with no network.
#
# A curl-installer is otherwise untestable until a release exists — which is exactly when
# you find out it does not work. RECAP_BASE_URL and RECAP_API_URL exist for this: pointed at
# a directory laid out the way GitHub's release downloads are, the whole script runs for
# real, including the checksum verification and the install step.
#
#   nix develop -c tools/install_test.sh
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
contains() {
    case "$2" in
        *"$3"*) echo "ok   - $1" ;;
        *) echo "FAIL - $1: '$2' does not contain '$3'"; fail=1 ;;
    esac
}

version="9.9-test"
host_os="$(go env GOOS)"
host_arch="$(go env GOARCH)"

# A release directory shaped like GitHub's: .../download/v<version>/<files>
echo "building a fake release..."
tools/release-build.sh --version "$version" --out "$work/build" > /dev/null 2>&1
mkdir -p "$work/dl/v$version"
cp "$work/build"/* "$work/dl/v$version/"
mkdir -p "$work/api"
cat > "$work/api/latest.json" <<EOF
{
  "tag_name": "v$version",
  "name": "recap $version",
  "draft": false,
  "prerelease": false
}
EOF

export RECAP_BASE_URL="file://$work/dl"
export RECAP_API_URL="file://$work/api/latest.json"

run_install() {
    # A fresh HOME each time, so "did it create ~/.local/bin" is a real question, and the
    # test can never write into the real one.
    home="$1"; shift
    mkdir -p "$home"
    env HOME="$home" "$@" sh install.sh 2>&1 || echo "EXIT:$?"
}

# --- happy path --------------------------------------------------------------------------
out="$(run_install "$work/home1")"
check "installs into ~/.local/bin by default" \
    "$([ -x "$work/home1/.local/bin/recap" ] && echo yes || echo no)" "yes"
check "the installed binary runs and reports the version it claimed" \
    "$("$work/home1/.local/bin/recap" --version | cut -d' ' -f2)" "$version"
contains "says where it installed" "$out" ".local/bin/recap"
check "resolved the version from the API without being told" \
    "$(printf '%s' "$out" | grep -c "recap $version ($host_os/$host_arch)")" "1"

# --- a corrupted download -----------------------------------------------------------------
cp "$work/dl/v$version/SHA256SUMS" "$work/good-sums"
# Replace every hash with 64 zeros, which no file has. The obvious "change the first
# character" trick is a no-op whenever that character is already what you changed it to —
# it silently stopped corrupting anything the day a release hash happened to start with 0,
# and this test passed while verifying nothing.
awk '{ printf "%064d  %s\n", 0, $2 }' "$work/good-sums" > "$work/dl/v$version/SHA256SUMS"
out="$(run_install "$work/home2")"
contains "a checksum mismatch is reported" "$out" "checksum mismatch"
check "a checksum mismatch installs nothing" \
    "$([ -e "$work/home2/.local/bin/recap" ] && echo installed || echo nothing)" "nothing"
contains "a checksum mismatch fails" "$out" "EXIT:1"
cp "$work/good-sums" "$work/dl/v$version/SHA256SUMS"

# --- an archive missing from SHA256SUMS ----------------------------------------------------
grep -v "_${host_os}_${host_arch}" "$work/good-sums" > "$work/dl/v$version/SHA256SUMS"
out="$(run_install "$work/home3")"
contains "an archive absent from SHA256SUMS is refused" "$out" "not listed in SHA256SUMS"
check "an unlisted archive installs nothing" \
    "$([ -e "$work/home3/.local/bin/recap" ] && echo installed || echo nothing)" "nothing"
cp "$work/good-sums" "$work/dl/v$version/SHA256SUMS"

# --- an unsupported architecture -------------------------------------------------------------
# Faked by shadowing uname rather than by an override flag, so the real detection code runs.
mkdir -p "$work/fakebin"
cat > "$work/fakebin/uname" <<'EOF'
#!/bin/sh
case "${1:-}" in
    -m) echo riscv64 ;;
    *) echo Linux ;;
esac
EOF
chmod +x "$work/fakebin/uname"
out="$(env HOME="$work/home4" PATH="$work/fakebin:$PATH" sh install.sh 2>&1 || echo "EXIT:$?")"
contains "an unsupported arch names the Nix alternative" "$out" "nix profile install"
contains "an unsupported arch names the from-source alternative" "$out" "go build ./cmd/recap"
contains "an unsupported arch fails" "$out" "EXIT:1"
check "an unsupported arch installs nothing" \
    "$([ -e "$work/home4/.local/bin/recap" ] && echo installed || echo nothing)" "nothing"

# --- a custom install directory ---------------------------------------------------------------
out="$(run_install "$work/home5" RECAP_INSTALL_DIR="$work/custom bin")"
check "RECAP_INSTALL_DIR is honoured, spaces and all" \
    "$([ -x "$work/custom bin/recap" ] && echo yes || echo no)" "yes"
check "RECAP_INSTALL_DIR leaves the default alone" \
    "$([ -e "$work/home5/.local/bin/recap" ] && echo created || echo untouched)" "untouched"

# --- pinning a version --------------------------------------------------------------------------
# With the API pointed at nothing, RECAP_VERSION alone has to be enough: that is what makes
# it the way round the rate limit.
run_install "$work/home6" RECAP_VERSION="$version" RECAP_API_URL="file://$work/api/nope.json" > /dev/null
check "RECAP_VERSION installs without asking the API" \
    "$([ -x "$work/home6/.local/bin/recap" ] && echo yes || echo no)" "yes"
run_install "$work/home7" RECAP_VERSION="v$version" RECAP_API_URL="file://$work/api/nope.json" > /dev/null
check "RECAP_VERSION accepts a leading v" \
    "$([ -x "$work/home7/.local/bin/recap" ] && echo yes || echo no)" "yes"

# --- an unreachable API ----------------------------------------------------------------------------
out="$(run_install "$work/home8" RECAP_API_URL="file://$work/api/nope.json")"
contains "an unreachable API points at RECAP_VERSION" "$out" "RECAP_VERSION"
contains "an unreachable API fails" "$out" "EXIT:1"

# --- a reply with no release in it --------------------------------------------------------------------
echo '{"message":"Not Found"}' > "$work/api/empty.json"
out="$(run_install "$work/home9" RECAP_API_URL="file://$work/api/empty.json")"
contains "a reply with no release says so" "$out" "no published recap release"

# --- the PATH warning -----------------------------------------------------------------------------------
out="$(run_install "$work/home10" PATH="/usr/bin:/bin")"
contains "warns when the install dir is not on PATH" "$out" "is not on your PATH"
out="$(env HOME="$work/home11" PATH="$work/home11/.local/bin:$PATH" sh install.sh 2>&1)"
case "$out" in
    *"is not on your PATH"*) echo "FAIL - warns about PATH when it should not"; fail=1 ;;
    *) echo "ok   - stays quiet when the install dir is already on PATH" ;;
esac

# --- upgrading in place ------------------------------------------------------------------------------------
run_install "$work/home1" > /dev/null
check "re-running upgrades in place" \
    "$("$work/home1/.local/bin/recap" --version | cut -d' ' -f2)" "$version"

[ "$fail" -eq 0 ] || { echo "install tests failed"; exit 1; }
echo "all install tests passed"
