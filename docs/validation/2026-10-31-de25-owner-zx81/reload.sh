set -eu
test "$(cat /proc/sys/kernel/random/boot_id)" = 1cdac9ba-52eb-4268-87c0-d6533ec445f6
/tmp/fes-owner-zx81/owner-check --boot-id 1cdac9ba-52eb-4268-87c0-d6533ec445f6
rmdir /sys/kernel/config/device-tree/overlays/fes-owner-zx81
test ! -e /sys/kernel/config/device-tree/overlays/fes-owner-zx81
mkdir /sys/kernel/config/device-tree/overlays/fes-owner-zx81
cat /tmp/fes-owner-zx81/zx81.dtbo > /sys/kernel/config/device-tree/overlays/fes-owner-zx81/dtbo
test "$(cat /sys/kernel/config/device-tree/overlays/fes-owner-zx81/status)" = applied
test "$(cat /sys/class/fpga_manager/fpga0/state)" = operating
cat /proc/sys/kernel/random/boot_id
/tmp/fes-owner-zx81/owner-check --boot-id 1cdac9ba-52eb-4268-87c0-d6533ec445f6
echo FES_OWNER_ZX81_REPROGRAM_PASS
