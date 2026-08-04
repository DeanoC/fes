#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc2-restore.XXXXXX")
fixture=$(CDPATH='' cd -- "$fixture" && pwd -P)
cleanup() {
  rm -rf "$fixture"
}
trap cleanup EXIT INT TERM

restore=$repo/scripts/restore-poc1b-sd.sh
test -x "$restore"

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

fixture_poc1b_dev_sha=$(printf '%s\n' poc1b-dev-root | shasum -a 256 | awk '{print $1}')
fixture_poc2_dev_sha=$(printf '%s\n' poc2-dev-root | shasum -a 256 | awk '{print $1}')
fixture_kernel_sha=$(printf '%s\n' reproduced-kernel | shasum -a 256 | awk '{print $1}')
fixture_poc1a_lock_sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
fixture_poc1b_lock_sha=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
fixture_poc2_lock_sha=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc

write_file() {
  write_path=$1
  write_value=$2
  mkdir -p "$(dirname "$write_path")"
  printf '%s\n' "$write_value" > "$write_path"
}

create_volume() {
  create_path=$1
  linux=$create_path/linux
  write_file "$linux/linux.img.pre-poc2" poc1b-dev-root
  write_file "$linux/linux.img" poc2-dev-root
  write_file "$linux/zImage_dtb" reproduced-kernel
  write_file "$create_path/fogcast/cache/snes/retained.bin" retained-cache
  cat > "$linux/poc2-checkpoint.state" <<EOF
format=1
checkpoint=installed
root_backup_sha256=$(sha256_file "$linux/linux.img.pre-poc2")
accepted_dev_root_sha256=$(sha256_file "$linux/linux.img.pre-poc2")
poc2_dev_root_sha256=$(sha256_file "$linux/linux.img")
accepted_kernel_sha256=$(sha256_file "$linux/zImage_dtb")
poc1a_lock_sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
poc1b_lock_sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
poc2_lock_sha256=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
current_root_sha256=$(sha256_file "$linux/linux.img")
EOF
}

restore_fixture() {
  restore_volume=$1
  MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ALLOWED_TEST_VOLUME=$restore_volume \
  MISTER_REMOTE_EXPECTED_POC1B_DEV_ROOT_SHA256=$fixture_poc1b_dev_sha \
  MISTER_REMOTE_EXPECTED_POC2_DEV_ROOT_SHA256=$fixture_poc2_dev_sha \
  MISTER_REMOTE_EXPECTED_KERNEL_SHA256=$fixture_kernel_sha \
  MISTER_REMOTE_EXPECTED_POC1A_LOCK_SHA256=$fixture_poc1a_lock_sha \
  MISTER_REMOTE_EXPECTED_POC1B_LOCK_SHA256=$fixture_poc1b_lock_sha \
  MISTER_REMOTE_EXPECTED_POC2_LOCK_SHA256=$fixture_poc2_lock_sha \
    sh "$restore" "$restore_volume"
}

expect_restore_failure() {
  failure_volume=$1
  shift
  if MISTER_REMOTE_TEST_MODE=1 \
    MISTER_REMOTE_ALLOWED_TEST_VOLUME=$failure_volume \
    MISTER_REMOTE_EXPECTED_POC1B_DEV_ROOT_SHA256=$fixture_poc1b_dev_sha \
    MISTER_REMOTE_EXPECTED_POC2_DEV_ROOT_SHA256=$fixture_poc2_dev_sha \
    MISTER_REMOTE_EXPECTED_KERNEL_SHA256=$fixture_kernel_sha \
    MISTER_REMOTE_EXPECTED_POC1A_LOCK_SHA256=$fixture_poc1a_lock_sha \
    MISTER_REMOTE_EXPECTED_POC1B_LOCK_SHA256=$fixture_poc1b_lock_sha \
    MISTER_REMOTE_EXPECTED_POC2_LOCK_SHA256=$fixture_poc2_lock_sha \
      "$@" sh "$restore" "$failure_volume" >/dev/null 2>&1; then
    echo 'POC 1B restore unexpectedly succeeded' >&2
    exit 1
  fi
}

if MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ALLOWED_TEST_VOLUME=$fixture \
    sh "$restore" / >/dev/null 2>&1; then
  echo 'restore accepted filesystem root' >&2
  exit 1
fi

if MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ALLOWED_TEST_VOLUME=$fixture \
    sh "$restore" "$fixture/not-the-volume" >/dev/null 2>&1; then
  echo 'restore accepted an unrelated volume' >&2
  exit 1
fi

valid=$fixture/valid-volume
create_volume "$valid"
backup_before=$(sha256_file "$valid/linux/linux.img.pre-poc2")
kernel_before=$(sha256_file "$valid/linux/zImage_dtb")
cache_before=$(sha256_file "$valid/fogcast/cache/snes/retained.bin")
restore_fixture "$valid"
test "$(sha256_file "$valid/linux/linux.img")" = "$backup_before"
test "$(sha256_file "$valid/linux/zImage_dtb")" = "$kernel_before"
test "$(sha256_file "$valid/fogcast/cache/snes/retained.bin")" = "$cache_before"
test "$(sha256_file "$valid/linux/linux.img.pre-poc2")" = "$backup_before"
restore_fixture "$valid"

