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

verify_lexical_volume_path() {
  lexical_volume=$1
  case "$lexical_volume" in
    .|..|./*|../*|*/./*|*/.|*/../*|*/..) \
      fail 'volume path contains an unsafe component' ;;
  esac
  case "$lexical_volume" in
    /*)
      checked_path=
      remaining_path=${lexical_volume#/}
      ;;
    *)
      checked_path=.
      remaining_path=$lexical_volume
      ;;
  esac
  while [ -n "$remaining_path" ]; do
    case "$remaining_path" in
      */*)
        path_component=${remaining_path%%/*}
        remaining_path=${remaining_path#*/}
        ;;
      *)
        path_component=$remaining_path
        remaining_path=
        ;;
    esac
    checked_path=$checked_path/$path_component
    [ ! -L "$checked_path" ] || fail 'volume path contains a linked component'
  done
}

volume=$1
while [ "$volume" != / ] && [ "${volume%/}" != "$volume" ]; do
  volume=${volume%/}
done
verify_lexical_volume_path "$volume"
[ -d "$volume" ] || fail 'volume does not exist'
volume=$(CDPATH='' cd -- "$volume" && pwd -P)
test_mode=${MISTER_REMOTE_TEST_MODE:-0}
accepted_dev_root_sha=e038679bc82623b2911ef0e3876233ed95c6b3d546e77320cd6db2992647faa7
poc2_dev_root_sha=3e66d1bba5aeda791b238aa549fbb55c15d06ff30152fdfd29bef5359cd08daa
accepted_kernel_sha=cb66e22edb04a44d883e82f62fa7eeca0d7d2b715b08ab72ad2dc5bc2a3178c5
accepted_poc1a_lock_sha=8ef39d9d603c1a7a7bd20550f8f7c05dfb05d770509065c4f3e3663e81430d99
accepted_poc1b_lock_sha=8b395f61bdab5c9807ded401279eabfa29df20eaf52c03eb6954b9d7dc2641af
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
    accepted_dev_root_sha=${MISTER_REMOTE_EXPECTED_POC1B_DEV_ROOT_SHA256:-}
    poc2_dev_root_sha=${MISTER_REMOTE_EXPECTED_POC2_DEV_ROOT_SHA256:-}
    accepted_kernel_sha=${MISTER_REMOTE_EXPECTED_KERNEL_SHA256:-}
    accepted_poc1a_lock_sha=${MISTER_REMOTE_EXPECTED_POC1A_LOCK_SHA256:-}
    accepted_poc1b_lock_sha=${MISTER_REMOTE_EXPECTED_POC1B_LOCK_SHA256:-}
    accepted_poc2_lock_sha=${MISTER_REMOTE_EXPECTED_POC2_LOCK_SHA256:-}
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

restore_storage_is_safe() {
  [ -d "$volume" ] && [ ! -L "$volume" ] || return 1
  [ -d "$linux" ] && [ ! -L "$linux" ] || return 1
  if [ -n "${linux_physical:-}" ]; then
    current_linux=$(CDPATH='' cd -- "$linux" 2>/dev/null && pwd -P) || return 1
    [ "$current_linux" = "$linux_physical" ] || return 1
  fi
}

verify_restore_storage() {
  restore_storage_is_safe || fail 'restore storage path is missing, linked, or changed'
}

clear_restore_temp() {
  verify_restore_storage
  [ ! -L "$root_temp" ] || fail 'restore temporary path is linked'
  if [ -e "$root_temp" ]; then
    [ -f "$root_temp" ] || fail 'restore temporary path is unsafe'
    (
      CDPATH='' cd -- "$linux"
      [ "$(pwd -P)" = "$linux_physical" ]
      [ -f ./linux.img.poc1b-restore.new ]
      [ ! -L ./linux.img.poc1b-restore.new ]
      rm -f ./linux.img.poc1b-restore.new
    ) || fail 'cannot safely clear restore temporary path'
  fi
  [ ! -e "$root_temp" ] && [ ! -L "$root_temp" ] || \
    fail 'cannot clear restore temporary path'
}

create_restore_temp() {
  verify_restore_storage
  [ ! -e "$root_temp" ] && [ ! -L "$root_temp" ] || \
    fail 'restore temporary path already exists'
  if ! (
    CDPATH='' cd -- "$linux"
    [ "$(pwd -P)" = "$linux_physical" ]
    [ ! -e ./linux.img.poc1b-restore.new ]
    [ ! -L ./linux.img.poc1b-restore.new ]
    umask 077
    set -C
    cat ./linux.img.pre-poc2 > ./linux.img.poc1b-restore.new
  ); then
    fail 'cannot create restore temporary file without following links'
  fi
  verify_restore_storage
  verify_regular "$root_temp" 'temporary POC 1B root restore'
}

