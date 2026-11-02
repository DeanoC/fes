set -eu
test "$(cat /proc/sys/kernel/random/boot_id)" = 3678ada3-13a3-42a7-9266-ddfbca4e50b1
rmdir /sys/kernel/config/device-tree/overlays/fes-owner-zx81
test -z "$(ls -A /sys/kernel/config/device-tree/overlays)"
/tmp/fes-owner-zx81/owner-check --boot-id 3678ada3-13a3-42a7-9266-ddfbca4e50b1
umount /sys/kernel/config
test "$(sha256sum /lib/firmware/fes-owner-zx81-71303759.rbf | cut -d ' ' -f 1)" = 7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a
rm /lib/firmware/fes-owner-zx81-71303759.rbf
rm -rf /tmp/fes-owner-zx81
test ! -e /lib/firmware/fes-owner-zx81-71303759.rbf
test ! -e /tmp/fes-owner-zx81
test -z "$(cat /sys/module/firmware_class/parameters/path)"
dmesg | tail -25
echo FES_OWNER_ZX81_CLEANUP_PASS
