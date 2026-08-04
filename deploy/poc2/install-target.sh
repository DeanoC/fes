#!/bin/sh
set -eu

fail() {
  printf 'install-poc2-target: %s\n' "$1" >&2
  exit 1
}

usage() {
  printf '%s\n' 'usage: install-target.sh PACKAGE.tar.gz' >&2
  exit 2
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

valid_sha256() {
  printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{64}$'
}

toml_value() {
  value_file=$1
  value_section=$2
  value_key=$3
  awk -v wanted_section="$value_section" -v wanted_key="$value_key" '
    /^\[/ {
      section=$0
      gsub(/^\[|\]$/, "", section)
      next
    }
    section == wanted_section && $0 ~ "^" wanted_key "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$value_file"
}

artifact_value() {
  value_file=$1
  wanted_name=$2
  wanted_field=$3
  awk -v wanted_name="$wanted_name" -v wanted_field="$wanted_field" '
    /^\[\[artifacts\]\]$/ { in_artifact=1; name=""; next }
    in_artifact && /^name[[:space:]]*=/ {
      name=$0
      sub(/^[^=]*=[[:space:]]*/, "", name)
      quote=substr(name, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(name, length(name), 1) == quote) {
        name=substr(name, 2, length(name) - 2)
      }
      next
    }
    in_artifact && name == wanted_name &&
        $0 ~ "^" wanted_field "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$value_file"
}

state_value() {
  state_key=$1
  awk -F= -v wanted="$state_key" '$1 == wanted { print substr($0, index($0, "=") + 1); exit }' \
    "$state"
}

verify_regular() {
  [ -f "$1" ] && [ ! -L "$1" ] || fail "missing regular file: $2"
}

verify_hash() {
  verify_path=$1
  verify_expected=$2
  verify_label=$3
  valid_sha256 "$verify_expected" || fail "missing or invalid hash: $verify_label"
  verify_regular "$verify_path" "$verify_label"
  [ "$(sha256_file "$verify_path")" = "$verify_expected" ] || \
    fail "unexpected hash for $verify_label"
}

storage_chain_is_safe() {
  for storage_directory in "$root/media" "$root/media/fat" "$linux"; do
    [ -d "$storage_directory" ] && [ ! -L "$storage_directory" ] || return 1
  done
  if [ -n "${linux_physical:-}" ]; then
    current_linux=$(CDPATH='' cd -- "$linux" 2>/dev/null && pwd -P) || return 1
    [ "$current_linux" = "$linux_physical" ] || return 1
  fi
}

verify_storage_chain() {
  storage_chain_is_safe || fail 'FAT storage path is missing, linked, or changed'
}

clear_stale_temp() {
  stale_path=$1
  stale_label=$2
  verify_storage_chain
  [ ! -L "$stale_path" ] || fail "linked temporary path: $stale_label"
  if [ -e "$stale_path" ]; then
    [ -f "$stale_path" ] || fail "unsafe temporary path: $stale_label"
    rm -f "$stale_path"
  fi
  [ ! -e "$stale_path" ] && [ ! -L "$stale_path" ] || \
    fail "cannot clear temporary path: $stale_label"
}

create_exclusive_copy() {
  copy_source=$1
  copy_target=$2
  copy_label=$3
  verify_storage_chain
  [ ! -e "$copy_target" ] && [ ! -L "$copy_target" ] || \
    fail "temporary path already exists: $copy_label"
  if ! (
    umask 077
    set -C
    cat "$copy_source" > "$copy_target"
  ); then
    fail "cannot create temporary file without following links: $copy_label"
  fi
  verify_storage_chain
  verify_regular "$copy_target" "$copy_label"
}

cleanup_temp_file() {
  cleanup_path=$1
  if [ -f "$cleanup_path" ] && [ ! -L "$cleanup_path" ]; then
    rm -f "$cleanup_path"
  fi
}

verify_accepted_artifact() {
  accepted_name=$1
  accepted_path=$2
  recorded_path=$(artifact_value "$poc1a_lock" "$accepted_name" path)
  recorded_hash=$(artifact_value "$poc1a_lock" "$accepted_name" sha256)
  [ "$recorded_path" = "$accepted_path" ] || \
    fail "unexpected accepted path for $accepted_name"
  verify_hash "$root$accepted_path" "$recorded_hash" "$accepted_name"
}

sync_storage() {
  sync
}

backup_once() {
  backup_current=$1
  backup_path=$2
  backup_expected=$3
  verify_hash "$backup_current" "$backup_expected" 'accepted POC 1B development root'
  if [ -e "$backup_path" ] || [ -L "$backup_path" ]; then
    verify_hash "$backup_path" "$backup_expected" 'one-time POC 1B root backup'
    return
  fi
  backup_temp=$backup_path.poc2.new
  create_exclusive_copy "$backup_current" "$backup_temp" 'temporary POC 1B root backup'
  sync_storage
  verify_hash "$backup_temp" "$backup_expected" 'temporary POC 1B root backup'
  mv "$backup_temp" "$backup_path"
  sync_storage
  verify_hash "$backup_path" "$backup_expected" 'one-time POC 1B root backup'
}

write_state() {
  write_checkpoint=$1
  write_current_root=$2
  state_temp=$state.poc2.new
  verify_storage_chain
  [ ! -e "$state_temp" ] && [ ! -L "$state_temp" ] || \
    fail 'checkpoint state temporary path already exists'
  if ! (
    umask 077
    set -C
    {
      printf '%s\n' 'format=1'
      printf 'checkpoint=%s\n' "$write_checkpoint"
      printf 'root_backup_sha256=%s\n' "$accepted_dev_root_sha"
      printf 'accepted_dev_root_sha256=%s\n' "$accepted_dev_root_sha"
      printf 'poc2_dev_root_sha256=%s\n' "$poc2_dev_root_sha"
      printf 'accepted_kernel_sha256=%s\n' "$accepted_kernel_sha"
      printf 'poc1a_lock_sha256=%s\n' "$poc1a_lock_sha"
      printf 'poc1b_lock_sha256=%s\n' "$poc1b_lock_sha"
      printf 'poc2_lock_sha256=%s\n' "$poc2_lock_sha"
      printf 'current_root_sha256=%s\n' "$write_current_root"
    } > "$state_temp"
  ); then
    fail 'cannot create checkpoint state without following links'
  fi
  verify_storage_chain
  verify_regular "$state_temp" 'temporary POC 2 checkpoint state'
  sync_storage
  mv -f "$state_temp" "$state"
  sync_storage
}

verify_state() {
  verify_regular "$state" 'POC 2 checkpoint state'
  [ "$(wc -l < "$state" | tr -d ' ')" -eq 10 ] || fail 'invalid checkpoint state shape'
  for state_key in format checkpoint root_backup_sha256 \
    accepted_dev_root_sha256 poc2_dev_root_sha256 accepted_kernel_sha256 \
    poc1a_lock_sha256 poc1b_lock_sha256 poc2_lock_sha256 current_root_sha256; do
    [ "$(grep -c "^$state_key=" "$state")" -eq 1 ] || fail 'invalid checkpoint state shape'
  done
  [ "$(state_value format)" = 1 ] || fail 'invalid checkpoint state format'
  case "$(state_value checkpoint)" in prepared|installed) : ;; *) fail 'invalid checkpoint state' ;; esac
  [ "$(state_value root_backup_sha256)" = "$accepted_dev_root_sha" ] || fail 'checkpoint backup identity changed'
  [ "$(state_value accepted_dev_root_sha256)" = "$accepted_dev_root_sha" ] || fail 'checkpoint POC 1B root identity changed'
  [ "$(state_value poc2_dev_root_sha256)" = "$poc2_dev_root_sha" ] || fail 'checkpoint POC 2 root identity changed'
  [ "$(state_value accepted_kernel_sha256)" = "$accepted_kernel_sha" ] || fail 'checkpoint kernel identity changed'
  [ "$(state_value poc1a_lock_sha256)" = "$poc1a_lock_sha" ] || fail 'checkpoint POC 1A lock identity changed'
  [ "$(state_value poc1b_lock_sha256)" = "$poc1b_lock_sha" ] || fail 'checkpoint POC 1B lock identity changed'
  [ "$(state_value poc2_lock_sha256)" = "$poc2_lock_sha" ] || fail 'checkpoint POC 2 lock identity changed'
  recorded_checkpoint=$(state_value checkpoint)
  recorded_current_root=$(state_value current_root_sha256)
  case "$recorded_checkpoint" in
    prepared) [ "$recorded_current_root" = "$accepted_dev_root_sha" ] || fail 'prepared checkpoint root identity changed' ;;
    installed) [ "$recorded_current_root" = "$poc2_dev_root_sha" ] || fail 'installed checkpoint root identity changed' ;;
  esac
  verify_hash "$root_backup" "$accepted_dev_root_sha" 'one-time POC 1B root backup'
}

