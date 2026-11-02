#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
native_rootfs=$repo/buildroot/board/fogcast-target/native-rootfs-overlay
native_post_build=$repo/buildroot/board/fogcast-target/native-post-build.sh
native_extra=$repo/scripts/native-extra-cores.sh
native_runtime=$native_rootfs/etc/init.d/S40fes-kit-software
native_config=$repo/buildroot/configs/fogcast_target_native_dev_defconfig

for required_file in "$native_runtime" "$native_post_build" "$native_extra"; do
  test -f "$required_file"
done
grep -Fqx 'BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y' "$native_config"
grep -Fq 'BR2_ROOTFS_OVERLAY=' "$native_config"
grep -Fq 'native-rootfs-overlay' "$native_config"
grep -Fq 'BR2_ROOTFS_POST_BUILD_SCRIPT=' "$native_config"
grep -Fq 'native-post-build.sh' "$native_config"
grep -Fq 'board/fogcast-target/busybox.fragment' "$native_config"
grep -Fqx 'CONFIG_NTPD=y' "$repo/buildroot/board/fogcast-target/busybox.fragment"
grep -Fq 'package-only' "$native_post_build"
! grep -Fq 'LIBMISTER_RUNTIME_DIR' "$native_post_build"
! grep -Fq 'mister-main' "$native_post_build"
! grep -Fq 'format1' "$native_post_build"

fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-rootfs.XXXXXX")
trap 'chmod -R u+rwX "$fixture" 2>/dev/null || true; rm -rf "$fixture"' EXIT INT TERM

# Check the atomic sibling-write contract with an immutable /var/lib parent.
# This fixture tests filesystem placement; the image boot probe uses real D-Bus.
check_volatile_dbus() {
  python3 - "$1" <<'PY'
import os
from pathlib import Path
import stat
import sys

root = Path(sys.argv[1]).resolve()
volatile = root / 'run/dbus'
volatile.mkdir(parents=True, exist_ok=True)
directory = root / 'var/lib/dbus'
assert directory.resolve() == volatile, 'D-Bus writes must stay inside the target /run'
parent = root / 'var/lib'
mode = stat.S_IMODE(parent.stat().st_mode)
parent.chmod(0o555)
temporary, final = directory / 'machine-id.test-temp', directory / 'machine-id'
try:
    temporary.write_bytes(b'0123456789abcdef0123456789abcdef\n')
    os.replace(temporary, final)
    assert (volatile / 'machine-id').read_bytes() == b'0123456789abcdef0123456789abcdef\n'
    assert not temporary.exists()
finally:
    parent.chmod(mode)
    temporary.unlink(missing_ok=True)
    final.unlink(missing_ok=True)
PY
}

target=$fixture/target
mkdir -p "$target/usr/sbin" "$target/etc/init.d" "$target/root"
cp -R "$native_rootfs/." "$target/"
mkdir -p "$target/var/lib/dbus"
printf '%s\n' stale-build-machine-id >"$target/var/lib/dbus/machine-id"
printf '%s\n' runtime >"$target/usr/sbin/mister-runtime"
printf '%s\n' agent >"$target/usr/sbin/mister-agent"
chmod 0755 "$target/usr/sbin/mister-runtime" "$target/usr/sbin/mister-agent"
mkdir -p "$target/usr/libexec/bluetooth" "$target/usr/bin"
printf '%s\n' bluetooth-init >"$target/etc/init.d/S40bluetooth"
printf '%s\n' dbus-init >"$target/etc/init.d/S30dbus"
printf '%s\n' bluetoothd >"$target/usr/libexec/bluetooth/bluetoothd"
printf '%s\n' dbus-daemon >"$target/usr/bin/dbus-daemon"
chmod 0755 "$target/etc/init.d/S40bluetooth" "$target/etc/init.d/S30dbus" \
  "$target/usr/libexec/bluetooth/bluetoothd" "$target/usr/bin/dbus-daemon"

fogcast=$fixture/fogcast
mkdir -p "$fogcast/bin"
printf '%s\n' agent >"$fogcast/bin/mister-agent-linux-armv7"
chmod 0755 "$fogcast/bin/mister-agent-linux-armv7"
printf '%s\n' kit >"$fogcast/bin/fogcast-kit-linux-armv7"
chmod 0755 "$fogcast/bin/fogcast-kit-linux-armv7"
printf '%s\n' tenfoot >"$fogcast/bin/fogcast-tenfoot-linux-armv7"
chmod 0755 "$fogcast/bin/fogcast-tenfoot-linux-armv7"
export FOGCAST_DIR=$fogcast
for helper in fes-kit-data fes-kit-software fes-bluetooth; do
  printf '%s\n' helper >"$fogcast/bin/$helper-linux-armv7"
  chmod 0755 "$fogcast/bin/$helper-linux-armv7"
