#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-install.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

installer=$repo/deploy/poc1b/install-target.sh
restore=$repo/scripts/restore-poc1a-sd.sh
test -x "$installer"
test -x "$restore"

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

write_file() {
  write_path=$1
  write_value=$2
  mkdir -p "$(dirname "$write_path")"
  printf '%s\n' "$write_value" > "$write_path"
}

baseline=$fixture/baseline
fat=$baseline/media/fat
write_file "$fat/MiSTer" main-mister
write_file "$fat/menu.rbf" menu
write_file "$fat/linux/zImage_dtb" accepted-kernel
write_file "$fat/linux/linux.img" stock-root
write_file "$fat/_Console/MegaDrive_20260603.rbf" megadrive
write_file "$fat/_Console/SNES_20260603.rbf" snes
write_file "$fat/config/inputs/input_081f_e401_v3.map" controller
mkdir -p "$baseline/tmp"

stage=$fixture/stage/poc1b
mkdir -p "$stage"
write_file "$stage/linux.img" poc1b-dev-root
write_file "$stage/zImage_dtb" reproduced-kernel

module_root=$fixture/module-root
write_file "$module_root/lib/modules/5.15.1-MiSTer/kernel/test.ko" module
COPYFILE_DISABLE=1 tar -czf "$stage/modules.tar.gz" -C "$module_root" .
modules_sha=$(sha256_file "$stage/modules.tar.gz")
cat > "$stage/kernel-manifest.toml" <<EOF
release = "5.15.1-MiSTer"

[[artifacts]]
name = "modules.tar.gz"
sha256 = "$modules_sha"
size = 1
EOF

cat > "$stage/poc1a.lock.toml" <<EOF
format = 1

[[artifacts]]
name = "main_mister"
path = "/media/fat/MiSTer"
sha256 = "$(sha256_file "$fat/MiSTer")"
size = 1
source = "fixture"

[[artifacts]]
name = "menu"
path = "/media/fat/menu.rbf"
sha256 = "$(sha256_file "$fat/menu.rbf")"
size = 1
source = "fixture"

[[artifacts]]
name = "kernel"
path = "/media/fat/linux/zImage_dtb"
sha256 = "$(sha256_file "$fat/linux/zImage_dtb")"
size = 1
source = "fixture"

[[artifacts]]
name = "megadrive_core"
path = "/media/fat/_Console/MegaDrive_20260603.rbf"
sha256 = "$(sha256_file "$fat/_Console/MegaDrive_20260603.rbf")"
size = 1
source = "fixture"

[[artifacts]]
name = "snes_core"
path = "/media/fat/_Console/SNES_20260603.rbf"
sha256 = "$(sha256_file "$fat/_Console/SNES_20260603.rbf")"
size = 1
source = "fixture"

[[artifacts]]
name = "controller_input_081f_e401_v3.map"
path = "/media/fat/config/inputs/input_081f_e401_v3.map"
sha256 = "$(sha256_file "$fat/config/inputs/input_081f_e401_v3.map")"
size = 1
source = "fixture"

[runtime]
kernel_release = "5.15.1-MiSTer"
EOF

cat > "$stage/poc1b.lock.toml" <<EOF
format = 1

[outputs]
prod_rootfs_sha256 = "$(sha256_file "$stage/linux.img")"
dev_rootfs_sha256 = "$(sha256_file "$stage/linux.img")"
reproduced_kernel_sha256 = "$(sha256_file "$stage/zImage_dtb")"
EOF

binary_archive=$fixture/binary-kernel.tar.gz
COPYFILE_DISABLE=1 tar -czf "$binary_archive" -C "$fixture/stage" \
  poc1b/poc1a.lock.toml \
  poc1b/poc1b.lock.toml \
  poc1b/linux.img

source_archive=$fixture/source-kernel.tar.gz
COPYFILE_DISABLE=1 tar -czf "$source_archive" -C "$fixture/stage" \
  poc1b/poc1a.lock.toml \
  poc1b/poc1b.lock.toml \
  poc1b/linux.img \
  poc1b/zImage_dtb \
  poc1b/modules.tar.gz \
  poc1b/kernel-manifest.toml

install_fixture() {
  install_root=$1
  shift
  MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ROOT=$install_root \
  MISTER_REMOTE_ALLOWED_TEST_ROOT=$install_root \
    sh "$installer" "$@"
}

expect_install_failure() {
  failure_root=$1
  shift
  if install_fixture "$failure_root" "$@" >/dev/null 2>&1; then
    echo 'POC 1B installer unexpectedly succeeded' >&2
    exit 1
  fi
}

unsafe_root=$fixture/unsafe-root
cp -R "$baseline" "$unsafe_root"
if MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ROOT=$unsafe_root \
  MISTER_REMOTE_ALLOWED_TEST_ROOT=$baseline \
    sh "$installer" binary-kernel "$binary_archive" >/dev/null 2>&1; then
  echo 'installer accepted a test root outside its explicit fixture' >&2
  exit 1
fi
if MISTER_REMOTE_TEST_MODE=1 MISTER_REMOTE_ALLOWED_TEST_ROOT=$baseline \
  sh "$installer" binary-kernel "$binary_archive" >/dev/null 2>&1; then
  echo 'installer accepted an empty test root' >&2
  exit 1
fi

missing_asset=$fixture/missing-asset
cp -R "$baseline" "$missing_asset"
rm "$missing_asset/media/fat/menu.rbf"
expect_install_failure "$missing_asset" binary-kernel "$binary_archive"

changed_kernel=$fixture/changed-kernel
cp -R "$baseline" "$changed_kernel"
write_file "$changed_kernel/media/fat/linux/zImage_dtb" changed-kernel
expect_install_failure "$changed_kernel" binary-kernel "$binary_archive"

