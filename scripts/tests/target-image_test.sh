#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-image.XXXXXX")
trap 'chmod -R u+w "$fixture" 2>/dev/null || true; rm -rf "$fixture"' EXIT INT TERM

for curl_symbol in BR2_PACKAGE_LIBCURL BR2_PACKAGE_LIBCURL_CURL; do
  grep -Fqx "$curl_symbol=y" \
    "$repo/buildroot/configs/fogcast_target_dev_defconfig"
  grep -Fqx "$curl_symbol=y" \
    "$repo/buildroot/configs/fogcast_target_native_dev_defconfig"
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
for variant in prod dev native-dev; do
  case "$variant" in
    native-dev) defconfig=$repo/buildroot/configs/fogcast_target_native_dev_defconfig ;;
    *) defconfig=$repo/buildroot/configs/fogcast_target_${variant}_defconfig ;;
  esac
  grep -Fqx 'BR2_PRIMARY_SITE="https://sources.buildroot.net"' \
    "$defconfig"
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
target_image_verify=$(
  awk '
    /^target-image-verify:/ { in_target=1; next }
    in_target && /^[^[:space:]]/ { exit }
    in_target { print }
  ' "$repo/Makefile"
)
printf '%s\n' "$target_image_verify" | grep -Fq \
  'scripts/verify-target-image.sh prod build/output/target-image/prod/linux.img build/output/target-image/prod/manifest.tsv build/output/target-image/prod/library-report.tsv'
printf '%s\n' "$target_image_verify" | grep -Fq \
  'scripts/verify-target-image.sh dev build/output/target-image/dev/linux.img build/output/target-image/dev/manifest.tsv build/output/target-image/dev/library-report.tsv'
native_target_image_verify=$(
  awk '
    /^target-image-native-verify:/ { in_target=1; next }
    in_target && /^[^[:space:]]/ { exit }
    in_target { print }
  ' "$repo/Makefile"
)
printf '%s\n' "$native_target_image_verify" | grep -Fq \
  'scripts/verify-target-image.sh native-dev build/output/target-image/native-dev/linux.img build/output/target-image/native-dev/manifest.tsv build/output/target-image/native-dev/library-report.tsv build/output/target-image/native-dev/megadrive.selection.toml'
if grep -Fq 'readonly=on' "$repo/scripts/qemu-smoke-target-image.sh"; then
  echo 'QEMU smoke config uses unsupported read-only SD backing' >&2
  exit 1
fi

TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/build-target-image.sh" \
  --validate-inside-path prod \
  /target-image-output/work-1-prod \
  /work/build/output/target-image/work-1-prod/images/rootfs.ext4
TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/build-target-image.sh" \
  --validate-inside-path native-dev \
  /target-image-output/work-2-native-dev \
  /work/build/output/target-image/work-2-native-dev/images/rootfs.ext4
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
if TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/build-target-image.sh" \
  --validate-inside-path staging \
  /target-image-output/work-1-staging \
  /work/build/output/target-image/work-1-staging/images/rootfs.ext4 >/dev/null 2>&1; then
  echo 'inside path validator accepted a fourth variant' >&2
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

fake_selection=$fixture/fake-megadrive.selection.toml
cat > "$fake_selection" <<'EOF'
format = 1
origin = 'upstream'
abi = 'mister'
system = 'megadrive'
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
revision = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
artifact = 'releases/MegaDrive_20260603.rbf'
sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
size = 1
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
EOF
chmod 0444 "$fake_selection"
if [ -n "${TARGET_IMAGE_EXTRA_CORE_CACHE:-}" ]; then
  NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes' \
    sh "$repo/scripts/native-extra-cores.sh" copy-records \
      "$TARGET_IMAGE_EXTRA_CORE_CACHE" "$(dirname "$fake_selection")"
fi

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

: > "$build_log"
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_BUILD_ONCE=$fake_build \
TARGET_IMAGE_BUILD_LOG=$build_log \
NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=$fake_selection \
TARGET_IMAGE_OUTPUT_ROOT=$output_root \
  sh "$repo/scripts/build-target-image.sh" native-dev