done
modules=$fixture/bt-modules
mkdir -p "$modules/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth"
python3 - "$modules/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth" <<'PY'
import sys
from pathlib import Path

root = Path(sys.argv[1])
for name in ("btintel", "btbcm", "btrtl", "btusb"):
    module = b"\x7fELF\0vermagic=5.15.1-MiSTer SMP preempt mod_unload ARMv7 p2v8\0" + name.encode()
    (root / f"{name}.ko").write_bytes(module)
PY
ln -s /tmp "$modules/lib/modules/5.15.1-MiSTer/build"
tar -cf "$fixture/bt-modules.tar" -C "$modules" .
export FES_BLUETOOTH_MODULES_ARCHIVE=$fixture/bt-modules.tar
(cd "$repo/../sources/FogCast" && go build -o "$fogcast/bin/fes-kit-bundle-linux-amd64" ./cmd/fes-kit-bundle)
printf '%s\n' 1111111111111111111111111111111111111111 >"$fogcast/bin/kit-software.revision"
# This fixture tests rootfs wiring with tiny stand-ins. ELF closure validation
# has separate tests; only readelf is replaced here, the bundle packer is real.
mkdir "$fixture/tools"
cat >"$fixture/tools/readelf" <<'READELF'
#!/bin/sh
if [ "$1" = -h ]; then
  if [ "${FES_TARGET_BOARD:-de10-nano}" = de25-nano ]; then
    printf '%s\n' 'Class: ELF64' 'Machine: AArch64' 'Data: little endian'
  else
    printf '%s\n' 'Class: ELF32' 'Machine: ARM' 'Data: little endian' 'Flags: Version5 EABI'
  fi
fi
if [ "$1" = --string-dump=.fes_menu_interfaces ]; then
  printf '%s\n' "String dump of section '.fes_menu_interfaces':" \
    '  [     0]  fes.video.fixed-720p60=1.0' \
    '  [    32]  fes.video.menu-display=1.0'
  if [ "${FES_TARGET_BOARD:-de10-nano}" = de25-nano ]; then
    printf '%s\n' '  [    55]  fes.fabric.de25-scanout=1.1'
  else
    printf '%s\n' '  [    1b]  fes.memory.hps-ddr=1.0'
  fi
fi
READELF
chmod 0755 "$fixture/tools/readelf"
PATH=$fixture/tools:$PATH
export PATH

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
fat_destination = '/idle.rbf'

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
expected_core=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --package|--selection|--cache|--output|--core-id)
      key=$1
      value=$2
      shift 2
      case "$key" in
        --package) package=$value ;;
        --selection) selection=$value ;;
        --cache) cache=$value ;;
        --output) output=$value ;;
        --core-id) expected_core=$value ;;
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
  verify-video-coverage) : ;;
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
    test "$expected_core" = "$core_id"
    grep -Fqx "core_id = '$core_id'" "$selection"
    if [ -n "${SELECTOR_LOG:-}" ]; then
      printf '%s %s %s\n' "$core_id" "$package" "$selection" >>"$SELECTOR_LOG"
    fi
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
  package_dir=$fixture/$package_core-$package_letter-package
  package_selection=$fixture/$package_core-$package_letter.package-selection.toml
  mkdir "$package_dir"
  printf "core_id = 'fes.%s'\npackage_id = '%s'\n" \
    "$package_core" "$package_id" >"$package_dir/manifest.toml"
  printf '%s package payload\n' "$package_core" >"$package_dir/core.rbf"
  if [ "$package_core" = menu ]; then
    if [ "${FES_TARGET_BOARD:-de10-nano}" = de25-nano ]; then
      platform=de25_nano
      menu_interfaces='fes.video.fixed-720p60 fes.fabric.de25-scanout fes.video.menu-display'
    else
      platform=de10_nano
      menu_interfaces='fes.video.fixed-720p60 fes.memory.hps-ddr fes.video.menu-display'
    fi
    printf "\n[target]\nplatform = '%s'\n" "$platform" >>"$package_dir/manifest.toml"
    for interface in $menu_interfaces; do
      minor=0
      [ "$interface" != fes.fabric.de25-scanout ] || minor=1
      printf "\n[[interfaces]]\nid = '%s'\nmajor = 1\nminor = %s\nrequired = true\n" \
        "$interface" "$minor" >>"$package_dir/manifest.toml"
    done
  fi
  if [ "$package_core" = ramtest ]; then
    cat >>"$package_dir/manifest.toml" <<EOF
