#!/bin/sh
# Fake sysfs: optical and zero-capacity disks are skipped, a USB stick and
# the SD/FESDATA card are not.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
script=$repo/buildroot/board/fogcast-target/native-rootfs-overlay/usr/sbin/fes-block-admit
init=$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S11fes-block-admit
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fes-block-admit.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

fail() {
  printf 'block-admit_test: %s\n' "$1" >&2
  exit 1
}

test -x "$script" || fail 'helper is not executable'
sh -n "$script"
sh -n "$init"
if command -v dash >/dev/null 2>&1; then dash -n "$script"; dash -n "$init"; fi

write_dev() {
  name=$1
  size=$2
  removable=$3
  type=$4
  timeout=$5
  dir=$fixture/sys/class/block/$name
  mkdir -p "$dir/device"
  printf '%s\n' "$size" > "$dir/size"
  printf '%s\n' "$removable" > "$dir/removable"
  if [ -n "$type" ]; then
    printf '%s\n' "$type" > "$dir/device/type"
  fi
  if [ -n "$timeout" ]; then
    printf '%s\n' "$timeout" > "$dir/device/timeout"
  fi
}

write_dev sda 0 1 5 180
write_dev sr0 0 1 5 180
write_dev sr1 2048 1 5 180
write_dev sdc 0 1 0 180
write_dev sdd 0 0 0 180
write_dev sdb 15633408 1 0 180
write_dev sdb1 15633376 1 0 180
# Multi-digit partition with no type of its own inherits the optical parent.
mkdir -p "$fixture/sys/class/block/sda/sda10"
printf '%s\n' 2048 > "$fixture/sys/class/block/sda/sda10/size"
write_dev mmcblk0 62333952 1 '' 180
write_dev mmcblk0p1 2097152 1 '' 180
write_dev mmcblk0p3 41943040 1 '' 180
# Opening this would block. The helper must not read it.
mkfifo "$fixture/sys/class/block/sda/device/inquiry"

expect() {
  name=$1
  want=$2
  got=$(FES_SYSFS=$fixture/sys "$script" --reason "$name")
  [ "$got" = "$want" ] || fail "$name reason $got, want $want"
}

expect sda optical
expect sda10 optical
expect sr0 optical
expect sr1 optical
expect sdc no-media
expect sdd zero-capacity
expect sdb ok
expect sdb1 ok
expect mmcblk0 ok
expect mmcblk0p1 ok
expect mmcblk0p3 ok

FES_SYSFS=$fixture/sys "$script"

timeout_of() {
  tr -d ' \t\r\n' < "$fixture/sys/class/block/$1/device/timeout"
}

[ "$(timeout_of sda)" = 1 ] || fail 'optical timeout was not shortened'
[ "$(timeout_of sr0)" = 1 ] || fail 'sr0 timeout was not shortened'
[ "$(timeout_of sr1)" = 1 ] || fail 'sr1 timeout was not shortened'
[ "$(timeout_of sdc)" = 1 ] || fail 'no-media timeout was not shortened'
[ "$(timeout_of sdd)" = 1 ] || fail 'zero-capacity timeout was not shortened'
[ "$(timeout_of sdb)" = 180 ] || fail 'USB stick timeout changed'
[ "$(timeout_of sdb1)" = 180 ] || fail 'USB partition timeout changed'
[ "$(timeout_of mmcblk0)" = 180 ] || fail 'SD timeout changed'
[ "$(timeout_of mmcblk0p3)" = 180 ] || fail 'FESDATA timeout changed'

FES_SYSFS=$fixture/sys FES_BLOCK_ADMIT=$script "$init" start
[ "$(timeout_of sdb)" = 180 ] || fail 'init changed the USB stick'
[ "$(timeout_of mmcblk0p3)" = 180 ] || fail 'init changed FESDATA'

printf '%s\n' 'block-admit_test: passed'