test -f "$output_root/native-dev/linux.img"
cmp -s "$fake_selection" "$output_root/native-dev/megadrive.selection.toml"
test "$(stat -c %a "$output_root/native-dev/megadrive.selection.toml")" = 444
cmp -s "$fake_selection" "$output_root/work-1-native-dev/megadrive.selection.toml"
cmp -s "$fake_selection" "$output_root/work-2-native-dev/megadrive.selection.toml"
test "$(wc -l < "$build_log" | tr -d ' ')" -eq 2
grep -Fq "native-dev|$output_root/work-1-native-dev|1751459412" "$build_log"
grep -Fq "native-dev|$output_root/work-2-native-dev|1751459412" "$build_log"
grep -Fq 'run_1_sha256=' "$output_root/native-dev/reproducibility.txt"
test "$(awk -F= '$1 == "run_1_sha256" { print $2 }' "$output_root/native-dev/reproducibility.txt")" = \
  "$(awk -F= '$1 == "run_2_sha256" { print $2 }' "$output_root/native-dev/reproducibility.txt")"

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
  *mister-agent*|*fogcast-kit*) printf '%s\n' 'ELF 32-bit LSB executable, ARM, EABI5 version 1 (SYSV), statically linked, stripped' ;;
  *mister-runtime*) printf '%s\n' 'ELF 32-bit LSB pie executable, ARM, EABI5 version 1 (SYSV), dynamically linked, stripped' ;;
  *) /usr/bin/file "$@" ;;
esac
EOF
cat > "$fake_bin/readelf" <<'EOF'
#!/bin/sh
case "$*" in
  *'-d '*mister-runtime|'-d '*mister-runtime)
    printf '%s\n' \
      ' 0x00000001 (NEEDED)                     Shared library: [libstdc++.so.6]' \
      ' 0x00000001 (NEEDED)                     Shared library: [libgcc_s.so.1]' \
      ' 0x00000001 (NEEDED)                     Shared library: [libc.so.6]'
    ;;
  *) printf '%s\n' '  Class:                             ELF32' '  Machine:                           ARM' ;;
esac
EOF
chmod 0755 "$fake_bin/file" "$fake_bin/readelf"

synthetic_idle=$fixture/synthetic-idle.rbf
printf '%s\n' 'synthetic native idle fixture' > "$synthetic_idle"
synthetic_idle_sha=$(sha256sum "$synthetic_idle" | awk '{print $1}')
synthetic_idle_size=$(wc -c < "$synthetic_idle" | tr -d ' ')
synthetic_megadrive=$fixture/synthetic-megadrive.rbf
printf '%s\n' 'synthetic native Mega Drive fixture' > "$synthetic_megadrive"
synthetic_megadrive_sha=$(sha256sum "$synthetic_megadrive" | awk '{print $1}')
synthetic_megadrive_size=$(wc -c < "$synthetic_megadrive" | tr -d ' ')
synthetic_runtime_commit=1111111111111111111111111111111111111111
synthetic_idle_commit=2222222222222222222222222222222222222222
synthetic_megadrive_commit=3333333333333333333333333333333333333333
native_input_lock=$fixture/native-runtime.inputs.lock.toml
cat > "$native_input_lock" <<EOF
format = 1

[mister_runtime]
commit = '$synthetic_runtime_commit'
mount_path = '/runtime-source'

[idle_rbf]
repository = 'https://fixture.invalid/fogcast/synthetic-idle'
commit = '$synthetic_idle_commit'
path = 'synthetic-idle.rbf'
sha256 = '$synthetic_idle_sha'
size = $synthetic_idle_size
install_path = '/usr/share/mister-runtime/idle.rbf'

[megadrive_rbf]
repository = 'https://fixture.invalid/fogcast/synthetic-megadrive'
commit = '$synthetic_megadrive_commit'
path = 'synthetic-megadrive.rbf'
sha256 = '$synthetic_megadrive_sha'
size = $synthetic_megadrive_size
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
EOF

native_selection=$fixture/megadrive.selection.toml
cat > "$native_selection" <<EOF
format = 1
origin = 'source-built'
abi = 'mister'
system = 'megadrive'
repository = 'https://fixture.invalid/source-built-megadrive'
revision = '4444444444444444444444444444444444444444'
artifact = 'megadrive.rbf'
sha256 = '$synthetic_megadrive_sha'
size = $synthetic_megadrive_size
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
recipe = 'scripts/rebuild_core.py'
recipe_sha256 = '5555555555555555555555555555555555555555555555555555555555555555'
toolchain = 'fixture-toolchain'
EOF
chmod 0444 "$native_selection"

normal_override_log=$fixture/normal-lock-override.log
set +e
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_ROOT_FIXTURE_NATIVE_INPUT_LOCK=$native_input_lock \
TARGET_IMAGE_CONTAINER_RUNTIME=/bin/false \
  sh "$repo/scripts/verify-target-image.sh" native-dev \
    "$fixture/not-an-image" "$fixture/not-a-manifest" "$fixture/not-a-library-report" \
    > "$normal_override_log" 2>&1
