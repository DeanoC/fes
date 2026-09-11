#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
image=${1:-$repo/build/output/target-image/dev/linux.img}
target_host=${FOGCAST_TARGET_HOST:-192.168.10.84}
target_user=${FOGCAST_TARGET_USER:-root}
target_password=${FOGCAST_TARGET_PASSWORD:-1}
remote_image=/media/fat/linux/linux.img.new
ssh_options='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'

usage() {
  printf 'usage: deploy-target-image.sh [IMAGE]\n' >&2
  exit 2
}

[ "$#" -le 1 ] || usage
[ -f "$image" ] || {
  printf 'deploy-target-image: image does not exist: %s\n' "$image" >&2
  exit 1
}
[ -s "$image" ] || {
  printf 'deploy-target-image: image is empty: %s\n' "$image" >&2
  exit 1
}
command -v sshpass >/dev/null 2>&1 || {
  printf '%s\n' 'deploy-target-image: sshpass is required' >&2
  exit 2
}

target="$target_user@$target_host"
# MiSTer ships Dropbear's legacy SCP service, not an SFTP subsystem.
sshpass -p "$target_password" scp -O $ssh_options \
  "$image" "$target:$remote_image"
sshpass -p "$target_password" ssh $ssh_options "$target" \
  "test -s $remote_image && mv $remote_image /media/fat/linux/linux.img && sync"

# Reboot deliberately runs as a separate best-effort SSH call: MiSTer closes
# the connection while rebooting, which commonly makes the client return nonzero.
sshpass -p "$target_password" ssh $ssh_options "$target" \
  'reboot >/dev/null 2>&1 &' >/dev/null 2>&1 || true
printf 'deployed %s to %s and requested reboot\n' "$image" "$target"
