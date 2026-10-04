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
mount_log=$tmp/mount-args
fake_mount=$tmp/mount
cat > "$fake_mount" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$FES_REBOOT_BACKSTOP_MOUNT_LOG"
for root do :; done
if [ "$1" = -o ]; then
  [ -d "$root/sys/kernel" ] || exit 0
else
  mkdir -p "$root/sys/kernel"
fi
for name in panic hung_task_panic hung_task_check_interval_secs hung_task_timeout_secs; do
  [ -e "$root/sys/kernel/$name" ] || : > "$root/sys/kernel/$name"
  chmod 644 "$root/sys/kernel/$name"
done
EOF
chmod +x "$fake_mount"
for name in panic hung_task_panic hung_task_check_interval_secs hung_task_timeout_secs; do
  : > "$proc/sys/kernel/$name"
done
FES_REBOOT_BACKSTOP_PROC=$proc FES_REBOOT_BACKSTOP_MOUNT=$fake_mount \
  FES_REBOOT_BACKSTOP_MOUNT_LOG=$mount_log "$script" || fail 'script failed with proc fixtures'
[ ! -e "$mount_log" ] || fail 'mount was called although proc settings existed'
[ "$(cat "$proc/sys/kernel/panic")" = 3 ] || fail 'panic value is not 3'
[ "$(cat "$proc/sys/kernel/hung_task_panic")" = 1 ] || fail 'hung_task_panic value is not 1'
[ "$(cat "$proc/sys/kernel/hung_task_check_interval_secs")" = 2 ] || fail 'check interval is not 2'
[ "$(cat "$proc/sys/kernel/hung_task_timeout_secs")" = 20 ] || fail 'timeout is not 20'

if [ "$(id -u)" -ne 0 ]; then
  for name in panic hung_task_panic hung_task_check_interval_secs hung_task_timeout_secs; do
    chmod 444 "$proc/sys/kernel/$name"
  done
  : > "$mount_log"
  FES_REBOOT_BACKSTOP_PROC=$proc FES_REBOOT_BACKSTOP_MOUNT=$fake_mount \
    FES_REBOOT_BACKSTOP_MOUNT_LOG=$mount_log "$script" || fail 'script failed with read-only proc fixtures'
  [ "$(sed -n '1p' "$mount_log")" = "-o remount,rw proc $proc" ] || fail 'read-only proc remount arguments are incorrect'
  [ "$(wc -l < "$mount_log" | tr -d ' ')" = 1 ] || fail 'fresh proc mount was attempted after successful remount'
  [ "$(cat "$proc/sys/kernel/hung_task_panic")" = 1 ] || fail 'read-only proc hung_task_panic value is not 1'
else
  printf 'reboot-backstop: skipping chmod-based read-only case as root\n'
fi

missing_proc=$tmp/missing-proc
mkdir -p "$missing_proc"
: > "$mount_log"
FES_REBOOT_BACKSTOP_PROC=$missing_proc FES_REBOOT_BACKSTOP_MOUNT=$fake_mount \
  FES_REBOOT_BACKSTOP_MOUNT_LOG=$mount_log "$script" || fail 'script failed after proc mount'
[ "$(cat "$mount_log")" = "-o remount,rw proc $missing_proc
-t proc proc $missing_proc" ] || fail 'proc mount arguments or order are incorrect'
[ "$(cat "$missing_proc/sys/kernel/panic")" = 3 ] || fail 'mounted proc panic value is not 3'
[ "$(cat "$missing_proc/sys/kernel/hung_task_panic")" = 1 ] || fail 'mounted proc hung_task_panic value is not 1'
[ "$(cat "$missing_proc/sys/kernel/hung_task_check_interval_secs")" = 2 ] || fail 'mounted proc check interval is not 2'
[ "$(cat "$missing_proc/sys/kernel/hung_task_timeout_secs")" = 20 ] || fail 'mounted proc timeout is not 20'
order=$(awk '/^write_setting / { print $2 }' "$script" | tr '\n' ' ')
[ "$order" = 'panic hung_task_panic hung_task_check_interval_secs hung_task_timeout_secs ' ] ||
  fail 'settings are not written in the required order with timeout last'

mkdir -p "$tmp/empty"
FES_REBOOT_BACKSTOP_PROC=$tmp/empty FES_REBOOT_BACKSTOP_MOUNT=false "$script" || fail 'empty proc root did not exit successfully'
if grep -Eq '^[[:space:]]*(sleep|sync|reboot)([[:space:]]|$)' "$script"; then
  fail 'script contains a sleep, sync, or reboot command'
fi
printf 'reboot backstop checks passed\n'
