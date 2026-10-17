#!/bin/sh
# Fake sysfs: optical drives and disks with no usable media are skipped. A
# USB stick with size greater than 0 and the SD/FESDATA card are not.
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
expect sdd no-media
expect sdb ok
expect sdb1 ok
expect mmcblk0 ok
expect mmcblk0p1 ok
expect mmcblk0p3 ok

# Empty JMS583 shape: the block node exists, size has not been published,
# SCSI type is 0 (disk). That must be no-media, never ok.
mkdir -p "$fixture/sys/class/block/sde/device"
printf '%s\n' 1 > "$fixture/sys/class/block/sde/removable"
printf '%s\n' 0 > "$fixture/sys/class/block/sde/device/type"
printf '%s\n' running > "$fixture/sys/class/block/sde/device/state"
printf '%s\n' 30 > "$fixture/sys/class/block/sde/device/timeout"
expect sde no-media

# Empty size file and a non-numeric size are unreadable.
mkdir -p "$fixture/sys/class/block/sdf/device"
printf '%s\n' 1 > "$fixture/sys/class/block/sdf/removable"
: > "$fixture/sys/class/block/sdf/size"
printf '%s\n' 30 > "$fixture/sys/class/block/sdf/device/timeout"
expect sdf no-media
write_dev sdg nope 1 0 30
expect sdg no-media

# A positive size does not admit a disk the kernel has taken offline, nor a
# partition of that disk.
write_dev sdh 15633408 1 0 180
printf '%s\n' offline > "$fixture/sys/class/block/sdh/device/state"
mkdir -p "$fixture/sys/class/block/sdh/sdh1"
printf '%s\n' 15633376 > "$fixture/sys/class/block/sdh/sdh1/size"
expect sdh no-media
expect sdh1 no-media

# ALUA unavailable is not ready. A running stick stays ok.
write_dev sdi 15633408 1 0 180
printf '%s\n' unavailable > "$fixture/sys/class/block/sdi/device/access_state"
expect sdi no-media
printf '%s\n' running > "$fixture/sys/class/block/sdb/device/state"
expect sdb ok

FES_SYSFS=$fixture/sys "$script"

timeout_of() {
  tr -d ' \t\r\n' < "$fixture/sys/class/block/$1/device/timeout"
}

[ "$(timeout_of sda)" = 1 ] || fail 'optical timeout was not shortened'
[ "$(timeout_of sr0)" = 1 ] || fail 'sr0 timeout was not shortened'
[ "$(timeout_of sr1)" = 1 ] || fail 'sr1 timeout was not shortened'
[ "$(timeout_of sdc)" = 1 ] || fail 'no-media timeout was not shortened'
[ "$(timeout_of sdd)" = 1 ] || fail 'zero-size timeout was not shortened'
[ "$(timeout_of sde)" = 1 ] || fail 'missing-size timeout was not shortened'
[ "$(timeout_of sdf)" = 1 ] || fail 'empty-size timeout was not shortened'
[ "$(timeout_of sdg)" = 1 ] || fail 'non-numeric size timeout was not shortened'
[ "$(timeout_of sdh)" = 1 ] || fail 'offline disk timeout was not shortened'
[ "$(timeout_of sdi)" = 1 ] || fail 'unavailable disk timeout was not shortened'
[ "$(timeout_of sdb)" = 180 ] || fail 'USB stick timeout changed'
[ "$(timeout_of sdb1)" = 180 ] || fail 'USB partition timeout changed'
[ "$(timeout_of mmcblk0)" = 180 ] || fail 'SD timeout changed'
[ "$(timeout_of mmcblk0p3)" = 180 ] || fail 'FESDATA timeout changed'

FES_SYSFS=$fixture/sys FES_BLOCK_ADMIT=$script FES_HOTPLUG=$fixture/hotplug \
  FES_USB_QUIRK=true "$init" start
[ "$(timeout_of sdb)" = 180 ] || fail 'init changed the USB stick'
[ "$(timeout_of mmcblk0p3)" = 180 ] || fail 'init changed FESDATA'
[ "$(tr -d ' \t\r\n' < "$fixture/hotplug")" = /sbin/mdev ] || fail 'hotplug helper was not mdev'

# Sysfs block entries are symlinks. The helper must follow them.
ln -s sda "$fixture/sys/class/block/dock"
expect dock optical

printf '%s\n' 'block-admit_test: passed'
