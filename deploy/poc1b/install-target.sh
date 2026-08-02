#!/bin/sh
set -eu

fail() {
  printf 'install-poc1b-target: %s\n' "$1" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'usage: install-target.sh binary-kernel|source-kernel PACKAGE.tar.gz' >&2
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

manifest_artifact_value() {
  artifact_value "$1" "$2" "$3"
}

state_value() {
  state_key=$1
  awk -F= -v wanted="$state_key" '$1 == wanted { print substr($0, index($0, "=") + 1); exit }' \
    "$state"
}

verify_regular() {
  [ -f "$1" ] && [ ! -L "$1" ] || fail "missing regular file: $1"
}

verify_hash() {
  verify_path=$1
  verify_expected=$2
  verify_label=$3
  valid_sha256 "$verify_expected" || fail "missing or invalid hash: $verify_label"
  verify_regular "$verify_path"
  verify_actual=$(sha256_file "$verify_path")
  [ "$verify_actual" = "$verify_expected" ] || \
    fail "unexpected hash for $verify_label"
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

atomic_copy() {
  copy_source=$1
  copy_target=$2
  copy_expected=$3
  copy_label=$4
  copy_temp=$copy_target.poc1b.new
  rm -f "$copy_temp"
  mv "$copy_source" "$copy_temp"
  sync_storage
  verify_hash "$copy_temp" "$copy_expected" "$copy_label temporary"
  if [ "$test_mode" = 1 ] && \
     [ "${MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME:-0}" = 1 ]; then
    fail "simulated interruption before rename"
  fi
  mv -f "$copy_temp" "$copy_target"
  sync_storage
  verify_hash "$copy_target" "$copy_expected" "$copy_label"
}

backup_once() {
  backup_current=$1
  backup_path=$2
  verify_regular "$backup_current"
  current_hash=$(sha256_file "$backup_current")
  if [ -e "$backup_path" ]; then
    verify_regular "$backup_path"
    [ "$(sha256_file "$backup_path")" = "$current_hash" ] || \
      fail "existing one-time backup differs: $backup_path"
    return
  fi
  backup_temp=$backup_path.poc1b.new
  rm -f "$backup_temp"
  cp "$backup_current" "$backup_temp"
  sync_storage
  [ "$(sha256_file "$backup_temp")" = "$current_hash" ] || \
    fail "backup verification failed: $backup_path"
  mv "$backup_temp" "$backup_path"
  sync_storage
}

write_state() {
  write_checkpoint=$1
  write_root_hash=$2
  write_kernel_hash=$3
  state_temp=$state.poc1b.new
  {
    printf '%s\n' 'format=1'
    printf 'checkpoint=%s\n' "$write_checkpoint"
    printf 'root_backup_sha256=%s\n' "$root_backup_sha"
    printf 'kernel_backup_sha256=%s\n' "$kernel_backup_sha"
    printf 'current_root_sha256=%s\n' "$write_root_hash"
    printf 'current_kernel_sha256=%s\n' "$write_kernel_hash"
  } > "$state_temp"
  sync_storage
  mv -f "$state_temp" "$state"
  sync_storage
}

stop_runtime() {
  [ -z "$root" ] || return 0
  if [ -f /run/mister-agent-supervisor.pid ]; then
    supervisor_pid=$(cat /run/mister-agent-supervisor.pid)
    case "$supervisor_pid" in
      ''|*[!0-9]*) fail 'invalid agent supervisor PID' ;;
    esac
    kill "$supervisor_pid" 2>/dev/null || true
  fi
  killall mister-agent 2>/dev/null || true
  killall MiSTer 2>/dev/null || true
}

[ "$#" -eq 2 ] || usage
checkpoint=$1
archive=$2
case "$checkpoint" in
  binary-kernel|source-kernel) : ;;
  *) usage ;;
esac

