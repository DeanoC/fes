#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${TARGET_IMAGE_LOCK:-$repo_root/build/target-image.sources.lock.toml}
lock_bin=${TARGET_IMAGE_LOCK_BIN:-$repo_root/bin/target-image-lock-linux-amd64}
cache=${TARGET_IMAGE_CACHE:-$repo_root/build/cache/target-image}
test_mode=${TARGET_IMAGE_TEST_MODE:-0}

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
    printf 'fetch-target-image-sources: %s is not a full commit\n' "$require_name" >&2
    exit 2
  fi
}

[ -f "$lock" ] || {
  printf 'fetch-target-image-sources: lock does not exist: %s\n' "$lock" >&2
  exit 2
}
[ -x "$lock_bin" ] || {
  printf 'fetch-target-image-sources: lock verifier is not executable: %s\n' "$lock_bin" >&2
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
  buildroot_repo=${TARGET_IMAGE_BUILDROOT_REPO:?TARGET_IMAGE_BUILDROOT_REPO is required in test mode}
  creator_repo=${TARGET_IMAGE_IMAGE_CREATOR_REPO:?TARGET_IMAGE_IMAGE_CREATOR_REPO is required in test mode}
  kernel_repo=${TARGET_IMAGE_KERNEL_REPO:?TARGET_IMAGE_KERNEL_REPO is required in test mode}
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
      printf 'fetch-target-image-sources: refusing non-Git cache path: %s\n' "$fetch_destination" >&2
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
    printf 'fetch-target-image-sources: %s resolved to %s, expected %s\n' "$fetch_name" "$actual" "$fetch_commit" >&2
    exit 1
  fi
}

fetch_bare_repo() {
  fetch_name=$1
  fetch_url=$2
  fetch_commit=$3
  fetch_destination=$cache/$fetch_name.git
  if [ ! -f "$fetch_destination/HEAD" ]; then
    if [ -e "$fetch_destination" ]; then
      printf 'fetch-target-image-sources: refusing non-Git bare cache path: %s\n' "$fetch_destination" >&2
      exit 1
    fi
    git init --bare "$fetch_destination"
    git --git-dir="$fetch_destination" remote add origin "$fetch_url"
  fi
  test "$(git --git-dir="$fetch_destination" rev-parse --is-bare-repository)" = true || {
    printf 'fetch-target-image-sources: cache is not bare: %s\n' "$fetch_destination" >&2
    exit 1
  }
  git --git-dir="$fetch_destination" remote set-url origin "$fetch_url"
  git --git-dir="$fetch_destination" fetch --depth=1 origin "$fetch_commit"
  git --git-dir="$fetch_destination" update-ref refs/target-image/pinned "$fetch_commit"
  git --git-dir="$fetch_destination" cat-file -e "$fetch_commit^{commit}"
}

mkdir -p "$cache"
fetch_repo buildroot "$buildroot_repo" "$buildroot_commit"
fetch_repo image-creator "$creator_repo" "$creator_commit"
fetch_bare_repo linux-kernel "$kernel_repo" "$kernel_commit"

"$lock_bin" verify-inputs --lock "$lock" --cache "$cache"
printf 'target image sources fetched and verified in %s\n' "$cache"
