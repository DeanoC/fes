#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)

fail() {
  printf 'install-poc1b: %s\n' "$1" >&2
  exit 1
}

[ "$#" -eq 1 ] || {
  printf 'usage: MISTER_TARGET=user@host install-poc1b.sh binary-kernel|source-kernel\n' >&2
  exit 2
}
checkpoint=$1
case "$checkpoint" in
  binary-kernel|source-kernel) : ;;
  *) fail 'checkpoint must be binary-kernel or source-kernel' ;;
esac

target=${MISTER_TARGET:-}
[ -n "$target" ] || fail 'MISTER_TARGET is required'
printf '%s\n' "$target" | grep -Eq '^[A-Za-z0-9_.@:-]+$' || \
  fail 'MISTER_TARGET contains unsafe characters'

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
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

verify_hash() {
  verify_path=$1
  verify_expected=$2
  verify_label=$3
  [ -f "$verify_path" ] && [ ! -L "$verify_path" ] || \
    fail "missing regular file: $verify_label"
  printf '%s\n' "$verify_expected" | grep -Eq '^[0-9a-f]{64}$' || \
    fail "lock has no valid output hash: $verify_label"
  [ "$(sha256_file "$verify_path")" = "$verify_expected" ] || \
    fail "output hash mismatch: $verify_label"
}

poc1a_lock=$repo/build/sources.poc1a.lock.toml
poc1b_lock=$repo/build/sources.poc1b.lock.toml
dev_image=$repo/build/output/poc1b/dev/linux.img
kernel_dir=$repo/build/output/poc1b/kernel
kernel_image=$kernel_dir/zImage_dtb
modules=$kernel_dir/modules.tar.gz
kernel_manifest=$kernel_dir/manifest.toml

[ -f "$poc1a_lock" ] || fail 'POC 1A accepted lock is missing'
[ -f "$poc1b_lock" ] || fail 'POC 1B source lock is missing'
dev_sha=$(toml_value "$poc1b_lock" outputs dev_rootfs_sha256)
kernel_sha=$(toml_value "$poc1b_lock" outputs reproduced_kernel_sha256)
verify_hash "$dev_image" "$dev_sha" 'development root image'
POC1B_CONTAINER_RUNTIME=${POC1B_CONTAINER_RUNTIME:-docker} \
  "$repo/scripts/verify-poc1b-image.sh" dev \
  "$dev_image" \
  "$repo/build/output/poc1b/dev/manifest.tsv" \
  "$repo/build/output/poc1b/dev/library-report.tsv"
if [ "$checkpoint" = source-kernel ]; then
  verify_hash "$kernel_image" "$kernel_sha" 'reproduced kernel image'
  [ -f "$modules" ] && [ -f "$kernel_manifest" ] || \
    fail 'reproduced modules and manifest are required'
  POC1B_CONTAINER_RUNTIME=${POC1B_CONTAINER_RUNTIME:-docker} \
    "$repo/scripts/verify-poc1b-kernel.sh" "$kernel_dir"
fi

stage=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-package.XXXXXX")
archive=$stage/$checkpoint.tar.gz
cleanup() {
  rm -f "$stage/poc1b/poc1a.lock.toml" \
    "$stage/poc1b/poc1b.lock.toml" "$stage/poc1b/linux.img" \
    "$stage/poc1b/zImage_dtb" "$stage/poc1b/modules.tar.gz" \
    "$stage/poc1b/kernel-manifest.toml" "$archive"
  rmdir "$stage/poc1b" "$stage" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

mkdir "$stage/poc1b"
cp "$poc1a_lock" "$stage/poc1b/poc1a.lock.toml"
cp "$poc1b_lock" "$stage/poc1b/poc1b.lock.toml"
cp "$dev_image" "$stage/poc1b/linux.img"
if [ "$checkpoint" = source-kernel ]; then
  cp "$kernel_image" "$stage/poc1b/zImage_dtb"
  cp "$modules" "$stage/poc1b/modules.tar.gz"
  cp "$kernel_manifest" "$stage/poc1b/kernel-manifest.toml"
fi
"$repo/scripts/scan-poc1b-secrets.sh" "$stage/poc1b"

case "$checkpoint" in
  binary-kernel)
    COPYFILE_DISABLE=1 tar -czf "$archive" -C "$stage" \
      poc1b/poc1a.lock.toml \
      poc1b/poc1b.lock.toml \
      poc1b/linux.img
    ;;
  source-kernel)
    COPYFILE_DISABLE=1 tar -czf "$archive" -C "$stage" \
      poc1b/poc1a.lock.toml \
      poc1b/poc1b.lock.toml \
      poc1b/linux.img \
      poc1b/zImage_dtb \
      poc1b/modules.tar.gz \
      poc1b/kernel-manifest.toml
    ;;
esac

remote_archive=/media/fat/linux/mister-remote-poc1b-$checkpoint.tar.gz
scp "$archive" "$target:$remote_archive"
ssh "$target" sh -s -- "$checkpoint" "$remote_archive" < \
  "$repo/deploy/poc1b/install-target.sh"
