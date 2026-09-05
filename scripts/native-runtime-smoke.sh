#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=$repo/build/native-runtime.inputs.lock.toml
[ "$#" -le 1 ] || {
  printf '%s\n' 'usage: native-runtime-smoke.sh [MEGADRIVE_SELECTION_FILE]' >&2
  exit 2
}
selection_file=${1:-${NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE:-$repo/build/cache/target-image/native/megadrive.selection.toml}}

host_api=${FOGCAST_HOST_API:-http://127.0.0.1:8787}
target_api=${FOGCAST_TARGET_API:-http://192.168.10.239:8182}
target_host=${FOGCAST_TARGET_HOST:-192.168.10.239}
target_user=${FOGCAST_TARGET_USER:-root}
target_password=${FOGCAST_TARGET_PASSWORD:-1}
poll_attempts=${FOGCAST_POLL_ATTEMPTS:-60}
poll_interval=${FOGCAST_POLL_INTERVAL:-1}
call_timeout=${FOGCAST_CALL_TIMEOUT:-5}
ssh_options='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'
work_dir=

cleanup() {
  [ -z "$work_dir" ] || /bin/rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf 'native-runtime-smoke: %s\n' "$1" >&2
  exit 1
}

for required_command in awk cmp curl mktemp python3 sleep sshpass stat timeout; do
  command -v "$required_command" >/dev/null 2>&1 ||
    fail 'required command is unavailable'
done

case "$poll_attempts" in
  ''|0|0*|*[!0-9]*|????*)
    fail 'poll attempts must be an integer from 1 to 300'
    ;;
esac
[ "$poll_attempts" -le 300 ] 2>/dev/null ||
  fail 'poll attempts must be an integer from 1 to 300'

valid_decimal() {
  decimal_value=$1
  zero_allowed=$2
  python3 - "$decimal_value" "$zero_allowed" <<'PY' >/dev/null 2>&1
from decimal import Decimal, InvalidOperation
import re
import sys

text = sys.argv[1]
zero_allowed = sys.argv[2] == "yes"
if len(text) > 8 or re.fullmatch(r"(?:0|[1-9][0-9]*)(?:\.[0-9]+)?", text) is None:
    raise SystemExit(1)
try:
    value = Decimal(text)
except InvalidOperation:
    raise SystemExit(1)
if value > Decimal("60") or (value == 0 and not zero_allowed):
    raise SystemExit(1)
PY
}

valid_decimal "$poll_interval" yes ||
  fail 'poll interval must be a decimal from 0 to 60 seconds'
valid_decimal "$call_timeout" no ||
  fail 'call timeout must be a decimal greater than 0 and at most 60 seconds'

[ -f "$lock" ] || fail 'native input lock is unavailable'
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-native-runtime-smoke-run.XXXXXX") ||
  fail 'temporary workspace is unavailable'

read_lock_value() {
  read_section=$1
  read_key=$2
  awk -v wanted_section="$read_section" -v wanted_key="$read_key" '
    /^\[/ {
      section=$0
      gsub(/^\[|\]$/, "", section)
      next
    }
    section == wanted_section && $0 ~ "^[[:space:]]*" wanted_key "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$lock"
}

read_selection_value() {
  selection_key=$1
  awk -v wanted_key="$selection_key" '
    /^[[:space:]]*(#|$)/ { next }
    $0 ~ "^[[:space:]]*" wanted_key "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$selection_file"
}

[ -f "$selection_file" ] && [ ! -L "$selection_file" ] || fail 'Mega Drive selection is not a regular non-symlink file'
selection_mode=$(stat -c %a "$selection_file" 2>/dev/null || true)
printf '%s\n' "$selection_mode" | grep -Eq '^[0145]{3,4}$' ||
  fail 'Mega Drive selection must not be writable'
awk '
  /^[[:space:]]*(#|$)/ { next }
  /^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=/ {
    key=$0
    sub(/^[[:space:]]*/, "", key)
    sub(/[[:space:]]*=.*$/, "", key)
    if (!(key == "format" || key == "origin" || key == "abi" ||
          key == "system" || key == "repository" || key == "revision" ||
          key == "artifact" || key == "sha256" || key == "size" ||
          key == "install_path" || key == "recipe" ||
          key == "recipe_sha256" || key == "toolchain" || key == "label")) bad=1
    count[key]++
    next
  }
  { bad=1 }
  END { for (key in count) if (count[key] != 1) bad=1; exit bad ? 1 : 0 }
' "$selection_file" || fail 'Mega Drive selection is not a closed normalized record'

runtime_commit=$(read_lock_value mister_runtime commit)
idle_repository=$(read_lock_value idle_rbf repository)
idle_commit=$(read_lock_value idle_rbf commit)
idle_path=$(read_lock_value idle_rbf path)
idle_sha=$(read_lock_value idle_rbf sha256)
idle_size=$(read_lock_value idle_rbf size)
idle_install_path=$(read_lock_value idle_rbf install_path)
megadrive_repository=$(read_lock_value megadrive_rbf repository)
megadrive_commit=$(read_lock_value megadrive_rbf commit)
megadrive_path=$(read_lock_value megadrive_rbf path)
megadrive_sha=$(read_lock_value megadrive_rbf sha256)
megadrive_size=$(read_lock_value megadrive_rbf size)
megadrive_install_path=$(read_lock_value megadrive_rbf install_path)
selection_format=$(read_selection_value format)
selection_origin=$(read_selection_value origin)
selection_abi=$(read_selection_value abi)
selection_system=$(read_selection_value system)
selection_repository=$(read_selection_value repository)
selection_revision=$(read_selection_value revision)
selection_artifact=$(read_selection_value artifact)
selection_sha=$(read_selection_value sha256)
selection_size=$(read_selection_value size)
selection_install_path=$(read_selection_value install_path)
selection_recipe=$(read_selection_value recipe)
selection_recipe_sha=$(read_selection_value recipe_sha256)
selection_toolchain=$(read_selection_value toolchain)
selection_label=$(read_selection_value label)
case "$selection_origin" in
  source-built)
    [ "$selection_format" = 1 ] && [ "$selection_abi" = mister ] && [ "$selection_system" = megadrive ] &&
      [ "$selection_artifact" = megadrive.rbf ] &&
      [ "$selection_install_path" = /usr/share/mister-runtime/cores/megadrive.rbf ] &&
      [ -n "$selection_recipe" ] && [ -n "$selection_recipe_sha" ] && [ -n "$selection_toolchain" ] ||
      fail 'Mega Drive source-built selection is invalid'
    ;;
  upstream)
    [ "$selection_format" = 1 ] && [ "$selection_abi" = mister ] && [ "$selection_system" = megadrive ] &&
      [ "$selection_install_path" = /usr/share/mister-runtime/cores/megadrive.rbf ] &&
      [ -z "$selection_recipe" ] && [ -z "$selection_recipe_sha" ] &&
      [ -z "$selection_toolchain" ] && [ -z "$selection_label" ] ||
      fail 'Mega Drive upstream selection is invalid'
    ;;
  *) fail 'Mega Drive selection origin is invalid' ;;
esac
printf '%s\n' "$selection_revision" | grep -Eq '^[0-9a-f]{40}$' || fail 'Mega Drive selection revision is invalid'
printf '%s\n' "$selection_sha" | grep -Eq '^[0-9a-f]{64}$' || fail 'Mega Drive selection SHA-256 is invalid'
printf '%s\n' "$selection_size" | grep -Eq '^[1-9][0-9]*$' || fail 'Mega Drive selection size is invalid'
printf '%s\n' "$selection_repository" | grep -Eq '^https://[^[:space:]]+$' || fail 'Mega Drive selection repository is invalid'
for selection_value in "$selection_origin" "$selection_abi" "$selection_system" \
  "$selection_repository" "$selection_revision" "$selection_artifact" \
  "$selection_sha" "$selection_size" "$selection_install_path" \
  "$selection_recipe" "$selection_recipe_sha" "$selection_toolchain" "$selection_label"; do
  printf '%s' "$selection_value" | LC_ALL=C grep -q '[[:cntrl:]]' &&
    fail 'Mega Drive selection contains a control character'
done
[ "$selection_origin" != upstream ] || {
  [ "$selection_repository" = "$megadrive_repository" ] &&
    [ "$selection_revision" = "$megadrive_commit" ] &&
    [ "$selection_artifact" = "$megadrive_path" ] &&
    [ "$selection_sha" = "$megadrive_sha" ] &&
    [ "$selection_size" = "$megadrive_size" ] ||
    fail 'Mega Drive upstream selection differs from lock'
}
for lock_value in "$runtime_commit" "$idle_repository" "$idle_commit" \
  "$idle_path" "$idle_sha" "$idle_size" "$idle_install_path" \
  "$megadrive_repository" "$megadrive_commit" "$megadrive_path" \
  "$megadrive_sha" "$megadrive_size" "$megadrive_install_path"; do
  [ -n "$lock_value" ] || fail 'native input lock is invalid'
done

read_ready_boot() {
  health_response=$(curl --fail --silent \
    --connect-timeout "$call_timeout" --max-time "$call_timeout" \
    "$target_api/v1/health" 2>/dev/null) ||
    return 1
  printf '%s' "$health_response" | python3 -c '
import json, re, sys
try:
    response = json.load(sys.stdin)
except Exception:
    raise SystemExit(1)
ready = response.get("ready")
boot_id = response.get("boot_id")
if ready is not True or not isinstance(boot_id, str) or re.fullmatch(
        r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}",
        boot_id) is None:
    raise SystemExit(1)
sys.stdout.write(boot_id.lower())
' 2>/dev/null
}

