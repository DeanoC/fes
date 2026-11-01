#!/usr/bin/env bash
# Re-render docs/ux/references/v1 with the production rooms engine and
# software renderer. The harness is copied into the tenfoot package only for
# the run and removed afterwards; nothing under sources/ changes.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"
mkdir -p "${1:-$here/..}"
out="$(cd "${1:-$here/..}" && pwd)"
harness="$repo/sources/FogCast/ui/tenfoot/zz_v1_renders_test.go"
cp "$here/v1_renders_test.go.txt" "$harness"
trap 'rm -f "$harness"' EXIT
cd "$repo/sources/FogCast"
FES_V1_RENDERS="$out" FES_V1_PACK="$here/v1ref" \
  CGO_ENABLED=0 go test ./ui/tenfoot -run '^TestV1Renders$' -count=1 -v