normal_override_status=$?
set -e
test "$normal_override_status" -eq 2 || {
  echo 'normal image verification did not explicitly reject the fixture lock override' >&2
  exit 1
}
grep -Fq 'native input lock override is only permitted with --root-fixture' \
  "$normal_override_log"

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
  if [ "$variant" != native-dev ]; then
    : > "$root/usr/sbin/mister-disable-menu-blanking"
    chmod 0755 "$root/usr/sbin/mister-disable-menu-blanking"
  fi
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
  if [ "$variant" = dev ] || [ "$variant" = native-dev ]; then
    ln -s /run/dropbear "$root/etc/dropbear"
    : > "$root/usr/sbin/dropbearmulti"
    chmod 0755 "$root/usr/sbin/dropbearmulti"
    ln -s dropbearmulti "$root/usr/sbin/dropbear"
  fi
  if [ "$variant" = native-dev ]; then
    ln -s busybox "$root/usr/bin/readlink"
    rm "$root/etc/init.d/S40mister-main"
    cat > "$root/etc/init.d/S40mister-runtime" <<'EOF'
#!/bin/sh
supervisor_pid=/run/mister-runtime-supervisor.pid
case "${1:-start}" in
  start)
    /usr/sbin/mister-supervise mister-runtime /usr/sbin/mister-runtime &
    printf '%s\n' "$!" > /run/mister-runtime-supervisor.pid
    ;;
esac
EOF
    cat > "$root/etc/init.d/S50mister-agent" <<'EOF'
#!/bin/sh
supervisor_pid=/run/mister-agent-supervisor.pid
case "${1:-start}" in
  start)
    /usr/sbin/mister-supervise mister-agent /usr/sbin/mister-agent \
      --config /media/fat/fogcast/agent.toml --runtime native &
    printf '%s\n' "$!" > /run/mister-agent-supervisor.pid
    ;;
esac
EOF
    chmod 0755 "$root/etc/init.d/S40mister-runtime" "$root/etc/init.d/S50mister-agent"
    cp "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S60fogcast-kit" "$root/etc/init.d/S60fogcast-kit"
    : > "$root/usr/sbin/fogcast-kit"
    chmod 0755 "$root/usr/sbin/fogcast-kit"
    : > "$root/usr/sbin/mister-runtime"
    chmod 0755 "$root/usr/sbin/mister-runtime"
    mkdir -p "$root/usr/share/mister-runtime"
    cp "$synthetic_idle" \
      "$root/usr/share/mister-runtime/idle.rbf"
    mkdir -p "$root/usr/share/mister-runtime/cores"
    cp "$synthetic_megadrive" \
      "$root/usr/share/mister-runtime/cores/megadrive.rbf"
    synthetic_agent_sha=$(sha256sum "$root/usr/sbin/mister-agent" | awk '{print $1}')
    cat > "$root/usr/share/mister-runtime/build-inputs" <<EOF
format=1
mister_runtime_commit=$synthetic_runtime_commit
mister_agent_sha256=$synthetic_agent_sha
fogcast_kit_sha256=$synthetic_agent_sha
idle_repository=https://fixture.invalid/fogcast/synthetic-idle
idle_commit=$synthetic_idle_commit
idle_path=synthetic-idle.rbf
idle_sha256=$synthetic_idle_sha
idle_size=$synthetic_idle_size
idle_install_path=/usr/share/mister-runtime/idle.rbf
megadrive_origin=source-built
megadrive_abi=mister
megadrive_system=megadrive
megadrive_repository=https://fixture.invalid/source-built-megadrive
megadrive_revision=4444444444444444444444444444444444444444
megadrive_artifact=megadrive.rbf
megadrive_sha256=$synthetic_megadrive_sha
megadrive_size=$synthetic_megadrive_size
megadrive_install_path=/usr/share/mister-runtime/cores/megadrive.rbf
megadrive_recipe=scripts/rebuild_core.py
megadrive_recipe_sha256=5555555555555555555555555555555555555555555555555555555555555555
megadrive_toolchain=fixture-toolchain
EOF
  fi
}

verify_fixture() {
  verify_variant=$1
  verify_root=$2
  verify_manifest=$3
  verify_libraries=$4
  verify_selection=${5:-$native_selection}
  PATH="$fake_bin:$PATH" TARGET_IMAGE_TEST_MODE=1 \
  TARGET_IMAGE_ROOT_FIXTURE_NATIVE_INPUT_LOCK=$native_input_lock \
  TARGET_IMAGE_ROOT_FIXTURE_NATIVE_MEGA_DRIVE_SELECTION=$verify_selection \
    sh "$repo/scripts/verify-target-image.sh" --root-fixture \
      "$verify_variant" "$verify_root" "$verify_manifest" "$verify_libraries"
}

