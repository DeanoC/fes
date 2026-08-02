#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-inventory-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM
root=$fixture/root
fakebin=$fixture/bin

mkdir -p \
  "$root/media/fat/_Console" \
  "$root/media/fat/config/inputs" \
  "$root/media/fat/linux" \
  "$root/lib" \
  "$fakebin"
for path in \
  media/fat/MiSTer \
  media/fat/menu.rbf \
  media/fat/linux/linux.img \
  media/fat/linux/zImage_dtb \
  media/fat/_Console/MegaDrive_20260603.rbf \
  media/fat/_Console/SNES_20260603.rbf \
  media/fat/config/inputs/input_081f_e401_v3.map \
  lib/libfake-real.so; do
  printf '%s\n' "$path" > "$root/$path"
done
ln -s libfake-real.so "$root/lib/libfake.so"

# These single-quoted arguments deliberately write positional parameters into
# the fake tools rather than expanding the test runner's own arguments.
# shellcheck disable=SC2016
printf '%s\n' '#!/bin/sh' 'shasum -a 256 "$1"' > "$fakebin/sha256sum"
# shellcheck disable=SC2016
printf '%s\n' '#!/bin/sh' '/usr/bin/stat -L -f "%z" "$3"' > "$fakebin/stat"
printf '%s\n' '#!/bin/sh' 'printf "%s\n" "libfake.so => /lib/libfake.so (0x00000000)"' > "$fakebin/ldd"
# shellcheck disable=SC2016
printf '%s\n' '#!/bin/sh' 'realpath "$2"' > "$fakebin/readlink"
chmod 0755 "$fakebin/sha256sum" "$fakebin/stat" "$fakebin/ldd" "$fakebin/readlink"

output=$fixture/inventory.toml
MISTER_REMOTE_ROOT=$root PATH=$fakebin:$PATH sh "$repo/deploy/poc1a/inventory.sh" > "$output"
grep -q '^format = 1$' "$output"
grep -q '^name = "controller_input_081f_e401_v3.map"$' "$output"
grep -q '^name = "linux_root"$' "$output"
grep -q '^path = "/media/fat/linux/linux.img"$' "$output"
grep -q '^path = "/media/fat/config/inputs/input_081f_e401_v3.map"$' "$output"
grep -q '^path = "/lib/libfake.so"$' "$output"
grep -q '^resolved_path = "/lib/libfake-real.so"$' "$output"
expected_library_size=$(wc -c < "$root/lib/libfake-real.so" | tr -d ' ')
grep -q "^size = $expected_library_size$" "$output"
test "$(grep -c '^\[\[artifacts\]\]$' "$output")" -eq 7
test "$(grep -c '^\[\[libraries\]\]$' "$output")" -eq 1

for target_script in capture-poc1a-lock.sh verify-poc1a-lock.sh; do
  target_log=$fixture/$target_script.log
  if (cd "$repo" && MISTER_TARGET=user@192.0.2.1 \
      sh "scripts/$target_script") > /dev/null 2> "$target_log"; then
    echo "$target_script accepted a non-root SSH target" >&2
    exit 1
  fi
  grep -q 'MISTER_TARGET must be root@' "$target_log"
done