[core]
id = 'fes.ramtest'
[payload]
sha256 = '$(sha256sum "$package_dir/core.rbf" | awk '{print $1}')'
[build]
repository = 'https://github.com/DeanoC/fes.git'
revision = '1111111111111111111111111111111111111111'
EOF
  fi
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
    menu)
      FES_MENU_PACKAGE_DIR=$package_dir
      FES_MENU_PACKAGE_SELECTION=$package_selection
      ;;
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
    sms)
      FES_SMS_PACKAGE_DIR=$package_dir
      FES_SMS_PACKAGE_SELECTION=$package_selection
      ;;
    sg1000)
      FES_SG1000_PACKAGE_DIR=$package_dir
      FES_SG1000_PACKAGE_SELECTION=$package_selection
      ;;
    c64)
      FES_C64_PACKAGE_DIR=$package_dir
      FES_C64_PACKAGE_SELECTION=$package_selection
      ;;
    spectrum)
      FES_SPECTRUM_PACKAGE_DIR=$package_dir
      FES_SPECTRUM_PACKAGE_SELECTION=$package_selection
      ;;
    atari-st)
      FES_ATARI_ST_PACKAGE_DIR=$package_dir
      FES_ATARI_ST_PACKAGE_SELECTION=$package_selection
      ;;
    ramtest)
      FES_RAMTEST_PACKAGE_DIR=$package_dir
      FES_RAMTEST_PACKAGE_SELECTION=$package_selection
      ;;
  esac
}

package_fixture menu d
package_fixture pong a
package_fixture zx81 b
package_fixture coleco c
package_fixture sms f
package_fixture sg1000 1
package_fixture spectrum 2
package_fixture ramtest e
package_fixture atari-st 4
# This fixture exercises an explicitly expanded image and asserts Pong/ZX81
# receipts below. The bootstrap profile is covered separately.
FES_PACKAGE_IDS=fes.menu,fes.pong,fes.zx81,fes.coleco,fes.sms,fes.sg1000,fes.spectrum,fes.ramtest,fes.atari-st
export FES_PACKAGE_IDS
export FES_MENU_PACKAGE_DIR FES_MENU_PACKAGE_SELECTION
export FES_PONG_PACKAGE_DIR FES_PONG_PACKAGE_SELECTION
export FES_ZX81_PACKAGE_DIR FES_ZX81_PACKAGE_SELECTION
export FES_COLECO_PACKAGE_DIR FES_COLECO_PACKAGE_SELECTION
export FES_SMS_PACKAGE_DIR FES_SMS_PACKAGE_SELECTION
export FES_SG1000_PACKAGE_DIR FES_SG1000_PACKAGE_SELECTION
export FES_SPECTRUM_PACKAGE_DIR FES_SPECTRUM_PACKAGE_SELECTION
export FES_RAMTEST_PACKAGE_DIR FES_RAMTEST_PACKAGE_SELECTION
export FES_ATARI_ST_PACKAGE_DIR FES_ATARI_ST_PACKAGE_SELECTION

# The factory build compiles fes-window; this fixture supplies its output.
window_driver=$fixture/fes-window
mkdir "$window_driver"
printf '%s\n' fixture-module >"$window_driver/fes_window.ko"
printf '%s\n' 0123456789abcdef >"$window_driver/version"
FES_WINDOW_DRIVER_DIR=$window_driver
export FES_WINDOW_DRIVER_DIR

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

# Produce a software-only update from separately built payload bytes against
# this retained base. Neither factory identity nor the input tree is modified.
update_payload=$fixture/update-payload
mkdir "$update_payload"
cp -R "$target/usr/share/fes/software/bin" "$target/usr/share/fes/software/share" "$update_payload/"
chmod u+w "$update_payload/bin/mister-agent"
printf '%s\n' new-agent >"$update_payload/bin/mister-agent"
factory_before=$(sha256sum "$target/usr/share/fes/software/manifest.json")
payload_before=$(sha256sum "$update_payload/bin/mister-agent")
python3 "$repo/scripts/kit-software.py" --base-root "$target" --payload "$update_payload" \
  --output "$fixture/update.tar" --version test-update --fogcast "$fogcast" \
  --runtime-revision 1111111111111111111111111111111111111111 >"$fixture/update.json"
