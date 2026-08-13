#!/usr/bin/env bash
# Build the release artefacts: one static tarball per supported platform, plus a SHA256SUMS
# covering all of them.
#
#   nix develop -c tools/release-build.sh              # version from flake.nix
#   nix develop -c tools/release-build.sh --version 0.2 --out /tmp/dist
#
# Run it inside `nix develop` so a laptop and CI use the same Go. CGO is off throughout,
# which is what makes four targets a loop rather than a toolchain problem: the only
# non-stdlib dependency is modernc.org/sqlite, which is pure Go.
#
# Two runs of the same commit produce byte-identical archives: the build is -trimpath, and
# every tar entry gets fixed ownership and the commit's own timestamp.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Windows is deliberately absent: recap reads /proc and unix paths.
PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"

version=""
out="dist"
while [ $# -gt 0 ]; do
    case "$1" in
        --version) version="$2"; shift 2 ;;
        --out) out="$2"; shift 2 ;;
        -h|--help) sed -n '2,13p' "${BASH_SOURCE[0]}"; exit 0 ;;
        *) echo "unknown argument: $1" >&2; exit 2 ;;
    esac
done

# flake.nix is the one place the version lives; the release workflow refuses to publish a
# tag that disagrees with it.
if [ -z "$version" ]; then
    version="$(tools/read-version.sh)"
fi

commit="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
# The commit's own timestamp, so the build date is a fact about the source rather than about
# when someone happened to run this — and so two runs agree.
if source_epoch="$(git log -1 --format=%ct 2>/dev/null)"; then
    build_date="$(date -u -d "@$source_epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
        || date -u -r "$source_epoch" +%Y-%m-%dT%H:%M:%SZ)"
else
    source_epoch=0
    build_date="unknown"
fi

ldflags="-s -w"
ldflags="$ldflags -X github.com/gortazar/recap/internal/cli.Version=$version"
ldflags="$ldflags -X github.com/gortazar/recap/internal/cli.Commit=$commit"
ldflags="$ldflags -X github.com/gortazar/recap/internal/cli.BuildDate=$build_date"

rm -rf "$out"
mkdir -p "$out"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

echo "recap $version ($commit)"

for platform in $PLATFORMS; do
    goos="${platform%/*}"
    goarch="${platform#*/}"
    name="recap_${version}_${goos}_${goarch}"

    mkdir -p "$stage/$name"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags "$ldflags" -o "$stage/$name/recap" ./cmd/recap
    # The repository has no LICENSE, so the tarball ships the binary and the README only.
    cp README.md "$stage/$name/"

    # --sort, --mtime, --owner/--group and --numeric-owner between them remove everything
    # that would otherwise differ between two runs or two machines; gzip -n keeps the
    # timestamp out of the gzip header for the same reason.
    tar --sort=name \
        --mtime="@$source_epoch" \
        --owner=0 --group=0 --numeric-owner \
        --format=gnu \
        -C "$stage" -cf - "$name" \
        | gzip -n -9 > "$out/$name.tar.gz"

    echo "  $out/$name.tar.gz"
done

# One SHA256SUMS for the lot, with bare filenames so `sha256sum -c` works from inside
# whatever directory they were downloaded into.
( cd "$out" && sha256sum ./*.tar.gz | sed 's| \./| |' > SHA256SUMS )
echo "  $out/SHA256SUMS"
