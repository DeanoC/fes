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
menu_blanking=$rootfs/usr/sbin/mister-disable-menu-blanking
agent=$rootfs/etc/init.d/S50mister-agent
smoke=$rootfs/etc/init.d/S49fogcast-target-smoke
supervise=$rootfs/usr/sbin/mister-supervise
post_build=$repo/buildroot/board/fogcast-target/post-build.sh
native_rootfs=$repo/buildroot/board/fogcast-target/native-rootfs-overlay
native_runtime=$native_rootfs/etc/init.d/S40mister-runtime
native_agent=$native_rootfs/etc/init.d/S50mister-agent
native_post_build=$repo/buildroot/board/fogcast-target/native-post-build.sh
native_agent_main=$repo/cmd/mister-agent/main.go

test -f "$menu_blanking" || {
  printf '%s\n' 'legacy image is missing the Menu blanking policy helper' >&2
  exit 1
}
test -x "$menu_blanking"
for required_file in "$fstab" "$inittab" "$network" "$main" "$menu_blanking" "$agent" "$smoke" "$supervise" "$post_build"; do
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
grep -Fq '/usr/sbin/mister-disable-menu-blanking /media/fat/MiSTer.ini' "$main"
grep -Fq '/media/fat/MiSTer /media/fat/menu.rbf >> /var/log/mister-main.log 2>&1 &' "$main"
blanking_line=$(grep -Fn '/usr/sbin/mister-disable-menu-blanking /media/fat/MiSTer.ini' "$main" | cut -d: -f1)
launch_line=$(grep -Fn '/media/fat/MiSTer /media/fat/menu.rbf >> /var/log/mister-main.log 2>&1 &' "$main" | cut -d: -f1)
[ "$blanking_line" -lt "$launch_line" ]
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
grep -Fq '"$target/usr/sbin/mister-disable-menu-blanking"' "$native_post_build"
grep -Fq '/work/build/cache/target-image/native/idle.rbf' "$native_post_build"
grep -Fq '"$target/usr/share/mister-runtime/idle.rbf"' "$native_post_build"
grep -Fq '/work/build/cache/target-image/native/megadrive.rbf' "$native_post_build"
grep -Fq '"$target/usr/share/mister-runtime/cores/megadrive.rbf"' "$native_post_build"
grep -Fq '"$target/usr/share/mister-runtime/build-inputs"' "$native_post_build"
! grep -Fq 'LIBMISTER_RUNTIME_DIR' "$native_post_build"
grep -Fq 'developmentRBFPath      = "/tmp/fogcast-development/core.rbf"' \
  "$native_agent_main"
grep -Eq '^[^#]+[[:space:]]+/tmp[[:space:]]+tmpfs[[:space:]]' "$fstab"

grep -Fq '/run/$name.pid' "$supervise"
grep -Fq '/var/log/$name.log' "$supervise"
grep -Fq '/bin/sleep 1' "$supervise"
! grep -Eq '(printf|log)[^#]*\$(\*|@)' "$supervise"

fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-rootfs.XXXXXX")
trap 'chmod -R u+w "$fixture" 2>/dev/null || true; rm -rf "$fixture"' EXIT INT TERM

menu_fixture=$fixture/menu-blanking
mkdir -p "$menu_fixture"

live_ini=$menu_fixture/live.ini
cat > "$live_ini" <<'EOF'
[MiSTer]
osd_timeout=5
video_off=1
video_off_logo=0
hdmi_off=0
EOF
cp "$live_ini" "$live_ini.original"
cat > "$menu_fixture/live.expected" <<'EOF'
[MiSTer]
osd_timeout=0
video_off=0
video_off_logo=0
hdmi_off=0
EOF
sh "$menu_blanking" "$live_ini"
cmp "$live_ini" "$menu_fixture/live.expected"
cmp "$live_ini.fogcast-backup" "$live_ini.original"
live_hash=$(sha256sum "$live_ini" "$live_ini.fogcast-backup")
sh "$menu_blanking" "$live_ini"
[ "$live_hash" = "$(sha256sum "$live_ini" "$live_ini.fogcast-backup")" ]

if command -v busybox >/dev/null 2>&1; then
  busybox_path=$menu_fixture/busybox-path
  mkdir -p "$busybox_path"
  busybox --install -s "$busybox_path"
  cp "$live_ini.original" "$menu_fixture/busybox.ini"
  PATH=$busybox_path /bin/sh "$menu_blanking" "$menu_fixture/busybox.ini"
  cmp "$menu_fixture/busybox.ini" "$menu_fixture/live.expected"
fi

