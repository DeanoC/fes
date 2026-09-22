#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
native_rootfs=$repo/buildroot/board/fogcast-target/native-rootfs-overlay
native_post_build=$repo/buildroot/board/fogcast-target/native-post-build.sh
native_extra=$repo/scripts/native-extra-cores.sh
native_runtime=$native_rootfs/etc/init.d/S40mister-runtime
native_agent=$native_rootfs/etc/init.d/S50mister-agent
native_kit=$native_rootfs/etc/init.d/S60fogcast-kit
native_config=$repo/buildroot/configs/fogcast_target_native_dev_defconfig

for required_file in "$native_runtime" "$native_agent" "$native_kit" "$native_post_build" "$native_extra"; do
  test -f "$required_file"
done
sh "$repo/scripts/validate-native-init-services.sh" "$native_runtime" "$native_agent"
grep -Fqx 'BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y' "$native_config"
grep -Fq 'BR2_ROOTFS_OVERLAY=' "$native_config"
grep -Fq 'native-rootfs-overlay' "$native_config"
grep -Fq 'BR2_ROOTFS_POST_BUILD_SCRIPT=' "$native_config"
grep -Fq 'native-post-build.sh' "$native_config"
grep -Fq 'package-only' "$native_post_build"
! grep -Fq 'LIBMISTER_RUNTIME_DIR' "$native_post_build"
! grep -Fq 'mister-main' "$native_post_build"
! grep -Fq 'format1' "$native_post_build"

fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-rootfs.XXXXXX")
trap 'chmod -R u+rwX "$fixture" 2>/dev/null || true; rm -rf "$fixture"' EXIT INT TERM

target=$fixture/target
mkdir -p "$target/usr/sbin" "$target/etc/init.d"
cp -R "$native_rootfs/." "$target/"
printf '%s\n' runtime >"$target/usr/sbin/mister-runtime"
printf '%s\n' agent >"$target/usr/sbin/mister-agent"
chmod 0755 "$target/usr/sbin/mister-runtime" "$target/usr/sbin/mister-agent"
mkdir -p "$target/usr/libexec/bluetooth" "$target/usr/bin"
printf '%s\n' bluetooth-init >"$target/etc/init.d/S40bluetooth"
printf '%s\n' dbus-init >"$target/etc/init.d/S30dbus"
printf '%s\n' bluetoothd >"$target/usr/libexec/bluetooth/bluetoothd"
printf '%s\n' dbus-daemon >"$target/usr/bin/dbus-daemon"

fogcast=$fixture/fogcast
mkdir -p "$fogcast/bin"
printf '%s\n' agent >"$fogcast/bin/mister-agent-linux-armv7"
chmod 0755 "$fogcast/bin/mister-agent-linux-armv7"
printf '%s\n' kit >"$fogcast/bin/fogcast-kit-linux-armv7"
chmod 0755 "$fogcast/bin/fogcast-kit-linux-armv7"
export FOGCAST_DIR=$fogcast

for library in \
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
  /lib/libz.so.1; do
  mkdir -p "$target$(dirname "$library")"
  printf '%s\n' library >"$target$library"
done
mkdir -p "$target/usr/lib"

"$repo/buildroot/board/fogcast-target/prepare-native-rootfs.sh" "$target"

idle=$fixture/idle.rbf
printf '%s\n' idle >"$idle"
idle_sha=$(sha256sum "$idle" | awk '{print $1}')
idle_size=$(wc -c <"$idle" | tr -d ' ')
lock=$fixture/native-runtime.inputs.lock.toml
cat >"$lock" <<EOF
format = 1

[mister_runtime]
commit = '1111111111111111111111111111111111111111'
mount_path = '/runtime-source'

[splash_rbf]
repository = 'https://fixture.invalid/idle'
commit = '2222222222222222222222222222222222222222'
path = 'menu.rbf'
sha256 = '$idle_sha'
size = $idle_size
fat_destination = '/menu.rbf'

[idle_rbf]
repository = 'https://fixture.invalid/idle'
commit = '2222222222222222222222222222222222222222'
path = 'menu.rbf'
sha256 = '$idle_sha'
size = $idle_size
install_path = '/usr/share/mister-runtime/idle.rbf'
EOF

selector=$fixture/target-image-lock
cat >"$selector" <<'SELECTOR'
#!/bin/sh
set -eu
command=$1
shift
package=
selection=
cache=
output=
print_inputs=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --package|--selection|--cache|--output)
      key=$1
      value=$2
      shift 2
      case "$key" in
        --package) package=$value ;;
        --selection) selection=$value ;;
        --cache) cache=$value ;;
        --output) output=$value ;;
      esac
      ;;
    --print-inputs) print_inputs=1; shift ;;
    *) shift ;;
  esac
