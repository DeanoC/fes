set -eu
stage=/tmp/fes-owner-retention
boot=$stage/boot
backup=$boot/fes-owner-before-retention
mounted=0
changed=0
hash_is() { test "$(sha256sum "$1" | cut -d ' ' -f 1)" = "$2"; }
cleanup() {
 rc=$?
 trap - EXIT
 if test "$rc" != 0 && test "$changed" = 1; then
  echo RESTORING_PRE_RETENTION_FILES >&2
  cp "$backup/Image" "$boot/Image"
  cp "$backup/manifest" /usr/share/fes/kernel-modules.json
  sync
 }
 if test "$mounted" = 1; then umount "$boot"; fi
 exit "$rc"
}
trap cleanup EXIT
test "$(id -u)" = 0
test "$(uname -m)" = aarch64
test "$(uname -r)" = 6.12.11-fes-de25-owner
test "$(cat /proc/sys/kernel/random/boot_id)" = 9191e320-2d97-41a2-8e28-735ab8695f7c
test "$(cat /sys/block/mmcblk0/device/cid)" = 275048534436344760702d33e0017c00
test -c /dev/fes-de25-scanout
test -z "$(cat /sys/module/firmware_class/parameters/path)"
! grep -q '^/dev/mmcblk0p1 ' /proc/mounts
! grep -q ' /sys/kernel/config ' /proc/mounts
test ! -e /tmp/fes-owner-zx81
hash_is /etc/fes-kernel-release 3134c56dd2bda9a457aa6fa201b901f43c45f57d9c60689488ec5c3c30e10f02
hash_is /etc/init.d/S15fpga 3727ea0d8c4dd78eaf711f50244b41acb128a0aebf4a1103fa450884ed738921
hash_is /etc/mdev.conf 2df2faf2c005b5b7ceaa10328d7beaa1683bfc718c8fb681427efde299cae504
hash_is /usr/share/fes/kernel-modules.json 2c158c2c6135f93d2194384cdcb2b73235bf0830ac2b7d2cfc4b23a53445b7c0
hash_is "$stage/Image" a4604214a70855ec824ffbf70c45abbd82983dcbefd68bb9f51de7b7089ab478
hash_is "$stage/manifest" 65924051b1381476129add83244af33cbe3f1afb0503f3e845347697f7fd073c
mkdir "$boot"
mount -t vfat -o rw,nodev,nosuid,noexec /dev/mmcblk0p1 "$boot"
mounted=1
test ! -e "$backup"
hash_is "$boot/Image" 01357da8a2f9971cb3bf9e7f99bd176bea46a893554d7b1c5364339310fe9b9b
hash_is "$boot/socfpga_agilex5_de25_nano.dtb" d57bac64ffbb1ff4ace06dc0ba3fbb2d589055340274493efbfba687c4f244f7
mkdir "$backup"
cp "$boot/Image" "$backup/Image"
cp /usr/share/fes/kernel-modules.json "$backup/manifest"
hash_is "$backup/Image" 01357da8a2f9971cb3bf9e7f99bd176bea46a893554d7b1c5364339310fe9b9b
hash_is "$backup/manifest" 2c158c2c6135f93d2194384cdcb2b73235bf0830ac2b7d2cfc4b23a53445b7c0
sync
cp "$stage/Image" "$boot/Image.fes-retention-new"
hash_is "$boot/Image.fes-retention-new" a4604214a70855ec824ffbf70c45abbd82983dcbefd68bb9f51de7b7089ab478
sync
changed=1
mv -f "$boot/Image.fes-retention-new" "$boot/Image"
cp "$stage/manifest" /usr/share/fes/kernel-modules.json.fes-retention-new
chmod 644 /usr/share/fes/kernel-modules.json.fes-retention-new
mv -f /usr/share/fes/kernel-modules.json.fes-retention-new /usr/share/fes/kernel-modules.json
hash_is "$boot/Image" a4604214a70855ec824ffbf70c45abbd82983dcbefd68bb9f51de7b7089ab478
hash_is /usr/share/fes/kernel-modules.json 65924051b1381476129add83244af33cbe3f1afb0503f3e845347697f7fd073c
sync
umount "$boot"
mounted=0
changed=0
echo DE25_RETENTION_UPDATE_STAGED_PASS
