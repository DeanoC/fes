#!/bin/sh
# The JMS583 quirk written at runtime is flag i on the usb-storage parameter.
# FAT linux/u-boot.txt is installed by media assembly and is not rewritten here.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
root=$(CDPATH='' cd -- "$repo/.." && pwd)
script=$repo/buildroot/board/fogcast-target/native-rootfs-overlay/usr/sbin/fes-usb-quirk
init=$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S02fes-usb-quirk
boot=$root/image/boot/u-boot.txt
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fes-usb-quirk.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

fail() {
  printf 'usb-quirk_test: %s\n' "$1" >&2
  exit 1
}

test -x "$script" || fail 'helper is not executable'
sh -n "$script"
sh -n "$init"
if command -v dash >/dev/null 2>&1; then dash -n "$script"; dash -n "$init"; fi

cmp - "$boot" << 'EOF' || fail 'image boot config is not the ignore quirk'
v=loglevel=4 usb-storage.quirks=152d:0583:i
EOF
grep -Fq '152d:0583:i' "$script" || fail 'flag i missing from helper'
! grep -Fq '152d:0583:u' "$script" || fail 'helper still installs flag u'
! grep -Fq '/media/fat' "$script" || fail 'helper still names the FAT mount'
! grep -Fq 'remount' "$script" || fail 'helper still remounts boot storage'

run() {
  FES_USB_STORAGE_QUIRKS=$fixture/quirks "$script"
}

: > "$fixture/quirks"
run >/dev/null
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = '152d:0583:i' ] || fail 'empty quirks parameter'
before=$(cat "$fixture/quirks")
run >/dev/null
[ "$before" = "$(cat "$fixture/quirks")" ] || fail 'second run rewrote quirks'

printf '%s\n' 'abcd:1234:u' > "$fixture/quirks"
run >/dev/null
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = 'abcd:1234:u,152d:0583:i' ] || fail 'quirks parameter was not extended'
run >/dev/null
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = 'abcd:1234:u,152d:0583:i' ] || fail 'quirks parameter grew on the second run'

printf '%s\n' '152d:0583:u' > "$fixture/quirks"
run >/dev/null
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = '152d:0583:u' ] || fail 'existing 152d:0583 entry was rewritten'

rm -f "$fixture/quirks"
run >/dev/null
[ -e "$fixture/quirks" ] && fail 'missing quirks parameter was created'

mkdir -p "$fixture/fat/linux"
printf '%s\n' 'ethaddr=02:46:43:00:00:01' > "$fixture/fat/linux/u-boot.txt"
cp "$fixture/fat/linux/u-boot.txt" "$fixture/before"
: > "$fixture/quirks"
FES_USB_STORAGE_QUIRKS=$fixture/quirks \
  FES_USB_QUIRK=$script \
  "$init" start >/dev/null
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = '152d:0583:i' ] || fail 'S02 did not set the quirks parameter'
cmp "$fixture/before" "$fixture/fat/linux/u-boot.txt" || fail 'S02 rewrote u-boot.txt'

printf '%s\n' 'usb-quirk_test: passed'
