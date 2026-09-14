#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)

test -d "$root/ui/tenfoot"
test -d "$root/ui/kitlauncher"
test ! -e "$root/host/tenfoot"
test ! -e "$root/kitlauncher"

if rg -n 'github.com/DeanoC/FogCast/host/tenfoot|github.com/DeanoC/FogCast/kitlauncher' \
    "$root" --glob '*.go' --glob '!vendor/**'; then
  echo 'legacy UI import path remains' >&2
  exit 1
fi
