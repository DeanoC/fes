#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-image.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

for curl_symbol in BR2_PACKAGE_LIBCURL BR2_PACKAGE_LIBCURL_CURL; do
  grep -Fqx "$curl_symbol=y" \
    "$repo/buildroot/configs/fogcast_target_dev_defconfig"
  if grep -Fqx "$curl_symbol=y" \
    "$repo/buildroot/configs/fogcast_target_prod_defconfig"; then
    echo "production image unexpectedly includes $curl_symbol" >&2
    exit 1
  fi
done
if grep -Fqx 'BR2_PACKAGE_CURL=y' \
  "$repo/buildroot/configs/fogcast_target_dev_defconfig"; then
  echo 'development image uses legacy BR2_PACKAGE_CURL symbol' >&2
  exit 1
fi
for variant in prod dev; do
  grep -Fqx 'BR2_PRIMARY_SITE="https://sources.buildroot.net"' \
    "$repo/buildroot/configs/fogcast_target_${variant}_defconfig"
done
for unrelated_dev_package in BR2_PACKAGE_FFMPEG BR2_PACKAGE_SDL2; do
  if grep -Fqx "$unrelated_dev_package=y" \
    "$repo/buildroot/configs/fogcast_target_dev_defconfig"; then
    echo "development target image unexpectedly includes $unrelated_dev_package" >&2
    exit 1
  fi
done

grep -Fq 'export E2FSPROGS_FAKE_TIME=$inside_epoch' \
  "$repo/scripts/build-target-image.sh"
grep -Fq '/bin/rm -rf "$inside_output"' \
  "$repo/scripts/build-target-image.sh"
grep -Fq 'image=$(readlink -f "$image")' \
  "$repo/scripts/verify-target-image.sh"
grep -Fq -- '-d GCC_PLUGINS' \
  "$repo/scripts/qemu-smoke-target-image.sh"
grep -Fq -- '-e BLK_DEV_LOOP' \
  "$repo/scripts/qemu-smoke-target-image.sh"
grep -Fq 'fetch --depth=1 "$kernel_bare" "$source_head"' \
  "$repo/scripts/qemu-smoke-target-image.sh"
grep -Fq 'toolchain=/target-image-output/work-2-prod/host/bin/arm-buildroot-linux-gnueabihf-' \
  "$repo/scripts/qemu-smoke-target-image.sh"
grep -Fq 'toolchain_root=/target-image-output/work-2-prod/host' \
  "$repo/scripts/qemu-smoke-target-image.sh"
if grep -Fq 'readonly=on' "$repo/scripts/qemu-smoke-target-image.sh"; then
  echo 'QEMU smoke config uses unsupported read-only SD backing' >&2
  exit 1
fi

TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/build-target-image.sh" \
  --validate-inside-path prod \
  /target-image-output/work-1-prod \
  /work/build/output/target-image/work-1-prod/images/rootfs.ext4
for rejected_path in \
  /target-image-output/../work \
  /target-image-output/work-1-dev \
  /target-image-output/work-3-prod; do
  if TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/build-target-image.sh" \
    --validate-inside-path prod "$rejected_path" \
    /work/build/output/target-image/work-1-prod/images/rootfs.ext4 >/dev/null 2>&1; then
    echo "inside path validator accepted $rejected_path" >&2
    exit 1
  fi
done
if TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/build-target-image.sh" \
  --validate-inside-path prod \
  /target-image-output/work-1-prod \
  /work/build/output/target-image/work-2-prod/images/rootfs.ext4 >/dev/null 2>&1; then
  echo 'inside path validator accepted mismatched run destinations' >&2
  exit 1
fi

grep -Fq 'test mode is required for --promote-existing' \
  "$repo/scripts/build-target-image.sh"
grep -Fq 'test mode is required for TARGET_IMAGE_BUILD_ONCE' \
  "$repo/scripts/build-target-image.sh"

fake_build=$fixture/fake-build
cat > "$fake_build" <<'EOF'
#!/bin/sh
set -eu
variant=$1
output=$2
epoch=$3
printf '%s|%s|%s\n' "$variant" "$output" "$epoch" >> "$TARGET_IMAGE_BUILD_LOG"
mkdir -p "$output/images"
payload="image-$variant"
case "$output:${TARGET_IMAGE_FAKE_DIFFER:-0}" in
  *work-2*:1) payload="$payload-different" ;;