test_mode=${MISTER_REMOTE_TEST_MODE:-0}
root=${MISTER_REMOTE_ROOT:-}
case "$test_mode" in
  0)
    [ -z "$root" ] || fail 'root override is forbidden outside test mode'
    [ "$archive" = "/media/fat/linux/mister-remote-poc1b-$checkpoint.tar.gz" ] || \
      fail 'production package path is not canonical'
    ;;
  1)
    allowed_root=${MISTER_REMOTE_ALLOWED_TEST_ROOT:-}
    [ -n "$root" ] && [ -n "$allowed_root" ] || \
      fail 'test root and explicit allowed fixture are required'
    [ -d "$root" ] && [ -d "$allowed_root" ] || fail 'test root does not exist'
    root=$(CDPATH='' cd -- "$root" && pwd -P)
    allowed_root=$(CDPATH='' cd -- "$allowed_root" && pwd -P)
    [ "$root" = "$allowed_root" ] || fail 'test root is outside explicit fixture'
    case "$root" in /|/tmp|/private/tmp) fail 'test root is too broad' ;; esac
    ;;
  *) fail 'invalid test mode' ;;
esac

verify_regular "$archive"
case "$checkpoint" in
  binary-kernel)
    expected_archive='poc1b/poc1a.lock.toml
poc1b/poc1b.lock.toml
poc1b/linux.img'
    ;;
  source-kernel)
    expected_archive='poc1b/poc1a.lock.toml
poc1b/poc1b.lock.toml
poc1b/linux.img
poc1b/zImage_dtb
poc1b/modules.tar.gz
poc1b/kernel-manifest.toml'
    ;;
esac
archive_members=$(tar -tzf "$archive") || fail 'package is not a readable gzip tar'
[ "$archive_members" = "$expected_archive" ] || \
  fail 'package does not contain the exact checkpoint allowlist'

linux=$root/media/fat/linux
work_parent=$linux
work=$work_parent/.mister-remote-poc1b-install.$$
payload=$work/poc1b
state=$linux/poc1b-checkpoint.state
root_image=$linux/linux.img
kernel_image=$linux/zImage_dtb
root_backup=$linux/linux.img.pre-poc1b
kernel_backup=$linux/zImage_dtb.pre-poc1b

cleanup() {
  rm -f "$linux/linux.img.poc1b.new" "$linux/zImage_dtb.poc1b.new" \
    "$linux/linux.img.pre-poc1b.poc1b.new" \
    "$linux/zImage_dtb.pre-poc1b.poc1b.new" "$state.poc1b.new"
  if [ -n "${payload:-}" ]; then
    rm -f "$payload/poc1a.lock.toml" "$payload/poc1b.lock.toml" \
      "$payload/linux.img" "$payload/zImage_dtb" "$payload/modules.tar.gz" \
      "$payload/kernel-manifest.toml"
    rmdir "$payload" "$work" 2>/dev/null || true
  fi
  if [ "$test_mode" = 0 ]; then
    rm -f "$archive"
  fi
}
trap cleanup EXIT INT TERM

mkdir -p "$work_parent" "$work"
tar -xzf "$archive" -C "$work"
poc1a_lock=$payload/poc1a.lock.toml
poc1b_lock=$payload/poc1b.lock.toml
verify_regular "$poc1a_lock"
verify_regular "$poc1b_lock"
verify_regular "$payload/linux.img"

dev_root_sha=$(toml_value "$poc1b_lock" outputs dev_rootfs_sha256)
reproduced_kernel_sha=$(toml_value "$poc1b_lock" outputs reproduced_kernel_sha256)
valid_sha256 "$dev_root_sha" || fail 'POC 1B lock has no development root output hash'
valid_sha256 "$reproduced_kernel_sha" || \
  fail 'POC 1B lock has no reproduced kernel output hash'
verify_hash "$payload/linux.img" "$dev_root_sha" 'packaged development root'

verify_accepted_artifact main_mister /media/fat/MiSTer
verify_accepted_artifact menu /media/fat/menu.rbf
verify_accepted_artifact megadrive_core /media/fat/_Console/MegaDrive_20260603.rbf
verify_accepted_artifact snes_core /media/fat/_Console/SNES_20260603.rbf
verify_accepted_artifact controller_input_081f_e401_v3.map \
  /media/fat/config/inputs/input_081f_e401_v3.map
accepted_kernel_sha=$(artifact_value "$poc1a_lock" kernel sha256)
[ "$(artifact_value "$poc1a_lock" kernel path)" = /media/fat/linux/zImage_dtb ] || \
  fail 'unexpected accepted kernel path'
valid_sha256 "$accepted_kernel_sha" || fail 'accepted kernel hash is invalid'
verify_regular "$root_image"
verify_regular "$kernel_image"

if [ "$checkpoint" = binary-kernel ]; then
  verify_hash "$kernel_image" "$accepted_kernel_sha" 'accepted binary kernel'
