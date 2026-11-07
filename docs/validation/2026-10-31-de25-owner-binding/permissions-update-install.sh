#!/bin/sh
set -eu
stage=/tmp/fes-owner-mdev-8281d03a1
backup=/root/fes-owner-mdev-backup-8281d03a1
changed=0
hash_is() { test "$(sha256sum "$1" | cut -d ' ' -f 1)" = "$2"; }
cleanup() {
 trial_rc=$?; trap - EXIT
 if test "$trial_rc" != 0 && test "$changed" = 1; then
  cp "$backup/mdev" /etc/mdev.conf; chmod 644 /etc/mdev.conf
  cp "$backup/startup" /etc/init.d/S15fpga; chmod 755 /etc/init.d/S15fpga
  cp "$backup/manifest" /usr/share/fes/kernel-modules.json; chmod 644 /usr/share/fes/kernel-modules.json
  mdev -s
  sync
 fi
 exit "$trial_rc"
}
trap cleanup EXIT
test "$(id -u)" = 0
test "$(uname -r)" = 6.12.11-fes-de25-owner
test "$(cat /proc/sys/kernel/random/boot_id)" = aef6b113-efe8-4d0c-b164-efbf302bfd70
test "$(cat /sys/block/mmcblk0/device/cid)" = 275048534436344760702d33e0017c00
test -c /dev/fes-de25-scanout
test ! -e /sys/bus/platform/drivers/simple-pm-bus/unbind
hash_is /etc/mdev.conf ef47e6283e802e737af7582795140c57d17344b32a338f23b19cf5dbe2355294
hash_is "$stage/mdev" 2df2faf2c005b5b7ceaa10328d7beaa1683bfc718c8fb681427efde299cae504
hash_is /etc/init.d/S15fpga 3727ea0d8c4dd78eaf711f50244b41acb128a0aebf4a1103fa450884ed738921
hash_is "$stage/startup" 3727ea0d8c4dd78eaf711f50244b41acb128a0aebf4a1103fa450884ed738921
hash_is /usr/share/fes/kernel-modules.json 7c67a02d802c0da02b8ef9ee7bc5e843cd46b6c5cad100a6b5cf35c940f02824
hash_is "$stage/manifest" 2c158c2c6135f93d2194384cdcb2b73235bf0830ac2b7d2cfc4b23a53445b7c0
umask 077
mkdir "$backup"
cp /etc/mdev.conf "$backup/mdev"
hash_is "$backup/mdev" ef47e6283e802e737af7582795140c57d17344b32a338f23b19cf5dbe2355294
cp /etc/init.d/S15fpga "$backup/startup"
hash_is "$backup/startup" 3727ea0d8c4dd78eaf711f50244b41acb128a0aebf4a1103fa450884ed738921
cp /usr/share/fes/kernel-modules.json "$backup/manifest"
hash_is "$backup/manifest" 7c67a02d802c0da02b8ef9ee7bc5e843cd46b6c5cad100a6b5cf35c940f02824
sync
changed=1
cp "$stage/mdev" /etc/mdev.conf.fes-new
chmod 644 /etc/mdev.conf.fes-new
mv -f /etc/mdev.conf.fes-new /etc/mdev.conf
hash_is /etc/mdev.conf 2df2faf2c005b5b7ceaa10328d7beaa1683bfc718c8fb681427efde299cae504
cp "$stage/startup" /etc/init.d/S15fpga.fes-new
chmod 755 /etc/init.d/S15fpga.fes-new
mv -f /etc/init.d/S15fpga.fes-new /etc/init.d/S15fpga
hash_is /etc/init.d/S15fpga 3727ea0d8c4dd78eaf711f50244b41acb128a0aebf4a1103fa450884ed738921
cp "$stage/manifest" /usr/share/fes/kernel-modules.json.fes-new
chmod 644 /usr/share/fes/kernel-modules.json.fes-new
mv -f /usr/share/fes/kernel-modules.json.fes-new /usr/share/fes/kernel-modules.json
hash_is /usr/share/fes/kernel-modules.json 2c158c2c6135f93d2194384cdcb2b73235bf0830ac2b7d2cfc4b23a53445b7c0
mdev -s
"$stage/owner_unconfigured" --boot-id aef6b113-efe8-4d0c-b164-efbf302bfd70
/etc/init.d/S15fpga start
sync
changed=0
echo DE25_OWNER_MDEV_PERSISTENT_RULE_PASS
