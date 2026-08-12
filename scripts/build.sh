#!/bin/sh
# Safe local-first bootstrap, matching the herdr-loop / herdr-sesh-bro policy:
# build the checked-out source when Go exists; otherwise accept only a release
# asset whose hash is pinned in this source tree. No release is pinned yet, so
# the fallback fails closed rather than executing an unverified download.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$root_dir"
mkdir -p bin

if command -v go >/dev/null 2>&1; then
  echo "herdr-notes: building with local Go toolchain..." >&2
  exec go build -trimpath -o bin/herdr-notes ./cmd/herdr-notes
fi

VERSION="v0.1.0"
os=$(uname -s)
arch=$(uname -m)
case "$os/$arch" in
  Darwin/arm64) target=darwin-arm64 ;;
  Darwin/x86_64) target=darwin-amd64 ;;
  Linux/x86_64) target=linux-amd64 ;;
  Linux/aarch64|Linux/arm64) target=linux-arm64 ;;
  *) echo "herdr-notes: unsupported $os/$arch; install Go" >&2; exit 1 ;;
esac

# Replace UNRELEASED with hashes from the release workflow when v0.1.0 exists.
case "$target" in
  *) expected=UNRELEASED ;;
esac
if [ "$expected" = UNRELEASED ]; then
  echo "herdr-notes: no verified $VERSION prebuilt is pinned for $target." >&2
  echo "herdr-notes: install Go (https://go.dev/dl/) and rerun." >&2
  exit 1
fi
