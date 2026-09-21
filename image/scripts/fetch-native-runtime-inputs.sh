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
  [ "$format" = 1 ] || {
    printf '%s\n' 'fetch-native-runtime-inputs: lock format must be 1' >&2
    exit 2
  }

  fetch_locked_rbf() {
    fetch_section=$1
    dest=$2
    repository=$(read_package_lock_value "$fetch_section" repository)
    revision=$(read_package_lock_value "$fetch_section" commit)
    source_path=$(read_package_lock_value "$fetch_section" path)
    expected_sha=$(read_package_lock_value "$fetch_section" sha256)
    expected_size=$(read_package_lock_value "$fetch_section" size)
    printf '%s\n' "$expected_sha" | grep -Eq '^[0-9a-f]{64}$' || exit 2
    printf '%s\n' "$expected_size" | grep -Eq '^[1-9][0-9]*$' || exit 2
    if [ -f "$dest" ] && [ ! -L "$dest" ]; then
      actual_sha=$(sha256sum "$dest" | awk '{print $1}')
      actual_size=$(wc -c <"$dest" | tr -d ' ')
      if [ "$actual_sha" = "$expected_sha" ] && [ "$actual_size" = "$expected_size" ]; then
        chmod 0444 "$dest"
        return 0
      fi
    fi
    for sibling in "$cache/idle.rbf" "$cache/splash.rbf"; do
      if [ "$sibling" != "$dest" ] && [ -f "$sibling" ] && [ ! -L "$sibling" ]; then
        sibling_sha=$(sha256sum "$sibling" | awk '{print $1}')
        sibling_size=$(wc -c <"$sibling" | tr -d ' ')
        if [ "$sibling_sha" = "$expected_sha" ]; then
          [ "$sibling_size" = "$expected_size" ] || {
            printf '%s\n' "fetch-native-runtime-inputs: cached $fetch_section size does not match the lock" >&2
            exit 1
          }
          temporary=$(mktemp "$cache/.${dest##*/}.XXXXXX")
          trap '/bin/rm -f -- "$temporary"' EXIT INT TERM
          cp -- "$sibling" "$temporary"
          chmod 0444 "$temporary"
          mv "$temporary" "$dest"
          trap - EXIT INT TERM
          return 0
        fi
      fi
    done
    temporary=$(mktemp "$cache/.${dest##*/}.XXXXXX")
    trap '/bin/rm -f -- "$temporary"' EXIT INT TERM
    case "$repository" in
      https://github.com/*) : ;;
      *)
        printf '%s\n' "fetch-native-runtime-inputs: $fetch_section repository must be a github.com HTTPS URL" >&2
        exit 2
        ;;
    esac
    owner_repo=${repository#https://github.com/}
    owner_repo=${owner_repo%.git}
    printf '%s\n' "$owner_repo" | grep -Eq '^[^/[:space:]]+/[^/[:space:]]+$' || {
      printf '%s\n' "fetch-native-runtime-inputs: $fetch_section repository is not owner/repo" >&2
      exit 2
    }
    github_token=${GITHUB_TOKEN:-${GH_TOKEN:-}}
    if [ -n "$github_token" ]; then
      url=https://api.github.com/repos/$owner_repo/contents/$source_path?ref=$revision
      if ! wget -q --header="Authorization: Bearer $github_token" \
          --header="Accept: application/vnd.github.raw" \
          -O "$temporary" "$url"; then
        printf '%s\n' "fetch-native-runtime-inputs: authenticated GitHub download of $fetch_section failed" >&2
        exit 1
      fi
    else
      url=https://raw.githubusercontent.com/$owner_repo/$revision/$source_path
      if ! wget -q -O "$temporary" "$url"; then
        printf '%s\n' "fetch-native-runtime-inputs: unauthenticated download of $fetch_section failed. Private GitHub pins require GITHUB_TOKEN or GH_TOKEN with contents:read." >&2
        exit 1
      fi
    fi
    actual_sha=$(sha256sum "$temporary" | awk '{print $1}')
    [ "$actual_sha" = "$expected_sha" ] || {
      printf '%s\n' "fetch-native-runtime-inputs: downloaded $fetch_section SHA-256 does not match the lock" >&2
      exit 1
    }
    actual_size=$(wc -c <"$temporary" | tr -d ' ')
    [ "$actual_size" = "$expected_size" ] || {
      printf '%s\n' "fetch-native-runtime-inputs: downloaded $fetch_section size does not match the lock" >&2
      exit 1
    }
    mv "$temporary" "$dest"
    chmod 0444 "$dest"
    trap - EXIT INT TERM
  }

  splash_destination=$(read_package_lock_value splash_rbf fat_destination)
  [ "$splash_destination" = /menu.rbf ] || {
    printf '%s\n' 'fetch-native-runtime-inputs: splash FAT destination must be /menu.rbf' >&2
    exit 2
  }
  idle_install_path=$(read_package_lock_value idle_rbf install_path)
  [ "$idle_install_path" = /usr/share/mister-runtime/idle.rbf ] || {
    printf '%s\n' 'fetch-native-runtime-inputs: idle install path must be absolute and fixed' >&2
    exit 2
  }

  mkdir -p "$cache"
  fetch_locked_rbf idle_rbf "$cache/idle.rbf"
  fetch_locked_rbf splash_rbf "$cache/splash.rbf"
  "$repo_root/scripts/native-extra-cores.sh" fetch "$cache"
  exit 0
fi
