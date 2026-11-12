set -eu
test "$(cat /proc/sys/kernel/random/boot_id)" = 3678ada3-13a3-42a7-9266-ddfbca4e50b1
/tmp/fes-owner-zx81/owner-check --boot-id 3678ada3-13a3-42a7-9266-ddfbca4e50b1
rmdir /sys/kernel/config/device-tree/overlays/fes-owner-zx81
test ! -e /sys/kernel/config/device-tree/overlays/fes-owner-zx81
mkdir /sys/kernel/config/device-tree/overlays/fes-owner-zx81
cat /tmp/fes-owner-zx81/zx81.dtbo > /sys/kernel/config/device-tree/overlays/fes-owner-zx81/dtbo
test "$(cat /sys/kernel/config/device-tree/overlays/fes-owner-zx81/status)" = applied
test "$(cat /sys/class/fpga_manager/fpga0/state)" = operating
cat /proc/sys/kernel/random/boot_id
/tmp/fes-owner-zx81/owner-check --boot-id 3678ada3-13a3-42a7-9266-ddfbca4e50b1
echo FES_OWNER_ZX81_REPROGRAM_PASS
