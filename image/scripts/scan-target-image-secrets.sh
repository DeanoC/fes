#!/bin/sh
set -eu

[ "$#" -eq 1 ] || {
  printf 'usage: scan-target-image-secrets.sh ROOT\n' >&2
  exit 2
}

root=$1
root=$(CDPATH='' cd -- "$root" && pwd -P)
scan_dir=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-secret-scan.XXXXXX")
cleanup() {
  /bin/rm -rf "$scan_dir"
}
trap cleanup EXIT INT TERM
: > "$scan_dir/matches"

if ! find "$root" -type f -exec sh -c '
  scan_dir=$1
  shift
  strings_output=$scan_dir/strings.$$
  trap '\''/bin/rm -f "$strings_output"'\'' EXIT INT TERM
  for candidate do
    if ! /usr/bin/strings -a "$candidate" > "$strings_output"; then
      printf "scan-target-image-secrets: strings failed: %s\n" "$candidate" >&2
      exit 1
    fi
    if grep -Eiq '\''(^|[[:space:]])(token|secret|bearer|api[_-]?key)[[:space:]]*(=|:)'\'' "$strings_output"; then
      printf "%s\n" "$candidate" >> "$scan_dir/matches"
    fi
  done
' sh "$scan_dir" {} +; then
  printf '%s\n' 'scan-target-image-secrets: one or more regular files could not be scanned' >&2
  exit 1
fi

if [ -s "$scan_dir/matches" ]; then
  printf '%s\n' 'scan-target-image-secrets: secret assignment found' >&2
  exit 1
fi