else
  verify_regular "$payload/zImage_dtb"
  verify_regular "$payload/modules.tar.gz"
  verify_regular "$payload/kernel-manifest.toml"
  verify_hash "$payload/zImage_dtb" "$reproduced_kernel_sha" \
    'packaged reproduced kernel'
  release=$(toml_value "$payload/kernel-manifest.toml" '' release)
  [ "$release" = 5.15.1-MiSTer ] || fail 'kernel manifest release is not accepted'
  modules_sha=$(manifest_artifact_value "$payload/kernel-manifest.toml" \
    modules.tar.gz sha256)
  valid_sha256 "$modules_sha" || fail 'kernel manifest has no module hash'
  verify_hash "$payload/modules.tar.gz" "$modules_sha" 'packaged kernel modules'
  module_members=$(tar -tzf "$payload/modules.tar.gz") || fail 'module archive is unreadable'
  printf '%s\n' "$module_members" | \
    grep -Eq '^\./lib/modules/5\.15\.1-MiSTer/.+\.ko(\.xz)?$' || \
    fail 'module archive has no matching kernel module'
  if printf '%s\n' "$module_members" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
    fail 'module archive contains an unsafe path'
  fi
fi

if [ ! -f "$state" ]; then
  backup_once "$root_image" "$root_backup"
  backup_once "$kernel_image" "$kernel_backup"
  root_backup_sha=$(sha256_file "$root_backup")
  kernel_backup_sha=$(sha256_file "$kernel_backup")
  write_state prepared "$(sha256_file "$root_image")" \
    "$(sha256_file "$kernel_image")"
else
  verify_regular "$state"
  [ "$(state_value format)" = 1 ] || fail 'invalid checkpoint state format'
  root_backup_sha=$(state_value root_backup_sha256)
  kernel_backup_sha=$(state_value kernel_backup_sha256)
  verify_hash "$root_backup" "$root_backup_sha" 'one-time root backup'
  verify_hash "$kernel_backup" "$kernel_backup_sha" 'one-time kernel backup'
fi

current_checkpoint=$(state_value checkpoint)
current_root_sha=$(sha256_file "$root_image")
current_kernel_sha=$(sha256_file "$kernel_image")

case "$checkpoint" in
  binary-kernel)
    [ "$current_kernel_sha" = "$accepted_kernel_sha" ] || \
      fail 'checkpoint 1 requires the accepted binary kernel'
    if [ "$current_root_sha" != "$root_backup_sha" ] && \
       [ "$current_root_sha" != "$dev_root_sha" ]; then
      fail 'checkpoint 1 found an unexpected current root image'
    fi
    if [ "$current_root_sha" != "$dev_root_sha" ]; then
      stop_runtime
      atomic_copy "$payload/linux.img" "$root_image" "$dev_root_sha" \
        'development root'
    fi
    write_state binary-kernel "$dev_root_sha" "$accepted_kernel_sha"
    ;;
  source-kernel)
    [ "$current_checkpoint" = binary-kernel ] || \
      fail 'checkpoint 2 requires accepted checkpoint 1 state'
    [ "$current_root_sha" = "$dev_root_sha" ] || \
      fail 'checkpoint 2 requires the accepted development root'
    [ "$current_kernel_sha" = "$accepted_kernel_sha" ] || \
      fail 'checkpoint 2 requires the accepted binary kernel'

    modules_dir=$linux/modules.poc1b/$release
    mkdir -p "$modules_dir"
    if [ -e "$modules_dir/modules.tar.gz" ]; then
      verify_hash "$modules_dir/modules.tar.gz" "$modules_sha" \
        'installed versioned kernel modules'
    else
      atomic_copy "$payload/modules.tar.gz" "$modules_dir/modules.tar.gz" \
        "$modules_sha" 'versioned kernel modules'
    fi
    cp "$payload/kernel-manifest.toml" "$modules_dir/manifest.toml.poc1b.new"
    sync_storage
    mv -f "$modules_dir/manifest.toml.poc1b.new" "$modules_dir/manifest.toml"
    sync_storage

    stop_runtime
    atomic_copy "$payload/zImage_dtb" "$kernel_image" \
      "$reproduced_kernel_sha" 'reproduced kernel'
    write_state source-kernel "$dev_root_sha" "$reproduced_kernel_sha"
    ;;
esac

printf 'POC 1B checkpoint installed: %s\n' "$checkpoint"
