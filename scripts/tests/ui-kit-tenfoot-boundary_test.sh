#!/bin/sh
set -eu

# ui/kitlauncher and cmd/fogcast-kit must not import ui/tenfoot or its
# subpackages, including tests and transitive go list dependencies.
# hostclient and ui/shared are allowed.

root=${FOGCAST_BOUNDARY_ROOT:-$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)}
failed=0

if ! command -v rg >/dev/null 2>&1; then
  echo 'rg is required' >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  echo 'go is required' >&2
  exit 1
fi

rg_check() {
  set +e
  hits=$(rg -n --no-heading "$@")
  status=$?
  set -e
  case $status in
    0)
      printf '%s\n' "$hits" >&2
      return 0
      ;;
    1)
      return 1
      ;;
    *)
      printf 'rg failed with exit %s\n' "$status" >&2
      exit 1
      ;;
  esac
}

fail() {
  printf '%s\n' "$1" >&2
  failed=1
}

scan_sources() {
  rel=$1
  label=$2
  if [ ! -d "$root/$rel" ]; then
    echo "required directory is missing: $rel" >&2
    exit 1
  fi
  if rg_check --glob '*.go' --glob '!vendor/**' \
      'github.com/DeanoC/FogCast/ui/tenfoot(/|")' "$root/$rel"; then
    fail "$label imports ui/tenfoot"
  fi
}

scan_deps() {
  spec=$1
  label=$2
  set +e
  deps=$(cd "$root" && GOWORK=off GOFLAGS=-buildvcs=false go list -deps -test -f '{{.ImportPath}}{{if .ForTest}} [{{.ForTest}}.test]{{end}}' "$spec")
  list_status=$?
  set -e
  if [ "$list_status" -ne 0 ]; then
    printf 'go list failed with exit %s\n' "$list_status" >&2
    if [ -n "$deps" ]; then
      printf '%s\n' "$deps" >&2
    fi
    exit 1
  fi
  set +e
  tenfoot_deps=$(printf '%s\n' "$deps" | grep -E '^github.com/DeanoC/FogCast/ui/tenfoot(/|[[:space:]]|$)')
  grep_status=$?
  set -e
  case $grep_status in
    0)
      printf '%s\n' "$tenfoot_deps" >&2
      fail "$label transitively depends on ui/tenfoot"
      ;;
    1)
      ;;
    *)
      printf 'grep failed with exit %s scanning go list output\n' "$grep_status" >&2
      exit 1
      ;;
  esac
}

scan_sources ui/kitlauncher 'kit launcher'
scan_sources cmd/fogcast-kit 'fogcast-kit'
scan_deps ./ui/kitlauncher 'kit launcher'
scan_deps ./cmd/fogcast-kit 'fogcast-kit'

if [ "$failed" -ne 0 ]; then
  exit 1
fi