esac
printf '%s\n' "$payload" > "$output/images/rootfs.ext4"
EOF
chmod 0755 "$fake_build"

build_log=$fixture/build.log
output_root=$fixture/output
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_BUILD_ONCE=$fake_build \
TARGET_IMAGE_BUILD_LOG=$build_log \
TARGET_IMAGE_OUTPUT_ROOT=$output_root \
  sh "$repo/scripts/build-target-image.sh" prod

test -f "$output_root/prod/linux.img"
test "$(wc -l < "$build_log" | tr -d ' ')" -eq 2
grep -Fq "prod|$output_root/work-1-prod|1751459412" "$build_log"
grep -Fq "prod|$output_root/work-2-prod|1751459412" "$build_log"

build_count=$(wc -l < "$build_log" | tr -d ' ')
printf '%s\n' prior > "$output_root/prod/linux.img"
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_OUTPUT_ROOT=$output_root \
  sh "$repo/scripts/build-target-image.sh" --promote-existing prod
test "$(cat "$output_root/prod/linux.img")" = image-prod
test "$(wc -l < "$build_log" | tr -d ' ')" -eq "$build_count"

printf '%s\n' prior > "$output_root/prod/linux.img"
if TARGET_IMAGE_TEST_MODE=1 \
  TARGET_IMAGE_BUILD_ONCE=$fake_build \
  TARGET_IMAGE_BUILD_LOG=$build_log \
  TARGET_IMAGE_FAKE_DIFFER=1 \
  TARGET_IMAGE_OUTPUT_ROOT=$output_root \
    sh "$repo/scripts/build-target-image.sh" prod >/dev/null 2>&1; then
  echo 'image build accepted non-reproducible output' >&2
  exit 1
fi
test "$(cat "$output_root/prod/linux.img")" = prior

fake_bin=$fixture/fake-bin
mkdir -p "$fake_bin"
cat > "$fake_bin/file" <<'EOF'
#!/bin/sh
case "$*" in
  *mister-agent*) printf '%s\n' 'ELF 32-bit LSB executable, ARM, EABI5 version 1 (SYSV), statically linked, stripped' ;;
  *) /usr/bin/file "$@" ;;
esac
EOF
cat > "$fake_bin/readelf" <<'EOF'
#!/bin/sh
printf '%s\n' '  Class:                             ELF32' '  Machine:                           ARM'
EOF
chmod 0755 "$fake_bin/file" "$fake_bin/readelf"

make_root() {
  root=$1
  variant=$2
  mkdir -p "$root/sbin" "$root/usr/bin" "$root/usr/sbin" "$root/etc/init.d" "$root/lib" "$root/run" "$root/tmp" "$root/var/log"
  : > "$root/sbin/init"
  : > "$root/usr/bin/busybox"
  : > "$root/usr/sbin/mister-agent"
  chmod 0755 "$root/sbin/init" "$root/usr/bin/busybox" "$root/usr/sbin/mister-agent"
  for service in S20mister-network S40mister-main S50mister-agent S49fogcast-target-smoke; do
    : > "$root/etc/init.d/$service"
    chmod 0755 "$root/etc/init.d/$service"
  done
  cat > "$root/etc/fstab" <<'EOF'
/dev/root / ext4 ro,noatime,noauto 0 1
tmpfs /run tmpfs nosuid,nodev,mode=0755 0 0
tmpfs /tmp tmpfs nosuid,nodev,mode=1777 0 0
tmpfs /var/log tmpfs nosuid,nodev,noexec,mode=0755 0 0
EOF
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
    mkdir -p "$root$(dirname "$library")"
    : > "$root$library"
  done
  if [ "$variant" = dev ]; then
    ln -s /run/dropbear "$root/etc/dropbear"
    : > "$root/usr/sbin/dropbearmulti"
    chmod 0755 "$root/usr/sbin/dropbearmulti"
    ln -s dropbearmulti "$root/usr/sbin/dropbear"
  fi
}

verify_fixture() {
  verify_variant=$1
  verify_root=$2
  verify_manifest=$3
  verify_libraries=$4
  PATH="$fake_bin:$PATH" TARGET_IMAGE_TEST_MODE=1 \
    sh "$repo/scripts/verify-target-image.sh" --root-fixture \
      "$verify_variant" "$verify_root" "$verify_manifest" "$verify_libraries"
}