"$fogcast/bin/fes-kit-bundle-linux-amd64" inspect --bundle "$fixture/update.tar" >/dev/null
# The image records its sealed menu's interfaces; both bundles list what their runtime admits.
grep -Fq '"requires":{"menu.fes.memory.hps-ddr":"1.0","menu.fes.video.fixed-720p60":"1.0","menu.fes.video.menu-display":"1.0"}' \
  "$target/etc/fes/software-base.json"
grep -Fq '"menu.fes.video.menu-display":"1.0"' "$target/usr/share/fes/software/manifest.json"
grep -Fq '"menu.fes.video.menu-display": "1.0"' "$fixture/update.json"
test "$factory_before" = "$(sha256sum "$target/usr/share/fes/software/manifest.json")"
test "$payload_before" = "$(sha256sum "$update_payload/bin/mister-agent")"
archive_before=$(sha256sum "$fixture/update.tar")
# An unsafe new member fails without replacing the previously built archive.
ln -s /etc/passwd "$update_payload/share/unsafe"
if python3 "$repo/scripts/kit-software.py" --base-root "$target" --payload "$update_payload" \
  --output "$fixture/update.tar" --version test-update --fogcast "$fogcast" \
  --runtime-revision 1111111111111111111111111111111111111111 >"$fixture/update-rejected.log" 2>&1; then
  echo 'software-only pack admitted a payload symlink' >&2
  exit 1
fi
test "$archive_before" = "$(sha256sum "$fixture/update.tar")"

test ! -e "$target/etc/init.d/S40mister-main"
test -x "$target/etc/init.d/S40bluetooth"
test -x "$target/etc/init.d/S30dbus"
test -x "$target/usr/libexec/bluetooth/bluetoothd"
test -x "$target/usr/bin/dbus-daemon"
test -L "$target/var/lib/bluetooth"
test "$(readlink "$target/var/lib/bluetooth")" = /run/fogcast/fesdata3/bluetooth
check_volatile_dbus "$target"
for fw in rtl8821c_fw.bin rtl8821c_config.bin rtl8761bu_fw.bin rtl8761bu_config.bin LICENCE.rtlwifi_firmware.txt; do
  test -f "$target/lib/firmware/rtl_bt/$fw"
done
(cd "$target/lib/firmware/rtl_bt" && sha256sum -c "$repo/firmware/rtl_bt/SHA256SUMS") >/dev/null
for ko in btintel.ko btbcm.ko btrtl.ko btusb.ko; do
  test -f "$target/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/$ko"
done
test ! -e "$target/lib/modules/5.15.1-MiSTer/build"
test -x "$target/etc/init.d/S35fes-bluetooth"
test -x "$target/etc/init.d/S41fes-bluetooth"
test -x "$target/usr/sbin/fes-bluetooth"
grep -Fq 'AutoEnable=true' "$target/etc/bluetooth/main.conf"
grep -Fq 'JustWorksRepairing = never' "$target/etc/bluetooth/main.conf"
grep -Fq 'PairableTimeout = 60' "$target/etc/bluetooth/main.conf"
grep -Fq 'DiscoverableTimeout = 60' "$target/etc/bluetooth/main.conf"
test ! -e "$target/usr/sbin/mister-disable-menu-blanking"
test -x "$target/usr/share/fes/software/bin/fogcast-kit"
cmp "$window_driver/fes_window.ko" "$target/usr/share/fes/software/share/fes-window/fes_window.ko"
cmp "$window_driver/version" "$target/usr/share/fes/software/share/fes-window/version"
test -x "$target/usr/share/fes/software/bin/fogcast-tenfoot"
test -x "$target/etc/init.d/S40fes-kit-software"
grep -Fq "fogcast_tenfoot_sha256=$(sha256sum "$target/usr/share/fes/software/bin/fogcast-tenfoot" | awk '{print $1}')" \
  "$target/usr/share/mister-runtime/build-inputs"
