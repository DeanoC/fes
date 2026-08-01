#!/bin/sh
set -eu

target=${MISTER_TARGET:-}
if [ -z "$target" ]; then
  echo 'verify-poc1a-lock: MISTER_TARGET is required' >&2
  exit 2
fi
if ! printf '%s\n' "$target" | grep -Eq '^[A-Za-z0-9_.@:-]+$'; then
  echo 'verify-poc1a-lock: MISTER_TARGET contains unsafe characters' >&2
  exit 2
fi
if [ ! -f build/sources.poc1a.lock.toml ]; then
  echo 'verify-poc1a-lock: build/sources.poc1a.lock.toml does not exist' >&2
  exit 2
fi

fresh_inventory=$(mktemp "${TMPDIR:-/tmp}/mister-remote-inventory.XXXXXX")
trap 'rm -f "$fresh_inventory"' EXIT INT TERM
remote_script=/tmp/mister-remote-inventory.sh
scp deploy/poc1a/inventory.sh "$target:$remote_script"
ssh "$target" sh /tmp/mister-remote-inventory.sh > "$fresh_inventory"
diff -u build/sources.poc1a.lock.toml "$fresh_inventory"
