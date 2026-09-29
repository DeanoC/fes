#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
scanner=$repo/scripts/scan-target-image-secrets.sh
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-secret-scan.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

fail() {
  printf 'scan-target-image-secrets_test: %s\n' "$1" >&2
  exit 1
}

test -x "$scanner" || fail 'scanner is not executable'
sh -n "$scanner"

scan_ok() {
  name=$1
  if ! sh "$scanner" "$fixture/$name" >"$fixture/$name.log" 2>&1; then
    cat "$fixture/$name.log" >&2
    fail "$name: scanner rejected a clean tree"
  fi
}

scan_secret() {
  name=$1
  if sh "$scanner" "$fixture/$name" >"$fixture/$name.log" 2>&1; then
    fail "$name: scanner accepted a secret assignment"
  fi
  grep -Fq 'secret assignment found' "$fixture/$name.log" ||
    fail "$name: missing secret-assignment error"
}

mkdir -p \
  "$fixture/sqlite-s" \
  "$fixture/sqlite-t" \
  "$fixture/api-key" \
  "$fixture/token" \
  "$fixture/bearer" \
  "$fixture/sqlite-plus-secret"

printf '%s\n' 'unrecognized token: "%s"' >"$fixture/sqlite-s/payload"
scan_ok sqlite-s

printf '%s\n' 'unrecognized token: "%T"' >"$fixture/sqlite-t/payload"
scan_ok sqlite-t

printf '%s\n' 'api_key=abc' >"$fixture/api-key/payload"
scan_secret api-key

printf '%s\n' 'token=xyz' >"$fixture/token/payload"
scan_secret token

printf '%s\n' 'bearer: abc' >"$fixture/bearer/payload"
scan_secret bearer

printf '%s\n' 'unrecognized token: "%s"' 'api_key=abc' >"$fixture/sqlite-plus-secret/payload"
scan_secret sqlite-plus-secret

printf '%s\n' 'scan-target-image-secrets tests passed'
