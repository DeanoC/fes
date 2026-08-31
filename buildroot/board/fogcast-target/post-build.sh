#!/bin/sh
set -eu

target=${1:?TARGET_DIR is required}
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo=$(CDPATH='' cd -- "$script_dir/../../.." && pwd)
agent=$repo/bin/mister-agent-linux-armv7

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

/bin/mkdir -p "$target/usr/sbin"
/usr/bin/install -m 0755 "$agent" "$target/usr/sbin/mister-agent"

# Buildroot's default skeleton aliases /var/log to /tmp. It must be its own
# mount point so the noexec tmpfs policy in fstab is observable and enforced.
/bin/rm -rf "$target/var/log"
/bin/mkdir -p "$target/var/log"

/bin/chmod 0755 "$target/etc/init.d"/S20mister-network \
  "$target/etc/init.d"/S30mister-dropbear \
  "$target/etc/init.d"/S40mister-main \
  "$target/etc/init.d"/S49fogcast-target-smoke \
  "$target/etc/init.d"/S50mister-agent \
  "$target/usr/sbin/mister-supervise"

if ! grep -Fqx 'BR2_PACKAGE_DROPBEAR=y' "${BR2_CONFIG:?BR2_CONFIG is required}"; then
  /bin/rm -rf "$target/etc/dropbear"
  /bin/rm -f "$target/etc/init.d/S30mister-dropbear" "$target/etc/init.d/S50dropbear"
  /bin/rm -f "$target/usr/bin/dbclient" "$target/usr/bin/dropbearkey" "$target/usr/bin/dropbearconvert"
  /bin/rm -f "$target/usr/sbin/dropbear" "$target/usr/sbin/dropbearmulti"
  /usr/bin/awk '!/console::respawn:\/sbin\/getty/' "$target/etc/inittab" > "$target/etc/inittab.target-image"
  /bin/mv "$target/etc/inittab.target-image" "$target/etc/inittab"
else
  /bin/rm -rf "$target/etc/dropbear"
  /bin/ln -s /run/dropbear "$target/etc/dropbear"
fi

# Main_MiSTer needs libbluetooth, not a Bluetooth or D-Bus service.
/bin/rm -f "$target/etc/init.d/S40bluetooth" "$target/etc/init.d/S30dbus"
/bin/rm -f "$target/usr/libexec/bluetooth/bluetoothd" "$target/usr/bin/dbus-daemon"

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

if find "$target" -type f \( \
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
