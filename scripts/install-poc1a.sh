#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo 'usage: MISTER_TARGET=user@host scripts/install-poc1a.sh PACKAGE.tar.gz' >&2
  exit 2
fi
target=${MISTER_TARGET:-}
package=$1
checksum=$package.sha256
if [ -z "$target" ]; then
  echo 'install-poc1a: MISTER_TARGET is required' >&2
  exit 2
fi
if ! printf '%s\n' "$target" | grep -Eq '^[A-Za-z0-9_.@:-]+$'; then
  echo 'install-poc1a: MISTER_TARGET contains unsafe characters' >&2
  exit 2
fi
if [ ! -f "$package" ] || [ ! -f "$checksum" ]; then
  echo 'install-poc1a: package and adjacent .sha256 file are required' >&2
  exit 2
fi

expected=$(awk 'NR == 1 { print $1 }' "$checksum")
actual=$(shasum -a 256 "$package" | awk '{ print $1 }')
if ! printf '%s\n' "$expected" | grep -Eq '^[0-9a-fA-F]{64}$' || [ "$actual" != "$expected" ]; then
  echo 'install-poc1a: checksum verification failed' >&2
  exit 1
fi

remote_archive=/tmp/mister-remote-poc1a.tar.gz
scp "$package" "$target:$remote_archive"
ssh "$target" sh -s -- "$remote_archive" < scripts/install-poc1a-target.sh
