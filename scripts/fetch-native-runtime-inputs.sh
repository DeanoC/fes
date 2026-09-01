#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${NATIVE_RUNTIME_INPUT_LOCK:-$repo_root/build/native-runtime.inputs.lock.toml}
cache=${NATIVE_RUNTIME_CACHE:-$repo_root/build/cache/target-image/native}

[ -f "$lock" ] || {
  printf '%s\n' 'fetch-native-runtime-inputs: lock is not a regular file' >&2
  exit 2
}

read_idle_value() {
  read_key=$1
  awk -v wanted_key="$read_key" '
    /^\[/ {
      section=$0
      gsub(/^\[|\]$/, "", section)
      next
    }
    section == "idle_rbf" && $0 ~ "^[[:space:]]*" wanted_key "[[:space:]]*=" {
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

repository=$(read_idle_value repository)
idle_commit=$(read_idle_value commit)
idle_path=$(read_idle_value path)
expected_sha=$(read_idle_value sha256)
expected_size=$(read_idle_value size)

[ "$repository" = https://github.com/MiSTer-devel/Distribution_MiSTer ] || {
  printf '%s\n' 'fetch-native-runtime-inputs: idle repository does not match the fixed source' >&2
  exit 2
}
[ "$idle_commit" = f7bde4becb452ca28f604ad9802bbed5c6b58e01 ] || {
  printf '%s\n' 'fetch-native-runtime-inputs: idle commit does not match the fixed source' >&2
  exit 2
}
[ "$idle_path" = menu.rbf ] || {
  printf '%s\n' 'fetch-native-runtime-inputs: idle path does not match the fixed source' >&2
  exit 2
}
printf '%s\n' "$expected_sha" | grep -Eq '^[0-9a-f]{64}$' || {
  printf '%s\n' 'fetch-native-runtime-inputs: idle SHA-256 is invalid' >&2
  exit 2
}
printf '%s\n' "$expected_size" | grep -Eq '^[1-9][0-9]*$' || {
  printf '%s\n' 'fetch-native-runtime-inputs: idle size is invalid' >&2
  exit 2
}

url=https://raw.githubusercontent.com/MiSTer-devel/Distribution_MiSTer/$idle_commit/$idle_path
mkdir -p "$cache"
temporary=$(mktemp "$cache/.idle.rbf.XXXXXX")
trap '/bin/rm -f -- "$temporary"' EXIT INT TERM
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

/bin/mv "$temporary" "$cache/idle.rbf"
trap - EXIT INT TERM
printf 'idle_sha256=%s\n' "$actual_sha"