prod_root=$fixture/prod-root
dev_root=$fixture/dev-root
native_root=$fixture/native-root
make_root "$prod_root" prod
make_root "$dev_root" dev
make_root "$native_root" native-dev
if [ -n "${TARGET_IMAGE_EXTRA_CORE_CACHE:-}" ] &&
  { [ "${NATIVE_RUNTIME_SYSTEMS:-megadrive}" = 'megadrive pong snes nes' ] ||
    [ -n "${FES_PONG_PACKAGE_DIR:-}" ]; }; then
  sh "$repo/scripts/native-extra-cores.sh" install \
    "$TARGET_IMAGE_EXTRA_CORE_CACHE" "$native_root"
  sh "$repo/scripts/native-extra-cores.sh" build-inputs \
    "$TARGET_IMAGE_EXTRA_CORE_CACHE" "$native_root" >> \
    "$native_root/usr/share/mister-runtime/build-inputs"
fi
verify_fixture prod "$prod_root" "$fixture/prod.manifest" "$fixture/prod.libraries"
verify_fixture dev "$dev_root" "$fixture/dev.manifest" "$fixture/dev.libraries"
verify_fixture native-dev "$native_root" "$fixture/native.manifest" "$fixture/native.libraries"
# The additional-core helper fixture supplies real externally selected pairs.
if [ -n "${TARGET_IMAGE_EXTRA_CORE_CACHE:-}" ]; then
  extra_root=$fixture/four-system-root
  cp -R "$native_root" "$extra_root"
  NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes' "$repo/scripts/native-extra-cores.sh" install "$TARGET_IMAGE_EXTRA_CORE_CACHE" "$extra_root"
  NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes' "$repo/scripts/native-extra-cores.sh" copy-records "$TARGET_IMAGE_EXTRA_CORE_CACHE" "$(dirname "$native_selection")"
  NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes' verify_fixture native-dev "$extra_root" "$fixture/extra.manifest" "$fixture/extra.libraries"
  if NATIVE_RUNTIME_SYSTEMS=megadrive verify_fixture native-dev "$extra_root" \
    "$fixture/unselected.manifest" "$fixture/unselected.libraries" >/dev/null 2>&1; then
    echo 'image accepted cores outside the caller-selected set' >&2; exit 1
  fi
  rm "$extra_root/usr/share/mister-runtime/cores/nes.rbf"
  if NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes' verify_fixture native-dev "$extra_root" "$fixture/missing.manifest" "$fixture/missing.libraries" >/dev/null 2>&1; then
    echo 'four-system image accepted a missing core' >&2; exit 1
  fi
fi
for prohibited_development_rbf in \
  /usr/share/mister-runtime/development.rbf \
  /usr/share/mister-runtime/cores/development.rbf \
  /tmp/fogcast-development/core.rbf; do
  test ! -e "$native_root$prohibited_development_rbf"
done

native_without_readlink=$fixture/native-without-readlink
cp -R "$native_root" "$native_without_readlink"
rm "$native_without_readlink/usr/bin/readlink"
if verify_fixture native-dev "$native_without_readlink" \
  "$fixture/native-without-readlink.manifest" \
  "$fixture/native-without-readlink.libraries" >/dev/null 2>&1; then
  echo 'native image verifier accepted missing /usr/bin/readlink' >&2
  exit 1
fi
LC_ALL=C sort -c "$fixture/prod.manifest"
LC_ALL=C sort -c "$fixture/prod.libraries"
test "$(wc -l < "$fixture/prod.libraries" | tr -d ' ')" -eq 14
grep -Eq '^/lib/libz\.so\.1[[:space:]]+/lib/libz\.so\.1[[:space:]]+[0-9a-f]{64}$' \
  "$fixture/prod.libraries"
grep -Eq '^/lib/libstdc\+\+\.so\.6[[:space:]]+/lib/libstdc\+\+\.so\.6[[:space:]]+[0-9a-f]{64}$' \
  "$fixture/native.libraries"
grep -Eq "^usr/share/mister-runtime/idle\\.rbf[[:space:]]+file[[:space:]]+$synthetic_idle_sha$" \
  "$fixture/native.manifest"
grep -Eq "^usr/share/mister-runtime/cores/megadrive\\.rbf[[:space:]]+file[[:space:]]+$synthetic_megadrive_sha$" \
  "$fixture/native.manifest"
grep -Eq '^usr/share/mister-runtime/build-inputs[[:space:]]+file[[:space:]]+[0-9a-f]{64}$' \
  "$fixture/native.manifest"

native_wrong_selection=$fixture/native-wrong-selection.toml
sed 's/megadrive/other-system/' "$native_selection" > "$native_wrong_selection"
if verify_fixture native-dev "$native_root" "$fixture/native-wrong-selection.manifest" \
  "$fixture/native-wrong-selection.libraries" "$native_wrong_selection" >/dev/null 2>&1; then
  echo 'native verifier accepted a selection with the wrong system' >&2
  exit 1
fi

