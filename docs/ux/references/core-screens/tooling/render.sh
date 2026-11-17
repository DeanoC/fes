#!/usr/bin/env bash
# Re-render docs/ux/references/core-screens with the production rooms engine and
# software renderer. A Go overlay supplies the throwaway harness without
# creating or replacing a file in the tenfoot package.
set -euo pipefail
tooling="$(cd "$(dirname "$0")" && pwd)"
here="$(cd "$tooling/.." && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"
if [[ $# -gt 2 ]]; then
  echo "usage: $0 [--update [directory] | directory]" >&2
  exit 2
fi

mode=check
destination=$here
if [[ ${1:-} == --update ]]; then
  mode=update
  destination=${2:-$here}
elif [[ $# -eq 1 ]]; then
  mode=render
  destination=$1
fi
mkdir -p "$destination"
destination="$(cd "$destination" && pwd)"

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/fes-core-screens-render.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT
out="$tmpdir/rendered"
mkdir -p "$out"
overlay="$tmpdir/overlay.json"
python3 - "$repo/sources/FogCast/ui/tenfoot/zz_core_screens_test.go" \
  "$tooling/core_screens_test.go.txt" "$overlay" <<'PY'
import json
import sys

with open(sys.argv[3], "w", encoding="utf-8") as f:
    json.dump({"Replace": {sys.argv[1]: sys.argv[2]}}, f)
PY

cd "$repo/sources/FogCast"
FES_CORE_SCREENS="$out" FES_CORE_SCREENS_PACK="$tooling/coreref" \
  CGO_ENABLED=0 go test -overlay "$overlay" ./ui/tenfoot -run '^TestCoreScreens$' -count=1

rendered_count=$(find "$out" -maxdepth 1 -type f -name '*.png' | wc -l)
if [[ $rendered_count -ne 42 ]]; then
  echo "expected 42 renders, got $rendered_count" >&2
  exit 1
fi

case "$mode" in
  check)
    expected_count=$(find "$here" -maxdepth 1 -type f -name '*.png' | wc -l)
    if [[ $expected_count -ne 42 ]]; then
      echo "expected 42 committed reference PNGs, got $expected_count" >&2
      exit 1
    fi
    for expected in "$here"/*.png; do
      cmp "$out/${expected##*/}" "$expected"
    done
    echo "PASS: all 42 core screen reference PNGs match"
    ;;
  update|render)
    cp "$out"/*.png "$destination/"
    echo "Rendered 42 core screen reference PNGs into $destination"
    ;;
esac