done
read_value() {
  awk -F"'" -v wanted="$1" \
    '$1 ~ "^[[:space:]]*" wanted "[[:space:]]*=" { print $2; exit }' "$selection"
}
case "$command" in
  select-package)
    package_id=$(read_value package_id)
    core_id=$(read_value core_id)
    test -d "$package"
    test -n "$core_id"
    mkdir -p "$cache/core-packages/$package_id"
    cp "$package/manifest.toml" "$cache/core-packages/$package_id/manifest.toml"
    cp "$package/core.rbf" "$cache/core-packages/$package_id/core.rbf"
    chmod 0444 "$cache/core-packages/$package_id/manifest.toml" \
      "$cache/core-packages/$package_id/core.rbf"
    chmod 0555 "$cache/core-packages/$package_id"
    cp "$selection" "$output"
    chmod 0444 "$output"
    ;;
  verify-package)
    package_id=$(read_value package_id)
    core_id=$(read_value core_id)
    test -d "$package"
    test "$(basename "$package")" = "$package_id"
    test -f "$selection"
    grep -Fqx "core_id = '$core_id'" "$selection"
    if [ "$print_inputs" -eq 1 ]; then
      printf '%s_package_id=%s\n' "$core_id" "$package_id"
    fi
    ;;
  *) exit 2 ;;
esac
SELECTOR
chmod 0755 "$selector"

package_fixture() {
  package_core=$1
  package_letter=$2
  package_id=$(printf '%064d' 0 | tr '0' "$package_letter")
  package_dir=$fixture/$package_core-package
  package_selection=$fixture/$package_core.package-selection.toml
  mkdir "$package_dir"
  printf "core_id = 'fes.%s'\npackage_id = '%s'\n" \
    "$package_core" "$package_id" >"$package_dir/manifest.toml"
  printf '%s package payload\n' "$package_core" >"$package_dir/core.rbf"
  chmod 0444 "$package_dir/manifest.toml" "$package_dir/core.rbf"
  chmod 0555 "$package_dir"
  cat >"$package_selection" <<EOF
format = 2
kind = 'core-package'
core_id = 'fes.$package_core'
package_id = '$package_id'
EOF
  chmod 0444 "$package_selection"
  case "$package_core" in
    pong)
      FES_PONG_PACKAGE_DIR=$package_dir
      FES_PONG_PACKAGE_SELECTION=$package_selection
      ;;
    zx81)
      FES_ZX81_PACKAGE_DIR=$package_dir
      FES_ZX81_PACKAGE_SELECTION=$package_selection
      ;;
    coleco)
      FES_COLECO_PACKAGE_DIR=$package_dir
      FES_COLECO_PACKAGE_SELECTION=$package_selection
      ;;
  esac
}

package_fixture pong a
package_fixture zx81 b
package_fixture coleco c
export FES_PACKAGE_IDS=fes.pong,fes.zx81,fes.coleco
export FES_PONG_PACKAGE_DIR FES_PONG_PACKAGE_SELECTION
export FES_ZX81_PACKAGE_DIR FES_ZX81_PACKAGE_SELECTION
export FES_COLECO_PACKAGE_DIR FES_COLECO_PACKAGE_SELECTION

cache=$fixture/cache
mkdir "$cache"
cp "$idle" "$cache/idle.rbf"
chmod 0444 "$cache/idle.rbf"
NATIVE_RUNTIME_MODE=package-only \
  TARGET_IMAGE_LOCK_BIN="$selector" \
  "$native_extra" fetch "$cache"

NATIVE_RUNTIME_MODE=package-only \
  TARGET_IMAGE_LOCK_BIN="$selector" \
  NATIVE_RUNTIME_INPUT_LOCK="$lock" \
  NATIVE_RUNTIME_IDLE_FILE="$cache/idle.rbf" \
  "$native_post_build" "$target"

test ! -e "$target/etc/init.d/S40mister-main"
test ! -e "$target/etc/init.d/S40bluetooth"
test ! -e "$target/etc/init.d/S30dbus"
test ! -e "$target/usr/libexec/bluetooth/bluetoothd"
test ! -e "$target/usr/bin/dbus-daemon"
test ! -e "$target/usr/sbin/mister-disable-menu-blanking"
test -x "$target/usr/sbin/fogcast-kit"
test -x "$target/etc/init.d/S60fogcast-kit"
test "$(stat -c %a "$target/usr/share/mister-runtime/idle.rbf")" = 644
test "$(find "$target" -type f -iname '*.rbf' | wc -l | tr -d ' ')" -eq 4
test "$(stat -c %a "$target/usr/share/mister-runtime/core-packages/$(printf '%064d' 0 | tr 0 a)")" = 555
test "$(stat -c %a "$target/usr/share/mister-runtime/core-packages/$(printf '%064d' 0 | tr 0 a)/core.rbf")" = 444
test "$(stat -c %a "$target/usr/share/mister-runtime/selections/fes-pong.package.toml")" = 444
grep -Fq 'mister_runtime_commit=1111111111111111111111111111111111111111' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.pong_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.zx81_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.coleco_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"

NATIVE_RUNTIME_MODE=package-only \
  TARGET_IMAGE_LOCK_BIN="$selector" \
  "$native_extra" verify-image "$cache" "$target"

if NATIVE_RUNTIME_MODE=format1 \
  TARGET_IMAGE_LOCK_BIN="$selector" \
  "$native_post_build" "$target" >/dev/null 2>&1; then
  echo 'native post-build accepted the retired format-1 mode' >&2
  exit 1
fi

printf '%s\n' 'target image native package-only rootfs passed'
