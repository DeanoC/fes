#!/bin/sh
# The diagnostic rootfs is writable; generate unique SSH host keys on the board.
set -eu

target=${1:?TARGET_DIR is required}
case "$target" in
  /*) ;;
  *) printf '%s\n' 'de25 post-build: TARGET_DIR must be absolute' >&2; exit 1 ;;
esac
[ -d "$target/etc" ] || {
  printf '%s\n' 'de25 post-build: target has no etc directory' >&2
  exit 1
}

# Buildroot normally links this to /var/run. Keep keys on the writable rootfs
# so subsequent boots retain the board's identity, without sharing built keys.
if [ -L "$target/etc/dropbear" ]; then
  rm "$target/etc/dropbear"
fi
mkdir -p "$target/etc/dropbear"
chmod 0700 "$target/etc/dropbear"
if [ -n "$(find "$target/etc/dropbear" -name 'dropbear_*_host_key' -print)" ]; then
  printf '%s\n' 'de25 post-build: SSH host keys must be generated on the board' >&2
  exit 1
fi