missing_output_stage=$fixture/missing-output/poc1b
mkdir -p "$missing_output_stage"
cp "$stage/poc1a.lock.toml" "$missing_output_stage/poc1a.lock.toml"
cp "$stage/linux.img" "$missing_output_stage/linux.img"
printf '%s\n' 'format = 1' > "$missing_output_stage/poc1b.lock.toml"
missing_output_archive=$fixture/missing-output.tar.gz
COPYFILE_DISABLE=1 tar -czf "$missing_output_archive" -C "$fixture/missing-output" \
  poc1b/poc1a.lock.toml poc1b/poc1b.lock.toml poc1b/linux.img
missing_output_root=$fixture/missing-output-root
cp -R "$baseline" "$missing_output_root"
expect_install_failure "$missing_output_root" binary-kernel "$missing_output_archive"

tainted_stage=$fixture/tainted
cp -R "$fixture/stage" "$tainted_stage"
write_file "$tainted_stage/poc1b/secret.txt" 'token = "must-not-install"'
write_file "$tainted_stage/poc1b/game.sfc" rom-data
tainted_archive=$fixture/tainted.tar.gz
COPYFILE_DISABLE=1 tar -czf "$tainted_archive" -C "$tainted_stage" \
  poc1b/poc1a.lock.toml poc1b/poc1b.lock.toml poc1b/linux.img \
  poc1b/secret.txt poc1b/game.sfc
tainted_root=$fixture/tainted-root
cp -R "$baseline" "$tainted_root"
expect_install_failure "$tainted_root" binary-kernel "$tainted_archive"

traversal_archive=$fixture/traversal.tar.gz
python3 - "$traversal_archive" <<'PY'
import io
import sys
import tarfile

with tarfile.open(sys.argv[1], "w:gz") as archive:
    member = tarfile.TarInfo("../escape")
    payload = b"escape\n"
    member.size = len(payload)
    archive.addfile(member, io.BytesIO(payload))
PY
traversal_root=$fixture/traversal-root
cp -R "$baseline" "$traversal_root"
expect_install_failure "$traversal_root" binary-kernel "$traversal_archive"
test ! -e "$traversal_root/escape"

interrupted_root=$fixture/interrupted-root
cp -R "$baseline" "$interrupted_root"
stock_root_sha=$(sha256_file "$interrupted_root/media/fat/linux/linux.img")
if ( MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME=1 \
  install_fixture "$interrupted_root" binary-kernel "$binary_archive" ) \
    >/dev/null 2>&1; then
  echo 'interrupted installer unexpectedly succeeded' >&2
  exit 1
fi
test "$(sha256_file "$interrupted_root/media/fat/linux/linux.img")" = \
  "$stock_root_sha"

binary_root=$fixture/binary-root
cp -R "$baseline" "$binary_root"
install_fixture "$binary_root" binary-kernel "$binary_archive"
test "$(sha256_file "$binary_root/media/fat/linux/linux.img")" = \
  "$(sha256_file "$stage/linux.img")"
test "$(sha256_file "$binary_root/media/fat/linux/zImage_dtb")" = \
  "$(sha256_file "$fat/linux/zImage_dtb")"
test -f "$binary_root/media/fat/linux/linux.img.pre-poc1b"
test -f "$binary_root/media/fat/linux/zImage_dtb.pre-poc1b"
install_fixture "$binary_root" binary-kernel "$binary_archive"

write_file "$binary_root/media/fat/linux/linux.img.pre-poc1b" tampered-backup
expect_install_failure "$binary_root" binary-kernel "$binary_archive"

checkpoint2_bad_root=$fixture/checkpoint2-bad-root
cp -R "$baseline" "$checkpoint2_bad_root"
install_fixture "$checkpoint2_bad_root" binary-kernel "$binary_archive"
write_file "$checkpoint2_bad_root/media/fat/linux/linux.img" changed-root
expect_install_failure "$checkpoint2_bad_root" source-kernel "$source_archive"

source_root=$fixture/source-root
cp -R "$baseline" "$source_root"
install_fixture "$source_root" binary-kernel "$binary_archive"
install_fixture "$source_root" source-kernel "$source_archive"
test "$(sha256_file "$source_root/media/fat/linux/zImage_dtb")" = \
  "$(sha256_file "$stage/zImage_dtb")"
installed_modules=$source_root/media/fat/linux/modules.poc1b/5.15.1-MiSTer/modules.tar.gz
test "$(sha256_file "$installed_modules")" = "$modules_sha"

missing_backup_root=$fixture/missing-backup-root
cp -R "$source_root" "$missing_backup_root"
rm "$missing_backup_root/media/fat/linux/zImage_dtb.pre-poc1b"
if MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ALLOWED_TEST_VOLUME=$missing_backup_root/media/fat \
    sh "$restore" "$missing_backup_root/media/fat" >/dev/null 2>&1; then
  echo 'offline restore accepted a missing backup' >&2
  exit 1
fi

root_backup_sha=$(sha256_file "$source_root/media/fat/linux/linux.img.pre-poc1b")
kernel_backup_sha=$(sha256_file "$source_root/media/fat/linux/zImage_dtb.pre-poc1b")
MISTER_REMOTE_TEST_MODE=1 \
MISTER_REMOTE_ALLOWED_TEST_VOLUME=$source_root/media/fat \
  sh "$restore" "$source_root/media/fat"
test "$(sha256_file "$source_root/media/fat/linux/linux.img")" = \
  "$root_backup_sha"
test "$(sha256_file "$source_root/media/fat/linux/zImage_dtb")" = \
  "$kernel_backup_sha"
