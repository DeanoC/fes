#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
overlay=$repo/buildroot/board/mister-remote/rootfs-overlay
agent=$overlay/etc/init.d/S50mister-agent
post_build=$repo/buildroot/board/mister-remote/post-build.sh
example_config=$repo/deploy/poc1a/agent.toml.example

test -f "$agent"
test -f "$post_build"
test ! -e "$overlay/media/fat/fogcast"
grep -Fqx 'cache_max_bytes = 2147483648' "$example_config"
make -C "$repo" -n test | grep -Fqx 'sh scripts/tests/poc2-rootfs_test.sh'

require_policy() {
  policy=$1
  grep -Fq -- "$policy" "$agent" || {
    printf 'POC 2 init policy is missing: %s\n' "$policy" >&2
    exit 1
  }
}

require_policy '[ ! -f /media/fat/mister-remote/agent.toml ]'
require_policy '/bin/mkdir -p /media/fat/fogcast/cache/megadrive /media/fat/fogcast/cache/snes'
require_policy '/bin/chmod 0700 /media/fat/fogcast/cache /media/fat/fogcast/cache/megadrive /media/fat/fogcast/cache/snes'
require_policy '/usr/sbin/mister-supervise mister-agent /usr/sbin/mister-agent --config /media/fat/mister-remote/agent.toml &'

fat_ready_line=$(grep -nF '[ ! -f /media/fat/mister-remote/agent.toml ]' "$agent" | head -n 1 | cut -d: -f1)
cache_root_line=$(grep -nF '/bin/mkdir -p /media/fat/fogcast/cache/megadrive /media/fat/fogcast/cache/snes' "$agent" | head -n 1 | cut -d: -f1)
cache_mode_line=$(grep -nF '/bin/chmod 0700 /media/fat/fogcast/cache /media/fat/fogcast/cache/megadrive /media/fat/fogcast/cache/snes' "$agent" | head -n 1 | cut -d: -f1)
supervisor_line=$(grep -nF '/usr/sbin/mister-supervise mister-agent /usr/sbin/mister-agent --config /media/fat/mister-remote/agent.toml &' "$agent" | head -n 1 | cut -d: -f1)
test "$fat_ready_line" -lt "$cache_root_line"
test "$cache_root_line" -lt "$cache_mode_line"
test "$cache_mode_line" -lt "$supervisor_line"
if grep -Eq '(/bin/)?rm[[:space:]].*fogcast/cache' "$agent"; then
  echo 'POC 2 init policy recursively removes or resets persistent cache state' >&2
  exit 1
fi

fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc2-rootfs.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM
prod_config=$fixture/prod.config
printf '%s\n' '# BR2_PACKAGE_DROPBEAR is not set' > "$prod_config"

prepare_target() {
  prepare_name=$1
  prepare_target=$fixture/$prepare_name
  mkdir -p "$prepare_target/root" "$prepare_target/etc/init.d" "$prepare_target/usr/lib" \
    "$prepare_target/usr/sbin" "$prepare_target/usr/libexec/bluetooth" \
    "$prepare_target/etc/dropbear" "$prepare_target/var" "$prepare_target/tmp"
  cp -R "$overlay/." "$prepare_target/"
  ln -s ../tmp "$prepare_target/var/log"
  : > "$prepare_target/etc/init.d/S50dropbear"
  : > "$prepare_target/etc/init.d/S30dbus"
  : > "$prepare_target/usr/sbin/dropbear"
  : > "$prepare_target/usr/libexec/bluetooth/bluetoothd"
  : > "$prepare_target/usr/lib/libstdc++.so.6.0.28-gdb.py"
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
    mkdir -p "$prepare_target$(dirname "$library")"
    : > "$prepare_target$library"
  done
  printf '%s\n' "$prepare_target"
}

baseline=$(prepare_target baseline)
BR2_CONFIG=$prod_config "$post_build" "$baseline"

case_number=0
for relative in \
  var/lib/fogcast/library.sqlite3 \
  var/lib/fogcast/library.sqlite3-wal \
  var/lib/fogcast/library.sqlite3-shm \
  tmp/.fogcast-rom-synthetic \
  run/.fogcast-active-synthetic.tmp \
  run/fogcast-active.json \
  media/fat/fogcast/cache/snes/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.sfc \
  media/fat/fogcast/cache/snes/.fogcast-upload-synthetic.part
do
  case_number=$((case_number + 1))
  target=$(prepare_target "forbidden-$case_number")
  mkdir -p "$(dirname "$target/$relative")"
  printf '%s\n' 'synthetic-policy-payload' > "$target/$relative"
  if BR2_CONFIG=$prod_config "$post_build" "$target" >/dev/null 2>&1; then
    printf 'post-build accepted POC 2 runtime payload: %s\n' "$relative" >&2
    exit 1
  fi
done

target=$(prepare_target embedded-cache-directory)
mkdir -p "$target/media/fat/fogcast/cache/megadrive" "$target/media/fat/fogcast/cache/snes"
if BR2_CONFIG=$prod_config "$post_build" "$target" >/dev/null 2>&1; then
  echo 'post-build accepted embedded target cache directories' >&2
  exit 1
fi

target=$(prepare_target embedded-token)
printf '%s\n' 'token = "synthetic-private-token"' > "$target/etc/fogcast-private"
if BR2_CONFIG=$prod_config "$post_build" "$target" >/dev/null 2>&1; then
  echo 'post-build accepted an embedded token assignment' >&2
  exit 1
fi

target=$(prepare_target embedded-nas-path)
printf '%s\n' '/Volumes/SyntheticPrivateNAS/Games' > "$target/etc/fogcast-private"
if BR2_CONFIG=$prod_config "$post_build" "$target" >/dev/null 2>&1; then
  echo 'post-build accepted an embedded NAS path' >&2
  exit 1
fi
