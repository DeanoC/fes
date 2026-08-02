#!/bin/sh
set -eu

fail() {
  printf 'restore-poc1a-sd: %s\n' "$1" >&2
  exit 1
}

[ "$#" -eq 1 ] || {
  printf 'usage: restore-poc1a-sd.sh /Volumes/MISTER\n' >&2
  exit 2
}

volume=$1
[ -d "$volume" ] || fail 'volume does not exist'
volume=$(CDPATH='' cd -- "$volume" && pwd -P)
test_mode=${MISTER_REMOTE_TEST_MODE:-0}
case "$test_mode" in
  0)
    [ "$(dirname "$volume")" = /Volumes ] || \
      fail 'offline restore is restricted to a directly mounted /Volumes entry'
    ;;
  1)
    allowed_volume=${MISTER_REMOTE_ALLOWED_TEST_VOLUME:-}
    [ -n "$allowed_volume" ] && [ -d "$allowed_volume" ] || \
      fail 'explicit allowed test volume is required'
    allowed_volume=$(CDPATH='' cd -- "$allowed_volume" && pwd -P)
    [ "$volume" = "$allowed_volume" ] || fail 'test volume is outside fixture'
    case "$volume" in /|/tmp|/private/tmp) fail 'test volume is too broad' ;; esac
    ;;
  *) fail 'invalid test mode' ;;
esac

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

state_value() {
  state_key=$1
  awk -F= -v wanted="$state_key" '$1 == wanted { print substr($0, index($0, "=") + 1); exit }' \
    "$state"
}

verify_regular() {
  [ -f "$1" ] && [ ! -L "$1" ] || fail "missing regular file: $1"
}

verify_backup() {
  verify_path=$1
  verify_expected=$2
  printf '%s\n' "$verify_expected" | grep -Eq '^[0-9a-f]{64}$' || \
    fail 'checkpoint state contains an invalid backup hash'
  verify_regular "$verify_path"
  [ "$(sha256_file "$verify_path")" = "$verify_expected" ] || \
    fail "backup hash mismatch: $verify_path"
}

linux=$volume/linux
state=$linux/poc1b-checkpoint.state
root_backup=$linux/linux.img.pre-poc1b
kernel_backup=$linux/zImage_dtb.pre-poc1b
root_target=$linux/linux.img
kernel_target=$linux/zImage_dtb
root_temp=$linux/linux.img.poc1a-restore.new
kernel_temp=$linux/zImage_dtb.poc1a-restore.new

cleanup() {
  rm -f "$root_temp" "$kernel_temp"
}
trap cleanup EXIT INT TERM

verify_regular "$state"
[ "$(state_value format)" = 1 ] || fail 'invalid checkpoint state format'
root_backup_sha=$(state_value root_backup_sha256)
kernel_backup_sha=$(state_value kernel_backup_sha256)
verify_backup "$root_backup" "$root_backup_sha"
verify_backup "$kernel_backup" "$kernel_backup_sha"

printf 'POC 1A root backup:   %s\n' "$root_backup_sha"
printf 'POC 1A kernel backup: %s\n' "$kernel_backup_sha"

rm -f "$root_temp" "$kernel_temp"
cp "$root_backup" "$root_temp"
cp "$kernel_backup" "$kernel_temp"
sync
verify_backup "$root_temp" "$root_backup_sha"
verify_backup "$kernel_temp" "$kernel_backup_sha"

if [ "$test_mode" = 1 ] && \
   [ "${MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME:-0}" = 1 ]; then
  fail 'simulated interruption before restore rename'
fi

mv -f "$root_temp" "$root_target"
sync
mv -f "$kernel_temp" "$kernel_target"
sync
verify_backup "$root_target" "$root_backup_sha"
verify_backup "$kernel_target" "$kernel_backup_sha"
printf '%s\n' 'POC 1A root and kernel restored from one-time backups'