missing_ini=$menu_fixture/missing.ini
cat > "$menu_fixture/missing.expected" <<'EOF'
[MiSTer]
osd_timeout=0
video_off=0
EOF
sh "$menu_blanking" "$missing_ini"
cmp "$missing_ini" "$menu_fixture/missing.expected"
test ! -e "$missing_ini.fogcast-backup"

commented_ini=$menu_fixture/commented.ini
cat > "$commented_ini" <<'EOF'
[MiSTer]
;osd_timeout=30
;video_off=0
keep=this
EOF
cat > "$menu_fixture/commented.expected" <<'EOF'
[MiSTer]
;osd_timeout=30
;video_off=0
keep=this
osd_timeout=0
video_off=0
EOF
sh "$menu_blanking" "$commented_ini"
cmp "$commented_ini" "$menu_fixture/commented.expected"

duplicates_ini=$menu_fixture/duplicates.ini
cat > "$duplicates_ini" <<'EOF'
preamble=keep
[MiSTer]
 OSD_TIMEOUT = 5
video_off=1
keep=this-too
osd_timeout=30
VIDEO_OFF = 2
[Menu]
osd_timeout=8
video_off=9
[video=1280,720,60]
osd_timeout=99
video_off=99
EOF
cat > "$menu_fixture/duplicates.expected" <<'EOF'
preamble=keep
[MiSTer]
osd_timeout=0
video_off=0
keep=this-too
[Menu]
osd_timeout=0
video_off=0
[video=1280,720,60]
osd_timeout=0
video_off=0
EOF
sh "$menu_blanking" "$duplicates_ini"
cmp "$duplicates_ini" "$menu_fixture/duplicates.expected"

no_section_ini=$menu_fixture/no-section.ini
cat > "$no_section_ini" <<'EOF'
[Other]
keep=this
EOF
cat > "$menu_fixture/no-section.expected" <<'EOF'
[Other]
keep=this

[MiSTer]
osd_timeout=0
video_off=0
EOF
sh "$menu_blanking" "$no_section_ini"
cmp "$no_section_ini" "$menu_fixture/no-section.expected"

protected_ini=$menu_fixture/protected.ini
cat > "$protected_ini" <<'EOF'
[MiSTer]
osd_timeout=5
video_off=1
EOF
printf '%s\n' 'existing complete backup' > "$protected_ini.fogcast-backup"
cp "$protected_ini.fogcast-backup" "$menu_fixture/protected-backup.expected"
sh "$menu_blanking" "$protected_ini"
cmp "$protected_ini.fogcast-backup" "$menu_fixture/protected-backup.expected"
grep -Fqx 'osd_timeout=0' "$protected_ini"
grep -Fqx 'video_off=0' "$protected_ini"

copy_failure_ini=$menu_fixture/copy-failure.ini
cat > "$copy_failure_ini" <<'EOF'
[MiSTer]
osd_timeout=5
video_off=1
EOF
cp "$copy_failure_ini" "$menu_fixture/copy-failure.original"
fake_bin=$menu_fixture/fake-bin
mkdir -p "$fake_bin"
cat > "$fake_bin/cp" <<'EOF'
#!/bin/sh
exit 9
EOF
chmod 0755 "$fake_bin/cp"
if PATH=$fake_bin:$PATH sh "$menu_blanking" "$copy_failure_ini" >/dev/null 2>&1; then
  echo 'Menu blanking helper accepted a failed backup copy' >&2
  exit 1
fi
cmp "$copy_failure_ini" "$menu_fixture/copy-failure.original"
test ! -e "$copy_failure_ini.fogcast-backup"
if find "$menu_fixture" -maxdepth 1 -type f \
  \( -name '.copy-failure.ini.fogcast.*' -o -name '.copy-failure.ini.fogcast-backup.*' \) \
  -print -quit | grep -q .; then
  echo 'Menu blanking helper left temporary files after a failed backup copy' >&2
  exit 1
fi

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
test -x "$target/usr/sbin/mister-disable-menu-blanking"
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