native_selection_symlink=$fixture/native-selection-symlink
ln -s "$native_selection" "$native_selection_symlink"
if verify_fixture native-dev "$native_root" "$fixture/native-selection-symlink.manifest" \
  "$fixture/native-selection-symlink.libraries" "$native_selection_symlink" >/dev/null 2>&1; then
  echo 'native verifier accepted a selection symlink' >&2
  exit 1
fi

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

native_missing_needed=$fixture/native-missing-needed
cp -R "$native_root" "$native_missing_needed"
rm "$native_missing_needed/lib/libstdc++.so.6"
if verify_fixture native-dev "$native_missing_needed" "$fixture/native-missing-needed.manifest" "$fixture/native-missing-needed.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a missing runtime NEEDED library' >&2
  exit 1
fi

native_extra_rbf=$fixture/native-extra-rbf
cp -R "$native_root" "$native_extra_rbf"
cp "$native_extra_rbf/usr/share/mister-runtime/idle.rbf" "$native_extra_rbf/extra.rbf"
if verify_fixture native-dev "$native_extra_rbf" "$fixture/native-extra-rbf.manifest" "$fixture/native-extra-rbf.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a duplicate packaged RBF' >&2
  exit 1
fi

native_symlink_failures=0
native_moved_rbf_symlink=$fixture/native-moved-rbf-symlink
cp -R "$native_root" "$native_moved_rbf_symlink"
mv "$native_moved_rbf_symlink/usr/share/mister-runtime/cores/megadrive.rbf" \
  "$native_moved_rbf_symlink/usr/share/mister-runtime/cores/MegaDrive_20260603.rbf"
ln -s MegaDrive_20260603.rbf \
  "$native_moved_rbf_symlink/usr/share/mister-runtime/cores/megadrive.rbf"
if verify_fixture native-dev "$native_moved_rbf_symlink" \
  "$fixture/native-moved-rbf-symlink.manifest" \
  "$fixture/native-moved-rbf-symlink.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a moved Mega Drive RBF through the fixed-path symlink' >&2
  native_symlink_failures=$((native_symlink_failures + 1))
fi

native_extra_rbf_symlink=$fixture/native-extra-rbf-symlink
cp -R "$native_root" "$native_extra_rbf_symlink"
ln -s usr/share/mister-runtime/cores/megadrive.rbf \
  "$native_extra_rbf_symlink/extra.rbf"
if verify_fixture native-dev "$native_extra_rbf_symlink" \
  "$fixture/native-extra-rbf-symlink.manifest" \
  "$fixture/native-extra-rbf-symlink.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted an extra RBF symlink' >&2
  native_symlink_failures=$((native_symlink_failures + 1))
fi
[ "$native_symlink_failures" -eq 0 ] || exit 1

for prohibited_development_rbf in \
  /usr/share/mister-runtime/development.rbf \
  /usr/share/mister-runtime/cores/development.rbf \
  /tmp/fogcast-development/core.rbf; do
  case_name=$(printf '%s' "$prohibited_development_rbf" | tr '/.' '__')
  mutated_root=$fixture/native-prohibited-$case_name
  cp -R "$native_root" "$mutated_root"
  mkdir -p "$mutated_root$(dirname "$prohibited_development_rbf")"
  cp "$mutated_root/usr/share/mister-runtime/idle.rbf" \
    "$mutated_root$prohibited_development_rbf"
  if verify_fixture native-dev "$mutated_root" \
    "$fixture/native-prohibited-$case_name.manifest" \
    "$fixture/native-prohibited-$case_name.libraries" >/dev/null 2>&1; then
    echo "native verifier accepted prohibited development RBF: $prohibited_development_rbf" >&2
    exit 1
  fi
done

native_missing_megadrive=$fixture/native-missing-megadrive
cp -R "$native_root" "$native_missing_megadrive"
rm "$native_missing_megadrive/usr/share/mister-runtime/cores/megadrive.rbf"
if verify_fixture native-dev "$native_missing_megadrive" "$fixture/native-missing-megadrive.manifest" "$fixture/native-missing-megadrive.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a missing Mega Drive RBF' >&2
  exit 1
fi

native_renamed_megadrive=$fixture/native-renamed-megadrive
cp -R "$native_root" "$native_renamed_megadrive"
mv "$native_renamed_megadrive/usr/share/mister-runtime/cores/megadrive.rbf" \
  "$native_renamed_megadrive/usr/share/mister-runtime/cores/MegaDrive_20260603.rbf"
if verify_fixture native-dev "$native_renamed_megadrive" "$fixture/native-renamed-megadrive.manifest" "$fixture/native-renamed-megadrive.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a renamed Mega Drive RBF' >&2
  exit 1
fi

