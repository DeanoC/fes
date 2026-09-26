#!/bin/sh
set -eu

host_api=${FOGCAST_HOST_API:-http://127.0.0.1:8787}
poll_attempts=${FOGCAST_POLL_ATTEMPTS:-60}
poll_interval=${FOGCAST_POLL_INTERVAL:-1}
call_timeout=${FOGCAST_CALL_TIMEOUT:-30}
# A first launch may also compose a format-3 ROM before FPGA reconfiguration.
launch_timeout=${FOGCAST_LAUNCH_TIMEOUT:-${FOGCAST_CALL_TIMEOUT:-90}}

usage() {
  printf '%s\n' 'usage: package-runtime-smoke.sh [PONG_SELECTION] [ZX81_SELECTION] [COLECO_SELECTION]' >&2
  exit 2
}

[ "$#" -le 3 ] || usage
pong_selection=${1:-${FES_PONG_PACKAGE_SELECTION:-}}
zx81_selection=${2:-${FES_ZX81_PACKAGE_SELECTION:-}}
coleco_selection=${3:-${FES_COLECO_PACKAGE_SELECTION:-}}

fail() {
  printf 'package-runtime-smoke: %s\n' "$1" >&2
  exit 1
}

for required_command in curl mktemp python3 sleep; do
  command -v "$required_command" >/dev/null 2>&1 ||
    fail "required command is unavailable: $required_command"
done

case "$poll_attempts" in
  ''|0|*[!0-9]*) fail 'poll attempts must be a positive integer' ;;
esac
[ "$poll_attempts" -le 300 ] || fail 'poll attempts must be at most 300'
case "$poll_interval" in
  ''|*[!0-9.]*|*.*.*) fail 'poll interval must be a decimal' ;;
esac

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-package-runtime-smoke.XXXXXX") ||
  fail 'temporary workspace is unavailable'
active=0
launch_timed_out=0
timed_out_game_id=
timed_out_package_id=

matches_timed_out_launch() {
  python3 - "$timed_out_game_id" "$timed_out_package_id" "$work_dir/cleanup-session.json" <<'PY'
import json
import sys

game_id, package_id, path = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    session = json.load(handle)
if session.get("state") == "idle":
    print("idle")
elif (session.get("state") == "active"
      and session.get("game_id") in (None, "", game_id)
      and (session.get("core_package") or {}).get("package_id") == package_id):
    print("active")
else:
    raise SystemExit(1)
PY
}