test -x "$target/etc/init.d/S15fes-ethaddr"
test -x "$target/etc/init.d/S21fes-time"
test -x "$target/etc/init.d/S02fes-usb-quirk"
test -x "$target/etc/init.d/S11fes-block-admit"
test -x "$target/usr/sbin/fes-block-admit"
test -x "$target/usr/sbin/fes-usb-quirk"
grep -Fq '152d:0583:i' "$target/usr/sbin/fes-usb-quirk"
! grep -Fq '/media/fat' "$target/usr/sbin/fes-usb-quirk"
test -f "$target/etc/mdev.conf"
test "$(stat -c %a "$target/usr/share/mister-runtime/idle.rbf")" = 644
package_count=$(printf '%s' "$FES_PACKAGE_IDS" | awk -F, '{print NF}')
test "$(find "$target" -type f -iname '*.rbf' | wc -l | tr -d ' ')" -eq "$((package_count + 1))"
test "$(stat -c %a "$target/usr/share/mister-runtime/core-packages/$(printf '%064d' 0 | tr 0 a)")" = 555
test "$(stat -c %a "$target/usr/share/mister-runtime/core-packages/$(printf '%064d' 0 | tr 0 a)/core.rbf")" = 444
test "$(stat -c %a "$target/usr/share/mister-runtime/selections/fes-pong.package.toml")" = 444
grep -Fq 'mister_runtime_commit=1111111111111111111111111111111111111111' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.pong_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.menu_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
test "$(stat -c %a "$target/usr/share/mister-runtime/selections/fes-menu.package.toml")" = 444
grep -Fq 'fes.zx81_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.coleco_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.sms_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.sg1000_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
test "$(stat -c %a "$target/usr/share/mister-runtime/selections/fes-atari-st.package.toml")" = 444
grep -Fq 'fes.atari-st_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.spectrum_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"
grep -Fq 'fes.ramtest_package_id=' \
  "$target/usr/share/mister-runtime/build-inputs"

NATIVE_RUNTIME_MODE=package-only \
  TARGET_IMAGE_LOCK_BIN="$selector" \
  "$native_extra" verify-image "$cache" "$target"

