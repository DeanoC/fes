#!/bin/sh
set -eu

host_api=${FOGCAST_HOST_API:-http://127.0.0.1:8787}
target_api=${FOGCAST_TARGET_API:-http://192.168.10.239:8182}
target_host=${FOGCAST_TARGET_HOST:-192.168.10.239}
target_user=${FOGCAST_TARGET_USER:-root}
target_password=${FOGCAST_TARGET_PASSWORD:-1}
poll_attempts=${FOGCAST_POLL_ATTEMPTS:-30}
poll_interval=${FOGCAST_POLL_INTERVAL:-1}
ssh_options='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'

usage() {
  printf 'usage: target-smoke.sh GAME_ID EXPECTED_CORE\n' >&2
  exit 2
}

[ "$#" -eq 2 ] || usage
game_id=$1
expected_core=$2
case "$game_id" in
  ''|*[!A-Za-z0-9._-]*)
    printf 'target-smoke: invalid game ID: %s\n' "$game_id" >&2
    exit 2
    ;;
esac
case "$expected_core" in
  ''|*[!A-Za-z0-9._-]*)
    printf 'target-smoke: invalid expected core: %s\n' "$expected_core" >&2
    exit 2
    ;;
esac
case "$poll_attempts" in
  ''|*[!0-9]*)
    printf '%s\n' 'target-smoke: poll attempts must be an integer' >&2
    exit 2
    ;;
esac

command -v curl >/dev/null 2>&1 || {
  printf '%s\n' 'target-smoke: curl is required' >&2
  exit 2
}
command -v sshpass >/dev/null 2>&1 || {
  printf '%s\n' 'target-smoke: sshpass is required' >&2
  exit 2
}

curl --fail --silent --show-error "$host_api/api/v1/health" >/dev/null
curl --fail --silent --show-error "$target_api/v1/health" >/dev/null
curl --fail --silent --show-error -X POST \
  -H 'Content-Type: application/json' \
  --data "{\"game_id\":\"$game_id\"}" \
  "$host_api/api/v1/session/launch" >/dev/null

read_core() {
  sshpass -p "$target_password" ssh $ssh_options "$target_user@$target_host" \
    'cat /tmp/CORENAME 2>/dev/null || true' | tr -d '\r'
}

wait_for_core() {
  want=$1
  remaining=$poll_attempts
  while [ "$remaining" -gt 0 ]; do
    if [ "$(read_core)" = "$want" ]; then
      return 0
    fi
    remaining=$((remaining - 1))
    [ "$remaining" -gt 0 ] && sleep "$poll_interval"
  done
  printf 'target-smoke: expected core %s did not appear\n' "$want" >&2
  exit 1
}

wait_for_core "$expected_core"
curl --fail --silent --show-error -X POST \
  "$host_api/api/v1/session/stop" >/dev/null
wait_for_core MENU
printf 'target smoke passed: %s -> %s -> MENU\n' "$game_id" "$expected_core"