prod_root=$fixture/prod-root
dev_root=$fixture/dev-root
make_root "$prod_root" prod
make_root "$dev_root" dev
verify_fixture prod "$prod_root" "$fixture/prod.manifest" "$fixture/prod.libraries"
verify_fixture dev "$dev_root" "$fixture/dev.manifest" "$fixture/dev.libraries"
LC_ALL=C sort -c "$fixture/prod.manifest"
LC_ALL=C sort -c "$fixture/prod.libraries"
test "$(wc -l < "$fixture/prod.libraries" | tr -d ' ')" -eq 14
grep -Eq '^/lib/libz\.so\.1[[:space:]]+/lib/libz\.so\.1[[:space:]]+[0-9a-f]{64}$' \
  "$fixture/prod.libraries"

unreadable_root=$fixture/unreadable-root
cp -R "$prod_root" "$unreadable_root"
printf '%s\n' harmless > "$unreadable_root/etc/unreadable"
chmod 000 "$unreadable_root/etc/unreadable"
if verify_fixture prod "$unreadable_root" "$fixture/unreadable.manifest" "$fixture/unreadable.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted an unreadable regular file' >&2
  exit 1
fi
chmod 0600 "$unreadable_root/etc/unreadable"

kernel_cache=$fixture/kernel-cache
mkdir -p "$kernel_cache/arch/arm/boot/dts"
printf '%s\n' kernel > "$kernel_cache/arch/arm/boot/zImage"
printf '%s\n' dtb > "$kernel_cache/arch/arm/boot/dts/vexpress-v2p-ca9.dtb"
kernel_sha=$(shasum -a 256 "$kernel_cache/arch/arm/boot/zImage" | awk '{print $1}')
dtb_sha=$(shasum -a 256 "$kernel_cache/arch/arm/boot/dts/vexpress-v2p-ca9.dtb" | awk '{print $1}')
cat > "$kernel_cache/provenance.txt" <<EOF
format=1
base_key=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
zimage_sha256=$kernel_sha
dtb_sha256=$dtb_sha
EOF
TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/qemu-smoke-target-image.sh" \
  --verify-kernel-cache \
  aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  "$kernel_cache"
printf '%s\n' altered >> "$kernel_cache/arch/arm/boot/zImage"
if TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/qemu-smoke-target-image.sh" \
  --verify-kernel-cache \
  aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  "$kernel_cache" >/dev/null 2>&1; then
  echo 'QEMU cache verifier accepted an altered kernel' >&2
  exit 1
fi

missing_root=$fixture/missing-root
cp -R "$prod_root" "$missing_root"
rm "$missing_root/lib/libz.so.1"
if verify_fixture prod "$missing_root" "$fixture/missing.manifest" "$fixture/missing.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted a missing closure library' >&2
  exit 1
fi

prod_dropbear=$fixture/prod-dropbear
cp -R "$prod_root" "$prod_dropbear"
: > "$prod_dropbear/usr/sbin/dropbear"
chmod 0755 "$prod_dropbear/usr/sbin/dropbear"
if verify_fixture prod "$prod_dropbear" "$fixture/prod-dropbear.manifest" "$fixture/prod-dropbear.libraries" >/dev/null 2>&1; then
  echo 'production verifier accepted Dropbear' >&2
  exit 1
fi

dev_no_dropbear=$fixture/dev-no-dropbear
cp -R "$prod_root" "$dev_no_dropbear"
if verify_fixture dev "$dev_no_dropbear" "$fixture/dev-no-dropbear.manifest" "$fixture/dev-no-dropbear.libraries" >/dev/null 2>&1; then
  echo 'development verifier accepted a missing Dropbear server' >&2
  exit 1
fi

prod_sshd=$fixture/prod-sshd
cp -R "$prod_root" "$prod_sshd"
: > "$prod_sshd/usr/sbin/sshd"
chmod 0755 "$prod_sshd/usr/sbin/sshd"
if verify_fixture prod "$prod_sshd" "$fixture/prod-sshd.manifest" "$fixture/prod-sshd.libraries" >/dev/null 2>&1; then
  echo 'production verifier accepted an alternate SSH server' >&2
  exit 1
fi

