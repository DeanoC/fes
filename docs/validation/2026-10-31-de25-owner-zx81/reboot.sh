set -eu
test "$(cat /proc/sys/kernel/random/boot_id)" = 1cdac9ba-52eb-4268-87c0-d6533ec445f6
sync
printf "FES_OWNER_ZX81_SOFTWARE_REBOOT_REQUESTED\n"
reboot
