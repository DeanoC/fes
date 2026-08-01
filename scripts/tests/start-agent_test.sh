#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-supervisor-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM
base=$fixture/base
pidfile=$fixture/supervisor.pid
logfile=$fixture/agent.log
mkdir -p "$base"

# The single-quoted lines deliberately write shell expressions into the fake.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/bin/sh' \
  'printf "%s\n" "$$" > "$(dirname "$0")/agent.pid"' \
  'trap "exit 0" INT TERM' \
  'while true; do sleep 1; done' > "$base/mister-agent"
printf '%s\n' 'token = "test"' > "$base/agent.toml"
chmod 0755 "$base/mister-agent"

MISTER_REMOTE_BASE=$base \
MISTER_REMOTE_PIDFILE=$pidfile \
MISTER_REMOTE_LOGFILE=$logfile \
MISTER_REMOTE_RESTART_DELAY=0.01 \
  sh "$repo/deploy/poc1a/start-agent.sh" &
supervisor=$!

attempt=0
while [ ! -f "$base/agent.pid" ] && [ "$attempt" -lt 50 ]; do
  sleep 0.1
  attempt=$((attempt + 1))
done
test -f "$base/agent.pid"
agent=$(cat "$base/agent.pid")
kill "$supervisor"
wait "$supervisor"
test ! -e "$pidfile"
if kill -0 "$agent" 2>/dev/null; then
  echo 'supervisor left its agent child running' >&2
  exit 1
fi
