#!/bin/sh
set -eu

token=${MISTER_TOKEN:-}
version=${VERSION:-0.1.0}

if ! printf '%s\n' "$token" | grep -Eq '^[A-Za-z0-9_-]{32,128}$'; then
  echo 'package-poc1a: MISTER_TOKEN must contain 32-128 letters, digits, underscores, or hyphens' >&2
  exit 2
fi
if ! printf '%s\n' "$version" | grep -Eq '^[A-Za-z0-9._-]+$'; then
  echo 'package-poc1a: VERSION contains unsafe characters' >&2
  exit 2
fi

stage=dist/staging/mister-remote
archive="dist/mister-remote-poc1a-$version.tar.gz"
make build-agent VERSION="$version"
rm -rf "$stage"
mkdir -p "$stage"
cp bin/mister-agent-linux-armv7 "$stage/mister-agent"
cp deploy/poc1a/start-agent.sh "$stage/start-agent.sh"
cp deploy/poc1a/MiSTer.ini.fragment "$stage/MiSTer.ini.fragment"
sed "s|example-poc-token-not-valid|$token|" deploy/poc1a/agent.toml.example > "$stage/agent.toml"
chmod 0755 "$stage/mister-agent" "$stage/start-agent.sh"
chmod 0600 "$stage/agent.toml" "$stage/MiSTer.ini.fragment"

if find dist/staging -type f \( \
  -iname '*.rom' -o -iname '*.bin' -o -iname '*.gen' -o -iname '*.md' -o \
  -iname '*.sfc' -o -iname '*.smc' -o -iname '*.rbf' -o -iname '*.map' \
\) | grep -q .; then
  echo 'package-poc1a: forbidden game, core, or controller-map file in staging' >&2
  exit 1
fi

go run ./cmd/package-poc1a \
  --source "$stage" \
  --output "$archive" \
  --time 2026-08-01T00:00:00Z
shasum -a 256 "$archive" > "$archive.sha256"
printf 'created %s\n' "$archive"
