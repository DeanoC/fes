#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
rootfs=$repo/buildroot/board/mister-remote/rootfs-overlay
prod=$repo/buildroot/configs/mister_remote_poc1b_prod_defconfig
dev=$repo/buildroot/configs/mister_remote_poc1b_dev_defconfig

require_line() {
  require_file=$1
  require_value=$2
  grep -Fqx -- "$require_value" "$require_file" || {
    printf 'missing exact policy in %s: %s\n' "$require_file" "$require_value" >&2
    exit 1
  }
}

for config in "$prod" "$dev"; do
  test -f "$config"
  for required in \
    'BR2_arm=y' \
    'BR2_cortex_a9=y' \
    'BR2_ARM_ENABLE_VFP=y' \
    'BR2_ARM_EABIHF=y' \
    'BR2_TOOLCHAIN_BUILDROOT_GLIBC=y' \
    'BR2_INIT_BUSYBOX=y' \
    'BR2_ROOTFS_DEVICE_CREATION_DYNAMIC_MDEV=y' \
    'BR2_TARGET_GENERIC_HOSTNAME="mister"' \
    'BR2_TARGET_GENERIC_ISSUE="MiSTer Remote POC 1B"' \
    'BR2_SYSTEM_DHCP=""' \
    '# BR2_TARGET_GENERIC_REMOUNT_ROOTFS_RW is not set' \
    'BR2_REPRODUCIBLE=y' \
    'BR2_PACKAGE_IMLIB2=y' \
    'BR2_PACKAGE_FREETYPE=y' \
    'BR2_PACKAGE_LIBPNG=y' \
    'BR2_PACKAGE_BZIP2=y' \
    'BR2_PACKAGE_ZLIB=y' \
    'BR2_PACKAGE_BLUEZ5_UTILS=y' \
    'BR2_TOOLCHAIN_BUILDROOT_CXX=y' \
    'BR2_TARGET_ROOTFS_EXT2=y' \
    'BR2_TARGET_ROOTFS_EXT2_4=y' \
    'BR2_TARGET_ROOTFS_EXT2_SIZE="64M"' \
    'BR2_ROOTFS_OVERLAY="${BR2_EXTERNAL_MISTER_REMOTE_PATH}/board/mister-remote/rootfs-overlay"' \
    'BR2_ROOTFS_POST_BUILD_SCRIPT="${BR2_EXTERNAL_MISTER_REMOTE_PATH}/board/mister-remote/post-build.sh"'
  do
    require_line "$config" "$required"
  done

  if grep -Eq '^BR2_PACKAGE_(WPA_SUPPLICANT|SAMBA|SAMBA4|PROFTPD|VSFTPD|PYTHON|PYTHON3|BLUEZ5_UTILS_TEST|BLUEZ5_UTILS_CLIENT|GCC|IPKG|OPKG)=y$' "$config"; then
    printf 'forbidden package selected in %s\n' "$config" >&2
    exit 1
  fi
done

require_line "$prod" '# BR2_PACKAGE_DROPBEAR is not set'
require_line "$dev" 'BR2_PACKAGE_DROPBEAR=y'

fstab=$rootfs/etc/fstab
inittab=$rootfs/etc/inittab
network=$rootfs/etc/init.d/S20mister-network
main=$rootfs/etc/init.d/S40mister-main
agent=$rootfs/etc/init.d/S50mister-agent
supervise=$rootfs/usr/sbin/mister-supervise
post_build=$repo/buildroot/board/mister-remote/post-build.sh

for required_file in "$fstab" "$inittab" "$network" "$main" "$agent" "$supervise" "$post_build"; do
  test -f "$required_file"
done

grep -Eq '^/dev/root[[:space:]]+/[[:space:]]+ext4[[:space:]]+ro,noatime,noauto' "$fstab"
for volatile_mount in /dev/shm /run /tmp /var/log; do
  grep -Eq "^[^#]+[[:space:]]+$volatile_mount[[:space:]]+tmpfs[[:space:]]" "$fstab"
done
! grep -q 'remount,rw' "$inittab"
grep -Fq '::sysinit:/sbin/mdev -s' "$inittab"
grep -Fq '::sysinit:/etc/init.d/rcS' "$inittab"
grep -Fq 'console::respawn:/sbin/getty' "$inittab"

grep -Fq 'wait_seconds=10' "$network"
grep -Fq '/sbin/udhcpc -f -q -t 5 -T 2 -i eth0 -x hostname:mister -s /usr/share/udhcpc/default.script' "$network"
grep -Fq '7ca3cd2f224b9264d0889f593a0d77aafa5adda61910baba92c5ae401e26fcce' "$main"
grep -Fq '821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934' "$main"
grep -Fq '/usr/sbin/mister-supervise mister-main /media/fat/MiSTer /media/fat/menu.rbf' "$main"
grep -Fq 'wait_seconds=30' "$agent"
grep -Fq '/usr/sbin/mister-supervise mister-agent /usr/sbin/mister-agent --config /media/fat/mister-remote/agent.toml' "$agent"

grep -Fq '/run/$name.pid' "$supervise"
grep -Fq '/var/log/$name.log' "$supervise"
grep -Fq '/bin/sleep 1' "$supervise"
! grep -Eq '(printf|log)[^#]*\$(\*|@)' "$supervise"

fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-rootfs.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM
target=$fixture/target
mkdir -p "$target/root" "$target/etc/init.d" "$target/usr/sbin" "$target/usr/libexec/bluetooth" "$target/etc/dropbear"
cp -R "$rootfs/." "$target/"
: > "$target/etc/init.d/S50dropbear"
: > "$target/etc/init.d/S30dbus"
: > "$target/usr/sbin/dropbear"
: > "$target/usr/libexec/bluetooth/bluetoothd"

awk '
  /^\[\[libraries\]\]$/ { in_library=1; next }
  /^\[/ { in_library=0 }
  in_library && /^path = / {
    value=$0
    sub(/^[^=]*=[[:space:]]*"/, "", value)
    sub(/"[[:space:]]*$/, "", value)
    print value
  }
' "$repo/build/sources.poc1a.lock.toml" | while IFS= read -r library; do
  mkdir -p "$target$(dirname "$library")"
  : > "$target$library"
done

prod_config=$fixture/prod.config
printf '%s\n' '# BR2_PACKAGE_DROPBEAR is not set' > "$prod_config"
BR2_CONFIG=$prod_config "$post_build" "$target"
test -x "$target/usr/sbin/mister-agent"
test ! -e "$target/usr/sbin/dropbear"
test ! -e "$target/usr/libexec/bluetooth/bluetoothd"
test ! -e "$target/etc/init.d/S30dbus"
! grep -q 'console::respawn:/sbin/getty' "$target/etc/inittab"

: > "$target/forbidden.zip"
if BR2_CONFIG=$prod_config "$post_build" "$target" >/dev/null 2>&1; then
  echo 'post-build accepted a ROM/archive-like payload' >&2
  exit 1
fi