# Exercise the same factory scripts for DE25. These are rootfs-wiring fixtures:
# readelf and package selection remain the explicit stand-ins above. No FPGA
# seal, executable CPU behavior or kit acceptance is asserted by this section.
(
  export FES_TARGET_BOARD=de25-nano
  unset FES_WINDOW_DRIVER_DIR FES_BLUETOOTH_MODULES_ARCHIVE FES_BLUETOOTH_MODULES_REQUIRED
  unset FES_PONG_PACKAGE_DIR FES_PONG_PACKAGE_SELECTION FES_ZX81_PACKAGE_DIR FES_ZX81_PACKAGE_SELECTION
  unset FES_COLECO_PACKAGE_DIR FES_COLECO_PACKAGE_SELECTION FES_SMS_PACKAGE_DIR FES_SMS_PACKAGE_SELECTION
  unset FES_SG1000_PACKAGE_DIR FES_SG1000_PACKAGE_SELECTION FES_SPECTRUM_PACKAGE_DIR FES_SPECTRUM_PACKAGE_SELECTION
  unset FES_RAMTEST_PACKAGE_DIR FES_RAMTEST_PACKAGE_SELECTION FES_ATARI_ST_PACKAGE_DIR FES_ATARI_ST_PACKAGE_SELECTION
  FES_PACKAGE_IDS=fes.menu
  export FES_PACKAGE_IDS
  de25_target=$fixture/de25-target
  mkdir -p "$de25_target/etc/init.d" "$de25_target/usr/sbin" "$de25_target/root" "$de25_target/usr/lib"
  cp -R "$native_rootfs/." "$de25_target/"
  mkdir -p "$de25_target/var/lib/dbus"
  ln -s /run/dbus/machine-id "$de25_target/var/lib/dbus/machine-id"
  printf '%s\n' de25-nano >"$de25_target/etc/fes-board"
  cp "$target/etc/init.d/S30dbus" "$de25_target/etc/init.d/S30dbus"
  cp "$target/etc/init.d/S40bluetooth" "$de25_target/etc/init.d/S40bluetooth"
  printf '%s\n' runtime >"$de25_target/usr/sbin/mister-runtime"
  chmod 0755 "$de25_target/usr/sbin/mister-runtime"
  for name in mister-agent fogcast-kit fogcast-tenfoot fes-kit-data fes-kit-software fes-bluetooth; do
    cp "$fogcast/bin/$name-linux-armv7" "$fogcast/bin/$name-linux-aarch64"
  done
  for library in ld-linux-aarch64.so.1 libImlib2.so.1 libbluetooth.so.3 libbz2.so.1.0 libc.so.6 libdl.so.2 \
    libfreetype.so.6 libgcc_s.so.1 libm.so.6 libpng16.so.16 libpthread.so.0 librt.so.1 libstdc++.so.6 libz.so.1; do
    mkdir -p "$de25_target/lib"
    printf '%s\n' library >"$de25_target/lib/$library"
  done
  "$repo/buildroot/board/fogcast-target/prepare-native-rootfs.sh" "$de25_target"
  check_volatile_dbus "$de25_target"
  test ! -e "$de25_target/etc/init.d/S15fes-ethaddr"
  cmp "$repo/de25-nano/rootfs/overlay/etc/init.d/S38fes-network-id" "$de25_target/etc/init.d/S15fes-network-id"
  test "$(readlink "$de25_target/etc/dropbear")" = /media/fat/fogcast/ssh
  package_fixture menu 5
  export FES_MENU_PACKAGE_DIR FES_MENU_PACKAGE_SELECTION
  de25_idle=$FES_MENU_PACKAGE_DIR/core.rbf
  de25_sha=$(sha256sum "$de25_idle" | awk '{print $1}')
  de25_size=$(wc -c <"$de25_idle" | tr -d ' ')
  sed -e "s/^sha256 = .*/sha256 = '$de25_sha'/" -e "s/^size = .*/size = $de25_size/" \
    "$lock" >"$fixture/de25.lock.toml"
  de25_cache=$fixture/de25-cache
  mkdir "$de25_cache"
  cp "$de25_idle" "$de25_cache/idle.rbf"
  chmod 0444 "$de25_cache/idle.rbf"
  NATIVE_RUNTIME_MODE=package-only TARGET_IMAGE_LOCK_BIN="$selector" "$native_extra" fetch "$de25_cache"
  # Managed base intermediates omit S40. A rejected bundle must leave it
  # disabled; a successful factory assembly installs this source's script.
  rm "$de25_target/etc/init.d/S40fes-kit-software"
  printf '%s\n' invalid >"$fogcast/bin/kit-software.revision"
  if NATIVE_RUNTIME_MODE=package-only TARGET_IMAGE_LOCK_BIN="$selector" \
    NATIVE_RUNTIME_INPUT_LOCK="$fixture/de25.lock.toml" NATIVE_RUNTIME_IDLE_FILE="$de25_cache/idle.rbf" \
    "$native_post_build" "$de25_target" >"$fixture/de25-rejected-bundle.log" 2>&1; then
    printf '%s\n' 'factory accepted invalid software provenance' >&2
    exit 1
  fi
  test ! -e "$de25_target/etc/init.d/S40fes-kit-software"
  test ! -e "$de25_target/usr/share/fes/software/manifest.json"
  printf '%s\n' 1111111111111111111111111111111111111111 >"$fogcast/bin/kit-software.revision"
  NATIVE_RUNTIME_MODE=package-only TARGET_IMAGE_LOCK_BIN="$selector" \
    NATIVE_RUNTIME_INPUT_LOCK="$fixture/de25.lock.toml" NATIVE_RUNTIME_IDLE_FILE="$de25_cache/idle.rbf" \
    "$native_post_build" "$de25_target"
  grep -Fq '"board":"de25-nano"' "$de25_target/etc/fes/software-base.json"
  grep -Fq '"architecture":"aarch64"' "$de25_target/etc/fes/software-base.json"
  grep -Fq '"menu.fes.fabric.de25-scanout":"1.1"' "$de25_target/etc/fes/software-base.json"
  test ! -e "$de25_target/usr/share/fes/software/share/fes-window"
  test ! -e "$de25_target/lib/modules/5.15.1-MiSTer"
  cmp "$de25_idle" "$de25_target/usr/share/mister-runtime/idle.rbf"
  for helper in fes-kit-data fes-kit-software fes-bluetooth; do
    cmp "$fogcast/bin/$helper-linux-aarch64" "$de25_target/usr/sbin/$helper"
  done
  test -x "$de25_target/etc/init.d/S40fes-kit-software"
  cmp "$native_runtime" "$de25_target/etc/init.d/S40fes-kit-software"
  test -L "$de25_target/usr/sbin/mister-runtime"
)

