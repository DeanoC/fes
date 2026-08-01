#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${POC1B_LOCK:-$repo_root/build/sources.poc1b.lock.toml}
lock_bin=${POC1B_LOCK_BIN:-$repo_root/bin/poc1b-lock-linux-amd64}
cache=${POC1B_CACHE:-$repo_root/build/cache/poc1b}
test_mode=${POC1B_TEST_MODE:-0}

read_key() {
  read_section=$1
  read_key_name=$2
  awk -v wanted_section="$read_section" -v wanted_key="$read_key_name" '
    /^\[/ { section=$0; gsub(/^\[|\]$/, "", section); next }
    section == wanted_section && $0 ~ "^" wanted_key "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) && substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$lock"
}

require_commit() {
  require_name=$1
  require_value=$2
  if ! printf '%s\n' "$require_value" | grep -Eq '^[0-9a-f]{40}$'; then
    printf 'fetch-poc1b-sources: %s is not a full commit\n' "$require_name" >&2
    exit 2
  fi
}

[ -f "$lock" ] || {
  printf 'fetch-poc1b-sources: lock does not exist: %s\n' "$lock" >&2
  exit 2
}
[ -x "$lock_bin" ] || {
  printf 'fetch-poc1b-sources: lock verifier is not executable: %s\n' "$lock_bin" >&2
  exit 2
}

buildroot_commit=$(read_key buildroot commit)
creator_commit=$(read_key image_creator commit)
kernel_commit=$(read_key kernel commit)
require_commit buildroot.commit "$buildroot_commit"
require_commit image_creator.commit "$creator_commit"
require_commit kernel.commit "$kernel_commit"

buildroot_repo=https://github.com/buildroot/buildroot.git
creator_repo=https://github.com/MiSTer-devel/Linux_Image_creator_MiSTer.git
kernel_repo=https://github.com/MiSTer-devel/Linux-Kernel_MiSTer.git

if [ "$test_mode" = 1 ]; then
  buildroot_repo=${POC1B_BUILDROOT_REPO:?POC1B_BUILDROOT_REPO is required in test mode}
  creator_repo=${POC1B_IMAGE_CREATOR_REPO:?POC1B_IMAGE_CREATOR_REPO is required in test mode}
  kernel_repo=${POC1B_KERNEL_REPO:?POC1B_KERNEL_REPO is required in test mode}
else
  test "$buildroot_commit" = 004a792dcf10e6c474070c9571f7504411e786cc
  test "$creator_commit" = 8aba321b2162e54b56522aa30758b22d97eec8da
  test "$kernel_commit" = d7adb20b4ca595838289406c083fff78f004a8c3
fi

fetch_repo() {
  fetch_name=$1
  fetch_url=$2
  fetch_commit=$3
  fetch_destination=$cache/$fetch_name
  if [ ! -d "$fetch_destination/.git" ]; then
    if [ -e "$fetch_destination" ]; then
      printf 'fetch-poc1b-sources: refusing non-Git cache path: %s\n' "$fetch_destination" >&2
      exit 1
    fi
    git clone --filter=blob:none --no-checkout "$fetch_url" "$fetch_destination"
  fi
  git -C "$fetch_destination" remote set-url origin "$fetch_url"
  git -C "$fetch_destination" fetch --depth=1 origin "$fetch_commit"
  git -C "$fetch_destination" checkout --detach --force "$fetch_commit"
  git -C "$fetch_destination" clean -fdx
  actual=$(git -C "$fetch_destination" rev-parse HEAD)
  if [ "$actual" != "$fetch_commit" ]; then
    printf 'fetch-poc1b-sources: %s resolved to %s, expected %s\n' "$fetch_name" "$actual" "$fetch_commit" >&2
    exit 1
  fi
}

mkdir -p "$cache"
fetch_repo buildroot "$buildroot_repo" "$buildroot_commit"
fetch_repo image-creator "$creator_repo" "$creator_commit"
fetch_repo linux-kernel "$kernel_repo" "$kernel_commit"

"$lock_bin" verify-inputs --lock "$lock" --cache "$cache"
printf 'POC 1B sources fetched and verified in %s\n' "$cache"
