#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
rootfs=$repo/buildroot/board/fogcast-target/rootfs-overlay
prod=$repo/buildroot/configs/fogcast_target_prod_defconfig
dev=$repo/buildroot/configs/fogcast_target_dev_defconfig
native_dev=$repo/buildroot/configs/fogcast_target_native_dev_defconfig

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
    'BR2_ROOTFS_MERGED_USR=y' \
    'BR2_TARGET_GENERIC_HOSTNAME="mister"' \
    'BR2_TARGET_GENERIC_ISSUE="FogCast target"' \
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
    'BR2_TARGET_ROOTFS_EXT2_MKFS_OPTIONS="-U 9b3652c2-33f1-4a6b-9a53-9b667ab1b001 -E lazy_itable_init=0,lazy_journal_init=0,hash_seed=9b3652c2-33f1-4a6b-9a53-9b667ab1b001"' \
    'BR2_GLOBAL_PATCH_DIR="${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/patches"' \
    'BR2_ROOTFS_OVERLAY="${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/rootfs-overlay"' \
    'BR2_ROOTFS_POST_BUILD_SCRIPT="${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/post-build.sh"'
  do
    require_line "$config" "$required"
  done

  if grep -Eq '^BR2_PACKAGE_(WPA_SUPPLICANT|SAMBA|SAMBA4|PROFTPD|VSFTPD|PYTHON|PYTHON3|BLUEZ5_UTILS_TEST|BLUEZ5_UTILS_CLIENT|GCC|IPKG|OPKG)=y$' "$config"; then
    printf 'forbidden package selected in %s\n' "$config" >&2
    exit 1
  fi
done

e2fs_patch=$repo/buildroot/board/fogcast-target/patches/e2fsprogs/0001-create_inode-honor-fake-time-for-ctime.patch
test -f "$e2fs_patch"
grep -Fq 'inode.i_ctime = fs->now ? fs->now : st->st_ctime;' "$e2fs_patch"

require_line "$prod" '# BR2_PACKAGE_DROPBEAR is not set'
require_line "$dev" 'BR2_PACKAGE_DROPBEAR=y'

test -f "$native_dev"
for required in \
  'BR2_arm=y' \
  'BR2_cortex_a9=y' \
  'BR2_ARM_ENABLE_VFP=y' \
  'BR2_ARM_EABIHF=y' \
  'BR2_TOOLCHAIN_BUILDROOT_GLIBC=y' \
  'BR2_INIT_BUSYBOX=y' \
  'BR2_ROOTFS_DEVICE_CREATION_DYNAMIC_MDEV=y' \
  'BR2_ROOTFS_MERGED_USR=y' \
  'BR2_TARGET_GENERIC_HOSTNAME="mister"' \
  'BR2_TARGET_GENERIC_ISSUE="FogCast target"' \
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
  'BR2_PACKAGE_DROPBEAR=y' \
  'BR2_PACKAGE_LIBCURL=y' \
  'BR2_PACKAGE_LIBCURL_CURL=y' \
  'BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y' \
  'BR2_TARGET_ROOTFS_EXT2=y' \
  'BR2_TARGET_ROOTFS_EXT2_4=y' \
  'BR2_TARGET_ROOTFS_EXT2_SIZE="64M"' \
  'BR2_TARGET_ROOTFS_EXT2_MKFS_OPTIONS="-U 9b3652c2-33f1-4a6b-9a53-9b667ab1b001 -E lazy_itable_init=0,lazy_journal_init=0,hash_seed=9b3652c2-33f1-4a6b-9a53-9b667ab1b001"' \
  'BR2_GLOBAL_PATCH_DIR="${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/patches"' \
  'BR2_ROOTFS_OVERLAY="${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/rootfs-overlay ${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/native-rootfs-overlay"' \
  'BR2_ROOTFS_POST_BUILD_SCRIPT="${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/post-build.sh ${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/native-post-build.sh"'
do
  require_line "$native_dev" "$required"
done

fstab=$rootfs/etc/fstab
inittab=$rootfs/etc/inittab
network=$rootfs/etc/init.d/S20mister-network
main=$rootfs/etc/init.d/S40mister-main
agent=$rootfs/etc/init.d/S50mister-agent
smoke=$rootfs/etc/init.d/S49fogcast-target-smoke
supervise=$rootfs/usr/sbin/mister-supervise
post_build=$repo/buildroot/board/fogcast-target/post-build.sh
native_rootfs=$repo/buildroot/board/fogcast-target/native-rootfs-overlay
native_runtime=$native_rootfs/etc/init.d/S40mister-runtime
native_agent=$native_rootfs/etc/init.d/S50mister-agent
native_post_build=$repo/buildroot/board/fogcast-target/native-post-build.sh

for required_file in "$fstab" "$inittab" "$network" "$main" "$agent" "$smoke" "$supervise" "$post_build"; do
  test -f "$required_file"
done
for required_file in "$native_runtime" "$native_agent" "$native_post_build"; do
  test -f "$required_file"
