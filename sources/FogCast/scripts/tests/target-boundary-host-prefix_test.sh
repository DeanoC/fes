#!/bin/sh
set -eu

# Prove FogCast/host is matched at a package boundary: hostclient is allowed,
# host and host/... are still rejected.

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
guard=$here/target-boundary_test.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

root=$tmp/tree
for dir in \
  protocol corepackage kitlease hostclient \
  targetclient host ui/kitlauncher catalog internal/hostapi \
  internal/agent internal/httpapi internal/mister internal/misterruntime \
  internal/input internal/targetcache internal/applianceupdate internal/flightdiag
do
  mkdir -p "$root/$dir"
done

write_kit_import() {
  printf 'package kitlauncher\n\nimport _ "%s"\n' "$1" >"$root/ui/kitlauncher/client.go"
}

run() {
  name=$1
  want=$2
  set +e
  out=$(FOGCAST_BOUNDARY_ROOT=$root sh "$guard" 2>&1)
  status=$?
  set -e
  if [ "$status" -ne "$want" ]; then
    printf '%s: status %s want %s\n%s\n' "$name" "$status" "$want" "$out" >&2
    exit 1
  fi
  case $want in
    0)
      if printf '%s\n' "$out" | grep -q 'kit launcher still depends on host package'; then
        printf '%s: hostclient import treated as host\n%s\n' "$name" "$out" >&2
        exit 1
      fi
      ;;
    1)
      if ! printf '%s\n' "$out" | grep -q 'kit launcher still depends on host package'; then
        printf '%s: missing host-package rejection\n%s\n' "$name" "$out" >&2
        exit 1
      fi
      if printf '%s\n' "$out" | grep -q hostclient; then
        printf '%s: rejection mentioned hostclient\n%s\n' "$name" "$out" >&2
        exit 1
      fi
      ;;
  esac
}

write_kit_import 'github.com/DeanoC/FogCast/hostclient'
run hostclient 0

write_kit_import 'github.com/DeanoC/FogCast/host'
run host 1

write_kit_import 'github.com/DeanoC/FogCast/host/remote'
run host_subpackage 1
