#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${NATIVE_RUNTIME_INPUT_LOCK:-$repo_root/build/native-runtime.inputs.lock.toml}
cache=${NATIVE_RUNTIME_CACHE:-$repo_root/build/cache/target-image/native}
"$repo_root/scripts/native-extra-cores.sh" validate
source=${MEGADRIVE_RBF_SOURCE:-source-built}
bundle=${MEGADRIVE_RBF_BUNDLE:-}
selector_bin=${TARGET_IMAGE_LOCK_BIN:-$repo_root/bin/target-image-lock-linux-amd64}

case "$source" in
  source-built)
    [ -n "$bundle" ] || {
      printf '%s\n' 'fetch-native-runtime-inputs: source-built requires MEGADRIVE_RBF_BUNDLE' >&2
      exit 2
    }
    case "$bundle" in
      /*) : ;;
      *)
        printf '%s\n' 'fetch-native-runtime-inputs: source-built bundle must be absolute' >&2
        exit 2
        ;;
    esac
    [ -d "$bundle" ] && [ ! -L "$bundle" ] || {
      printf '%s\n' 'fetch-native-runtime-inputs: source-built bundle is not a directory' >&2
      exit 2
    }
    bundle=$(CDPATH='' cd -- "$bundle" && pwd -P)
    ;;
  upstream)
    [ -z "$bundle" ] || {
      printf '%s\n' 'fetch-native-runtime-inputs: upstream forbids MEGADRIVE_RBF_BUNDLE' >&2
      exit 2
    }
    ;;
  *)
    printf 'fetch-native-runtime-inputs: unsupported Mega Drive source: %s\n' "$source" >&2
    exit 2
    ;;
esac

[ -x "$selector_bin" ] || {
  printf 'fetch-native-runtime-inputs: selector is not executable: %s\n' "$selector_bin" >&2
  exit 2
}

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

  destination=${7:-$cache/$cache_name}
  /bin/mv "$temporary" "$destination"
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

case "$source" in
  source-built)
    "$selector_bin" select-megadrive \
      --source source-built \
      --bundle "$bundle" \
      --upstream-lock "$lock" \
      --cache "$cache" \
      --output "$cache/megadrive.selection.toml"
    ;;
  upstream)
    upstream_temporary=$(mktemp "$cache/.megadrive-upstream.XXXXXX")
    fetch_locked_rbf \
      megadrive_rbf \
      https://github.com/MiSTer-devel/MegaDrive_MiSTer \
      7365a137cfd8fa6f041e964d8b953159c0ec42d9 \
      releases/MegaDrive_20260603.rbf \
      megadrive.rbf \
      'Mega Drive' \
      "$upstream_temporary"
    trap '/bin/rm -f -- "$upstream_temporary"' EXIT INT TERM
    "$selector_bin" select-megadrive \
      --source upstream \
      --artifact "$upstream_temporary" \
      --upstream-lock "$lock" \
      --cache "$cache" \
      --output "$cache/megadrive.selection.toml"
    /bin/rm -f -- "$upstream_temporary"
    trap - EXIT INT TERM
    ;;
esac

"$repo_root/scripts/native-extra-cores.sh" fetch "$cache"
