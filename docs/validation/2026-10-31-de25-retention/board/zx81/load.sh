set -eu
test "$(cat /proc/sys/kernel/random/boot_id)" = 3678ada3-13a3-42a7-9266-ddfbca4e50b1
test "$(cat /sys/block/mmcblk0/device/cid)" = 275048534436344760702d33e0017c00
/tmp/fes-owner-zx81/owner-check --boot-id 3678ada3-13a3-42a7-9266-ddfbca4e50b1
test "$(sha256sum /tmp/fes-owner-zx81/zx81.rbf | cut -d " " -f 1)" = 7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a
test "$(sha256sum /tmp/fes-owner-zx81/de25-computer | cut -d " " -f 1)" = 0e1bcdb1a3351d184f8e1e1817c42e390785d261bae631063de0311cb7d06315
test "$(sha256sum /tmp/fes-owner-zx81/zx81.dtbo | cut -d " " -f 1)" = 3710d9a287112b2f3a5a4eaddb5ce2c13a75c7b0e07ac69f34c7d7b883c0b5aa
test "$(sha256sum /tmp/fes-owner-zx81/seven.p | cut -d " " -f 1)" = b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71
test "$(sha256sum /tmp/fes-owner-zx81/owner-check | cut -d " " -f 1)" = b5db0fc187290073086714b94ccbdd6729011cd20c781e4e7730028f931d0032
test -z "$(cat /sys/module/firmware_class/parameters/path)"
! grep -q ' /sys/kernel/config ' /proc/mounts
mount -t configfs configfs /sys/kernel/config
touch /tmp/fes-owner-zx81/configfs-mounted
test -z "$(ls -A /sys/kernel/config/device-tree/overlays)"
mkdir -p /lib/firmware
test ! -e /lib/firmware/fes-owner-zx81-71303759.rbf
cp /tmp/fes-owner-zx81/zx81.rbf /lib/firmware/fes-owner-zx81-71303759.rbf
chmod 644 /lib/firmware/fes-owner-zx81-71303759.rbf
test "$(sha256sum /lib/firmware/fes-owner-zx81-71303759.rbf | cut -d ' ' -f 1)" = 7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a
mkdir /sys/kernel/config/device-tree/overlays/fes-owner-zx81
printf 'FES_OWNER_ZX81_PROGRAM_START\n'
cat /tmp/fes-owner-zx81/zx81.dtbo > /sys/kernel/config/device-tree/overlays/fes-owner-zx81/dtbo
cat /sys/kernel/config/device-tree/overlays/fes-owner-zx81/status
cat /sys/class/fpga_manager/fpga0/state
test "$(cat /sys/kernel/config/device-tree/overlays/fes-owner-zx81/status)" = applied
test "$(cat /sys/class/fpga_manager/fpga0/state)" = operating
cat /proc/sys/kernel/random/boot_id
/tmp/fes-owner-zx81/owner-check --boot-id 3678ada3-13a3-42a7-9266-ddfbca4e50b1
dmesg | tail -40
printf 'FES_OWNER_ZX81_PROGRAM_PASS\n'
