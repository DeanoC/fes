#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${NATIVE_RUNTIME_INPUT_LOCK:-${FOGCAST_DIR:?FOGCAST_DIR is required}/build/native-runtime.inputs.lock.toml}
cache=${NATIVE_RUNTIME_CACHE:-$repo_root/build/cache/target-image/native}
"$repo_root/scripts/native-extra-cores.sh" validate
native_mode=${NATIVE_RUNTIME_MODE:-package-only}
[ "$native_mode" = package-only ] || {
  printf '%s\n' 'fetch-native-runtime-inputs: native runtime mode must be package-only' >&2
  exit 2
}

if [ "$native_mode" = package-only ]; then
  selector_bin=${TARGET_IMAGE_LOCK_BIN:-$repo_root/bin/target-image-lock-linux-amd64}
  [ -x "$selector_bin" ] || {
    printf 'fetch-native-runtime-inputs: selector is not executable: %s\n' "$selector_bin" >&2
    exit 2
  }
  [ -f "$lock" ] || {
    printf '%s\n' 'fetch-native-runtime-inputs: lock is not a regular file' >&2
    exit 2
  }
  read_package_lock_value() {
    package_section=$1
    package_key=$2
    awk -v wanted_section="$package_section" -v wanted_key="$package_key" '
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
        exit
      }
    ' "$lock"
  }
  format=$(read_package_lock_value '' format)
  repository=$(read_package_lock_value idle_rbf repository)
  revision=$(read_package_lock_value idle_rbf commit)
  source_path=$(read_package_lock_value idle_rbf path)
  expected_sha=$(read_package_lock_value idle_rbf sha256)
  expected_size=$(read_package_lock_value idle_rbf size)
  [ "$format" = 1 ] &&
    [ "$repository" = https://github.com/MiSTer-devel/Distribution_MiSTer ] &&
    [ "$revision" = f7bde4becb452ca28f604ad9802bbed5c6b58e01 ] &&
    [ "$source_path" = menu.rbf ] || {
    printf '%s\n' 'fetch-native-runtime-inputs: idle lock identity is invalid' >&2
    exit 2
  }
  printf '%s\n' "$expected_sha" | grep -Eq '^[0-9a-f]{64}$' || exit 2
  printf '%s\n' "$expected_size" | grep -Eq '^[1-9][0-9]*$' || exit 2
  mkdir -p "$cache"
  temporary=$(mktemp "$cache/.idle.rbf.XXXXXX")
  trap '/bin/rm -f -- "$temporary"' EXIT INT TERM
  url=https://raw.githubusercontent.com/${repository#https://github.com/}/$revision/$source_path
  wget -q -O "$temporary" "$url"
  actual_sha=$(sha256sum "$temporary" | awk '{print $1}')
  [ "$actual_sha" = "$expected_sha" ] || {
    printf '%s\n' 'fetch-native-runtime-inputs: downloaded idle SHA-256 does not match the lock' >&2
    exit 1
  }
  actual_size=$(wc -c <"$temporary" | tr -d ' ')
  [ "$actual_size" = "$expected_size" ] || {
    printf '%s\n' 'fetch-native-runtime-inputs: downloaded idle size does not match the lock' >&2
    exit 1
  }
  mv "$temporary" "$cache/idle.rbf"
  chmod 0444 "$cache/idle.rbf"
  trap - EXIT INT TERM
  "$repo_root/scripts/native-extra-cores.sh" fetch "$cache"
  exit 0
fi
