#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${NATIVE_RUNTIME_INPUT_LOCK:-$repo_root/build/native-runtime.inputs.lock.toml}
cache=${NATIVE_RUNTIME_CACHE:-$repo_root/build/cache/target-image/native}

[ -f "$lock" ] || {
  printf '%s\n' 'fetch-native-runtime-inputs: lock is not a regular file' >&2
  exit 2
}

read_lock_value() {
  read_section=$1
  read_key=$2
  awk -v wanted_section="$read_section" -v wanted_key="$read_key" '
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

mkdir -p "$cache"

fetch_locked_rbf() {
  fetch_section=$1
  fixed_repository=$2
  fixed_commit=$3
  fixed_path=$4
  cache_name=$5
  label=$6

  repository=$(read_lock_value "$fetch_section" repository)
  revision=$(read_lock_value "$fetch_section" commit)
  source_path=$(read_lock_value "$fetch_section" path)
  expected_sha=$(read_lock_value "$fetch_section" sha256)
  expected_size=$(read_lock_value "$fetch_section" size)

  [ "$repository" = "$fixed_repository" ] || {
    printf 'fetch-native-runtime-inputs: %s repository does not match the fixed source\n' "$label" >&2
    exit 2
  }
  [ "$revision" = "$fixed_commit" ] || {
    printf 'fetch-native-runtime-inputs: %s commit does not match the fixed source\n' "$label" >&2
    exit 2
  }
  [ "$source_path" = "$fixed_path" ] || {
    printf 'fetch-native-runtime-inputs: %s path does not match the fixed source\n' "$label" >&2
    exit 2
  }
  printf '%s\n' "$expected_sha" | grep -Eq '^[0-9a-f]{64}$' || {
    printf 'fetch-native-runtime-inputs: %s SHA-256 is invalid\n' "$label" >&2
    exit 2
  }
  printf '%s\n' "$expected_size" | grep -Eq '^[1-9][0-9]*$' || {
    printf 'fetch-native-runtime-inputs: %s size is invalid\n' "$label" >&2
    exit 2
  }

  url=https://raw.githubusercontent.com/${repository#https://github.com/}/$revision/$source_path
  temporary=$(mktemp "$cache/.$cache_name.XXXXXX")
  trap '/bin/rm -f -- "$temporary"' EXIT INT TERM
  wget -q -O "$temporary" "$url"

  actual_sha=$(sha256sum "$temporary" | awk '{print $1}')
  [ "$actual_sha" = "$expected_sha" ] || {
    printf 'fetch-native-runtime-inputs: downloaded %s SHA-256 does not match the lock\n' "$label" >&2
    exit 1
  }
  actual_size=$(wc -c <"$temporary" | tr -d ' ')
  [ "$actual_size" = "$expected_size" ] || {
    printf 'fetch-native-runtime-inputs: downloaded %s size does not match the lock\n' "$label" >&2
    exit 1
  }

  /bin/mv "$temporary" "$cache/$cache_name"
  trap - EXIT INT TERM
  printf '%s_sha256=%s\n' "$fetch_section" "$actual_sha"
}

fetch_locked_rbf \
  idle_rbf \
  https://github.com/MiSTer-devel/Distribution_MiSTer \
  f7bde4becb452ca28f604ad9802bbed5c6b58e01 \
  menu.rbf \
  idle.rbf \
  idle

fetch_locked_rbf \
  megadrive_rbf \
  https://github.com/MiSTer-devel/MegaDrive_MiSTer \
  7365a137cfd8fa6f041e964d8b953159c0ec42d9 \
  releases/MegaDrive_20260603.rbf \
  megadrive.rbf \
  'Mega Drive'
