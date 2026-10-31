set -eu
for d in /sys/devices/platform/20010200.dma-owner /sys/devices/platform/soc@0/16000000.iommu /sys/devices/platform/soc@0 /sys/devices/platform/firmware\:svc /sys/devices/platform/firmware\:svc\:fpga-mgr /sys/devices/platform/fpga-region; do
 printf '\nDEVICE %s\n' "$d"
 readlink -f "$d/driver" || true
 ls -ld "$d/driver/unbind" "$d/driver/module" 2>/dev/null || true
 ls -ld "$d"/supplier:* 2>/dev/null || true
done
printf '\nDEFERRED\n'; cat /sys/kernel/debug/devices_deferred 2>/dev/null || true
printf '\nMODULE LOAD CALLERS\n'; grep -r modprobe /etc/init.d /etc/mdev.conf 2>/dev/null || true
