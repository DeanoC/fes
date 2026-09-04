#!/bin/sh
set -eu

target=${1:?TARGET_DIR is required}
lock=${NATIVE_RUNTIME_INPUT_LOCK:-/work/build/native-runtime.inputs.lock.toml}
idle_input=${NATIVE_RUNTIME_IDLE_FILE:-/work/build/cache/target-image/native/idle.rbf}
megadrive_input=${NATIVE_RUNTIME_MEGADRIVE_FILE:-/work/build/cache/target-image/native/megadrive.rbf}

read_lock_value() {
  read_section=$1
  read_key=$2
  /usr/bin/awk -v wanted_section="$read_section" -v wanted_key="$read_key" '
    /^\[/ {
      section=$0
      gsub(/^\[|\]$/, "", section)
      next
    }
    section == wanted_section && $0 ~ "^[[:space:]]*" wanted_key "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
    }
  ' "$lock"
}

case "$target" in
  /*) : ;;
  *)
    printf 'native-post-build: TARGET_DIR must be absolute: %s\n' "$target" >&2
    exit 2
    ;;
esac
target=$(CDPATH='' cd -- "$target" && pwd -P)

[ -x "$target/usr/sbin/mister-runtime" ] || {
  printf '%s\n' 'native-post-build: mister-runtime is missing or not executable' >&2
  exit 1
}
[ -x "$target/usr/sbin/mister-agent" ] || {
  printf '%s\n' 'native-post-build: mister-agent is missing or not executable' >&2
  exit 1
}

runtime_commit=$(read_lock_value mister_runtime commit)
idle_repository=$(read_lock_value idle_rbf repository)
idle_commit=$(read_lock_value idle_rbf commit)
idle_path=$(read_lock_value idle_rbf path)
idle_sha=$(read_lock_value idle_rbf sha256)
idle_size=$(read_lock_value idle_rbf size)
idle_install_path=$(read_lock_value idle_rbf install_path)
megadrive_repository=$(read_lock_value megadrive_rbf repository)
megadrive_commit=$(read_lock_value megadrive_rbf commit)
megadrive_path=$(read_lock_value megadrive_rbf path)
megadrive_sha=$(read_lock_value megadrive_rbf sha256)
megadrive_size=$(read_lock_value megadrive_rbf size)
megadrive_install_path=$(read_lock_value megadrive_rbf install_path)

[ "$idle_install_path" = /usr/share/mister-runtime/idle.rbf ] || {
  printf '%s\n' 'native-post-build: idle install path differs from image policy' >&2
  exit 1
}
[ "$megadrive_install_path" = /usr/share/mister-runtime/cores/megadrive.rbf ] || {
  printf '%s\n' 'native-post-build: Mega Drive install path differs from image policy' >&2
  exit 1
}
[ "$(/usr/bin/sha256sum "$idle_input" | /usr/bin/awk '{print $1}')" = "$idle_sha" ] || {
  printf '%s\n' 'native-post-build: idle input digest differs from the lock' >&2
  exit 1
}
[ "$(/usr/bin/wc -c < "$idle_input" | /usr/bin/tr -d ' ')" = "$idle_size" ] || {
  printf '%s\n' 'native-post-build: idle input size differs from the lock' >&2
  exit 1
}
[ "$(/usr/bin/sha256sum "$megadrive_input" | /usr/bin/awk '{print $1}')" = "$megadrive_sha" ] || {
  printf '%s\n' 'native-post-build: Mega Drive input digest differs from the lock' >&2
  exit 1
}
[ "$(/usr/bin/wc -c < "$megadrive_input" | /usr/bin/tr -d ' ')" = "$megadrive_size" ] || {
  printf '%s\n' 'native-post-build: Mega Drive input size differs from the lock' >&2
  exit 1
}

/bin/rm -f "$target/etc/init.d/S40mister-main" \
  "$target/usr/sbin/mister-disable-menu-blanking"
/bin/mkdir -p "$target/usr/share/mister-runtime"
/usr/bin/install -m 0644 \
  "$idle_input" \
  "$target/usr/share/mister-runtime/idle.rbf"
/bin/mkdir -p "$target/usr/share/mister-runtime/cores"
/usr/bin/install -m 0644 \
  "$megadrive_input" \
  "$target/usr/share/mister-runtime/cores/megadrive.rbf"

installed_idle=$target/usr/share/mister-runtime/idle.rbf
installed_megadrive=$target/usr/share/mister-runtime/cores/megadrive.rbf
[ -f "$installed_idle" ] && [ ! -L "$installed_idle" ] || {
  printf '%s\n' 'native-post-build: installed idle RBF must be a regular non-symlink file' >&2
  exit 1
}
[ -f "$installed_megadrive" ] && [ ! -L "$installed_megadrive" ] || {
  printf '%s\n' 'native-post-build: installed Mega Drive RBF must be a regular non-symlink file' >&2
  exit 1
}
[ "$(/usr/bin/sha256sum "$installed_idle" | /usr/bin/awk '{print $1}')" = "$idle_sha" ]
[ "$(/usr/bin/wc -c < "$installed_idle" | /usr/bin/tr -d ' ')" = "$idle_size" ]
[ "$(/usr/bin/sha256sum "$installed_megadrive" | /usr/bin/awk '{print $1}')" = "$megadrive_sha" ]
[ "$(/usr/bin/wc -c < "$installed_megadrive" | /usr/bin/tr -d ' ')" = "$megadrive_size" ]
/bin/chmod 0755 "$target/etc/init.d/S40mister-runtime" \
  "$target/etc/init.d/S50mister-agent"

build_inputs="$target/usr/share/mister-runtime/build-inputs"
mister_agent_sha=$(/usr/bin/sha256sum "$target/usr/sbin/mister-agent" | /usr/bin/awk '{print $1}')
{
  printf 'format=1\n'
  printf 'mister_runtime_commit=%s\n' "$runtime_commit"
  printf 'mister_agent_sha256=%s\n' "$mister_agent_sha"
  printf 'idle_repository=%s\n' "$idle_repository"
  printf 'idle_commit=%s\n' "$idle_commit"
  printf 'idle_path=%s\n' "$idle_path"
  printf 'idle_sha256=%s\n' "$idle_sha"
  printf 'idle_size=%s\n' "$idle_size"
  printf 'idle_install_path=%s\n' "$idle_install_path"
  printf 'megadrive_repository=%s\n' "$megadrive_repository"
  printf 'megadrive_commit=%s\n' "$megadrive_commit"
  printf 'megadrive_path=%s\n' "$megadrive_path"
  printf 'megadrive_sha256=%s\n' "$megadrive_sha"
  printf 'megadrive_size=%s\n' "$megadrive_size"
  printf 'megadrive_install_path=%s\n' "$megadrive_install_path"
} > "$build_inputs"

rbf_count=$(find "$target" -iname '*.rbf' | /usr/bin/wc -l | /usr/bin/tr -d ' ')
[ "$rbf_count" -eq 2 ] || {
  printf 'native-post-build: expected exactly two installed RBFs, found %s\n' "$rbf_count" >&2
  exit 1
}
