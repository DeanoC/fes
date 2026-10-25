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

# S35's storage waiter must stop before a late mount can restart Bluetooth.
cat > "$fixture/storage-sleep" <<'INNER'
#!/bin/sh
printf '%s\n' started > "$BT_STORAGE_SLEEP_STARTED"
exec /bin/sleep 2
INNER
chmod +x "$fixture/storage-sleep"
sed \
  -e "s|/run/fes-bluetooth-storage.pid|$fixture/storage-pid|g" \
  -e "s|/var/log/fes-bluetooth-storage.log|$fixture/storage-log|g" \
  -e "s|/run/fogcast/fesdata3|$fixture/data|g" \
  -e "s|/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth|$fixture/modules|g" \
  -e "s|/proc/mounts|$fixture/mounts|g" \
  -e "s|/sys/class/bluetooth/hci0|$fixture/hci0|g" \
  -e "s|/bin/sleep|$fixture/storage-sleep|g" \
  -e "s|/bin/kill|$fixture/storage-kill|g" \
  -e "s|/bin/rm|$fixture/rm|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S35fes-bluetooth" > "$fixture/storage-init"
cat > "$fixture/storage-kill" <<'INNER'
#!/bin/sh
if [ "${1:-}" != -0 ]; then printf '%s\n' "$1" >> "$BT_STORAGE_SIGNALS"; fi
exec /bin/kill "$@"
INNER
chmod +x "$fixture/storage-kill" "$fixture/storage-init"

check_storage_waiter_cancelled() {
  action=$1
  rm -rf "$fixture/data" "$fixture/mounts"
  rm -f "$fixture/storage-pid" "$fixture/storage-sleep-started" "$fixture/storage-signals"
  : > "$fixture/mounts"
  BT_STORAGE_SLEEP_STARTED=$fixture/storage-sleep-started \
    BT_STORAGE_SIGNALS=$fixture/storage-signals "$fixture/storage-init" start
  tries=0
  while [ ! -s "$fixture/storage-sleep-started" ] && [ "$tries" -lt 30 ]; do
    /bin/sleep 0.1
    tries=$((tries + 1))
  done
  test -s "$fixture/storage-sleep-started"
  test -s "$fixture/storage-pid"
  waiter=$(cat "$fixture/storage-pid")
  BT_STORAGE_SLEEP_STARTED=$fixture/storage-sleep-started \
    BT_STORAGE_SIGNALS=$fixture/storage-signals "$fixture/storage-init" "$action"
  grep -Fxq "$waiter" "$fixture/storage-signals"
  test ! -e "$fixture/storage-pid"
  printf 'device %s ext4 rw 0 0\n' "$fixture/data" > "$fixture/mounts"
  /bin/sleep 2.2
  test ! -e "$fixture/data/bluetooth"
}

check_storage_waiter_cancelled stop
check_storage_waiter_cancelled restart