native_wrong_megadrive=$fixture/native-wrong-megadrive
cp -R "$native_root" "$native_wrong_megadrive"
printf '%s\n' altered > "$native_wrong_megadrive/usr/share/mister-runtime/cores/megadrive.rbf"
if verify_fixture native-dev "$native_wrong_megadrive" "$fixture/native-wrong-megadrive.manifest" "$fixture/native-wrong-megadrive.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a mismatched Mega Drive RBF' >&2
  exit 1
fi

native_mgl=$fixture/native-mgl
cp -R "$native_root" "$native_mgl"
printf '%s\n' '<mistergamedescription/>' > "$native_mgl/usr/share/mister-runtime/launch.mgl"
if verify_fixture native-dev "$native_mgl" "$fixture/native-mgl.manifest" "$fixture/native-mgl.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a conventional MGL payload' >&2
  exit 1
fi

native_fifo=$fixture/native-fifo
cp -R "$native_root" "$native_fifo"
sed 's#--runtime native &#--runtime native --command-pipe /dev/MiSTer_cmd \&#' \
  "$native_fifo/etc/init.d/S50mister-agent" > "$native_fifo/etc/init.d/S50mister-agent.new"
mv "$native_fifo/etc/init.d/S50mister-agent.new" "$native_fifo/etc/init.d/S50mister-agent"
chmod 0755 "$native_fifo/etc/init.d/S50mister-agent"
if verify_fixture native-dev "$native_fifo" "$fixture/native-fifo.manifest" "$fixture/native-fifo.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted conventional FIFO startup wiring' >&2
  exit 1
fi

native_main=$fixture/native-main
cp -R "$native_root" "$native_main"
: > "$native_main/etc/init.d/S40mister-main"
if verify_fixture native-dev "$native_main" "$fixture/native-main.manifest" "$fixture/native-main.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted the Main init service' >&2
  exit 1
fi

native_menu_helper=$fixture/native-menu-helper
cp -R "$native_root" "$native_menu_helper"
: > "$native_menu_helper/usr/sbin/mister-disable-menu-blanking"
if verify_fixture native-dev "$native_menu_helper" "$fixture/native-menu-helper.manifest" "$fixture/native-menu-helper.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted the legacy Menu configuration helper' >&2
  exit 1
fi

native_wrong_inputs=$fixture/native-wrong-inputs
cp -R "$native_root" "$native_wrong_inputs"
sed 's#idle_path=synthetic-idle.rbf#idle_path=wrong-idle.rbf#' \
  "$native_wrong_inputs/usr/share/mister-runtime/build-inputs" > \
  "$native_wrong_inputs/usr/share/mister-runtime/build-inputs.new"
mv "$native_wrong_inputs/usr/share/mister-runtime/build-inputs.new" \
  "$native_wrong_inputs/usr/share/mister-runtime/build-inputs"
if verify_fixture native-dev "$native_wrong_inputs" "$fixture/native-wrong-inputs.manifest" "$fixture/native-wrong-inputs.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a build-input record that differs from the lock' >&2
  exit 1
fi

native_wrong_agent_identity=$fixture/native-wrong-agent-identity
cp -R "$native_root" "$native_wrong_agent_identity"
printf '%s\n' altered >> "$native_wrong_agent_identity/usr/sbin/mister-agent"
if verify_fixture native-dev "$native_wrong_agent_identity" "$fixture/native-wrong-agent-identity.manifest" "$fixture/native-wrong-agent-identity.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted an agent binary that differs from its build-input identity' >&2
  exit 1
fi

native_legacy_agent=$fixture/native-legacy-agent
cp -R "$native_root" "$native_legacy_agent"
sed 's/ --runtime native//' "$native_legacy_agent/etc/init.d/S50mister-agent" > \
  "$native_legacy_agent/etc/init.d/S50mister-agent.new"
mv "$native_legacy_agent/etc/init.d/S50mister-agent.new" \
  "$native_legacy_agent/etc/init.d/S50mister-agent"
if verify_fixture native-dev "$native_legacy_agent" "$fixture/native-legacy-agent.manifest" "$fixture/native-legacy-agent.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted an agent without explicit native backend selection' >&2
  exit 1
fi

service_validation_failures=0

native_commented_runtime=$fixture/native-commented-runtime
cp -R "$native_root" "$native_commented_runtime"
awk '
  $0 == "    /usr/sbin/mister-supervise mister-runtime /usr/sbin/mister-runtime &" {
    print "    # /usr/sbin/mister-supervise mister-runtime /usr/sbin/mister-runtime &"
    next
  }
  { print }
' "$native_commented_runtime/etc/init.d/S40mister-runtime" > \
  "$native_commented_runtime/etc/init.d/S40mister-runtime.new"
mv "$native_commented_runtime/etc/init.d/S40mister-runtime.new" \
  "$native_commented_runtime/etc/init.d/S40mister-runtime"
