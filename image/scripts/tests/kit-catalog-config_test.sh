#!/bin/sh
# The provisioned rootfs carries the catalog config both shells boot.
set -eu
image=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
overlay=$image/buildroot/board/fogcast-target/native-rootfs-overlay
config=$overlay/usr/share/fogcast/config.toml
init=$overlay/etc/init.d/S60fogcast-kit
[ -f "$config" ]
grep -q 'id = "sms-main"' "$config"
grep -q 'system = "sms"' "$config"
grep -q 'root = "/media/fat/games/sms"' "$config"
grep -q 'enabled = false' "$config"
if grep -q 'token' "$config" || grep -q 'http' "$config"; then
  printf '%s\n' 'catalog config contains a host or token' >&2
  exit 1
fi
grep -q -- '-catalog-config /usr/share/fogcast/config.toml' "$init"
grep -q -- '--catalog-config /usr/share/fogcast/config.toml' "$init"
if grep 'print-kit-ui' "$init" | grep -q 'catalog-config'; then
  printf '%s\n' 'print-kit-ui probe must not take catalog-config' >&2
  exit 1
fi
