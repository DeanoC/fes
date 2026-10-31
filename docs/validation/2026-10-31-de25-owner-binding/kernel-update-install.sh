#!/bin/sh
set -eu
stage=/tmp/fes-owner-bus-8e7479d48
boot=$stage/boot
backup=$boot/fes-owner-before-bus
mounted=0
changed=0
hash_is() { test "$(sha256sum "$1" | cut -d ' ' -f 1)" = "$2"; }
cleanup() {
  trial_rc=$?
  trap - EXIT
  if test "$trial_rc" != 0 && test "$changed" = 1; then
    echo 'Restoring pre-update files after staging failure' >&2
    cp "$backup/Image" "$boot/Image"
    cp "$backup/release" /etc/fes-kernel-release
    chmod 644 /etc/fes-kernel-release
    cp "$backup/startup" /etc/init.d/S15fpga
    chmod 755 /etc/init.d/S15fpga
    cp "$backup/manifest" /usr/share/fes/kernel-modules.json
    chmod 644 /usr/share/fes/kernel-modules.json
    sync
  fi
  if test "$mounted" = 1; then umount "$boot"; fi
  exit "$trial_rc"
}
trap cleanup EXIT
# This narrowly scoped correction requires this exact idle failed-owner boot.
test "$(id -u)" = 0
test "$(uname -m)" = aarch64
test "$(uname -r)" = 6.12.11-fes-de25-owner
test "$(cat /proc/sys/kernel/random/boot_id)" = b989f017-f9f3-45dd-811b-0482b1b65421
test "$(cat /sys/block/mmcblk0/device/cid)" = 275048534436344760702d33e0017c00
test ! -e /dev/fes-de25-scanout
! grep -q '^/dev/mmcblk0p1 ' /proc/mounts
hash_is /etc/fes-kernel-release 3134c56dd2bda9a457aa6fa201b901f43c45f57d9c60689488ec5c3c30e10f02
hash_is "$stage/release" 3134c56dd2bda9a457aa6fa201b901f43c45f57d9c60689488ec5c3c30e10f02
hash_is /etc/init.d/S15fpga 720beb6ec383da4f38db073817862974f5b87891f2045c3f893ec5c1beb05dc7
hash_is "$stage/startup" 3727ea0d8c4dd78eaf711f50244b41acb128a0aebf4a1103fa450884ed738921
hash_is /usr/share/fes/kernel-modules.json 206748ae3cb7e92afd77d8a4719638a09f20ecfb2ee047966a865759a6ca79eb
hash_is "$stage/manifest" 7c67a02d802c0da02b8ef9ee7bc5e843cd46b6c5cad100a6b5cf35c940f02824
hash_is "$stage/Image" 01357da8a2f9971cb3bf9e7f99bd176bea46a893554d7b1c5364339310fe9b9b
mkdir "$boot"
mount -t vfat -o rw,nodev,nosuid,noexec /dev/mmcblk0p1 "$boot"
mounted=1
test ! -e "$backup"
hash_is "$boot/Image" b5a68fa8c0fef8679931af008811676b608f13214e5afb863cbd3626d43c1769
hash_is "$boot/socfpga_agilex5_de25_nano.dtb" d57bac64ffbb1ff4ace06dc0ba3fbb2d589055340274493efbfba687c4f244f7
mkdir "$backup"
cp "$boot/Image" "$backup/Image"
hash_is "$backup/Image" b5a68fa8c0fef8679931af008811676b608f13214e5afb863cbd3626d43c1769
cp /etc/fes-kernel-release "$backup/release"
hash_is "$backup/release" 3134c56dd2bda9a457aa6fa201b901f43c45f57d9c60689488ec5c3c30e10f02
cp /etc/init.d/S15fpga "$backup/startup"
hash_is "$backup/startup" 720beb6ec383da4f38db073817862974f5b87891f2045c3f893ec5c1beb05dc7
cp /usr/share/fes/kernel-modules.json "$backup/manifest"
hash_is "$backup/manifest" 206748ae3cb7e92afd77d8a4719638a09f20ecfb2ee047966a865759a6ca79eb
sync
cp "$stage/Image" "$boot/Image.fes-new"
hash_is "$boot/Image.fes-new" 01357da8a2f9971cb3bf9e7f99bd176bea46a893554d7b1c5364339310fe9b9b
sync
changed=1
mv -f "$boot/Image.fes-new" "$boot/Image"
cp "$stage/release" /etc/fes-kernel-release.fes-new
chmod 644 /etc/fes-kernel-release.fes-new
mv -f /etc/fes-kernel-release.fes-new /etc/fes-kernel-release
hash_is /etc/fes-kernel-release 3134c56dd2bda9a457aa6fa201b901f43c45f57d9c60689488ec5c3c30e10f02
cp "$stage/startup" /etc/init.d/S15fpga.fes-new
chmod 755 /etc/init.d/S15fpga.fes-new
mv -f /etc/init.d/S15fpga.fes-new /etc/init.d/S15fpga
hash_is /etc/init.d/S15fpga 3727ea0d8c4dd78eaf711f50244b41acb128a0aebf4a1103fa450884ed738921
cp "$stage/manifest" /usr/share/fes/kernel-modules.json.fes-new
chmod 644 /usr/share/fes/kernel-modules.json.fes-new
mv -f /usr/share/fes/kernel-modules.json.fes-new /usr/share/fes/kernel-modules.json
hash_is /usr/share/fes/kernel-modules.json 7c67a02d802c0da02b8ef9ee7bc5e843cd46b6c5cad100a6b5cf35c940f02824
hash_is "$boot/Image" 01357da8a2f9971cb3bf9e7f99bd176bea46a893554d7b1c5364339310fe9b9b
sync
umount "$boot"
mounted=0
changed=0
echo DE25_OWNER_BUS_UPDATE_STAGED_PASS