# Reuse the completed target while selecting a newer RAM Tester. Preparation
# verifies the retained notice, package and record, then replaces stale launchers
# before the secret scan. Post-build installs the new package and notice afterward.
old_ramtest_id=$(printf '%064d' 0 | tr 0 e)
old_notice=$target/usr/share/mister-runtime/core-notices/fes.ramtest/$old_ramtest_id/SOURCE.md
package_fixture ramtest 3
new_ramtest_id=$(printf '%064d' 0 | tr 0 3)
new_manifest=$FES_RAMTEST_PACKAGE_DIR/manifest.toml
chmod u+w "$FES_RAMTEST_PACKAGE_DIR" "$new_manifest"
sed 's/1111111111111111111111111111111111111111/2222222222222222222222222222222222222222/' \
  "$new_manifest" >"$fixture/new-manifest"
cat "$fixture/new-manifest" >"$new_manifest"
chmod 0444 "$new_manifest"
chmod 0555 "$FES_RAMTEST_PACKAGE_DIR"
warm_cache=$fixture/warm-cache
mkdir "$warm_cache"
cp "$idle" "$warm_cache/idle.rbf"
chmod 0444 "$warm_cache/idle.rbf"
NATIVE_RUNTIME_MODE=package-only TARGET_IMAGE_LOCK_BIN="$selector" \
  "$native_extra" fetch "$warm_cache"

prepare=$repo/buildroot/board/fogcast-target/prepare-native-rootfs.sh
prepare_reject() {
  reason=$1
  if TARGET_IMAGE_LOCK_BIN="$selector" "$prepare" "$target" >"$fixture/prepare-reject.log" 2>&1; then
    echo "warm native preparation accepted $reason" >&2
    exit 1
  fi
}
stale_launchers() {
  rm -f "$target/usr/sbin/fogcast-kit" "$target/usr/sbin/fogcast-tenfoot"
  printf '%s\n' 'const token = ++epoch;' >"$target/usr/sbin/fogcast-kit"
  printf '%s\n' 'const token = String(region);' >"$target/usr/sbin/fogcast-tenfoot"
}
stale_launchers
if "$repo/scripts/scan-target-image-secrets.sh" "$target" >"$fixture/stale-scan.log" 2>&1; then
  echo 'stale launcher fixture did not trigger the unchanged secret scan' >&2
  exit 1
fi

cp "$old_notice" "$fixture/old-source"
# A tampered retained notice must fail before any warm-rootfs changes occur.
printf '%s\n' 'tampered source notice' >"$fixture/tampered-source"
chmod u+w "$old_notice"
cp "$fixture/tampered-source" "$old_notice"
chmod 0444 "$old_notice"
stale_launchers
prepare_reject 'an arbitrarily changed retained source notice'
grep -Fq 'RAM Tester retained notice is stale, changed or unsealed: SOURCE.md' \
  "$fixture/prepare-reject.log"
cmp "$fixture/tampered-source" "$old_notice"
chmod u+w "$old_notice"
cp "$fixture/old-source" "$old_notice"
chmod 0444 "$old_notice"

# A pre-pin notice is stale and must fail closed without an in-place migration.
python3 - "$repo" "$target/usr/share/mister-runtime/core-packages/$old_ramtest_id" \
  "$old_notice" <<'PY'
import importlib.util
from pathlib import Path
import sys

repo, package, notice_path = map(Path, sys.argv[1:])
spec = importlib.util.spec_from_file_location('ramtest_notices', repo / 'scripts/ramtest-notices.py')
notices = importlib.util.module_from_spec(spec)
spec.loader.exec_module(notices)
current = notices.source_notice(package).decode()
current_details = notices._current_tool_lock_details(notices.ramtest_nextpnr_pin())
previous_details = ''' The tool lock is the one named by
the fes.ramtest lock_path in config/core-recipes.toml at that commit:
sources/misteross/toolchain.lock for packages built after RAM test moved to
the shared lock, sources/misteross/toolchains/ramtest.lock for earlier ones.'''
assert current_details in current
notice_path.chmod(0o644)
notice_path.write_text(current.replace(current_details, previous_details))
notice_path.chmod(0o444)
PY
cp "$old_notice" "$fixture/stale-source"
stale_launchers
prepare_reject 'a pre-pin retained source notice'
grep -Fq 'RAM Tester retained notice is stale, changed or unsealed: SOURCE.md' \
  "$fixture/prepare-reject.log"
cmp "$fixture/stale-source" "$old_notice"
chmod u+w "$old_notice"
cp "$fixture/old-source" "$old_notice"
chmod 0444 "$old_notice"

