#!/bin/sh
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$here/../../..
overlay=$root/image/buildroot/board/fogcast-target/native-rootfs-overlay
script=$overlay/usr/sbin/fes-reboot-backstop
inittab=$overlay/etc/inittab
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

fail() { printf 'reboot-backstop: %s\n' "$1" >&2; exit 1; }

[ -x "$script" ] || fail 'backstop script is not executable'
shutdown=$(grep '^::shutdown:' "$inittab")
expected=$(cat <<'EOF'
::shutdown:/etc/init.d/rcK
::shutdown:/sbin/swapoff -a
::shutdown:/bin/umount -a -r
::shutdown:/usr/sbin/fes-reboot-backstop
EOF
)
[ "$shutdown" = "$expected" ] || fail 'shutdown actions are missing, reordered, or changed'
[ "$(printf '%s\n' "$shutdown" | tail -n 1)" = '::shutdown:/usr/sbin/fes-reboot-backstop' ] ||
  fail 'backstop is not the last shutdown action'

proc=$tmp/proc
mkdir -p "$proc/sys/kernel"
for name in panic hung_task_panic hung_task_check_interval_secs hung_task_timeout_secs; do
  : > "$proc/sys/kernel/$name"
done
FES_REBOOT_BACKSTOP_PROC=$proc "$script" || fail 'script failed with proc fixtures'
[ "$(cat "$proc/sys/kernel/panic")" = 3 ] || fail 'panic value is not 3'
[ "$(cat "$proc/sys/kernel/hung_task_panic")" = 1 ] || fail 'hung_task_panic value is not 1'
[ "$(cat "$proc/sys/kernel/hung_task_check_interval_secs")" = 2 ] || fail 'check interval is not 2'
[ "$(cat "$proc/sys/kernel/hung_task_timeout_secs")" = 10 ] || fail 'timeout is not 10'
order=$(awk '/^write_setting / { print $2 }' "$script" | tr '\n' ' ')
[ "$order" = 'panic hung_task_panic hung_task_check_interval_secs hung_task_timeout_secs ' ] ||
  fail 'settings are not written in the required order with timeout last'

mkdir -p "$tmp/empty"
FES_REBOOT_BACKSTOP_PROC=$tmp/empty "$script" || fail 'empty proc root did not exit successfully'
if grep -Eq '^[[:space:]]*(sleep|sync|reboot)([[:space:]]|$)' "$script"; then
  fail 'script contains a sleep, sync, or reboot command'
fi
printf 'reboot backstop checks passed\n'