process_command() {
  command_pid=$1
  [ -r "$proc_root/$command_pid/cmdline" ] || return 1
  tr '\000' ' ' < "$proc_root/$command_pid/cmdline"
}

stop_supervisor() {
  supervisor_file=$1
  supervisor_label=$2
  supervisor_token_one=$3
  supervisor_token_two=$4
  [ -f "$supervisor_file" ] || return 0
  IFS= read -r supervisor_pid < "$supervisor_file"
  case "$supervisor_pid" in ''|*[!0-9]*) fail "invalid PID for $supervisor_label" ;; esac
  if ! kill -0 "$supervisor_pid" 2>/dev/null; then
    rm -f "$supervisor_file"
    return 0
  fi
  supervisor_command=$(process_command "$supervisor_pid") || fail "cannot identify $supervisor_label process"
  printf '%s\n' "$supervisor_command" | grep -Fq "$supervisor_token_one" || \
    fail "$supervisor_label PID belongs to another process"
  if [ -n "$supervisor_token_two" ]; then
    printf '%s\n' "$supervisor_command" | grep -Fq "$supervisor_token_two" || \
      fail "$supervisor_label PID belongs to another process"
  fi
  kill "$supervisor_pid" 2>/dev/null || fail "cannot stop $supervisor_label"
  stop_wait=5
  while kill -0 "$supervisor_pid" 2>/dev/null && [ "$stop_wait" -gt 0 ]; do
    sleep 1
    stop_wait=$((stop_wait - 1))
  done
  kill -0 "$supervisor_pid" 2>/dev/null && fail "$supervisor_label did not stop"
  rm -f "$supervisor_file"
}