native_fixture=$fixture/native-post-build
native_target=$native_fixture/target
native_cache=$native_fixture/cache
mkdir -p "$native_target/usr/sbin" "$native_target/etc/init.d" "$native_cache"
cp -R "$native_rootfs/." "$native_target/"
printf '%s\n' runtime > "$native_target/usr/sbin/mister-runtime"
printf '%s\n' agent > "$native_target/usr/sbin/mister-agent"
chmod 0755 "$native_target/usr/sbin/mister-runtime" "$native_target/usr/sbin/mister-agent"
printf '%s\n' idle > "$native_cache/idle.rbf"
printf '%s\n' megadrive > "$native_cache/megadrive.rbf"
native_idle_sha=$(sha256sum "$native_cache/idle.rbf" | awk '{print $1}')
native_idle_size=$(wc -c < "$native_cache/idle.rbf" | tr -d ' ')
native_megadrive_sha=$(sha256sum "$native_cache/megadrive.rbf" | awk '{print $1}')
native_megadrive_size=$(wc -c < "$native_cache/megadrive.rbf" | tr -d ' ')
native_agent_sha=$(sha256sum "$native_target/usr/sbin/mister-agent" | awk '{print $1}')
native_lock=$native_fixture/native-runtime.inputs.lock.toml
cat > "$native_lock" <<EOF
format = 1

[mister_runtime]
commit = '1111111111111111111111111111111111111111'
mount_path = '/runtime-source'

[idle_rbf]
repository = 'https://fixture.invalid/idle'
commit = '2222222222222222222222222222222222222222'
path = 'idle.rbf'
sha256 = '$native_idle_sha'
size = $native_idle_size
install_path = '/usr/share/mister-runtime/idle.rbf'

[megadrive_rbf]
repository = 'https://fixture.invalid/megadrive'
commit = '3333333333333333333333333333333333333333'
path = 'MegaDrive.rbf'
sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
size = 1
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
EOF

native_selection=$native_fixture/megadrive.selection.toml
cat > "$native_selection" <<EOF
format = 1
origin = 'source-built'
abi = 'mister'
system = 'megadrive'
repository = 'https://fixture.invalid/source-built-megadrive'
revision = '4444444444444444444444444444444444444444'
artifact = 'megadrive.rbf'
sha256 = '$native_megadrive_sha'
size = $native_megadrive_size
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
recipe = 'scripts/rebuild_core.py'
recipe_sha256 = '5555555555555555555555555555555555555555555555555555555555555555'
toolchain = 'fixture-toolchain'
EOF
chmod 0444 "$native_selection"
export NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE="$native_selection"

NATIVE_RUNTIME_INPUT_LOCK=$native_lock \
NATIVE_RUNTIME_IDLE_FILE=$native_cache/idle.rbf \
NATIVE_RUNTIME_MEGADRIVE_FILE=$native_cache/megadrive.rbf \
  "$native_post_build" "$native_target"
cmp "$repo/bin/fogcast-kit-linux-armv7" "$native_target/usr/sbin/fogcast-kit"
test -x "$native_target/etc/init.d/S60fogcast-kit"
grep -Fqx "fogcast_kit_sha256=$(sha256sum "$repo/bin/fogcast-kit-linux-armv7" | awk '{print $1}')" "$native_target/usr/share/mister-runtime/build-inputs"
cmp "$native_cache/idle.rbf" "$native_target/usr/share/mister-runtime/idle.rbf"
cmp "$native_cache/megadrive.rbf" "$native_target/usr/share/mister-runtime/cores/megadrive.rbf"
test "$(stat -c %a "$native_target/usr/share/mister-runtime/idle.rbf")" = 644
test "$(stat -c %a "$native_target/usr/share/mister-runtime/cores/megadrive.rbf")" = 644
test "$(find "$native_target" -type f -iname '*.rbf' | wc -l | tr -d ' ')" -eq 2
for prohibited_development_rbf in \
  /usr/share/mister-runtime/development.rbf \
  /usr/share/mister-runtime/cores/development.rbf \
  /tmp/fogcast-development/core.rbf; do
  test ! -e "$native_target$prohibited_development_rbf"
done
grep -Fqx "mister_runtime_commit=1111111111111111111111111111111111111111" \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx "mister_agent_sha256=$native_agent_sha" \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx "megadrive_sha256=$native_megadrive_sha" \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_install_path=/usr/share/mister-runtime/cores/megadrive.rbf' \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_origin=source-built' \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_abi=mister' \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_system=megadrive' \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_artifact=megadrive.rbf' \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_recipe=scripts/rebuild_core.py' \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_recipe_sha256=5555555555555555555555555555555555555555555555555555555555555555' \
  "$native_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_toolchain=fixture-toolchain' \
  "$native_target/usr/share/mister-runtime/build-inputs"

