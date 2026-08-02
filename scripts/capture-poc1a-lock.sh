#!/bin/sh
set -eu

target=${MISTER_TARGET:-}
if [ -z "$target" ]; then
  echo 'capture-poc1a-lock: MISTER_TARGET is required' >&2
  exit 2
fi
case "$target" in
  root@*) target_host=${target#root@} ;;
  *) echo 'capture-poc1a-lock: MISTER_TARGET must be root@HOST' >&2; exit 2 ;;
esac
case "$target_host" in
  ''|[!A-Za-z0-9]*|*[!A-Za-z0-9_.-]*)
    echo 'capture-poc1a-lock: MISTER_TARGET must be root@HOST with a hostname or IPv4 address' >&2
    exit 2
    ;;
esac

mkdir -p build
temporary=$(mktemp build/.sources.poc1a.lock.XXXXXX)
trap 'rm -f "$temporary"' EXIT INT TERM
remote_script=/tmp/mister-remote-inventory.sh
scp -- deploy/poc1a/inventory.sh "$target:$remote_script"
ssh -- "$target" sh /tmp/mister-remote-inventory.sh > "$temporary"

grep -q '^format = 1$' "$temporary"
for artifact in main_mister menu kernel linux_root megadrive_core snes_core; do
  grep -q "^name = \"$artifact\"$" "$temporary"
done
grep -q '^name = "controller_' "$temporary"
grep -q '^\[\[libraries\]\]$' "$temporary"
mv "$temporary" build/sources.poc1a.lock.toml
trap - EXIT INT TERM
echo 'captured build/sources.poc1a.lock.toml'