cleanup() {
  if [ "$launch_timed_out" -eq 1 ]; then
    remaining=$poll_attempts
    last_state=
    while [ "$remaining" -gt 0 ]; do
      if curl --fail --silent --show-error \
        --connect-timeout 2 --max-time 2 \
        "$host_api/api/v1/session" > "$work_dir/cleanup-session.json" 2>/dev/null; then
        last_state=$(matches_timed_out_launch) || last_state=
        if [ "$last_state" = active ]; then
          break
        fi
      else
        last_state=
      fi
      remaining=$((remaining - 1))
      [ "$remaining" -gt 0 ] && sleep "$poll_interval"
    done
    if [ "$last_state" = active ] || [ "$last_state" = idle ]; then
      # An interrupted launch can leave the host idle while retaining its
      # newly claimed kit lease. Stop also releases that idle lease.
      active=1
    else
      printf '%s\n' 'package-runtime-smoke: timed-out launch outcome is unresolved; check the host session and kit lease' >&2
    fi
  fi
  if [ "$active" -eq 1 ]; then
    if ! curl --fail --silent --show-error \
      --connect-timeout "$call_timeout" --max-time "$call_timeout" \
      -X POST "$host_api/api/v1/session/stop" > "$work_dir/cleanup-stop.json" 2>/dev/null; then
      printf '%s\n' 'package-runtime-smoke: cleanup Stop failed; check the host session and kit lease' >&2
    fi
  fi
  /bin/rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM

read_selection_package_id() {
  selection=$1
  [ -n "$selection" ] || return 1
  [ -f "$selection" ] && [ ! -L "$selection" ] || return 1
  awk '
    /^[[:space:]]*(#|$)/ { next }
    /^[[:space:]]*package_id[[:space:]]*=/ {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      if (seen++ != 0) exit 2
      print value
    }
    END { if (seen != 1) exit 2 }
  ' "$selection"
}

expected_package_id() {
  core=$1
  selection=$2
  if [ -n "$selection" ]; then
    read_selection_package_id "$selection" || fail "$core selection has no unique package_id"
    return
  fi
  mapping=${FES_PACKAGE_EXPECTED_IDS:-}
  [ -n "$mapping" ] || fail "$core requires a selection file or FES_PACKAGE_EXPECTED_IDS"
  expected=
  old_ifs=$IFS
  IFS=,
  for pair in $mapping; do
    pair_core=${pair%%=*}
    pair_id=${pair#*=}
    if [ "$pair_core" = "$core" ]; then
      [ "$expected" = "" ] || fail "$core appears more than once in FES_PACKAGE_EXPECTED_IDS"
      expected=$pair_id
    fi
  done
  IFS=$old_ifs
  [ -n "$expected" ] || fail "$core is missing from FES_PACKAGE_EXPECTED_IDS"
  printf '%s\n' "$expected"
}

validate_package_id() {
  printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{64}$' ||
    fail "invalid package ID for $2"
}

pong_package_id=$(expected_package_id fes.pong "$pong_selection")
zx81_package_id=$(expected_package_id fes.zx81 "$zx81_selection")
coleco_package_id=$(expected_package_id fes.coleco "$coleco_selection")
validate_package_id "$pong_package_id" fes.pong
validate_package_id "$zx81_package_id" fes.zx81
validate_package_id "$coleco_package_id" fes.coleco

curl --fail --silent --show-error \
  --connect-timeout "$call_timeout" --max-time "$call_timeout" \
  "$host_api/api/v1/health" > "$work_dir/health.json" ||
  fail 'host health request failed'
python3 - "$work_dir/health.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    value = json.load(handle)
if value.get("ready") is not True:
    raise SystemExit("host is not ready")
target = value.get("target") or {}
if target.get("reachable") is not True or target.get("ready") is not True:
    raise SystemExit("target is not ready through the host")
host_revision = (value.get("host") or {}).get("revision", "")
agent_revision = ((target.get("artifacts") or {}).get("agent_revision", ""))
if not host_revision or not agent_revision:
    raise SystemExit("host/target revisions are unavailable")
if host_revision != agent_revision:
    raise SystemExit(
        f"host revision {host_revision} does not match target agent {agent_revision}"
    )
print(host_revision)
PY

curl --fail --silent --show-error \
  --connect-timeout "$call_timeout" --max-time "$call_timeout" \
  "$host_api/api/v1/core-packages" > "$work_dir/packages.json" ||
  fail 'core package inventory request failed'
curl --fail --silent --show-error \
  --connect-timeout "$call_timeout" --max-time "$call_timeout" \
  "$host_api/api/v1/library/core-entries" > "$work_dir/entries.json" ||
  fail 'core entry inventory request failed'

game_id_for() {
  core=$1
  package_id=$2
  python3 - "$core" "$package_id" "$work_dir/packages.json" "$work_dir/entries.json" <<'PY'
import json
import sys

core, package_id, packages_path, entries_path = sys.argv[1:]
with open(packages_path, encoding="utf-8") as handle:
    packages = json.load(handle).get("packages", [])
with open(entries_path, encoding="utf-8") as handle:
    entries = json.load(handle).get("entries", [])

matches = [
    package for package in packages
    if package.get("package_id") == package_id
    and (package.get("descriptor") or {}).get("core", {}).get("id") == core
]
if len(matches) != 1:
    raise SystemExit(f"{core}: expected one installed package with ID {package_id}")
entry_matches = [
    entry for entry in entries
    if entry.get("core_id") == core
    and entry.get("package_id") == package_id
    and entry.get("game_id")
]
if len(entry_matches) != 1:
    raise SystemExit(f"{core}: expected one library entry for package {package_id}")
print(entry_matches[0]["game_id"])
PY
}

wait_for_session() {
  want_state=$1
  want_game=$2
  want_package=$3
  remaining=$poll_attempts
  while [ "$remaining" -gt 0 ]; do
    if curl --fail --silent --show-error \
      --connect-timeout "$call_timeout" --max-time "$call_timeout" \
      "$host_api/api/v1/session" > "$work_dir/session.json" 2>/dev/null &&
      python3 - "$want_state" "$want_game" "$want_package" "$work_dir/session.json" <<'PY'
import json
import sys

want_state, want_game, want_package, path = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    value = json.load(handle)
if value.get("state") != want_state:
    raise SystemExit(1)
if want_state == "active":
    if value.get("game_id") != want_game:
        raise SystemExit(1)
    if (value.get("core_package") or {}).get("package_id") != want_package:
        raise SystemExit(1)
PY
    then
      return 0
    fi
    remaining=$((remaining - 1))
    [ "$remaining" -gt 0 ] && sleep "$poll_interval"
  done
  fail "session did not reach $want_state for $want_game"
}

launch_and_stop() {
  core=$1
  package_id=$2
  game_id=$3
  printf 'launching %s (%s)\n' "$core" "$game_id"
  launch_body=$(printf '{"game_id":"%s"}' "$game_id")
  if ! curl --fail --silent --show-error \
    --connect-timeout "$call_timeout" --max-time "$launch_timeout" \
    -H 'Content-Type: application/json' \
    --data "$launch_body" "$host_api/api/v1/session/launch" > "$work_dir/launch.json"; then
    launch_timed_out=1
    timed_out_game_id=$game_id
    timed_out_package_id=$package_id
    fail "$core launch request failed"
  fi
  active=1
  python3 - "$game_id" "$package_id" "$work_dir/launch.json" <<'PY'
import json
import sys

game_id, package_id, path = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    value = json.load(handle)
if value.get("state") != "active" or value.get("game_id") != game_id:
    raise SystemExit("launch did not report an active session")
if (value.get("core_package") or {}).get("package_id") != package_id:
    raise SystemExit("launch selected an unexpected package")
PY
  wait_for_session active "$game_id" "$package_id"
  curl --fail --silent --show-error \
    --connect-timeout "$call_timeout" --max-time "$call_timeout" \
    -X POST "$host_api/api/v1/session/stop" > "$work_dir/stop.json" ||
    fail "$core stop request failed"
  active=0
  wait_for_session idle '' ''
  printf 'passed %s (%s)\n' "$core" "$package_id"
}

pong_game_id=$(game_id_for fes.pong "$pong_package_id") ||
  fail 'Pong package is not installed and selected in the host library'
zx81_game_id=$(game_id_for fes.zx81 "$zx81_package_id") ||
  fail 'ZX81 package is not installed and selected in the host library'
coleco_game_id=$(game_id_for fes.coleco "$coleco_package_id") ||
  fail 'Coleco package is not installed and selected in the host library'

wait_for_session idle '' ''
launch_and_stop fes.pong "$pong_package_id" "$pong_game_id"
launch_and_stop fes.zx81 "$zx81_package_id" "$zx81_game_id"
launch_and_stop fes.coleco "$coleco_package_id" "$coleco_game_id"
printf '%s\n' 'package runtime smoke passed: Pong, ZX81, Coleco'
