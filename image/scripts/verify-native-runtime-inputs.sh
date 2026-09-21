#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
native_mode=${NATIVE_RUNTIME_MODE:-package-only}

case "$native_mode" in
  package-only) : ;;
  *)
    printf '%s\n' 'verify-native-runtime-inputs: native runtime mode must be package-only' >&2
    exit 2
    ;;
esac

if [ "$native_mode" = package-only ]; then
  [ "$#" -eq 4 ] || {
    printf '%s\n' 'usage: verify-native-runtime-inputs.sh LOCK RUNTIME_SOURCE IDLE_FILE SPLASH_FILE (package-only)' >&2
    exit 2
  }
  lock=$1
  runtime_source=$2
  idle_file=$3
  splash_file=$4
  [ -f "$lock" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: lock is not a regular file' >&2
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

  verify_locked_rbf_file() {
    verify_section=$1
    verify_file=$2
    verify_label=$3
    expected_sha=$(read_package_lock_value "$verify_section" sha256)
    expected_size=$(read_package_lock_value "$verify_section" size)
    printf '%s\n' "$expected_sha" | grep -Eq '^[0-9a-f]{64}$' || {
      printf '%s\n' "verify-native-runtime-inputs: $verify_label SHA-256 is invalid" >&2
      exit 2
    }
    printf '%s\n' "$expected_size" | grep -Eq '^[1-9][0-9]*$' || {
      printf '%s\n' "verify-native-runtime-inputs: $verify_label size is invalid" >&2
      exit 2
    }
    [ -f "$verify_file" ] && [ ! -L "$verify_file" ] || {
      printf '%s\n' "verify-native-runtime-inputs: $verify_label input is not a regular file" >&2
      exit 1
    }
    actual_sha=$(sha256sum "$verify_file" | awk '{print $1}')
    [ "$actual_sha" = "$expected_sha" ] || {
      printf '%s\n' "verify-native-runtime-inputs: $verify_label SHA-256 does not match the lock" >&2
      exit 1
    }
    actual_size=$(wc -c <"$verify_file" | tr -d ' ')
    [ "$actual_size" = "$expected_size" ] || {
      printf '%s\n' "verify-native-runtime-inputs: $verify_label size does not match the lock" >&2
      exit 1
    }
    verify_mode=$(stat -c %a "$verify_file" 2>/dev/null || true)
    printf '%s\n' "$verify_mode" | grep -Eq '^[0145]{3,4}$' || {
      printf '%s\n' "verify-native-runtime-inputs: $verify_label RBF must not be writable" >&2
      exit 1
    }
    printf '%s\n' "$actual_sha"
  }

  format=$(read_package_lock_value '' format)
  expected_commit=$(read_package_lock_value mister_runtime commit)
  mount_path=$(read_package_lock_value mister_runtime mount_path)
  [ "$format" = 1 ] || {
    printf '%s\n' 'verify-native-runtime-inputs: lock format must be 1' >&2
    exit 2
  }
  splash_destination=$(read_package_lock_value splash_rbf fat_destination)
  [ "$splash_destination" = /menu.rbf ] || {
    printf '%s\n' 'verify-native-runtime-inputs: splash FAT destination must be /menu.rbf' >&2
    exit 2
  }
  install_path=$(read_package_lock_value idle_rbf install_path)
  printf '%s\n' "$expected_commit" | grep -Eq '^[0-9a-f]{40}$' || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime commit is invalid' >&2
    exit 2
  }
  [ "$mount_path" = /runtime-source ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime mount path must be /runtime-source' >&2
    exit 2
  }
  [ "$install_path" = /usr/share/mister-runtime/idle.rbf ] || {
    printf '%s\n' 'verify-native-runtime-inputs: idle install path must be absolute and fixed' >&2
    exit 2
  }
  case "$runtime_source" in
    /*) : ;;
    *)
      printf '%s\n' 'verify-native-runtime-inputs: runtime source must be absolute' >&2
      exit 2
      ;;
  esac
  [ -d "$runtime_source" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime source is not a directory' >&2
    exit 1
  }
  runtime_top=$(git -C "$runtime_source" rev-parse --show-toplevel 2>/dev/null) || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime source is not a Git checkout' >&2
    exit 1
  }
  runtime_top=$(CDPATH='' cd -- "$runtime_top" && pwd -P)
  actual_root=$(CDPATH='' cd -- "$runtime_source" && pwd -P)
  case "$actual_root" in
    "$runtime_top"|"$runtime_top/sources/libmister-runtime") : ;;
    *)
      printf '%s\n' 'verify-native-runtime-inputs: runtime source is not the checkout root or supported module' >&2
      exit 1
      ;;
  esac
  actual_commit=$(git -C "$runtime_source" rev-parse --verify HEAD 2>/dev/null) || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime HEAD is unavailable' >&2
    exit 1
  }
  [ "$actual_commit" = "$expected_commit" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime HEAD does not match the lock' >&2
    exit 1
  }
  [ -z "$(git -C "$runtime_source" status --porcelain --untracked-files=all -- .)" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime checkout is dirty' >&2
    exit 1
  }
  idle_sha=$(verify_locked_rbf_file idle_rbf "$idle_file" idle)
  splash_sha=$(verify_locked_rbf_file splash_rbf "$splash_file" splash)
  "$repo_root/scripts/native-extra-cores.sh" verify \
    "${NATIVE_RUNTIME_CACHE:-$repo_root/build/cache/target-image/native}"
  printf 'runtime_commit=%s\nidle_sha256=%s\nsplash_sha256=%s\n' \
    "$actual_commit" "$idle_sha" "$splash_sha"
  exit 0
fi