done
test -d "$rootfs/media/fat"

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
grep -Fq '[ -s "$verify_path" ]' "$main"
! grep -Fq 'sha256sum' "$main"
! grep -Fq 'refusing changed artifact' "$main"
grep -Fq 'wait_seconds=10' "$main"
grep -Fq 'waiting for /media/fat' "$main"
grep -Fq '/media/fat/MiSTer /media/fat/menu.rbf >> /var/log/mister-main.log 2>&1 &' "$main"
! grep -Fq 'mister-supervise mister-main' "$main"
grep -Fq 'wait_seconds=30' "$agent"
grep -Fq '[ ! -f /media/fat/fogcast/agent.toml ]' "$agent"
grep -Fq '/bin/mkdir -p /media/fat/fogcast/cache/megadrive /media/fat/fogcast/cache/snes' "$agent"
grep -Fq '/bin/chmod 0700 /media/fat/fogcast/cache /media/fat/fogcast/cache/megadrive /media/fat/fogcast/cache/snes' "$agent"
grep -Fq 'agent_binary=/media/fat/fogcast/mister-agent' "$agent"
grep -Fq '/usr/sbin/mister-supervise mister-agent "$agent_binary" --config /media/fat/fogcast/agent.toml' "$agent"
test ! -e "$rootfs/media/fat/fogcast"
grep -Fq 'TARGET_IMAGE_SMOKE_READY' "$smoke"

sh "$repo/scripts/validate-native-init-services.sh" "$native_runtime" "$native_agent"
grep -Fq '[ ! -f /media/fat/fogcast/agent.toml ]' "$native_agent"
grep -Fq '/bin/mkdir -p /media/fat/fogcast/cache/megadrive /media/fat/fogcast/cache/snes' "$native_agent"
if grep -Eq '/dev/MiSTer_cmd|CORENAME|/media/fat/MiSTer|agent_binary=|killall|pidof|pgrep|/proc/' "$native_agent"; then
  echo 'native agent init depends on Main, FIFO, CORENAME, FAT agent, or process inspection' >&2
  exit 1
fi
grep -Fq '/bin/rm -f "$target/etc/init.d/S40mister-main"' "$native_post_build"
grep -Fq '/work/build/cache/target-image/native/idle.rbf' "$native_post_build"
grep -Fq '"$target/usr/share/mister-runtime/idle.rbf"' "$native_post_build"
grep -Fq '"$target/usr/share/mister-runtime/build-inputs"' "$native_post_build"
! grep -Fq 'LIBMISTER_RUNTIME_DIR' "$native_post_build"

grep -Fq '/run/$name.pid' "$supervise"
grep -Fq '/var/log/$name.log' "$supervise"
grep -Fq '/bin/sleep 1' "$supervise"
! grep -Eq '(printf|log)[^#]*\$(\*|@)' "$supervise"

fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-rootfs.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM
target=$fixture/target
mkdir -p "$target/root" "$target/etc/init.d" "$target/usr/lib" "$target/usr/sbin" "$target/usr/libexec/bluetooth" "$target/etc/dropbear" "$target/var" "$target/tmp"
cp -R "$rootfs/." "$target/"
ln -s ../tmp "$target/var/log"
: > "$target/etc/init.d/S50dropbear"
: > "$target/etc/init.d/S30dbus"
: > "$target/usr/sbin/dropbear"
: > "$target/usr/libexec/bluetooth/bluetoothd"
: > "$target/usr/lib/libstdc++.so.6.0.28-gdb.py"

printf '%s\n' \
  /lib/ld-linux-armhf.so.3 \
  /lib/libImlib2.so.1 \
  /lib/libbluetooth.so.3 \
  /lib/libbz2.so.1.0 \
  /lib/libc.so.6 \
  /lib/libdl.so.2 \
  /lib/libfreetype.so.6 \
  /lib/libgcc_s.so.1 \
  /lib/libm.so.6 \
  /lib/libpng16.so.16 \
  /lib/libpthread.so.0 \
  /lib/librt.so.1 \
  /lib/libstdc++.so.6 \
  /lib/libz.so.1 | while IFS= read -r library; do
  mkdir -p "$target$(dirname "$library")"
  : > "$target$library"
done

prod_config=$fixture/prod.config
printf '%s\n' '# BR2_PACKAGE_DROPBEAR is not set' > "$prod_config"
BR2_CONFIG=$prod_config "$post_build" "$target"
test -d "$target/var/log"
test ! -L "$target/var/log"
test -x "$target/usr/sbin/mister-agent"
test ! -e "$target/usr/sbin/dropbear"
test ! -e "$target/usr/libexec/bluetooth/bluetoothd"
test ! -e "$target/etc/init.d/S30dbus"
test ! -e "$target/usr/lib/libstdc++.so.6.0.28-gdb.py"
! grep -q 'console::respawn:/sbin/getty' "$target/etc/inittab"

: > "$target/forbidden.zip"
if BR2_CONFIG=$prod_config "$post_build" "$target" >/dev/null 2>&1; then
  echo 'post-build accepted a ROM/archive-like payload' >&2
  exit 1
fi
