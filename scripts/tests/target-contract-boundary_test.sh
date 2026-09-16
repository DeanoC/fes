#!/bin/sh
set -eu

# Structural guard for the host/UI <-> target contract seam.
# Public protocol, corepackage, and kitlease directories are required.
# Missing public dirs are RED, never a passing defer. Public contracts must
# not import FogCast internal/. Host, UI, targetclient, catalog, and
# internal/hostapi must not import target implementation packages or the
# target kit-lease manager. Production Go files are scanned for those
# consumer rules; *_test.go files that deliberately exercise target servers
# are not treated as production dependencies. Every Go file, including tests,
# must not import internal/corepackage.

root=${FOGCAST_BOUNDARY_ROOT:-$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)}
failed=0

if ! command -v rg >/dev/null 2>&1; then
  echo 'rg is required' >&2
  exit 1
fi

fail() {
  printf '%s\n' "$1" >&2
  failed=1
}

# Search one path. Prints matches. Returns 0 on matches, 1 on no matches.
# Any rg status other than 0 or 1 is returned to the caller; this function
# must not exit, because command substitution would swallow that exit.
search() {
  pattern=$1
  path=$2
  shift 2
  set +e
  hits=$(rg -n --no-heading --glob '*.go' --glob '!vendor/**' "$@" "$pattern" -- "$path")
  status=$?
  set -e
  case $status in
    0)
      printf '%s\n' "$hits"
      return 0
      ;;
    1)
      return 1
      ;;
    *)
      printf 'rg failed with exit %s scanning %s\n' "$status" "$path" >&2
      return "$status"
      ;;
  esac
}

# Run search outside command-substitution conditionals. Exit >1 fails this
# script; 0 is a match; 1 is no match.
check() {
  message=$1
  shift
  set +e
  hits=$(search "$@")
  status=$?
  set -e
  if [ "$status" -gt 1 ]; then
    exit 1
  fi
  if [ "$status" -eq 0 ]; then
    printf '%s\n' "$hits" >&2
    fail "$message"
  fi
}

for dir in protocol corepackage kitlease; do
  if [ ! -d "$root/$dir" ]; then
    fail "required public contract directory is missing: $dir"
    continue
  fi
  check "public $dir/ imports FogCast internal/" \
    'github.com/DeanoC/FogCast/internal/' "$root/$dir" --glob '!*_test.go'
done

for dir in targetclient host ui catalog internal/hostapi; do
  path=$root/$dir
  if [ ! -d "$path" ]; then
    fail "required consumer directory is missing: $dir"
    continue
  fi
  check "$dir consumes internal/kitlease; use the public kitlease contract" \
    'github.com/DeanoC/FogCast/internal/kitlease' "$path" --glob '!*_test.go'
  check "$dir imports target implementation packages" \
    'github.com/DeanoC/FogCast/internal/(agent|httpapi|mister|misterruntime|input|targetcache|applianceupdate|flightdiag)(/|")' "$path" --glob '!*_test.go'
done

check "internal/corepackage import remains; use github.com/DeanoC/FogCast/corepackage" \
  'github.com/DeanoC/FogCast/internal/corepackage' "$root"

if [ "$failed" -ne 0 ]; then
  exit 1
fi