host_is_idle() {
  status_response=$(curl --fail --silent \
    --connect-timeout "$call_timeout" --max-time "$call_timeout" \
    "$host_api/api/v1/status" 2>/dev/null) ||
    return 1
  printf '%s' "$status_response" | python3 -c '
import json, sys
try:
    response = json.load(sys.stdin)
except Exception:
    raise SystemExit(1)
raise SystemExit(0 if response.get("state") == "idle" else 1)
' 2>/dev/null
}

wait_for_ready_idle() {
  previous_boot=$1
  remaining=$poll_attempts
  while [ "$remaining" -gt 0 ]; do
    observed_boot=
    target_ready=false
    host_idle=false
    if observed_boot=$(read_ready_boot); then
      target_ready=true
    fi
    if host_is_idle; then
      host_idle=true
    fi
    if [ "$target_ready" = true ] && [ "$host_idle" = true ] &&
      { [ -z "$previous_boot" ] || [ "$observed_boot" != "$previous_boot" ]; }; then
      printf '%s\n' "$observed_boot"
      return 0
    fi
    remaining=$((remaining - 1))
    [ "$remaining" -eq 0 ] || sleep "$poll_interval"
  done
  return 1
}

if ! boot_before=$(wait_for_ready_idle ''); then
  fail 'initial ready idle state was not observed'