package_id=b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0
package_source=$native_fixture/fes-pong-package
package_selection=$native_fixture/fes-pong.package-selection.toml
mkdir "$package_source"
cp "$repo/internal/corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml" "$package_source/manifest.toml"
cp "$repo/internal/corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf" "$package_source/core.rbf"
chmod 0444 "$package_source"/*
chmod 0555 "$package_source"
cat > "$package_selection" <<EOF
format = 2
kind = 'core-package'
core_id = 'fes.pong'
package_id = '$package_id'
payload_sha256 = 'e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1'
misteross_revision = '1111111111111111111111111111111111111111'
mister_packages_revision = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
install_path = '/usr/share/mister-runtime/core-packages/$package_id'
EOF
chmod 0444 "$package_selection"
package_selector=$native_fixture/target-image-lock
(cd "$repo" && go build -o "$package_selector" ./cmd/target-image-lock)
FES_PONG_PACKAGE_DIR=$package_source \
FES_PONG_PACKAGE_SELECTION=$package_selection \
TARGET_IMAGE_LOCK_BIN=$package_selector \
NATIVE_RUNTIME_SYSTEMS=megadrive \
  "$repo/scripts/native-extra-cores.sh" fetch "$native_cache"
native_package_target=$native_fixture/package-target
cp -R "$native_fixture/target" "$native_package_target"
FES_PONG_PACKAGE_DIR=$package_source \
FES_PONG_PACKAGE_SELECTION=$package_selection \
TARGET_IMAGE_LOCK_BIN=$package_selector \
NATIVE_RUNTIME_SYSTEMS=megadrive \
NATIVE_RUNTIME_INPUT_LOCK=$native_lock \
NATIVE_RUNTIME_IDLE_FILE=$native_cache/idle.rbf \
NATIVE_RUNTIME_MEGADRIVE_FILE=$native_cache/megadrive.rbf \
NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=$native_selection \
  "$native_post_build" "$native_package_target"
test "$(find "$native_package_target" -type f -iname '*.rbf' | wc -l | tr -d ' ')" -eq 3
test "$(stat -c %a "$native_package_target/usr/share/mister-runtime/core-packages/$package_id")" = 555
grep -Fqx "fes_pong_package_selection_sha256=$(sha256sum "$package_selection" | awk '{print $1}')" \
  "$native_package_target/usr/share/mister-runtime/build-inputs"
grep -Fqx "fes_pong_package_id=$package_id" \
  "$native_package_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'fes_pong_misteross_revision=1111111111111111111111111111111111111111' \
  "$native_package_target/usr/share/mister-runtime/build-inputs"
FES_PONG_PACKAGE_DIR=$package_source \
FES_PONG_PACKAGE_SELECTION=$package_selection \
TARGET_IMAGE_LOCK_BIN=$package_selector \
NATIVE_RUNTIME_SYSTEMS=megadrive \
  "$repo/scripts/native-extra-cores.sh" verify-image "$native_cache" "$native_package_target"

native_upstream_lock=$native_fixture/upstream-native-runtime.inputs.lock.toml
sed \
  -e "s/sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'/sha256 = '$native_megadrive_sha'/" \
  -e "s/^size = 1$/size = $native_megadrive_size/" \
  "$native_lock" > "$native_upstream_lock"
native_upstream_selection=$native_fixture/upstream-megadrive.selection.toml
cat > "$native_upstream_selection" <<EOF
format = 1
origin = 'upstream'
abi = 'mister'
system = 'megadrive'
repository = 'https://fixture.invalid/megadrive'
revision = '3333333333333333333333333333333333333333'
artifact = 'MegaDrive.rbf'
sha256 = '$native_megadrive_sha'
size = $native_megadrive_size
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
EOF
chmod 0444 "$native_upstream_selection"
native_upstream_target=$native_fixture/upstream-target
cp -R "$native_fixture/target" "$native_upstream_target"
NATIVE_RUNTIME_INPUT_LOCK=$native_upstream_lock \
NATIVE_RUNTIME_IDLE_FILE=$native_cache/idle.rbf \
NATIVE_RUNTIME_MEGADRIVE_FILE=$native_cache/megadrive.rbf \
NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=$native_upstream_selection \
  "$native_post_build" "$native_upstream_target"
grep -Fqx 'megadrive_origin=upstream' \
  "$native_upstream_target/usr/share/mister-runtime/build-inputs"
grep -Fqx 'megadrive_artifact=MegaDrive.rbf' \
  "$native_upstream_target/usr/share/mister-runtime/build-inputs"
if grep -Eq '^megadrive_(recipe|recipe_sha256|toolchain)=' \
  "$native_upstream_target/usr/share/mister-runtime/build-inputs"; then
  echo 'native post-build emitted source-built provenance for upstream selection' >&2
  exit 1
fi

native_wrong_selection=$native_fixture/wrong-selection.toml
sed "s/$native_megadrive_sha/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/" \
  "$native_selection" > "$native_wrong_selection"
chmod 0444 "$native_wrong_selection"
native_preserve_target=$native_fixture/preserve-target
cp -R "$native_target" "$native_preserve_target"
if NATIVE_RUNTIME_INPUT_LOCK=$native_lock \
  NATIVE_RUNTIME_IDLE_FILE=$native_cache/idle.rbf \
  NATIVE_RUNTIME_MEGADRIVE_FILE=$native_cache/megadrive.rbf \
  NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=$native_wrong_selection \
    "$native_post_build" "$native_preserve_target" >/dev/null 2>&1; then
  echo 'native post-build accepted a selection digest mismatch' >&2
  exit 1
fi
cmp "$native_target/usr/share/mister-runtime/build-inputs" \
  "$native_preserve_target/usr/share/mister-runtime/build-inputs"

native_post_build_symlink_failures=0
native_moved_rbf_symlink=$native_fixture/target-moved-rbf-symlink
cp -R "$native_target" "$native_moved_rbf_symlink"
mv "$native_moved_rbf_symlink/usr/share/mister-runtime/cores/megadrive.rbf" \
  "$native_moved_rbf_symlink/usr/share/mister-runtime/cores/MegaDrive_20260603.rbf"
ln -s MegaDrive_20260603.rbf \
  "$native_moved_rbf_symlink/usr/share/mister-runtime/cores/megadrive.rbf"
if NATIVE_RUNTIME_INPUT_LOCK=$native_lock \
  NATIVE_RUNTIME_IDLE_FILE=$native_cache/idle.rbf \
  NATIVE_RUNTIME_MEGADRIVE_FILE=$native_cache/megadrive.rbf \
    "$native_post_build" "$native_moved_rbf_symlink" >/dev/null 2>&1; then
  echo 'native post-build accepted a moved Mega Drive RBF through the fixed-path symlink' >&2
  native_post_build_symlink_failures=$((native_post_build_symlink_failures + 1))
fi

native_extra_rbf_symlink=$native_fixture/target-extra-rbf-symlink
cp -R "$native_target" "$native_extra_rbf_symlink"
ln -s cores/megadrive.rbf \
  "$native_extra_rbf_symlink/usr/share/mister-runtime/extra.rbf"
if NATIVE_RUNTIME_INPUT_LOCK=$native_lock \
  NATIVE_RUNTIME_IDLE_FILE=$native_cache/idle.rbf \
  NATIVE_RUNTIME_MEGADRIVE_FILE=$native_cache/megadrive.rbf \
    "$native_post_build" "$native_extra_rbf_symlink" >/dev/null 2>&1; then
  echo 'native post-build accepted an extra RBF symlink' >&2
  native_post_build_symlink_failures=$((native_post_build_symlink_failures + 1))
fi
[ "$native_post_build_symlink_failures" -eq 0 ] || exit 1

for prohibited_development_rbf in \
  /usr/share/mister-runtime/development.rbf \
  /usr/share/mister-runtime/cores/development.rbf \
  /tmp/fogcast-development/core.rbf; do
  case_name=$(printf '%s' "$prohibited_development_rbf" | tr '/.' '__')
  mutated_target=$native_fixture/target-prohibited-$case_name
  cp -R "$native_target" "$mutated_target"
  mkdir -p "$mutated_target$(dirname "$prohibited_development_rbf")"
  cp "$native_cache/idle.rbf" "$mutated_target$prohibited_development_rbf"
  if NATIVE_RUNTIME_INPUT_LOCK=$native_lock \
    NATIVE_RUNTIME_IDLE_FILE=$native_cache/idle.rbf \
    NATIVE_RUNTIME_MEGADRIVE_FILE=$native_cache/megadrive.rbf \
      "$native_post_build" "$mutated_target" >/dev/null 2>&1; then
    echo "native post-build accepted prohibited development RBF: $prohibited_development_rbf" >&2
    exit 1
  fi
done

cp "$native_target/usr/share/mister-runtime/cores/megadrive.rbf" \
  "$native_target/usr/share/mister-runtime/cores/duplicate.rbf"
if NATIVE_RUNTIME_INPUT_LOCK=$native_lock \
  NATIVE_RUNTIME_IDLE_FILE=$native_cache/idle.rbf \
  NATIVE_RUNTIME_MEGADRIVE_FILE=$native_cache/megadrive.rbf \
    "$native_post_build" "$native_target" >/dev/null 2>&1; then
  echo 'native post-build accepted a duplicate packaged RBF' >&2
  exit 1
fi
