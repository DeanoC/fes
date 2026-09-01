#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=$repo/build/native-runtime.inputs.lock.toml

host_api=${FOGCAST_HOST_API:-http://127.0.0.1:8787}
target_api=${FOGCAST_TARGET_API:-http://192.168.10.239:8182}
target_host=${FOGCAST_TARGET_HOST:-192.168.10.239}
target_user=${FOGCAST_TARGET_USER:-root}
target_password=${FOGCAST_TARGET_PASSWORD:-1}
poll_attempts=${FOGCAST_POLL_ATTEMPTS:-60}
poll_interval=${FOGCAST_POLL_INTERVAL:-1}
ssh_options='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'

fail() {
  printf 'native-runtime-smoke: %s\n' "$1" >&2
  exit 1
}

case "$poll_attempts" in
  ''|*[!0-9]*) fail 'poll attempts must be a positive integer' ;;
esac
[ "$poll_attempts" -gt 0 ] || fail 'poll attempts must be a positive integer'
python3 -c '
import math, sys
try:
    value = float(sys.argv[1])
except ValueError:
    raise SystemExit(1)
raise SystemExit(0 if math.isfinite(value) and value >= 0 else 1)
' "$poll_interval" >/dev/null 2>&1 || fail 'poll interval must be non-negative'

for required_command in awk curl python3 sleep sshpass; do
  command -v "$required_command" >/dev/null 2>&1 ||
    fail 'required command is unavailable'
done
[ -f "$lock" ] || fail 'native input lock is unavailable'

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

runtime_commit=$(read_lock_value mister_runtime commit)
idle_repository=$(read_lock_value idle_rbf repository)
idle_commit=$(read_lock_value idle_rbf commit)
idle_path=$(read_lock_value idle_rbf path)
idle_sha=$(read_lock_value idle_rbf sha256)
idle_size=$(read_lock_value idle_rbf size)
idle_install_path=$(read_lock_value idle_rbf install_path)
for lock_value in "$runtime_commit" "$idle_repository" "$idle_commit" \
  "$idle_path" "$idle_sha" "$idle_size" "$idle_install_path"; do
  [ -n "$lock_value" ] || fail 'native input lock is invalid'
done

expected_build_inputs=$(printf '%s\n' \
  'format=1' \
  "mister_runtime_commit=$runtime_commit" \
  "idle_repository=$idle_repository" \
  "idle_commit=$idle_commit" \
  "idle_path=$idle_path" \
  "idle_sha256=$idle_sha" \
  "idle_size=$idle_size" \
  "idle_install_path=$idle_install_path")

read_ready_boot() {
  health_response=$(curl --fail --silent "$target_api/v1/health" 2>/dev/null) ||
    return 1
  printf '%s' "$health_response" | python3 -c '
import json, sys
try:
    response = json.load(sys.stdin)
except Exception:
    raise SystemExit(1)
ready = response.get("ready")
boot_id = response.get("boot_id")
if ready is not True or not isinstance(boot_id, str) or not boot_id:
    raise SystemExit(1)
sys.stdout.write(boot_id)
' 2>/dev/null
}

host_is_idle() {
  status_response=$(curl --fail --silent "$host_api/api/v1/status" 2>/dev/null) ||
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

if ! inspection=$(sshpass -p "$target_password" ssh $ssh_options \
  "$target_user@$target_host" '
set -eu
printf "%s\n" FOGCAST_EXECUTABLES_BEGIN
for process_dir in /proc/[0-9]*; do
  executable=$(/bin/readlink "$process_dir/exe" 2>/dev/null || true)
  [ -z "$executable" ] || printf "%s\n" "$executable"
done
printf "%s\n" FOGCAST_EXECUTABLES_END
if [ -p /dev/MiSTer_cmd ]; then
  printf "%s\n" FOGCAST_COMMAND_PIPE_FIFO=1
else
  printf "%s\n" FOGCAST_COMMAND_PIPE_FIFO=0
fi
' 2>/dev/null); then
  fail 'target inspection failed'
fi

[ "$(printf '%s\n' "$inspection" | awk '$0 == "FOGCAST_EXECUTABLES_BEGIN" { count++ } END { print count + 0 }')" -eq 1 ] ||
  fail 'target inspection failed'
[ "$(printf '%s\n' "$inspection" | awk '$0 == "FOGCAST_EXECUTABLES_END" { count++ } END { print count + 0 }')" -eq 1 ] ||
  fail 'target inspection failed'

executables=$(printf '%s\n' "$inspection" | awk '
  $0 == "FOGCAST_EXECUTABLES_BEGIN" { inside=1; next }
  $0 == "FOGCAST_EXECUTABLES_END" { inside=0; exit }
  inside { print }
')
runtime_count=$(printf '%s\n' "$executables" |
  awk '$0 == "/usr/sbin/mister-runtime" { count++ } END { print count + 0 }')
agent_count=$(printf '%s\n' "$executables" |
  awk '$0 == "/usr/sbin/mister-agent" { count++ } END { print count + 0 }')
main_count=$(printf '%s\n' "$executables" |
  awk -F/ '$NF == "MiSTer" { count++ } END { print count + 0 }')

[ "$runtime_count" -eq 1 ] ||
  fail 'expected exactly one mister-runtime executable'
[ "$agent_count" -eq 1 ] ||
  fail 'expected exactly one mister-agent executable'
[ "$main_count" -eq 0 ] ||
  fail 'conventional MiSTer executable is running'
[ "$(printf '%s\n' "$inspection" | awk '$0 == "FOGCAST_COMMAND_PIPE_FIFO=1" { count++ } END { print count + 0 }')" -eq 0 ] ||
  fail '/dev/MiSTer_cmd is a FIFO'
[ "$(printf '%s\n' "$inspection" | awk '$0 == "FOGCAST_COMMAND_PIPE_FIFO=0" { count++ } END { print count + 0 }')" -eq 1 ] ||
  fail 'target inspection failed'

if ! installed_build_inputs=$(sshpass -p "$target_password" ssh $ssh_options \
  "$target_user@$target_host" \
  '/bin/cat /usr/share/mister-runtime/build-inputs' 2>/dev/null); then
  fail 'target inspection failed'
fi
[ "$installed_build_inputs" = "$expected_build_inputs" ] ||
  fail 'installed build inputs differ from lock'

curl --fail --silent -X POST "$host_api/api/v1/session/stop" \
  >/dev/null 2>&1 || fail 'host stop request failed'
host_is_idle || fail 'host did not remain idle after stop'

if reboot_output=$(sshpass -p "$target_password" ssh $ssh_options \
  "$target_user@$target_host" \
  'command -v reboot >/dev/null 2>&1 || exit 127; printf "%s\n" FOGCAST_REBOOT_STARTED; reboot >/dev/null 2>&1 &' \
  2>/dev/null); then
  :
else
  :
fi
printf '%s\n' "$reboot_output" | grep -Fqx FOGCAST_REBOOT_STARTED ||
  fail 'reboot command did not start'

if ! boot_after=$(wait_for_ready_idle "$boot_before"); then
  fail 'fresh ready idle state with changed boot ID was not observed'
fi

printf 'native runtime smoke passed: boot %s -> %s, idle -> idle\n' \
  "$boot_before" "$boot_after"
