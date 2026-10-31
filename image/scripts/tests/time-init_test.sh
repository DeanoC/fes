#!/bin/sh
# Exercise S21fes-time against a stub ntpd (no kit, no network).
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
script=$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S21fes-time
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fes-time.XXXXXX")
trap 'if [ -f "$fixture/pid" ]; then kill "$(cat "$fixture/pid")" 2>/dev/null || :; fi; rm -rf "$fixture"' EXIT INT TERM

test -x "$script"
sh -n "$script"
# The appliance shell is BusyBox ash; reject bash-only syntax where possible.
if command -v dash >/dev/null 2>&1; then dash -n "$script"; fi

fail() {
  printf 'time-init_test: %s\n' "$1" >&2
  exit 1
}

# The stub logs its arguments, then stays up like ntpd -n until it is killed.
cat > "$fixture/ntpd" <<SCRIPT
#!/bin/sh
printf '%s\n' "\$*" >> "$fixture/ntpd.log"
exec sleep 30
SCRIPT
chmod 755 "$fixture/ntpd"
: > "$fixture/ntpd.log"

run() {
  FES_TIME_PID=$fixture/pid FES_TIME_NTPD=$fixture/ntpd "$@"
}

wait_log_lines() {
  tries=50
  while [ "$(wc -l < "$fixture/ntpd.log")" -lt "$1" ] && [ "$tries" -gt 0 ]; do
    sleep 0.1
    tries=$((tries - 1))
  done
  [ "$(wc -l < "$fixture/ntpd.log")" -eq "$1" ] || fail "expected $1 ntpd start(s)"
}

# Start runs ntpd in the foreground mode against all four pool servers.
run "$script" start
wait_log_lines 1
[ "$(cat "$fixture/ntpd.log")" = '-n -p 0.pool.ntp.org -p 1.pool.ntp.org -p 2.pool.ntp.org -p 3.pool.ntp.org' ] ||
  fail "unexpected ntpd arguments: $(cat "$fixture/ntpd.log")"
pid=$(cat "$fixture/pid")
kill -0 "$pid" 2>/dev/null || fail 'ntpd is not running after start'

# A second start leaves the running ntpd alone.
run "$script" start
sleep 0.3
wait_log_lines 1
[ "$(cat "$fixture/pid")" = "$pid" ] || fail 'second start replaced the running ntpd'

# Stop ends ntpd and removes the pid file; a second stop is harmless.
run "$script" stop
tries=50
while kill -0 "$pid" 2>/dev/null && [ "$tries" -gt 0 ]; do sleep 0.1; tries=$((tries - 1)); done
kill -0 "$pid" 2>/dev/null && fail 'ntpd still running after stop'
[ ! -e "$fixture/pid" ] || fail 'pid file left after stop'
run "$script" stop

# A stale pid file from a previous boot does not stop a fresh start.
printf '%s\n' 999999 > "$fixture/pid"
run env FES_TIME_SERVERS='time.example' "$script" start
wait_log_lines 2
[ "$(tail -n 1 "$fixture/ntpd.log")" = '-n -p time.example' ] || fail 'server override not honoured'
run "$script" stop

# Unknown verbs are rejected.
if run "$script" bogus 2>/dev/null; then fail 'bogus verb accepted'; fi

printf '%s\n' 'time-init_test: ok'
