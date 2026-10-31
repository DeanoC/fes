set -eu
test "$(cat /proc/sys/kernel/random/boot_id)" = 1cdac9ba-52eb-4268-87c0-d6533ec445f6
printf 'KERNEL_NOTES\n'; sha256sum /sys/kernel/notes
printf 'ROOT_FILES\n'; sha256sum /etc/mdev.conf /etc/init.d/S15fpga /usr/share/fes/kernel-modules.json
printf 'OWNER_DOMAIN\n'; cat /sys/bus/platform/devices/20010200.dma-owner/iommu_group/type
ls -l /sys/bus/platform/devices/20010200.dma-owner/iommu_group/devices
for driver in fes-de25-dma of-fpga-region stratix10-svc 'Stratix10 SoC FPGA manager' simple-pm-bus arm-smmu-v3; do
 test -d "/sys/bus/platform/drivers/$driver"
 test ! -e "/sys/bus/platform/drivers/$driver/unbind"
 test ! -e "/sys/bus/platform/drivers/$driver/module"
done
printf 'BOOT_PAYLOADS\n'
! grep -q '^/dev/mmcblk0p1 ' /proc/mounts
mkdir /tmp/fes-owner-boot-proof
mount -t vfat -o ro,nodev,nosuid,noexec /dev/mmcblk0p1 /tmp/fes-owner-boot-proof
trap 'umount /tmp/fes-owner-boot-proof; rmdir /tmp/fes-owner-boot-proof' EXIT
sha256sum /tmp/fes-owner-boot-proof/Image /tmp/fes-owner-boot-proof/socfpga_agilex5_de25_nano.dtb /tmp/fes-owner-boot-proof/fes-owner-before-bus/Image
echo DE25_OWNER_READINESS_PASS