printf '%s\n' extra >"$(dirname "$old_notice")/EXTRA.md"
prepare_reject 'extra notice Markdown'
grep -Fq 'RAM Tester notices members differ' "$fixture/prepare-reject.log"
rm "$(dirname "$old_notice")/EXTRA.md"
printf '%s\n' extra >"$target/usr/share/EXTRA.md"
prepare_reject 'unrelated Markdown'
grep -Fq 'ROM, archive, database, staging, cache, or runtime configuration payload found' "$fixture/prepare-reject.log"
rm "$target/usr/share/EXTRA.md"

old_record=$target/usr/share/mister-runtime/selections/fes-ramtest.package.toml
cp "$old_record" "$fixture/old-record"
chmod u+w "$old_record"
sed "s/$old_ramtest_id/$new_ramtest_id/" "$fixture/old-record" >"$old_record"
chmod 0444 "$old_record"
prepare_reject 'a retained record for a different package'
chmod u+w "$old_record"
cat "$fixture/old-record" >"$old_record"
chmod 0444 "$old_record"

mv "$fogcast/bin/fogcast-kit-linux-armv7" "$fixture/selected-kit"
ln -s "$fixture/selected-kit" "$fogcast/bin/fogcast-kit-linux-armv7"
prepare_reject 'a symlinked selected launcher'
grep -Fq 'run make build-fogcast-kit' "$fixture/prepare-reject.log"
rm "$fogcast/bin/fogcast-kit-linux-armv7"
mv "$fixture/selected-kit" "$fogcast/bin/fogcast-kit-linux-armv7"
chmod 0644 "$fogcast/bin/fogcast-tenfoot-linux-armv7"
prepare_reject 'a non-executable selected tenfoot launcher'
grep -Fq 'run make build-fogcast-tenfoot-kit' "$fixture/prepare-reject.log"
chmod 0755 "$fogcast/bin/fogcast-tenfoot-linux-armv7"

stale_launchers
stale_assets=$target/usr/share/fes/software/share/fogcast
mkdir -p "$stale_assets"
printf '%s\n' 'stale-catalog' >"$stale_assets/config.toml"
SELECTOR_LOG=$fixture/retained-selector.log TARGET_IMAGE_LOCK_BIN="$selector" \
  "$prepare" "$target"
cmp "$native_rootfs/usr/share/fogcast/config.toml" "$target/usr/share/fogcast/config.toml"
grep -Fqx "fes.ramtest $target/usr/share/mister-runtime/core-packages/$old_ramtest_id $old_record" \
  "$fixture/retained-selector.log"
cmp "$fogcast/bin/fogcast-kit-linux-armv7" "$target/usr/sbin/fogcast-kit"
cmp "$fogcast/bin/fogcast-tenfoot-linux-armv7" "$target/usr/sbin/fogcast-tenfoot"
test "$(stat -c %a "$target/usr/sbin/fogcast-kit")" = 755
test "$(stat -c %a "$target/usr/sbin/fogcast-tenfoot")" = 755
# Exercise the normal container selector basename without an override too.
case "$(uname -s)" in
  Darwin) default_selector=$fogcast/bin/target-image-lock ;;
  *) default_selector=$fogcast/bin/target-image-lock-linux-amd64 ;;
esac
cp "$selector" "$default_selector"
"$prepare" "$target"
NATIVE_RUNTIME_MODE=package-only TARGET_IMAGE_LOCK_BIN="$selector" \
  NATIVE_RUNTIME_INPUT_LOCK="$lock" NATIVE_RUNTIME_IDLE_FILE="$warm_cache/idle.rbf" \
  "$native_post_build" "$target"
check_volatile_dbus "$target"
test ! -e "$old_notice"
new_notice=$target/usr/share/mister-runtime/core-notices/fes.ramtest/$new_ramtest_id/SOURCE.md
grep -Fqx 'Exact producing commit: 2222222222222222222222222222222222222222' "$new_notice"
NATIVE_RUNTIME_MODE=package-only TARGET_IMAGE_LOCK_BIN="$selector" \
  "$native_extra" verify-image "$warm_cache" "$target"
TARGET_IMAGE_LOCK_BIN="$selector" "$prepare" "$target"

if NATIVE_RUNTIME_MODE=format1 \
  TARGET_IMAGE_LOCK_BIN="$selector" \
  "$native_post_build" "$target" >/dev/null 2>&1; then
  echo 'native post-build accepted the retired format-1 mode' >&2
  exit 1
fi

printf '%s\n' 'target image native package-only rootfs passed'