chmod 0755 "$native_commented_runtime/etc/init.d/S40mister-runtime"
if verify_fixture native-dev "$native_commented_runtime" "$fixture/native-commented-runtime.manifest" "$fixture/native-commented-runtime.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a runtime launch present only in a comment' >&2
  service_validation_failures=$((service_validation_failures + 1))
fi

native_wrong_agent_launch=$fixture/native-wrong-agent-launch
cp -R "$native_root" "$native_wrong_agent_launch"
cat > "$native_wrong_agent_launch/etc/init.d/S50mister-agent" <<'EOF'
#!/bin/sh
supervisor_pid=/run/mister-agent-supervisor.pid
case "${1:-start}" in
  start)
    # /usr/sbin/mister-supervise mister-agent /usr/sbin/mister-agent \
    #   --config /media/fat/fogcast/agent.toml --runtime native &
    /usr/sbin/mister-supervise mister-agent /usr/sbin/mister-agent \
      --config /media/fat/fogcast/wrong.toml --runtime legacy &
    printf '%s\n' "$!" > /run/mister-agent-supervisor.pid
    ;;
esac
EOF
chmod 0755 "$native_wrong_agent_launch/etc/init.d/S50mister-agent"
if verify_fixture native-dev "$native_wrong_agent_launch" "$fixture/native-wrong-agent-launch.manifest" "$fixture/native-wrong-agent-launch.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted wrong active agent launch arguments' >&2
  service_validation_failures=$((service_validation_failures + 1))
fi

native_extra_launch=$fixture/native-extra-launch
cp -R "$native_root" "$native_extra_launch"
awk '
  { print }
  $0 == "    printf '\''%s\\n'\'' \"$!\" > /run/mister-runtime-supervisor.pid" {
    print "    /usr/sbin/mister-runtime"
  }
' "$native_extra_launch/etc/init.d/S40mister-runtime" > \
  "$native_extra_launch/etc/init.d/S40mister-runtime.new"
mv "$native_extra_launch/etc/init.d/S40mister-runtime.new" \
  "$native_extra_launch/etc/init.d/S40mister-runtime"
chmod 0755 "$native_extra_launch/etc/init.d/S40mister-runtime"
if verify_fixture native-dev "$native_extra_launch" "$fixture/native-extra-launch.manifest" "$fixture/native-extra-launch.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted an extra direct runtime launch command' >&2
  service_validation_failures=$((service_validation_failures + 1))
fi

assert_reject_extra_service_basename() {
  fixture_name=$1
  service_path=$2
  pid_write=$3
  extra_command=$4
  description=$5
  mutated_root=$fixture/$fixture_name
  cp -R "$native_root" "$mutated_root"
  PID_WRITE="$pid_write" EXTRA_COMMAND="$extra_command" awk '
    { print }
    $0 == ENVIRON["PID_WRITE"] {
      print "    " ENVIRON["EXTRA_COMMAND"]
    }
  ' "$mutated_root/$service_path" > "$mutated_root/$service_path.new"
  mv "$mutated_root/$service_path.new" "$mutated_root/$service_path"
  chmod 0755 "$mutated_root/$service_path"
  if verify_fixture native-dev "$mutated_root" \
    "$fixture/$fixture_name.manifest" "$fixture/$fixture_name.libraries" \
    >/dev/null 2>&1; then
    printf 'native verifier accepted %s\n' "$description" >&2
    service_validation_failures=$((service_validation_failures + 1))
  fi
}

assert_reject_extra_service_basename \
  native-extra-runtime-bare etc/init.d/S40mister-runtime \
  '    printf '\''%s\n'\'' "$!" > /run/mister-runtime-supervisor.pid' \
  mister-runtime 'a bare runtime basename launch'
assert_reject_extra_service_basename \
  native-extra-runtime-exec etc/init.d/S40mister-runtime \
  '    printf '\''%s\n'\'' "$!" > /run/mister-runtime-supervisor.pid' \
  'exec mister-runtime' 'an exec runtime basename launch'
assert_reject_extra_service_basename \
  native-extra-runtime-command etc/init.d/S40mister-runtime \
  '    printf '\''%s\n'\'' "$!" > /run/mister-runtime-supervisor.pid' \
  'command mister-runtime' 'a command runtime basename launch'
assert_reject_extra_service_basename \
  native-extra-agent-bare etc/init.d/S50mister-agent \
  '    printf '\''%s\n'\'' "$!" > /run/mister-agent-supervisor.pid' \
  mister-agent 'a bare agent basename launch'
assert_reject_extra_service_basename \
  native-extra-agent-exec etc/init.d/S50mister-agent \
  '    printf '\''%s\n'\'' "$!" > /run/mister-agent-supervisor.pid' \
  'exec mister-agent' 'an exec agent basename launch'