rw_root=$fixture/rw-root
cp -R "$prod_root" "$rw_root"
sed 's/ro,noatime/rw,noatime/' "$rw_root/etc/fstab" > "$rw_root/etc/fstab.new"
mv "$rw_root/etc/fstab.new" "$rw_root/etc/fstab"
if verify_fixture prod "$rw_root" "$fixture/rw.manifest" "$fixture/rw.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted a writable root policy' >&2
  exit 1
fi

symlink_mount_root=$fixture/symlink-mount-root
cp -R "$prod_root" "$symlink_mount_root"
rm -rf "$symlink_mount_root/var/log"
ln -s ../tmp "$symlink_mount_root/var/log"
if verify_fixture prod "$symlink_mount_root" "$fixture/symlink-mount.manifest" "$fixture/symlink-mount.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted a symlinked volatile mount point' >&2
  exit 1
fi

token_root=$fixture/token-root
cp -R "$prod_root" "$token_root"
printf '%s\n' 'token = "must-not-ship"' > "$token_root/etc/secret.conf"
if verify_fixture prod "$token_root" "$fixture/token.manifest" "$fixture/token.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted a token assignment' >&2
  exit 1
fi

large_token_root=$fixture/large-token-root
cp -R "$prod_root" "$large_token_root"
dd if=/dev/zero of="$large_token_root/etc/large-secret.bin" bs=1048576 count=3 2>/dev/null
printf '%s\n' 'TOKEN = must-not-ship' >> "$large_token_root/etc/large-secret.bin"
if verify_fixture prod "$large_token_root" "$fixture/large-token.manifest" "$fixture/large-token.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted a large uppercase token assignment' >&2
  exit 1
fi

rom_root=$fixture/rom-root
cp -R "$prod_root" "$rom_root"
: > "$rom_root/game.sfc"
if verify_fixture prod "$rom_root" "$fixture/rom.manifest" "$fixture/rom.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted a ROM payload' >&2
  exit 1
fi

bin_rom_root=$fixture/bin-rom-root
cp -R "$prod_root" "$bin_rom_root"
: > "$bin_rom_root/game.bin"
if verify_fixture prod "$bin_rom_root" "$fixture/bin-rom.manifest" "$fixture/bin-rom.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted a .bin game payload' >&2
  exit 1
fi

gdb_root=$fixture/gdb-root
cp -R "$prod_root" "$gdb_root"
mkdir -p "$gdb_root/usr/lib"
: > "$gdb_root/usr/lib/libstdc++.so.6.0.28-gdb.py"
if verify_fixture prod "$gdb_root" "$fixture/gdb.manifest" "$fixture/gdb.libraries" >/dev/null 2>&1; then
  echo 'image verifier accepted a path-bearing GDB auto-load helper' >&2
  exit 1
fi

smoke_log=$fixture/qemu-smoke.log
cat > "$smoke_log" <<'EOF'
mister-main: waiting for /media/fat payloads
TARGET_IMAGE_SMOKE_ROOT options=relatime,ro,data=ordered
TARGET_IMAGE_SMOKE_VOLATILE /run /tmp /var/log writable tmpfs
TARGET_IMAGE_SMOKE_READY
EOF
TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/qemu-smoke-target-image.sh" --verify-log prod "$smoke_log"

sed 's/relatime,ro,data/relatime,rw,road/' "$smoke_log" > "$smoke_log.bad"
if TARGET_IMAGE_TEST_MODE=1 \
  sh "$repo/scripts/qemu-smoke-target-image.sh" --verify-log prod "$smoke_log.bad" >/dev/null 2>&1; then
  echo 'QEMU smoke verifier accepted a writable root option list' >&2
  exit 1
fi

grep -v 'mister-main: waiting for /media/fat payloads' "$smoke_log" > "$smoke_log.no-wait"
if TARGET_IMAGE_TEST_MODE=1 \
  sh "$repo/scripts/qemu-smoke-target-image.sh" --verify-log prod "$smoke_log.no-wait" >/dev/null 2>&1; then
  echo 'QEMU smoke verifier accepted a missing bounded-wait state' >&2
  exit 1
fi
grep -Fq 'provenance=$kernel_output/provenance.txt' \
  "$repo/scripts/qemu-smoke-target-image.sh"
grep -Fq 'kernel_cache_valid "$expected_key" "$kernel_output"' \
  "$repo/scripts/qemu-smoke-target-image.sh"
grep -Fq '/work/scripts/verify-target-image-source-cache.sh' \
  "$repo/scripts/qemu-smoke-target-image.sh"
