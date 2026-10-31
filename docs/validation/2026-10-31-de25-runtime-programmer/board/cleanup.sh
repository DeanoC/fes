set -eu
test "$(cat /proc/sys/kernel/random/boot_id)" = 15e107d8-0ed1-4e5e-83d8-b27c8a49d853
test "$(cat /sys/kernel/config/device-tree/overlays/fes-runtime/status)" = applied
test "$(cat /sys/class/fpga_manager/fpga0/state)" = operating
test "$(sha256sum /lib/firmware/fes-runtime-core.rbf | cut -d ' ' -f 1)" = 7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a
rmdir /sys/kernel/config/device-tree/overlays/fes-runtime
test -z "$(ls -A /sys/kernel/config/device-tree/overlays)"
umount /sys/kernel/config
rm /lib/firmware/fes-runtime-core.rbf
rm -rf /tmp/fes-runtime-adapter
test ! -e /lib/firmware/fes-runtime-core.rbf
test ! -e /tmp/fes-runtime-adapter
sha256sum /sys/kernel/notes /usr/share/fes/kernel-modules.json
ls -ln /dev/fes-de25-scanout
dmesg | tail -18
echo DE25_RUNTIME_ADAPTER_CLEANUP_PASS
