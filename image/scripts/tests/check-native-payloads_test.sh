#!/bin/sh
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT INT TERM
notice=$fixture/usr/share/mister-runtime/core-notices/fes.ramtest/$(printf '%064d' 1)/SOURCE.md
mkdir -p "$(dirname "$notice")"
printf 'verified source notice\n' >"$notice"
sh "$repo/scripts/check-native-payloads.sh" "$fixture" "$notice"
for payload in game.md GAME.MD extra/SOURCE.md game.rom game.zip agent.toml debug-gdb.py; do
  mkdir -p "$fixture/extra"
  touch "$fixture/$payload"
  if sh "$repo/scripts/check-native-payloads.sh" "$fixture" "$notice"; then
    printf 'accepted forbidden payload: %s\n' "$payload" >&2
    exit 1
  fi
  rm "$fixture/$payload"
done
touch "$(dirname "$notice")/game.md"
if sh "$repo/scripts/check-native-payloads.sh" "$fixture" "$notice"; then
  printf '%s\n' 'accepted game payload next to verified notice' >&2
  exit 1
fi
