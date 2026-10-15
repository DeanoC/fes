#!/bin/sh
# The provisioned rootfs carries the catalog config both shells boot.
set -eu
image=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
overlay=$image/buildroot/board/fogcast-target/native-rootfs-overlay
config=$overlay/usr/share/fogcast/config.toml
[ -f "$config" ]
grep -q 'id = "sms-main"' "$config"
grep -q 'system = "sms"' "$config"
grep -q 'root = "/media/fat/games/sms"' "$config"
grep -q 'enabled = false' "$config"
grep -q 'state = "/run/fogcast/catalog/state"' "$config"
grep -q 'staging = "/run/fogcast/catalog/staging"' "$config"
if grep -E '^(state|staging) =' "$config" | grep -q '/usr'; then
  printf '%s\n' 'catalog state is under /usr' >&2
  exit 1
fi
if grep -q 'token' "$config" || grep -q 'http' "$config"; then
  printf '%s\n' 'catalog config contains a host or token' >&2
  exit 1
fi
