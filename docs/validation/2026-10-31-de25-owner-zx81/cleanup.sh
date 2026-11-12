set -eu
test "$(cat /proc/sys/kernel/random/boot_id)" = 1cdac9ba-52eb-4268-87c0-d6533ec445f6
rmdir /sys/kernel/config/device-tree/overlays/fes-owner-zx81
test -z "$(ls -A /sys/kernel/config/device-tree/overlays)"
/tmp/fes-owner-zx81/owner-check --boot-id 1cdac9ba-52eb-4268-87c0-d6533ec445f6
umount /sys/kernel/config
test "$(sha256sum /lib/firmware/fes-owner-zx81-71303759.rbf | cut -d ' ' -f 1)" = 7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a
rm /lib/firmware/fes-owner-zx81-71303759.rbf
rm -rf /tmp/fes-owner-zx81
test ! -e /lib/firmware/fes-owner-zx81-71303759.rbf
test ! -e /tmp/fes-owner-zx81
test -z "$(cat /sys/module/firmware_class/parameters/path)"
dmesg | tail -25
echo FES_OWNER_ZX81_CLEANUP_PASS
