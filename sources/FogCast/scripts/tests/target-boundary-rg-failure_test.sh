#!/bin/sh
set -eu

# Bounded proof that both boundary guards fail closed when rg exits 2,
# even against an otherwise valid fixture with every required directory.

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/bin"
printf '%s\n' '#!/bin/sh' 'exit 2' >"$tmp/bin/rg"
chmod +x "$tmp/bin/rg"

root=$tmp/tree
for dir in \
  protocol corepackage kitlease hostclient \
  targetclient host ui/kitlauncher catalog internal/hostapi \
  internal/agent internal/httpapi internal/mister internal/misterruntime \
  internal/input internal/targetcache internal/applianceupdate internal/flightdiag
do
  mkdir -p "$root/$dir"
done

PATH="$tmp/bin:$PATH"
export FOGCAST_BOUNDARY_ROOT=$root

run() {
  name=$1
  script=$2
  set +e
  out=$(sh "$script" 2>&1)
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    printf '%s fail-open: stub rg exit 2 produced status 0\n%s\n' "$name" "$out" >&2
    exit 1
  fi
  if ! printf '%s\n' "$out" | grep -q 'rg failed with exit 2'; then
    printf '%s did not report rg tool failure:\n%s\n' "$name" "$out" >&2
    exit 1
  fi
}

run target-contract-boundary_test.sh "$here/target-contract-boundary_test.sh"
run target-boundary_test.sh "$here/target-boundary_test.sh"
