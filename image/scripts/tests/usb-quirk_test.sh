#!/bin/sh
# The JMS583 quirk is flag u on v= in u-boot.txt, plus the built-in parameter.
# A bootargs= line is preserved and is not where the quirk is installed.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
script=$repo/buildroot/board/fogcast-target/native-rootfs-overlay/usr/sbin/fes-usb-quirk
init=$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S02fes-usb-quirk
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

run() {
  FES_UBOOT_TXT=$fixture/fat/linux/u-boot.txt \
    FES_USB_STORAGE_QUIRKS=$fixture/quirks \
    FES_MOUNTS=$fixture/mounts \
    FES_FAT=$fixture/fat \
    FES_MOUNT=$fixture/bin/mount \
    "$script"
}

mkdir -p "$fixture/fat/linux" "$fixture/bin"
cat > "$fixture/bin/mount" << EOF
#!/bin/sh
printf '%s\n' "\$*" >> "$fixture/mount.log"
exit 0
EOF
chmod 0755 "$fixture/bin/mount"
: > "$fixture/quirks"
printf '%s\n' "tmpfs $fixture/fat tmpfs rw 0 0" > "$fixture/mounts"

run >/dev/null
cmp - "$fixture/fat/linux/u-boot.txt" << 'EOF' || fail 'created v= line'
v=loglevel=4 usb-storage.quirks=152d:0583:u
EOF
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = '152d:0583:u' ] || fail 'empty quirks parameter'

before=$(cat "$fixture/fat/linux/u-boot.txt")
run >/dev/null
[ "$before" = "$(cat "$fixture/fat/linux/u-boot.txt")" ] || fail 'second run rewrote u-boot.txt'
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = '152d:0583:u' ] || fail 'second run rewrote quirks'

printf '%s\n' 'bootargs=quiet' 'ethaddr=02:46:43:00:00:84' > "$fixture/fat/linux/u-boot.txt"
run >/dev/null
cmp - "$fixture/fat/linux/u-boot.txt" << 'EOF' || fail 'existing lines were not preserved'
bootargs=quiet
ethaddr=02:46:43:00:00:84
v=loglevel=4 usb-storage.quirks=152d:0583:u
EOF

printf '%s\n' 'v=loglevel=7' 'ethaddr=02:46:43:00:00:84' > "$fixture/fat/linux/u-boot.txt"
run >/dev/null
cmp - "$fixture/fat/linux/u-boot.txt" << 'EOF' || fail 'custom loglevel was replaced'
v=loglevel=7 usb-storage.quirks=152d:0583:u
ethaddr=02:46:43:00:00:84
EOF

printf '%s\n' 'v=loglevel=4 usb-storage.quirks=aaaa:bbbb:u extra=1' > "$fixture/fat/linux/u-boot.txt"
run >/dev/null
cmp - "$fixture/fat/linux/u-boot.txt" << 'EOF' || fail 'existing quirks list was not extended'
v=loglevel=4 usb-storage.quirks=aaaa:bbbb:u,152d:0583:u extra=1
EOF

printf '%s\n' 'v=loglevel=4 usb-storage.quirks=152d:0583:u' > "$fixture/fat/linux/u-boot.txt"
before=$(cat "$fixture/fat/linux/u-boot.txt")
run >/dev/null
[ "$before" = "$(cat "$fixture/fat/linux/u-boot.txt")" ] || fail 'flag u was duplicated'

# A hand-set IGNORE_DEVICE flag is left alone. This script does not add i.
printf '%s\n' 'v=usb-storage.quirks=152d:0583:i' > "$fixture/fat/linux/u-boot.txt"
run >/dev/null
cmp - "$fixture/fat/linux/u-boot.txt" << 'EOF' || fail 'existing 152d:0583 flags were rewritten'
v=usb-storage.quirks=152d:0583:i
EOF

printf '%s\n' 'abcd:1234:u' > "$fixture/quirks"
run >/dev/null
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = 'abcd:1234:u,152d:0583:u' ] || fail 'quirks parameter was not extended'
run >/dev/null
[ "$(tr -d ' \t\r\n' < "$fixture/quirks")" = 'abcd:1234:u,152d:0583:u' ] || fail 'quirks parameter grew on the second run'

rm -f "$fixture/quirks"
run >/dev/null
[ -e "$fixture/quirks" ] && fail 'missing quirks parameter was created'

printf '%s\n' "tmpfs $fixture/fat tmpfs ro 0 0" > "$fixture/mounts"
: > "$fixture/mount.log"
printf '%s\n' 'ethaddr=02:46:43:00:00:01' > "$fixture/fat/linux/u-boot.txt"
run >/dev/null
grep -Fq 'usb-storage.quirks=152d:0583:u' "$fixture/fat/linux/u-boot.txt" || fail 'read-only FAT was not updated'
grep -Fq 'ethaddr=02:46:43:00:00:01' "$fixture/fat/linux/u-boot.txt" || fail 'ethaddr lost across remount'
grep -Fq -- "-o remount,rw $fixture/fat" "$fixture/mount.log" || fail 'FAT was not remounted read-write'
grep -Fq -- "-o remount,ro $fixture/fat" "$fixture/mount.log" || fail 'FAT was not restored read-only'

printf '%s\n' "tmpfs $fixture/fat tmpfs rw 0 0" > "$fixture/mounts"
rm -rf "$fixture/fat"
: > "$fixture/quirks"
if run > "$fixture/out" 2> "$fixture/err"; then
  :
else
  fail 'missing FAT failed the script'
fi
grep -Fq 'could not update' "$fixture/err" || fail 'missing FAT was silent'
[ ! -d "$fixture/fat" ] || fail 'missing FAT was created'

mkdir -p "$fixture/fat/linux"
: > "$fixture/quirks"
rm -f "$fixture/fat/linux/u-boot.txt"
FES_UBOOT_TXT=$fixture/fat/linux/u-boot.txt \
  FES_USB_STORAGE_QUIRKS=$fixture/quirks \
  FES_MOUNTS=$fixture/mounts \
  FES_FAT=$fixture/fat \
  FES_MOUNT=$fixture/bin/mount \
  FES_USB_QUIRK=$script \
  "$init" start >/dev/null
cmp - "$fixture/fat/linux/u-boot.txt" << 'EOF' || fail 'S02 did not install the quirk'
v=loglevel=4 usb-storage.quirks=152d:0583:u
EOF
grep -Fq '152d:0583:u' "$script" || fail 'flag u missing from helper'
grep -Fq 'IGNORE_DEVICE' "$script" || fail 'flag i tradeoff is not recorded'
! grep -Eq '152d:0583:i([^0-9a-z]|$)' "$script" || fail 'helper installs flag i'

printf '%s\n' 'usb-quirk_test: passed'
