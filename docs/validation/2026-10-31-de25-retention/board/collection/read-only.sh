set -eu
export LC_ALL=C
emit_text() { printf '%s\t' "$1"; printf '%s' "$2" | od -An -v -tx1 | tr -d ' \n'; printf '\n'; }
emit_file() { h=$(od -An -v -tx1 "$2"); printf '%s\t' "$1"; printf '%s' "$h" | tr -d ' \n'; printf '\n'; }
printf 'FES-DE25-FIRST-BOOT\t1\n'
emit_file boot_id_start /proc/sys/kernel/random/boot_id
emit_text architecture "$(uname -m)"
emit_text kernel_release "$(uname -r)"
emit_file board /etc/fes-board
emit_file model /proc/device-tree/model
emit_file compatible /proc/device-tree/compatible
emit_file cmdline /proc/cmdline
emit_file meminfo /proc/meminfo
emit_file mountinfo /proc/self/mountinfo
emit_file root_dev /sys/class/block/mmcblk0p2/dev
emit_file root_uevent /sys/class/block/mmcblk0p2/uevent
emit_file mac /sys/class/net/eth0/address
emit_file carrier /sys/class/net/eth0/carrier
emit_text addresses "$(ip -4 addr show dev eth0)"
emit_text routes "$(ip -4 route show)"
emit_text ssh_connection "${SSH_CONNECTION:?SSH_CONNECTION missing}"
emit_text file_hashes "$(sha256sum /etc/fes-board /etc/hostname /etc/inittab /etc/network/interfaces /etc/init.d/S50dropbear /bin/busybox /usr/sbin/dropbear)"
emit_file boot_id_end /proc/sys/kernel/random/boot_id
printf 'FES-DE25-FIRST-BOOT-END\t1\n'