stop_process_name() {
  process_name=$1
  command -v pidof >/dev/null 2>&1 || fail 'pidof is required to verify runtime shutdown'
  killall "$process_name" 2>/dev/null || true
  stop_wait=5
  while pidof "$process_name" >/dev/null 2>&1 && [ "$stop_wait" -gt 0 ]; do
    sleep 1
    stop_wait=$((stop_wait - 1))
  done
  pidof "$process_name" >/dev/null 2>&1 && fail "$process_name did not stop"
}

stop_runtime() {
  stop_supervisor "$root/tmp/mister-agent-supervisor.pid" \
    'POC 1A agent supervisor' start-agent.sh ''
  stop_supervisor "$root/run/mister-agent-supervisor.pid" \
    'POC 1B agent supervisor' mister-supervise mister-agent
  stop_supervisor "$root/run/mister-main-supervisor.pid" \
    'POC 1B Main supervisor' mister-supervise mister-main
  if [ -z "$root" ]; then
    stop_process_name mister-agent
    stop_process_name MiSTer
  fi
}

[ "$#" -eq 1 ] || usage
archive=$1
test_mode=${MISTER_REMOTE_TEST_MODE:-0}
root=${MISTER_REMOTE_ROOT:-}
proc_root=/proc
accepted_poc2_lock_sha=67727b050a55c024aaad165b982e5679d48d8b6ea8f8e4c1c0766e48ad1723a7
case "$test_mode" in
  0)
    [ "$(id -u)" -eq 0 ] || fail 'production installation requires UID 0'
    [ -z "$root" ] || fail 'root override is forbidden outside test mode'
    [ "$archive" = /media/fat/linux/mister-remote-poc2.tar.gz ] || \
      fail 'production package path is not canonical'
    ;;
  1)
    allowed_root=${MISTER_REMOTE_ALLOWED_TEST_ROOT:-}
    [ -n "$root" ] && [ -n "$allowed_root" ] || fail 'test root and explicit allowed fixture are required'
    [ -d "$root" ] && [ -d "$allowed_root" ] || fail 'test root does not exist'
    root=$(CDPATH='' cd -- "$root" && pwd -P)
    allowed_root=$(CDPATH='' cd -- "$allowed_root" && pwd -P)
    [ "$root" = "$allowed_root" ] || fail 'test root is outside explicit fixture'
    case "$root" in /|/tmp|/private/tmp) fail 'test root is too broad' ;; esac
    proc_root=$root/proc
    accepted_poc2_lock_sha=${MISTER_REMOTE_EXPECTED_POC2_LOCK_SHA256:-}
    valid_sha256 "$accepted_poc2_lock_sha" || fail 'test POC 2 lock hash is required'
    ;;
  *) fail 'invalid test mode' ;;
