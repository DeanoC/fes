#!/bin/sh
set -eu

base=/media/fat/mister-remote
pidfile=/tmp/mister-agent-supervisor.pid
logfile=/tmp/mister-agent.log

if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
  exit 0
fi

echo "$$" > "$pidfile"
trap 'rm -f "$pidfile"' EXIT INT TERM

while true; do
  "$base/mister-agent" --config "$base/agent.toml" >> "$logfile" 2>&1 || true
  sleep 1
done