interrupted=$fixture/interrupted
create_volume "$interrupted"
if MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ALLOWED_TEST_VOLUME=$interrupted \
  MISTER_REMOTE_EXPECTED_POC1B_DEV_ROOT_SHA256=$fixture_poc1b_dev_sha \
  MISTER_REMOTE_EXPECTED_POC2_DEV_ROOT_SHA256=$fixture_poc2_dev_sha \
  MISTER_REMOTE_EXPECTED_KERNEL_SHA256=$fixture_kernel_sha \
  MISTER_REMOTE_EXPECTED_POC1A_LOCK_SHA256=$fixture_poc1a_lock_sha \
  MISTER_REMOTE_EXPECTED_POC1B_LOCK_SHA256=$fixture_poc1b_lock_sha \
  MISTER_REMOTE_EXPECTED_POC2_LOCK_SHA256=$fixture_poc2_lock_sha \
  MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME=1 \
    sh "$restore" "$interrupted" >/dev/null 2>&1; then
  echo 'interrupted restore unexpectedly succeeded' >&2
  exit 1
fi
test "$(sha256_file "$interrupted/linux/linux.img")" = \
  "$(printf '%s\n' poc2-dev-root | shasum -a 256 | awk '{print $1}')"

forged_state=$fixture/forged-state
create_volume "$forged_state"
write_file "$forged_state/linux/linux.img.pre-poc2" attacker-root
write_file "$forged_state/linux/zImage_dtb" attacker-kernel
forged_backup_sha=$(sha256_file "$forged_state/linux/linux.img.pre-poc2")
forged_kernel_sha=$(sha256_file "$forged_state/linux/zImage_dtb")
sed \
  -e "s/^root_backup_sha256=.*/root_backup_sha256=$forged_backup_sha/" \
  -e "s/^accepted_dev_root_sha256=.*/accepted_dev_root_sha256=$forged_backup_sha/" \
  -e "s/^accepted_kernel_sha256=.*/accepted_kernel_sha256=$forged_kernel_sha/" \
  -e 's/^poc1a_lock_sha256=.*/poc1a_lock_sha256=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd/' \
  -e 's/^poc1b_lock_sha256=.*/poc1b_lock_sha256=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee/' \
  "$forged_state/linux/poc2-checkpoint.state" > "$forged_state/linux/state.forged"
mv "$forged_state/linux/state.forged" "$forged_state/linux/poc2-checkpoint.state"
expect_restore_failure "$forged_state" env

symlink_target=$fixture/symlink-volume-target
symlink_volume=$fixture/symlink-volume
create_volume "$symlink_target"
ln -s "$symlink_target" "$symlink_volume"
expect_restore_failure "$symlink_volume" env
expect_restore_failure "$symlink_volume/" env
expect_restore_failure "$symlink_volume/." env

dot_component_volume=$fixture/dot-component-volume
create_volume "$dot_component_volume"
expect_restore_failure "$dot_component_volume/." env

linked_restore_temp=$fixture/linked-restore-temp
linked_restore_outside=$fixture/linked-restore-outside
create_volume "$linked_restore_temp"
write_file "$linked_restore_outside" must-not-change
linked_restore_before=$(sha256_file "$linked_restore_outside")
ln -s "$linked_restore_outside" \
  "$linked_restore_temp/linux/linux.img.poc1b-restore.new"
expect_restore_failure "$linked_restore_temp" env
test "$(sha256_file "$linked_restore_outside")" = "$linked_restore_before"

for kind in missing-backup tampered-backup linked-backup linked-state linked-linux \
  tampered-current-state unexpected-root unexpected-kernel; do
  volume=$fixture/$kind
  create_volume "$volume"
  case "$kind" in
    missing-backup) rm "$volume/linux/linux.img.pre-poc2" ;;
    tampered-backup) write_file "$volume/linux/linux.img.pre-poc2" tampered ;;
    linked-backup)
      mv "$volume/linux/linux.img.pre-poc2" "$volume/linux/backup.real"
      ln -s backup.real "$volume/linux/linux.img.pre-poc2"
      ;;
    linked-state)
      mv "$volume/linux/poc2-checkpoint.state" "$volume/linux/state.real"
      ln -s state.real "$volume/linux/poc2-checkpoint.state"
      ;;
    linked-linux)
      mv "$volume/linux" "$fixture/restore-linked-linux-outside"
      ln -s "$fixture/restore-linked-linux-outside" "$volume/linux"
      ;;
    tampered-current-state)
      sed 's/^current_root_sha256=.*/current_root_sha256=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd/' \
        "$volume/linux/poc2-checkpoint.state" > "$volume/linux/state.changed"
      mv "$volume/linux/state.changed" "$volume/linux/poc2-checkpoint.state"
      ;;
    unexpected-root) write_file "$volume/linux/linux.img" unrelated-root ;;
    unexpected-kernel) write_file "$volume/linux/zImage_dtb" unrelated-kernel ;;
  esac
  expect_restore_failure "$volume" env
done

printf '%s\n' 'POC 1B offline restore policy passed'