assert_reject_extra_service_basename \
  native-extra-agent-command etc/init.d/S50mister-agent \
  '    printf '\''%s\n'\'' "$!" > /run/mister-agent-supervisor.pid' \
  'command mister-agent' 'a command agent basename launch'
assert_reject_extra_service_basename \
  native-extra-runtime-double-quoted etc/init.d/S40mister-runtime \
  '    printf '\''%s\n'\'' "$!" > /run/mister-runtime-supervisor.pid' \
  'env FOGCAST_SERVICE=runtime "mister-runtime"' \
  'an env-prefixed double-quoted runtime basename launch'
assert_reject_extra_service_basename \
  native-extra-runtime-single-quoted etc/init.d/S40mister-runtime \
  '    printf '\''%s\n'\'' "$!" > /run/mister-runtime-supervisor.pid' \
  "command 'mister-runtime'" \
  'a command-prefixed single-quoted runtime basename launch'
assert_reject_extra_service_basename \
  native-extra-runtime-backslash-escaped etc/init.d/S40mister-runtime \
  '    printf '\''%s\n'\'' "$!" > /run/mister-runtime-supervisor.pid' \
  'exec mister\-runtime' \
  'an exec-prefixed backslash-escaped runtime basename launch'
assert_reject_extra_service_basename \
  native-extra-agent-double-quoted etc/init.d/S50mister-agent \
  '    printf '\''%s\n'\'' "$!" > /run/mister-agent-supervisor.pid' \
  'env FOGCAST_SERVICE=agent "mister-agent"' \
  'an env-prefixed double-quoted agent basename launch'
assert_reject_extra_service_basename \
  native-extra-agent-single-quoted etc/init.d/S50mister-agent \
  '    printf '\''%s\n'\'' "$!" > /run/mister-agent-supervisor.pid' \
  "command 'mister-agent'" \
  'a command-prefixed single-quoted agent basename launch'
assert_reject_extra_service_basename \
  native-extra-agent-backslash-escaped etc/init.d/S50mister-agent \
  '    printf '\''%s\n'\'' "$!" > /run/mister-agent-supervisor.pid' \
  'exec mister\-agent' \
  'an exec-prefixed backslash-escaped agent basename launch'

native_missing_pid_write=$fixture/native-missing-pid-write
cp -R "$native_root" "$native_missing_pid_write"
awk '
  $0 == "    printf '\''%s\\n'\'' \"$!\" > /run/mister-runtime-supervisor.pid" { next }
  { print }
' "$native_missing_pid_write/etc/init.d/S40mister-runtime" > \
  "$native_missing_pid_write/etc/init.d/S40mister-runtime.new"
mv "$native_missing_pid_write/etc/init.d/S40mister-runtime.new" \
  "$native_missing_pid_write/etc/init.d/S40mister-runtime"
chmod 0755 "$native_missing_pid_write/etc/init.d/S40mister-runtime"
if verify_fixture native-dev "$native_missing_pid_write" "$fixture/native-missing-pid-write.manifest" "$fixture/native-missing-pid-write.libraries" >/dev/null 2>&1; then
  echo 'native verifier accepted a missing supervisor PID write' >&2
  service_validation_failures=$((service_validation_failures + 1))
fi

test "$service_validation_failures" -eq 0 || exit 1

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

legacy_rbf_root=$fixture/legacy-rbf-root
cp -R "$prod_root" "$legacy_rbf_root"
: > "$legacy_rbf_root/menu.rbf"
if verify_fixture prod "$legacy_rbf_root" "$fixture/legacy-rbf.manifest" "$fixture/legacy-rbf.libraries" >/dev/null 2>&1; then
  echo 'legacy verifier accepted a bundled RBF' >&2
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

native_smoke_log=$fixture/qemu-native-smoke.log
cat > "$native_smoke_log" <<'EOF'
TARGET_IMAGE_SMOKE_ROOT options=relatime,ro,data=ordered
TARGET_IMAGE_SMOKE_VOLATILE /run /tmp /var/log writable tmpfs
TARGET_IMAGE_SMOKE_READY
EOF
TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/qemu-smoke-target-image.sh" \
  --verify-log native-dev "$native_smoke_log"
{
  printf '%s\n' 'mister-main: waiting for /media/fat payloads'
  cat "$native_smoke_log"
} > "$native_smoke_log.main-wait"
if TARGET_IMAGE_TEST_MODE=1 \
  sh "$repo/scripts/qemu-smoke-target-image.sh" --verify-log native-dev "$native_smoke_log.main-wait" >/dev/null 2>&1; then
  echo 'native QEMU smoke accepted a Main payload wait' >&2
  exit 1
fi

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
