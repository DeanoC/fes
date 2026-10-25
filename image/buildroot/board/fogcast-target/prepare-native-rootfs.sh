#!/bin/sh
set -eu

target=${1:?TARGET_DIR is required}
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo=$(CDPATH='' cd -- "$script_dir/../../.." && pwd)
fogcast=${FOGCAST_DIR:?FOGCAST_DIR is required}
agent=$fogcast/bin/mister-agent-linux-armv7
launcher=$fogcast/bin/fogcast-kit-linux-armv7
tenfoot=$fogcast/bin/fogcast-tenfoot-linux-armv7

required_libraries() {
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
    /lib/libz.so.1
}

case "$target" in
  /*) : ;;
  *)
    printf 'post-build: TARGET_DIR must be absolute: %s\n' "$target" >&2
    exit 2
    ;;
esac
target=$(CDPATH='' cd -- "$target" && pwd -P)
[ -x "$agent" ] || {
  printf 'post-build: freshly built agent is missing: %s\n' "$agent" >&2
  exit 1
}
[ -f "$launcher" ] && [ ! -L "$launcher" ] && [ -x "$launcher" ] || {
  printf '%s\n' 'post-build: run make build-fogcast-kit before image assembly' >&2
  exit 1
}
[ -f "$tenfoot" ] && [ ! -L "$tenfoot" ] && [ -x "$tenfoot" ] || {
  printf '%s\n' 'post-build: run make build-fogcast-tenfoot-kit before image assembly' >&2
  exit 1
}

# A retained notice must match the current template exactly. Fail closed on
# stale or changed text; only the normal fresh package install generates notices.
verified_notice=$(/usr/bin/python3 "$repo/scripts/ramtest-notices.py" verify-retained "$target")
if [ -n "$verified_notice" ]; then
  selector=${TARGET_IMAGE_LOCK_BIN:-}
  if [ -z "$selector" ]; then
    case "$(uname -s)" in
      Darwin) selector=$fogcast/bin/target-image-lock ;;
      *) selector=$fogcast/bin/target-image-lock-linux-amd64 ;;
    esac
  fi
  [ -f "$selector" ] && [ ! -L "$selector" ] && [ -x "$selector" ] || {
    printf '%s\n' 'post-build: target-image-lock is missing or unsafe' >&2
    exit 1
  }
  retained_id=$(basename "$(dirname "$verified_notice")")
  "$selector" verify-package --core-id fes.ramtest \
    --package "$target/usr/share/mister-runtime/core-packages/$retained_id" \
    --selection "$target/usr/share/mister-runtime/selections/fes-ramtest.package.toml" >/dev/null
fi

# Warm trees hold aliases from the previous assembled factory bundle. Restore
# the last runtime only when Buildroot has not installed a new regular binary;
# FogCast executables below always come from the selected fresh build.
for binary in mister-runtime mister-agent fogcast-kit fogcast-tenfoot; do
  destination=$target/usr/sbin/$binary
  if [ -L "$destination" ]; then
    [ "$(readlink "$destination")" = "/run/fogcast/software/current/bin/$binary" ] || exit 1
    /bin/rm "$destination"
    if [ "$binary" = mister-runtime ]; then
      /usr/bin/install -m 0755 "$target/usr/share/fes/software/bin/$binary" "$destination"
    fi
  fi
done
# Warm trees hold aliases from the previous assembled factory bundle. The
# Buildroot overlay does not replace that symlink, so install this build's
# overlay assets before the new bundle is sealed. Drop the previous bundle
# before scanning selected bytes.
if [ -L "$target/usr/share/fogcast" ]; then
  [ "$(readlink "$target/usr/share/fogcast")" = /run/fogcast/software/current/share/fogcast ] || exit 1
  /bin/rm "$target/usr/share/fogcast"
  overlay_assets=$script_dir/native-rootfs-overlay/usr/share/fogcast
  [ -d "$overlay_assets" ] && [ ! -L "$overlay_assets" ] || exit 1
  /bin/cp -R "$overlay_assets" "$target/usr/share/fogcast"
fi
if [ -e "$target/usr/share/fes/software" ] || [ -L "$target/usr/share/fes/software" ]; then
  [ -d "$target/usr/share/fes/software" ] && [ ! -L "$target/usr/share/fes/software" ] || exit 1
  /bin/chmod -R u+rwX "$target/usr/share/fes/software"
  /bin/rm -rf "$target/usr/share/fes/software"
fi
/bin/mkdir -p "$target/usr/sbin"
/bin/rm -f "$target/usr/sbin/mister-supervise"
/usr/bin/install -m 0755 "$agent" "$target/usr/sbin/mister-agent"
# Warm Buildroot trees retain the previous launchers. Replace every selected
# FogCast executable before scanning the tree that this build will ship.
/usr/bin/install -m 0755 "$launcher" "$target/usr/sbin/fogcast-kit"
/usr/bin/install -m 0755 "$tenfoot" "$target/usr/sbin/fogcast-tenfoot"

# Buildroot's default skeleton aliases /var/log to /tmp. It must be its own
# mount point so the noexec tmpfs policy in fstab is observable and enforced.
/bin/rm -rf "$target/var/log"
/bin/mkdir -p "$target/var/log"

/bin/chmod 0755 "$target/etc/init.d"/S02fes-usb-quirk \
  "$target/etc/init.d"/S11fes-block-admit \
  "$target/etc/init.d"/S15fes-ethaddr \
  "$target/etc/init.d"/S20mister-network \
  "$target/etc/init.d"/S30dbus \
  "$target/etc/init.d"/S30mister-dropbear \
  "$target/etc/init.d"/S35fes-bluetooth \
  "$target/etc/init.d"/S40bluetooth \
  "$target/etc/init.d"/S41fes-bluetooth \
  "$target/etc/init.d"/S49fogcast-target-smoke \
  "$target/usr/sbin/fes-block-admit" \
  "$target/usr/sbin/fes-usb-quirk" \
  "$target/usr/sbin/fes-reboot-backstop"

/bin/rm -rf "$target/etc/dropbear"
/bin/ln -s /run/dropbear "$target/etc/dropbear"

# BlueZ link keys live at /run/fogcast/fesdata3/bluetooth on the FESDATA3 ext4
# partition, not on the read-only root or FAT. S35 creates this directory.
/bin/rm -rf "$target/var/lib/bluetooth"
/bin/mkdir -p "$target/var/lib"
/bin/ln -s /run/fogcast/fesdata3/bluetooth "$target/var/lib/bluetooth"
# dbus-uuidgen --ensure writes the machine id. Keep that write on /run.
[ -L "$target/var/lib/dbus" ] && /bin/rm -f "$target/var/lib/dbus"
/bin/mkdir -p "$target/var/lib/dbus"
/bin/rm -rf "$target/var/lib/dbus/machine-id"
/bin/ln -s /run/dbus/machine-id "$target/var/lib/dbus/machine-id"

firmware_src=$repo/firmware/rtl_bt
(cd "$firmware_src" && /usr/bin/sha256sum -c SHA256SUMS) >/dev/null
/bin/mkdir -p "$target/lib/firmware/rtl_bt"
for fw in \
  rtl8821c_fw.bin \
  rtl8821c_config.bin \
  rtl8761bu_fw.bin \
  rtl8761bu_config.bin \
  LICENCE.rtlwifi_firmware.txt
do
  [ -f "$firmware_src/$fw" ] && [ ! -L "$firmware_src/$fw" ] || {
    printf 'post-build: bluetooth firmware missing: %s\n' "$fw" >&2
    exit 1
  }
  /usr/bin/install -m 0644 "$firmware_src/$fw" "$target/lib/firmware/rtl_bt/$fw"
done

modules_archive=${FES_BLUETOOTH_MODULES_ARCHIVE:-$repo/build/cache/target-image/image-creator/modules.tar.gz}
if [ -f "$modules_archive" ]; then
  "$repo/scripts/install-bluetooth-modules.sh" "$target" "$modules_archive"
elif [ "${FES_BLUETOOTH_MODULES_REQUIRED:-0}" = 1 ]; then
  printf 'post-build: bluetooth module archive is missing: %s\n' "$modules_archive" >&2
  exit 1
fi

# GCC's GDB auto-load helper embeds the per-run Buildroot output path.
find "$target/usr/lib" -type f -name '*-gdb.py' -delete

/bin/rm -f "$target/root/.empty"
if find "$target/root" -type f -print -quit | grep -q .; then
  printf '%s\n' 'post-build: regular files under /root are forbidden' >&2
  exit 1
fi

if [ -e "$target/media/fat/fogcast/cache" ]; then
  printf '%s\n' 'post-build: target cache state must not be embedded in the root image' >&2
  exit 1
fi

# All Markdown/game payloads except that exact verified source notice remain
# forbidden, including additional Markdown in or beside the notice directory.
if find "$target" -type f ! -path "$verified_notice" \
  ! -path "$target/usr/lib/firmware/rtl_bt/rtl8821c_fw.bin" \
  ! -path "$target/lib/firmware/rtl_bt/rtl8821c_fw.bin" \
  ! -path "$target/usr/lib/firmware/rtl_bt/rtl8821c_config.bin" \
  ! -path "$target/lib/firmware/rtl_bt/rtl8821c_config.bin" \
  ! -path "$target/usr/lib/firmware/rtl_bt/rtl8761bu_fw.bin" \
  ! -path "$target/lib/firmware/rtl_bt/rtl8761bu_fw.bin" \
  ! -path "$target/usr/lib/firmware/rtl_bt/rtl8761bu_config.bin" \
  ! -path "$target/lib/firmware/rtl_bt/rtl8761bu_config.bin" \
  \( \
  -iname '*.rom' -o -iname '*.sfc' -o -iname '*.smc' -o \
  -iname '*.md' -o -iname '*.gen' -o -iname '*.zip' -o -iname '*.bin' -o \
  -iname '*.sqlite' -o -iname '*.sqlite-*' -o \
  -iname '*.sqlite3' -o -iname '*.sqlite3-*' -o \
  -iname '*.db' -o -iname '*.db-*' -o \
  -name '.fogcast-rom-*' -o -name '.fogcast-*.part' -o \
  -name '.fogcast-active-*.tmp' -o -name 'fogcast-active.json' -o \
  -name 'agent.toml' \
\) -print -quit | grep -q .; then
  printf '%s\n' 'post-build: ROM, archive, database, staging, cache, or runtime configuration payload found' >&2
  exit 1
fi

"$repo/scripts/scan-target-image-secrets.sh" "$target"

if find "$target" -type f -exec sh -c '
  for candidate do
    if /usr/bin/strings -a "$candidate" | grep -Fq "/Volumes/"; then
      printf "%s\n" found
    fi
  done
' sh {} + | grep -q .; then
  printf '%s\n' 'post-build: host library path found' >&2
  exit 1
fi

library_count=0
while IFS= read -r library; do
  library_count=$((library_count + 1))
  if [ ! -e "$target$library" ]; then
    printf 'post-build: required library path does not resolve: %s\n' "$library" >&2
    exit 1
  fi
  resolved=$(readlink -f "$target$library")
  case "$resolved" in
    "$target"/*) : ;;
    *)
      printf 'post-build: required library escapes target: %s -> %s\n' "$library" "$resolved" >&2
      exit 1
      ;;
  esac
done <<EOF
$(required_libraries)
EOF

if [ "$library_count" -ne 14 ]; then
  printf 'post-build: expected 14 required libraries, found %s\n' "$library_count" >&2
  exit 1
fi

if [ "$(/usr/bin/id -u)" -eq 0 ]; then
  /bin/chown -h -R 0:0 "$target"
fi