fi

inspection=$work_dir/inspection
if ! timeout "$call_timeout" sshpass -p "$target_password" ssh $ssh_options \
  "$target_user@$target_host" '
set -eu
printf "%s\n" FOGCAST_EXECUTABLES_BEGIN
for process_dir in /proc/[0-9]*; do
  executable=$(/usr/bin/readlink "$process_dir/exe" 2>/dev/null || true)
  [ -z "$executable" ] || printf "%s\n" "$executable"
done
printf "%s\n" FOGCAST_EXECUTABLES_END
if [ -p /dev/MiSTer_cmd ]; then
  printf "%s\n" FOGCAST_COMMAND_PIPE_FIFO=1
else
  printf "%s\n" FOGCAST_COMMAND_PIPE_FIFO=0
fi
agent_sha=$(/usr/bin/sha256sum /usr/sbin/mister-agent | /usr/bin/awk "{print \$1}")
printf "FOGCAST_AGENT_SHA256=%s\n" "$agent_sha"
' > "$inspection" 2>/dev/null; then
  fail 'target inspection failed'
fi

[ "$(awk '$0 == "FOGCAST_EXECUTABLES_BEGIN" { count++ } END { print count + 0 }' "$inspection")" -eq 1 ] ||
  fail 'target inspection failed'
[ "$(awk '$0 == "FOGCAST_EXECUTABLES_END" { count++ } END { print count + 0 }' "$inspection")" -eq 1 ] ||
  fail 'target inspection failed'

executables=$work_dir/executables
awk '
  $0 == "FOGCAST_EXECUTABLES_BEGIN" { inside=1; next }
  $0 == "FOGCAST_EXECUTABLES_END" { inside=0; exit }
  inside { print }
