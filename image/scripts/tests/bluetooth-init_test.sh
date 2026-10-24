#!/bin/sh
# Manual restart waits for the old agent, while shutdown stop stays immediate.
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fes-bluetooth-init.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

cat > "$fixture/kill" <<'INNER'
#!/bin/sh
if [ "${1:-}" = -0 ]; then
  [ -e "$BT_ALIVE" ]
  exit $?
fi
[ "$1" = 4242 ] || exit 2
printf '%s\n' "$1" >> "$BT_SIGNALS"
if [ "${BT_AUTO_EXIT:-0}" = 1 ]; then /bin/rm -f "$BT_ALIVE"; fi
INNER
cat > "$fixture/sleep" <<'INNER'
#!/bin/sh
[ "$1" = 1 ] || exit 2
if [ -f "$BT_SLEEP_COUNT" ]; then
  IFS= read -r count < "$BT_SLEEP_COUNT" || true
else
  count=0
fi
count=$((count + 1))
printf '%s\n' "$count" > "$BT_SLEEP_COUNT"
if [ "${BT_EXIT_AFTER:-0}" -gt 0 ] && [ "$count" -ge "$BT_EXIT_AFTER" ]; then
  /bin/rm -f "$BT_ALIVE"
fi
INNER
cat > "$fixture/rm" <<'INNER'
#!/bin/sh
exec /bin/rm "$@"
INNER
cat > "$fixture/agent" <<'INNER'
#!/bin/sh
printf '%s\n' start >> "$BT_STARTS"
INNER
chmod +x "$fixture/kill" "$fixture/sleep" "$fixture/rm" "$fixture/agent"

sed \
  -e "s|/run/fes-bluetooth.pid|$fixture/pid|g" \
  -e "s|/var/log/fes-bluetooth.log|$fixture/log|g" \
  -e "s|/usr/sbin/fes-bluetooth|$fixture/agent|g" \
  -e "s|/bin/kill|$fixture/kill|g" \
  -e "s|/bin/sleep|$fixture/sleep|g" \
  -e "s|/bin/rm|$fixture/rm|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S41fes-bluetooth" > "$fixture/init"
chmod +x "$fixture/init"

export BT_ALIVE=$fixture/alive
export BT_SIGNALS=$fixture/signals
export BT_SLEEP_COUNT=$fixture/sleep-count
export BT_STARTS=$fixture/starts

setup_old_agent() {
  rm -f "$BT_ALIVE" "$BT_SIGNALS" "$BT_SLEEP_COUNT" "$BT_STARTS"
  : > "$BT_ALIVE"
  printf '%s\n' 4242 > "$fixture/pid"
}

wait_for_start() {
  tries=0
  while [ ! -f "$BT_STARTS" ] && [ "$tries" -lt 20 ]; do
    /bin/sleep 0.1
    tries=$((tries + 1))
  done
  test -f "$BT_STARTS"
}

setup_old_agent
BT_AUTO_EXIT=1 "$fixture/init" restart
wait_for_start
test "$(cat "$BT_STARTS")" = start
test "$(cat "$BT_SIGNALS")" = 4242
test ! -e "$BT_SLEEP_COUNT"
test "$(cat "$fixture/pid")" != 4242

setup_old_agent
BT_EXIT_AFTER=2 "$fixture/init" restart
wait_for_start
test "$(cat "$BT_STARTS")" = start
test "$(cat "$BT_SLEEP_COUNT")" = 2
test "$(cat "$fixture/pid")" != 4242

setup_old_agent
if BT_EXIT_AFTER=0 "$fixture/init" restart 2> "$fixture/restart.log"; then
  echo 'restart started a second agent while the original remained alive' >&2
  exit 1
fi
grep -Fq 'did not exit; restart cancelled' "$fixture/restart.log"
test "$(cat "$fixture/pid")" = 4242
test ! -e "$BT_STARTS"
test "$(cat "$BT_SLEEP_COUNT")" = 5
test -e "$BT_ALIVE"

setup_old_agent
"$fixture/init" stop
test ! -e "$fixture/pid"
test ! -e "$BT_SLEEP_COUNT"
test -e "$BT_ALIVE"
