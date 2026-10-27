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
cat > "$fixture/readlink" <<'INNER'
#!/bin/sh
case "${1:-}" in
  /proc/*/exe) printf '%s\n' "$BT_PROCESS_EXE" ;;
  *) exit 1 ;;
esac
INNER
cat > "$fixture/agent" <<'INNER'
#!/bin/sh
printf '%s\n' start >> "$BT_STARTS"
INNER
chmod +x "$fixture/kill" "$fixture/sleep" "$fixture/rm" "$fixture/readlink" "$fixture/agent"

sed \
  -e "s|/run/fes-bluetooth.pid|$fixture/pid|g" \
  -e "s|/var/log/fes-bluetooth.log|$fixture/log|g" \
  -e "s|/usr/sbin/fes-bluetooth|$fixture/agent|g" \
  -e "s|/usr/bin/readlink|$fixture/readlink|g" \
  -e "s|/bin/kill|$fixture/kill|g" \
  -e "s|/bin/sleep|$fixture/sleep|g" \
  -e "s|/bin/rm|$fixture/rm|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S41fes-bluetooth" > "$fixture/init"
chmod +x "$fixture/init"

export BT_ALIVE=$fixture/alive
export BT_SIGNALS=$fixture/signals
export BT_SLEEP_COUNT=$fixture/sleep-count
export BT_STARTS=$fixture/starts
export BT_PROCESS_EXE=$fixture/agent

setup_old_agent() {
  rm -f "$BT_ALIVE" "$BT_SIGNALS" "$BT_SLEEP_COUNT" "$BT_STARTS"
  : > "$BT_ALIVE"
  BT_PROCESS_EXE=$fixture/agent
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

setup_old_agent
BT_PROCESS_EXE=/usr/bin/sleep "$fixture/init" stop
test ! -e "$fixture/pid"
test ! -e "$BT_SIGNALS"
test -e "$BT_ALIVE"

setup_old_agent
BT_PROCESS_EXE=/usr/bin/sleep "$fixture/init" start
wait_for_start
test ! -e "$BT_SIGNALS"
test "$(cat "$BT_STARTS")" = start
test "$(cat "$fixture/pid")" != 4242

setup_old_agent
BT_PROCESS_EXE=/usr/bin/sleep "$fixture/init" restart
wait_for_start
test ! -e "$BT_SIGNALS"
test "$(cat "$BT_STARTS")" = start
test "$(cat "$fixture/pid")" != 4242

# S35 restart must replace its storage waiter, and stop must wait for the
# waiter to finish an in-flight service restart before returning.
cat > "$fixture/storage-sleep" <<'INNER'
#!/bin/sh
case "$1" in
  1) printf '%s\n' started > "$BT_STORAGE_SLEEP_STARTED"; exec /bin/sleep 0.2 ;;
  2) exec /bin/sleep 0.05 ;;
  *) exit 2 ;;
esac
INNER
cat > "$fixture/storage-bluetoothd" <<'INNER'
#!/bin/sh
[ "${1:-}" = restart ] || exit 2
printf '%s\n' S40-start >> "$BT_STORAGE_SERVICE_EVENTS"
/bin/sleep 0.5
printf '%s\n' S40-end >> "$BT_STORAGE_SERVICE_EVENTS"
INNER
cat > "$fixture/storage-agent" <<'INNER'
#!/bin/sh
[ "${1:-}" = restart ] || exit 2
printf '%s\n' S41-start >> "$BT_STORAGE_SERVICE_EVENTS"
INNER
chmod +x "$fixture/storage-sleep" "$fixture/storage-bluetoothd" "$fixture/storage-agent"
sed \
  -e "s|/run/fes-bluetooth-storage.pid|$fixture/storage-pid|g" \
  -e "s|/var/log/fes-bluetooth-storage.log|$fixture/storage-log|g" \
  -e "s|/run/fogcast/fesdata3|$fixture/data|g" \
  -e "s|/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth|$fixture/modules|g" \
  -e "s|/proc/mounts|$fixture/mounts|g" \
  -e "s|/sys/class/bluetooth/hci0|$fixture/hci0|g" \
  -e "s|/etc/init.d/S40bluetooth|$fixture/storage-bluetoothd|g" \
  -e "s|/etc/init.d/S41fes-bluetooth|$fixture/storage-agent|g" \
  -e "s|/bin/sleep|$fixture/storage-sleep|g" \
  -e "s|/bin/kill|$fixture/storage-kill|g" \
  -e "s|/bin/rm|$fixture/rm|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S35fes-bluetooth" > "$fixture/storage-init"
cat > "$fixture/storage-kill" <<'INNER'
#!/bin/sh
if [ "${1:-}" = -0 ]; then exec /bin/kill -0 "$2"; fi
printf '%s\n' "$1" >> "$BT_STORAGE_SIGNALS"
exec /bin/kill "$@"
INNER
chmod +x "$fixture/storage-kill" "$fixture/storage-init"

wait_for_storage_file() {
  file=$1
  tries=0
  while [ ! -e "$file" ] && [ "$tries" -lt 100 ]; do
    /bin/sleep 0.05
    tries=$((tries + 1))
  done
  test -e "$file"
}

rm -rf "$fixture/data" "$fixture/mounts"
rm -f "$fixture/storage-pid" "$fixture/storage-sleep-started" "$fixture/storage-signals" "$fixture/storage-events"
: > "$fixture/mounts"
export BT_STORAGE_SLEEP_STARTED=$fixture/storage-sleep-started
export BT_STORAGE_SIGNALS=$fixture/storage-signals
export BT_STORAGE_SERVICE_EVENTS=$fixture/storage-events
"$fixture/storage-init" start
wait_for_storage_file "$fixture/storage-sleep-started"
waiter=$(cat "$fixture/storage-pid")
"$fixture/storage-init" stop
grep -Fxq "$waiter" "$fixture/storage-signals"
test ! -e "$fixture/storage-pid"
printf 'device %s ext4 rw 0 0\n' "$fixture/data" > "$fixture/mounts"
/bin/sleep 0.3
test ! -e "$fixture/data/bluetooth"

# Restart cancels the pre-mount waiter and launches a replacement that handles
# the mount when it becomes ready.
rm -f "$fixture/storage-sleep-started" "$fixture/storage-signals"
: > "$fixture/mounts"
"$fixture/storage-init" start
wait_for_storage_file "$fixture/storage-sleep-started"
waiter=$(cat "$fixture/storage-pid")
"$fixture/storage-init" restart
grep -Fxq "$waiter" "$fixture/storage-signals"
test -s "$fixture/storage-pid"
printf 'device %s ext4 rw 0 0\n' "$fixture/data" > "$fixture/mounts"
wait_for_storage_file "$fixture/data/bluetooth"
test "$(stat -c '%a' "$fixture/data/bluetooth")" = 700

# If stop arrives while S40 is running, it waits for the waiter to finish and
# the waiter does not proceed to restart the Bluetooth agent afterward.
rm -f "$fixture/storage-pid" "$fixture/storage-sleep-started" "$fixture/storage-signals" "$fixture/storage-events"
: > "$fixture/mounts"
: > "$fixture/hci0"
printf 'device %s ext4 rw 0 0\n' "$fixture/data" > "$fixture/mounts"
"$fixture/storage-init" start
wait_for_storage_file "$fixture/storage-events"
grep -Fxq S40-start "$fixture/storage-events"
(
  "$fixture/storage-init" stop
  : > "$fixture/stop-returned"
) &
stop_pid=$!
/bin/sleep 0.1
test ! -e "$fixture/stop-returned"
wait "$stop_pid"
test -e "$fixture/stop-returned"
grep -Fxq S40-end "$fixture/storage-events"
if grep -Fxq S41-start "$fixture/storage-events"; then
  echo 'storage waiter restarted the Bluetooth agent after stop' >&2
  exit 1
fi
test ! -e "$fixture/storage-pid"