esac

linux=$root/media/fat/linux
verify_storage_chain
linux_physical=$(CDPATH='' cd -- "$linux" && pwd -P)
verify_storage_chain
verify_regular "$archive" 'POC 2 package'
expected_archive='poc2/poc1a.lock.toml
poc2/poc1b.lock.toml
poc2/poc2.lock.toml
poc2/linux.img'
archive_members=$(gzip -dc "$archive" | tar -tf -) || fail 'package is not a readable gzip tar'
[ "$archive_members" = "$expected_archive" ] || fail 'package does not contain the exact POC 2 allowlist'

work=$linux/.mister-remote-poc2-install
payload=$work/poc2
state=$linux/poc2-checkpoint.state
root_image=$linux/linux.img
kernel_image=$linux/zImage_dtb
root_backup=$linux/linux.img.pre-poc2
root_temp=$linux/linux.img.poc2.new

cleanup_work() {
  cleanup_temp_file "$root_temp"
  cleanup_temp_file "$root_backup.poc2.new"
  cleanup_temp_file "$state.poc2.new"
  if [ -n "${payload:-}" ] && [ -d "$payload" ] && [ ! -L "$payload" ]; then
    rm -f "$payload/poc1a.lock.toml" "$payload/poc1b.lock.toml" \
      "$payload/poc2.lock.toml" "$payload/linux.img"
    rmdir "$payload" 2>/dev/null || true
  fi
  if [ -n "${work:-}" ] && [ -d "$work" ] && [ ! -L "$work" ]; then
    rmdir "$work" 2>/dev/null || true
  fi
}
cleanup_archive() {
  [ "$test_mode" = 1 ] && return
  (
    CDPATH='' cd -- "$linux" 2>/dev/null || exit 0
    [ "$(pwd -P)" = "$linux_physical" ] || exit 0
    [ ! -L ./mister-remote-poc2.tar.gz ] || exit 0
    if [ -e ./mister-remote-poc2.tar.gz ]; then
      [ -f ./mister-remote-poc2.tar.gz ] || exit 0
      rm -f ./mister-remote-poc2.tar.gz
    fi
  )
}
cleanup() {
  if storage_chain_is_safe; then
    cleanup_work
    cleanup_archive
  fi
}
trap cleanup EXIT INT TERM

verify_storage_chain
clear_stale_temp "$root_temp" 'POC 2 root replacement'
clear_stale_temp "$root_backup.poc2.new" 'POC 1B root backup'
clear_stale_temp "$state.poc2.new" 'POC 2 checkpoint state'

if [ -e "$work" ] || [ -L "$work" ]; then
  [ -d "$work" ] && [ ! -L "$work" ] || fail 'POC 2 staging path is not an exact directory'
  unexpected=$(find "$work" -mindepth 1 \
    ! -path "$payload" \
    ! -path "$payload/poc1a.lock.toml" \
    ! -path "$payload/poc1b.lock.toml" \
    ! -path "$payload/poc2.lock.toml" \
    ! -path "$payload/linux.img" \
    -print -quit)
  [ -z "$unexpected" ] || fail 'POC 2 staging directory contains unexpected entries'
  cleanup_work
fi
mkdir "$work"
[ -d "$work" ] && [ ! -L "$work" ] || fail 'cannot create exact POC 2 staging directory'
gzip -dc "$archive" | tar -xf - -C "$work"
[ -d "$payload" ] && [ ! -L "$payload" ] || fail 'package payload directory is unsafe'
poc1a_lock=$payload/poc1a.lock.toml
poc1b_lock=$payload/poc1b.lock.toml
poc2_lock=$payload/poc2.lock.toml
verify_regular "$poc1a_lock" 'POC 1A lock'
verify_regular "$poc1b_lock" 'POC 1B lock'
verify_regular "$poc2_lock" 'POC 2 lock'
verify_regular "$payload/linux.img" 'POC 2 development root'

