#!/bin/sh
set -eu

root=$1
# The caller has already verified this notice's contents and selected package.
verified_notice=$2
if find "$root" -type f ! -path "$verified_notice" \( \
  -iname '*.rom' -o -iname '*.sfc' -o -iname '*.smc' -o \
  -iname '*.md' -o -iname '*.gen' -o -iname '*.zip' -o \
  -iname '*.bin' -o -iname '*.mgl' -o -iname '*.map' -o -name 'agent.toml' \
  -o -name '*-gdb.py' \
\) -print -quit | grep -q .; then
  printf '%s\n' 'verify-target-image: forbidden game, runtime, or debug payload found' >&2
  exit 1
fi
