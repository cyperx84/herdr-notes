#!/bin/sh
# Safe local-first bootstrap, matching the herdr-loop / herdr-sesh-bro policy:
# build the checked-out source when Go exists; otherwise download only a release
# asset whose SHA256 is pinned in this source tree.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$root_dir"
mkdir -p bin

if command -v go >/dev/null 2>&1; then
  echo "herdr-notes: building with local Go toolchain..." >&2
  exec go build -trimpath -ldflags "-X main.version=v0.1.0" -o bin/herdr-notes ./cmd/herdr-notes
fi

VERSION="v0.1.0"
REPO="cyperx84/herdr-notes"
os=$(uname -s)
arch=$(uname -m)
case "$os/$arch" in
  Darwin/arm64)
    target=darwin-arm64
    expected=802293aa5c78cbe9c071a4ccb125642e0ca3792ee9eff6ab792067191528c50e
    ;;
  Darwin/x86_64)
    target=darwin-amd64
    expected=1b672b291cb9067aa9b39ac7e367dabffc459542bce1c9a2be8607c97002f5d7
    ;;
  Linux/x86_64)
    target=linux-amd64
    expected=c4c0a9f4aa4c0d64823a36685369397197e2b3cb663b6819d20e91791f8bb7f1
    ;;
  Linux/aarch64|Linux/arm64)
    target=linux-arm64
    expected=09a2db263393f689dab68991d7acbcdc0db04605e3d87a1664fd4d7ab5cb8948
    ;;
  *) echo "herdr-notes: unsupported $os/$arch; install Go" >&2; exit 1 ;;
esac

asset="herdr-notes-${VERSION}-${target}.tar.gz"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "herdr-notes: no Go toolchain; downloading verified $asset" >&2
if command -v curl >/dev/null 2>&1; then
  curl -fsSL -o "$tmp/$asset" "$url"
elif command -v wget >/dev/null 2>&1; then
  wget -q -O "$tmp/$asset" "$url"
else
  echo "herdr-notes: curl or wget is required when Go is unavailable" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$asset" | cut -d ' ' -f 1)
elif command -v shasum >/dev/null 2>&1; then
  got=$(shasum -a 256 "$tmp/$asset" | cut -d ' ' -f 1)
else
  echo "herdr-notes: sha256sum or shasum is required to verify downloads" >&2
  exit 1
fi
if [ "$got" != "$expected" ]; then
  echo "herdr-notes: checksum mismatch for $asset" >&2
  echo "expected: $expected" >&2
  echo "got:      $got" >&2
  exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp"
install -m 0755 "$tmp/herdr-notes" bin/herdr-notes
echo "herdr-notes: installed verified $asset" >&2
