#!/usr/bin/env bash
set -uo pipefail
root=$1
timeout 480s bash "$root/bench.sh" "$root"
rc=$?
printf 'SUPERVISOR_FINISH %s exit=%s\n' "$(date -u -Is)" "$rc"
printf '%s\n' "$rc" > "$root/supervisor-exit"
exit "$rc"
