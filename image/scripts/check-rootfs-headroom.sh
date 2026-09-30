#!/bin/sh
set -eu

if [ "$#" -ne 4 ]; then
  printf '%s\n' 'usage: check-rootfs-headroom.sh BLOCK_COUNT FREE_BLOCKS BLOCK_SIZE CONFIGURED_BYTES' >&2
  exit 2
fi
blocks=$1
free=$2
block_size=$3
configured_bytes=$4
case "$blocks:$free:$block_size:$configured_bytes" in
  *[!0-9:]*|::*|*::*|*::*)
    printf '%s\n' 'check-rootfs-headroom: invalid ext filesystem block usage' >&2
    exit 2 ;;
esac
if [ "$free" -gt "$blocks" ] || [ "$block_size" -eq 0 ] || [ "$configured_bytes" -eq 0 ]; then
  printf '%s\n' 'check-rootfs-headroom: invalid ext filesystem block usage' >&2
  exit 2
fi
used=$(( (blocks - free) * block_size ))
maximum=$(( configured_bytes * 85 / 100 ))
if [ "$used" -gt "$maximum" ]; then
  printf 'verify-target-image: populated rootfs uses %s bytes; maximum is 85%% of configured 128 MiB (%s bytes)\n' "$used" "$maximum" >&2
  exit 1
fi
