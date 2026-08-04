#!/bin/sh
set -eu

fail() {
  printf 'restore-poc1b-sd: %s\n' "$1" >&2
  exit 1
}

[ "$#" -eq 1 ] || {
  printf 'usage: restore-poc1b-sd.sh /Volumes/MISTER\n' >&2
  exit 2
}

volume=$1
[ -d "$volume" ] || fail 'volume does not exist'
volume=$(CDPATH='' cd -- "$volume" && pwd -P)
test_mode=${MISTER_REMOTE_TEST_MODE:-0}
accepted_poc2_lock_sha=67727b050a55c024aaad165b982e5679d48d8b6ea8f8e4c1c0766e48ad1723a7
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
    accepted_poc2_lock_sha=${MISTER_REMOTE_EXPECTED_POC2_LOCK_SHA256:-}
    printf '%s\n' "$accepted_poc2_lock_sha" | grep -Eq '^[0-9a-f]{64}$' || \
      fail 'test POC 2 lock hash is required'
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

valid_sha256() {
  printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{64}$'
}

verify_regular() {
  [ -f "$1" ] && [ ! -L "$1" ] || fail "missing regular file: $2"
}

verify_hash() {
  verify_path=$1
  verify_expected=$2
  verify_label=$3
  valid_sha256 "$verify_expected" || fail "invalid recorded hash: $verify_label"
  verify_regular "$verify_path" "$verify_label"
  [ "$(sha256_file "$verify_path")" = "$verify_expected" ] || fail "hash mismatch: $verify_label"
}

linux=$volume/linux
[ -d "$linux" ] && [ ! -L "$linux" ] || fail 'volume linux directory is missing or linked'
state=$linux/poc2-checkpoint.state
root_backup=$linux/linux.img.pre-poc2
root_target=$linux/linux.img
kernel_target=$linux/zImage_dtb
root_temp=$linux/linux.img.poc1b-restore.new

cleanup() {
  rm -f "$root_temp"
}
trap cleanup EXIT INT TERM

verify_regular "$state" 'POC 2 checkpoint state'
[ "$(wc -l < "$state" | tr -d ' ')" -eq 10 ] || fail 'invalid checkpoint state shape'
for state_key in format checkpoint root_backup_sha256 \
  accepted_dev_root_sha256 poc2_dev_root_sha256 accepted_kernel_sha256 \
  poc1a_lock_sha256 poc1b_lock_sha256 poc2_lock_sha256 current_root_sha256; do
  [ "$(grep -c "^$state_key=" "$state")" -eq 1 ] || fail 'invalid checkpoint state shape'
done
[ "$(state_value format)" = 1 ] || fail 'invalid checkpoint state format'
case "$(state_value checkpoint)" in prepared|installed) : ;; *) fail 'invalid checkpoint state' ;; esac

backup_sha=$(state_value root_backup_sha256)
accepted_dev_sha=$(state_value accepted_dev_root_sha256)
poc2_dev_sha=$(state_value poc2_dev_root_sha256)
kernel_sha=$(state_value accepted_kernel_sha256)
poc1a_lock_sha=$(state_value poc1a_lock_sha256)
poc1b_lock_sha=$(state_value poc1b_lock_sha256)
poc2_lock_sha=$(state_value poc2_lock_sha256)
for recorded_hash in "$backup_sha" "$accepted_dev_sha" "$poc2_dev_sha" "$kernel_sha" \
  "$poc1a_lock_sha" "$poc1b_lock_sha" "$poc2_lock_sha"; do
  valid_sha256 "$recorded_hash" || fail 'checkpoint state contains an invalid hash'
done
[ "$backup_sha" = "$accepted_dev_sha" ] || fail 'checkpoint backup is not the accepted POC 1B root'
[ "$poc2_lock_sha" = "$accepted_poc2_lock_sha" ] || fail 'checkpoint belongs to another POC 2 lock'
recorded_current_sha=$(state_value current_root_sha256)
case "$(state_value checkpoint)" in
  prepared) [ "$recorded_current_sha" = "$accepted_dev_sha" ] || fail 'prepared checkpoint root identity changed' ;;
  installed) [ "$recorded_current_sha" = "$poc2_dev_sha" ] || fail 'installed checkpoint root identity changed' ;;
esac
verify_hash "$root_backup" "$backup_sha" 'one-time POC 1B root backup'
verify_hash "$kernel_target" "$kernel_sha" 'unchanged reproduced kernel'
verify_regular "$root_target" 'current root image'
current_root_sha=$(sha256_file "$root_target")
if [ "$current_root_sha" != "$poc2_dev_sha" ] && [ "$current_root_sha" != "$accepted_dev_sha" ]; then
  fail 'current root is neither the locked POC 2 nor POC 1B development root'
fi

printf 'POC 1B root backup: %s\n' "$backup_sha"
printf 'Reproduced kernel:  %s\n' "$kernel_sha"
rm -f "$root_temp"
cp "$root_backup" "$root_temp"
sync
verify_hash "$root_temp" "$backup_sha" 'temporary POC 1B root restore'
if [ "$test_mode" = 1 ] && [ "${MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME:-0}" = 1 ]; then
  fail 'simulated interruption before POC 1B restore rename'
fi
mv -f "$root_temp" "$root_target"
sync
verify_hash "$root_target" "$accepted_dev_sha" 'restored POC 1B development root'
verify_hash "$kernel_target" "$kernel_sha" 'unchanged reproduced kernel'
verify_hash "$root_backup" "$backup_sha" 'retained one-time POC 1B root backup'
printf '%s\n' 'POC 1B development root restored from the POC 2 checkpoint backup'