for expected_hash in "$accepted_dev_root_sha" "$poc2_dev_root_sha" \
  "$accepted_kernel_sha" "$accepted_poc1a_lock_sha" \
  "$accepted_poc1b_lock_sha" "$accepted_poc2_lock_sha"; do
  valid_sha256 "$expected_hash" || fail 'accepted provenance contains an invalid hash'
done

linux=$volume/linux
[ -d "$linux" ] && [ ! -L "$linux" ] || fail 'volume linux directory is missing or linked'
linux_physical=$(CDPATH='' cd -- "$linux" && pwd -P)
state=$linux/poc2-checkpoint.state
root_backup=$linux/linux.img.pre-poc2
root_target=$linux/linux.img
kernel_target=$linux/zImage_dtb
root_temp=$linux/linux.img.poc1b-restore.new

cleanup() {
  if restore_storage_is_safe; then
    (
      CDPATH='' cd -- "$linux"
      [ "$(pwd -P)" = "$linux_physical" ] || exit 0
      if [ -f ./linux.img.poc1b-restore.new ] && \
        [ ! -L ./linux.img.poc1b-restore.new ]; then
        rm -f ./linux.img.poc1b-restore.new
      fi
    )
  fi
}
verify_restore_storage
clear_restore_temp
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

recorded_backup_sha=$(state_value root_backup_sha256)
recorded_accepted_dev_sha=$(state_value accepted_dev_root_sha256)
recorded_poc2_dev_sha=$(state_value poc2_dev_root_sha256)
recorded_kernel_sha=$(state_value accepted_kernel_sha256)
recorded_poc1a_lock_sha=$(state_value poc1a_lock_sha256)
recorded_poc1b_lock_sha=$(state_value poc1b_lock_sha256)
recorded_poc2_lock_sha=$(state_value poc2_lock_sha256)
for recorded_hash in "$recorded_backup_sha" "$recorded_accepted_dev_sha" \
  "$recorded_poc2_dev_sha" "$recorded_kernel_sha" "$recorded_poc1a_lock_sha" \
  "$recorded_poc1b_lock_sha" "$recorded_poc2_lock_sha"; do
  valid_sha256 "$recorded_hash" || fail 'checkpoint state contains an invalid hash'
done
[ "$recorded_backup_sha" = "$accepted_dev_root_sha" ] || fail 'checkpoint backup identity changed'
[ "$recorded_accepted_dev_sha" = "$accepted_dev_root_sha" ] || fail 'checkpoint POC 1B root identity changed'
[ "$recorded_poc2_dev_sha" = "$poc2_dev_root_sha" ] || fail 'checkpoint POC 2 root identity changed'
[ "$recorded_kernel_sha" = "$accepted_kernel_sha" ] || fail 'checkpoint kernel identity changed'
[ "$recorded_poc1a_lock_sha" = "$accepted_poc1a_lock_sha" ] || fail 'checkpoint POC 1A lock identity changed'
[ "$recorded_poc1b_lock_sha" = "$accepted_poc1b_lock_sha" ] || fail 'checkpoint POC 1B lock identity changed'
[ "$recorded_poc2_lock_sha" = "$accepted_poc2_lock_sha" ] || fail 'checkpoint belongs to another POC 2 lock'
recorded_current_sha=$(state_value current_root_sha256)
case "$(state_value checkpoint)" in
  prepared) [ "$recorded_current_sha" = "$accepted_dev_root_sha" ] || fail 'prepared checkpoint root identity changed' ;;
  installed) [ "$recorded_current_sha" = "$poc2_dev_root_sha" ] || fail 'installed checkpoint root identity changed' ;;
esac
verify_hash "$root_backup" "$accepted_dev_root_sha" 'one-time POC 1B root backup'
verify_hash "$kernel_target" "$accepted_kernel_sha" 'unchanged reproduced kernel'
verify_regular "$root_target" 'current root image'
current_root_sha=$(sha256_file "$root_target")
if [ "$current_root_sha" != "$poc2_dev_root_sha" ] && \
  [ "$current_root_sha" != "$accepted_dev_root_sha" ]; then
  fail 'current root is neither the locked POC 2 nor POC 1B development root'
fi

printf 'POC 1B root backup: %s\n' "$accepted_dev_root_sha"
printf 'Reproduced kernel:  %s\n' "$accepted_kernel_sha"
create_restore_temp
sync
verify_hash "$root_temp" "$accepted_dev_root_sha" 'temporary POC 1B root restore'
if [ "$test_mode" = 1 ] && [ "${MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME:-0}" = 1 ]; then
  fail 'simulated interruption before POC 1B restore rename'
fi
mv -f "$root_temp" "$root_target"
sync
verify_hash "$root_target" "$accepted_dev_root_sha" 'restored POC 1B development root'
verify_hash "$kernel_target" "$accepted_kernel_sha" 'unchanged reproduced kernel'
verify_hash "$root_backup" "$accepted_dev_root_sha" 'retained one-time POC 1B root backup'
printf '%s\n' 'POC 1B development root restored from the POC 2 checkpoint backup'