' "$inspection" > "$executables"
runtime_count=$(awk '$0 == "/usr/sbin/mister-runtime" { count++ } END { print count + 0 }' "$executables")
agent_count=$(awk '$0 == "/usr/sbin/mister-agent" { count++ } END { print count + 0 }' "$executables")
main_count=$(awk '
  {
    executable=$0
    sub(/ \(deleted\)$/, "", executable)
    count_parts=split(executable, parts, "/")
    if (parts[count_parts] == "MiSTer") count++
  }
  END { print count + 0 }
' "$executables")

[ "$runtime_count" -eq 1 ] ||
  fail 'expected exactly one mister-runtime executable'
[ "$agent_count" -eq 1 ] ||
  fail 'expected exactly one mister-agent executable'
[ "$main_count" -eq 0 ] ||
  fail 'conventional MiSTer executable is running'
[ "$(awk '$0 == "FOGCAST_COMMAND_PIPE_FIFO=1" { count++ } END { print count + 0 }' "$inspection")" -eq 0 ] ||
  fail '/dev/MiSTer_cmd is a FIFO'
[ "$(awk '$0 == "FOGCAST_COMMAND_PIPE_FIFO=0" { count++ } END { print count + 0 }' "$inspection")" -eq 1 ] ||
  fail 'target inspection failed'
agent_sha=$(awk -F= '
  $1 == "FOGCAST_AGENT_SHA256" { count++; value=$2 }
  END { if (count == 1) print value }
' "$inspection")
printf '%s\n' "$agent_sha" | grep -Eq '^[0-9a-f]{64}$' ||
  fail 'target inspection failed'

expected_build_inputs=$work_dir/expected-build-inputs
{
  printf '%s\n' 'format=1'
  printf 'mister_runtime_commit=%s\n' "$runtime_commit"
  printf 'mister_agent_sha256=%s\n' "$agent_sha"
  printf 'idle_repository=%s\n' "$idle_repository"
  printf 'idle_commit=%s\n' "$idle_commit"
  printf 'idle_path=%s\n' "$idle_path"
  printf 'idle_sha256=%s\n' "$idle_sha"
  printf 'idle_size=%s\n' "$idle_size"
  printf 'idle_install_path=%s\n' "$idle_install_path"
  printf 'megadrive_origin=%s\n' "$selection_origin"
  printf 'megadrive_abi=%s\n' "$selection_abi"
  printf 'megadrive_system=%s\n' "$selection_system"
  printf 'megadrive_repository=%s\n' "$selection_repository"
  printf 'megadrive_revision=%s\n' "$selection_revision"
  printf 'megadrive_artifact=%s\n' "$selection_artifact"
  printf 'megadrive_sha256=%s\n' "$selection_sha"
  printf 'megadrive_size=%s\n' "$selection_size"
  printf 'megadrive_install_path=%s\n' "$selection_install_path"
  if [ "$selection_origin" = source-built ]; then
    printf 'megadrive_recipe=%s\n' "$selection_recipe"
    printf 'megadrive_recipe_sha256=%s\n' "$selection_recipe_sha"
    printf 'megadrive_toolchain=%s\n' "$selection_toolchain"
    [ -z "$selection_label" ] || printf 'megadrive_label=%s\n' "$selection_label"
  fi
} > "$expected_build_inputs"

installed_build_inputs=$work_dir/installed-build-inputs
if ! timeout "$call_timeout" sshpass -p "$target_password" ssh $ssh_options \
  "$target_user@$target_host" \
  '/bin/cat /usr/share/mister-runtime/build-inputs' \
  > "$installed_build_inputs" 2>/dev/null; then
  fail 'target inspection failed'
fi
cmp -s "$expected_build_inputs" "$installed_build_inputs" ||
  fail 'installed build inputs differ from selection'

curl --fail --silent \
  --connect-timeout "$call_timeout" --max-time "$call_timeout" \
  -X POST "$host_api/api/v1/session/stop" \
  >/dev/null 2>&1 || fail 'host stop request failed'
host_is_idle || fail 'host did not remain idle after stop'

reboot_output=$work_dir/reboot-output
if timeout "$call_timeout" sshpass -p "$target_password" ssh $ssh_options \
  "$target_user@$target_host" \
  'command -v reboot >/dev/null 2>&1 || exit 127; printf "%s\n" FOGCAST_REBOOT_STARTED; reboot >/dev/null 2>&1 &' \
  > "$reboot_output" 2>/dev/null; then
  :
else
  :
fi
reboot_expected=$work_dir/reboot-expected
printf '%s\n' FOGCAST_REBOOT_STARTED > "$reboot_expected"
cmp -s "$reboot_expected" "$reboot_output" ||
  fail 'reboot command did not start'

if ! boot_after=$(wait_for_ready_idle "$boot_before"); then
  fail 'fresh ready idle state with changed boot ID was not observed'
fi

printf 'native runtime smoke passed: boot %s -> %s, idle -> idle\n' \
  "$boot_before" "$boot_after"
