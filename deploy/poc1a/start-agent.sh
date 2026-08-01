#!/bin/sh
set -eu

base=${MISTER_REMOTE_BASE:-/media/fat/mister-remote}
pidfile=${MISTER_REMOTE_PIDFILE:-/tmp/mister-agent-supervisor.pid}
logfile=${MISTER_REMOTE_LOGFILE:-/tmp/mister-agent.log}
restart_delay=${MISTER_REMOTE_RESTART_DELAY:-1}
child=

if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
  exit 0
fi

echo "$$" > "$pidfile"

cleanup() {
  if [ -n "$child" ] && kill -0 "$child" 2>/dev/null; then
    kill "$child" 2>/dev/null || true
    wait "$child" 2>/dev/null || true
  fi
  rm -f "$pidfile"
}
on_signal() {
  exit 0
}
trap cleanup EXIT
trap on_signal INT TERM

while true; do
  "$base/mister-agent" --config "$base/agent.toml" >> "$logfile" 2>&1 &
  child=$!
  wait "$child" 2>/dev/null || true
  child=
  sleep "$restart_delay"
done
