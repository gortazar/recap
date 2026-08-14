#!/bin/sh
# Install recap without a Go toolchain or Nix.
#
#   curl -fsSL https://raw.githubusercontent.com/gortazar/recap/main/install.sh | sh
#
# or, if you would rather read it first (and you should):
#
#   curl -fsSL https://raw.githubusercontent.com/gortazar/recap/main/install.sh -o install.sh
#   less install.sh
#   sh install.sh
#
# Environment:
#   RECAP_VERSION       install this exact version instead of the latest release. Also the
#                       way past GitHub's unauthenticated API rate limit (60/hour/IP).
#   RECAP_INSTALL_DIR   where to put the binary (default: $HOME/.local/bin, so no sudo).
#   RECAP_BASE_URL      where release archives live. Exists for the tests.
#   RECAP_API_URL       where to ask what the latest release is. Exists for the tests.
#
# POSIX sh, no bashisms: this gets piped into whatever /bin/sh happens to be.
set -eu

REPO="gortazar/recap"
API_URL="${RECAP_API_URL:-https://api.github.com/repos/$REPO/releases/latest}"
BASE_URL="${RECAP_BASE_URL:-https://github.com/$REPO/releases/download}"
INSTALL_DIR="${RECAP_INSTALL_DIR:-$HOME/.local/bin}"

die() {
    echo "install.sh: $*" >&2
    exit 1
}

# Anything recap has no binary for. Better to say so than to install something that cannot
# run: both alternatives below build from source and work anywhere Go does.
unsupported() {
    cat >&2 <<EOF
install.sh: no recap release for $1.

recap ships binaries for linux and darwin on amd64 and arm64. On anything else, build it:

  nix profile install github:$REPO

or, with a Go toolchain:

  git clone https://github.com/$REPO && cd recap && go build ./cmd/recap
EOF
    exit 1
}

for tool in curl tar; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required and was not found"
done

os="$(uname -s)"
case "$os" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) unsupported "$os" ;;
esac

arch="$(uname -m)"
case "$arch" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) unsupported "$os/$arch" ;;
esac

if [ -n "${RECAP_VERSION:-}" ]; then
    version="${RECAP_VERSION#v}"
else
    # A rate-limited or offline API must produce a message pointing at the way round it,
    # not a parse error on an error body.
    body="$(curl -fsSL "$API_URL" 2>/dev/null)" || die "could not ask $API_URL for the latest release.
If this is GitHub's rate limit (60 requests an hour per IP, unauthenticated), pin a version instead:
  RECAP_VERSION=0.4 sh install.sh"
    version="$(printf '%s' "$body" | tr ',' '\n' | grep '"tag_name"' | head -1 |
        sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/')"
    [ -n "$version" ] || die "no published recap release found at $API_URL.
If one exists, pin it: RECAP_VERSION=0.4 sh install.sh"
fi

name="recap_${version}_${os}_${arch}"
archive="$name.tar.gz"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "recap $version ($os/$arch)"

curl -fsSL "$BASE_URL/v$version/$archive" -o "$tmp/$archive" ||
    die "could not download $BASE_URL/v$version/$archive"
curl -fsSL "$BASE_URL/v$version/SHA256SUMS" -o "$tmp/SHA256SUMS" ||
    die "could not download $BASE_URL/v$version/SHA256SUMS"

# Verified before anything is unpacked, so a bad download never becomes files on disk.
# Note what this does and does not prove: the checksums are served from the same release as
# the binary, so this catches a corrupted or truncated download, not a compromised release.
expected="$(grep "  $archive\$" "$tmp/SHA256SUMS" || true)"
[ -n "$expected" ] || die "$archive is not listed in SHA256SUMS"

if command -v sha256sum >/dev/null 2>&1; then
    ( cd "$tmp" && printf '%s\n' "$expected" | sha256sum -c - >/dev/null 2>&1 ) ||
        die "checksum mismatch for $archive — nothing installed"
elif command -v shasum >/dev/null 2>&1; then
    ( cd "$tmp" && printf '%s\n' "$expected" | shasum -a 256 -c - >/dev/null 2>&1 ) ||
        die "checksum mismatch for $archive — nothing installed"
else
    die "neither sha256sum nor shasum found; refusing to install an unverified binary"
fi

tar -xzf "$tmp/$archive" -C "$tmp" || die "could not unpack $archive"
[ -f "$tmp/$name/recap" ] || die "$archive did not contain a recap binary"

mkdir -p "$INSTALL_DIR" || die "could not create $INSTALL_DIR"
# install(1) rather than cp: it sets the mode in one step, and replaces the directory entry
# rather than writing through it, so a running recap is not corrupted mid-upgrade.
install -m 755 "$tmp/$name/recap" "$INSTALL_DIR/recap" ||
    die "could not install into $INSTALL_DIR"

echo "installed $INSTALL_DIR/recap"
"$INSTALL_DIR/recap" --version

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        cat >&2 <<EOF

$INSTALL_DIR is not on your PATH. Add it:

  export PATH="$INSTALL_DIR:\$PATH"
EOF
        ;;
esac
