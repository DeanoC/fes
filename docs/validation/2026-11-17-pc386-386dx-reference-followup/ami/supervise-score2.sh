#!/usr/bin/env bash
set -uo pipefail
root=$1
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
driver=$script_dir/score2.sh
expected_driver_sha256=7c39d9caafa80e19d426e7143fe2f887c26579acb4bb0ac78c6c67fa23aef6bb
actual_driver_sha256=$(sha256sum "$driver")
actual_driver_sha256=${actual_driver_sha256%% *}
if [[ "$actual_driver_sha256" != "$expected_driver_sha256" ]]; then
  printf 'SUPERVISOR_DRIVER_MISMATCH expected=%s actual=%s\n' \
    "$expected_driver_sha256" "$actual_driver_sha256" >&2
  exit 1
fi
printf 'SUPERVISOR_DRIVER %s sha256=%s\n' "$driver" "$actual_driver_sha256"
timeout 480s bash "$driver" "$root"
rc=$?
printf 'SUPERVISOR_FINISH %s exit=%s\n' "$(date -u -Is)" "$rc"
printf '%s\n' "$rc" > "$root/supervisor-exit"
exit "$rc"