poc2_lock_sha=$(sha256_file "$poc2_lock")
[ "$poc2_lock_sha" = "$accepted_poc2_lock_sha" ] || fail 'POC 2 lock is not the accepted lock'
poc1a_lock_sha=$(sha256_file "$poc1a_lock")
poc1b_lock_sha=$(sha256_file "$poc1b_lock")
[ "$(toml_value "$poc2_lock" '' format)" = 1 ] || fail 'invalid POC 2 lock format'
[ "$(toml_value "$poc2_lock" base poc1a_lock_sha256)" = "$poc1a_lock_sha" ] || fail 'POC 1A lock does not match POC 2 provenance'
[ "$(toml_value "$poc2_lock" base poc1b_lock_sha256)" = "$poc1b_lock_sha" ] || fail 'POC 1B lock does not match POC 2 provenance'
accepted_dev_root_sha=$(toml_value "$poc2_lock" base accepted_dev_root_sha256)
accepted_kernel_sha=$(toml_value "$poc2_lock" base accepted_kernel_sha256)
poc2_dev_root_sha=$(toml_value "$poc2_lock" outputs dev_rootfs_sha256)
valid_sha256 "$accepted_dev_root_sha" || fail 'POC 2 lock has no accepted POC 1B root hash'
valid_sha256 "$accepted_kernel_sha" || fail 'POC 2 lock has no accepted kernel hash'
valid_sha256 "$poc2_dev_root_sha" || fail 'POC 2 lock has no development root hash'
[ "$(toml_value "$poc1b_lock" outputs dev_rootfs_sha256)" = "$accepted_dev_root_sha" ] || fail 'POC 1B development root provenance changed'
[ "$(toml_value "$poc1b_lock" outputs reproduced_kernel_sha256)" = "$accepted_kernel_sha" ] || fail 'POC 1B kernel provenance changed'
verify_hash "$payload/linux.img" "$poc2_dev_root_sha" 'packaged POC 2 development root'

verify_accepted_artifact main_mister /media/fat/MiSTer
verify_accepted_artifact menu /media/fat/menu.rbf
verify_accepted_artifact megadrive_core /media/fat/_Console/MegaDrive_20260603.rbf
verify_accepted_artifact snes_core /media/fat/_Console/SNES_20260603.rbf
verify_accepted_artifact controller_input_081f_e401_v3.map /media/fat/config/inputs/input_081f_e401_v3.map
verify_hash "$kernel_image" "$accepted_kernel_sha" 'accepted reproduced kernel'
verify_regular "$root_image" 'current root image'

current_root_sha=$(sha256_file "$root_image")
if [ ! -f "$state" ]; then
  [ "$current_root_sha" = "$accepted_dev_root_sha" ] || fail 'POC 2 checkpoint requires the accepted POC 1B development root'
  backup_once "$root_image" "$root_backup" "$accepted_dev_root_sha"
  write_state prepared "$accepted_dev_root_sha"
else
  verify_state
  if [ "$current_root_sha" != "$accepted_dev_root_sha" ] && [ "$current_root_sha" != "$poc2_dev_root_sha" ]; then
    fail 'POC 2 checkpoint found an unexpected current root image'
  fi
fi

if [ "$current_root_sha" != "$poc2_dev_root_sha" ]; then
  stop_runtime
  create_exclusive_copy "$payload/linux.img" "$root_temp" 'temporary POC 2 development root'
  sync_storage
  verify_hash "$root_temp" "$poc2_dev_root_sha" 'temporary POC 2 development root'
  if [ "$test_mode" = 1 ] && [ "${MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME:-0}" = 1 ]; then
    fail 'simulated interruption before POC 2 root rename'
  fi
  mv -f "$root_temp" "$root_image"
  sync_storage
  verify_hash "$root_image" "$poc2_dev_root_sha" 'installed POC 2 development root'
  if [ "$test_mode" = 1 ] && [ "${MISTER_REMOTE_TEST_INTERRUPT_AFTER_RENAME:-0}" = 1 ]; then
    fail 'simulated interruption after POC 2 root rename'
  fi
fi
write_state installed "$poc2_dev_root_sha"
verify_hash "$kernel_image" "$accepted_kernel_sha" 'unchanged reproduced kernel'
verify_hash "$root_backup" "$accepted_dev_root_sha" 'unchanged one-time POC 1B root backup'
printf '%s\n' 'POC 2 development-root checkpoint installed'
