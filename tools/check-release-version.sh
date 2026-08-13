#!/usr/bin/env bash
# Assert that a release tag and the version recorded in flake.nix are the same thing.
#
#   tools/check-release-version.sh v0.2
#
# A release whose binary reports a different number than its tag is worse than no release:
# every bug report after it is against a version that does not exist. The release workflow
# runs this before it builds anything, so an inconsistent tag fails in seconds rather than
# after publishing.
#
# It is a script rather than inline yaml so it can be tested without pushing a tag.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

if [ $# -ne 1 ]; then
    echo "usage: $(basename "$0") <tag>" >&2
    exit 2
fi

tag="$1"

case "$tag" in
    v?*) ;;
    *)
        echo "tag '$tag' does not look like a release tag (expected v<version>, e.g. v0.2)" >&2
        exit 1
        ;;
esac

tag_version="${tag#v}"
flake_version="$(tools/read-version.sh)"

if [ "$tag_version" != "$flake_version" ]; then
    cat >&2 <<EOF
tag $tag says version $tag_version, flake.nix says $flake_version.

Publishing this would ship a binary whose --version disagrees with the release it came
from. Either the version in flake.nix was not bumped, or the tag is wrong:

  git tag -d $tag                 # if the tag is wrong
  \$EDITOR flake.nix               # if the version is
EOF
    exit 1
fi

echo "$tag matches the version in flake.nix ($flake_version)"
