#!/usr/bin/env bash
set -euo pipefail
root=${1:-$(cd "$(dirname "$0")/.." && pwd)}
for file in "$root/README.md" "$root/docs/support-matrix.md"; do
 grep -Fx 'Hardware-supported package paths: 0.' "$file" >/dev/null
 if grep -Eq 'hardware: yes|Hardware-supported systems: 1' "$file"; then
  echo "retired hardware acceptance leaked into current support: $file" >&2; exit 1
 fi
done
echo 'hardware support truth passed'
