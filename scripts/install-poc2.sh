#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)

fail() {
  printf 'install-poc2: %s\n' "$1" >&2
  exit 1
}

[ "$#" -eq 0 ] || {
  printf 'usage: MISTER_TARGET=root@host install-poc2.sh\n' >&2
  exit 2
}

target=${MISTER_TARGET:-}
[ -n "$target" ] || fail 'MISTER_TARGET is required'
case "$target" in root@*) target_host=${target#root@} ;; *) fail 'MISTER_TARGET must be root@HOST' ;; esac
case "$target_host" in
  ''|[!A-Za-z0-9]*|*[!A-Za-z0-9_.-]*) fail 'MISTER_TARGET must be root@HOST with a hostname or IPv4 address' ;;
esac

lock_tool=$repo/bin/poc2-lock
poc1a_lock=$repo/build/sources.poc1a.lock.toml
poc1b_lock=$repo/build/sources.poc1b.lock.toml
poc2_lock=$repo/build/outputs.poc2.lock.toml
dev_image=$repo/build/output/poc1b/dev/linux.img
test -x "$lock_tool" || fail 'bin/poc2-lock is required; run make build-poc2-lock'
"$lock_tool" verify \
  --lock "$poc2_lock" \
  --poc1a-lock "$poc1a_lock" \
  --poc1b-lock "$poc1b_lock" \
  --prod "$repo/build/output/poc1b/prod/linux.img" \
  --dev "$dev_image"
POC1B_CONTAINER_RUNTIME=${POC1B_CONTAINER_RUNTIME:-docker} \
  "$repo/scripts/verify-poc1b-image.sh" dev \
  "$dev_image" \
  "$repo/build/output/poc1b/dev/manifest.tsv" \
  "$repo/build/output/poc1b/dev/library-report.tsv"

stage=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc2-package.XXXXXX")
archive=$stage/poc2.tar.gz
cleanup() {
  rm -f "$stage/poc2/poc1a.lock.toml" "$stage/poc2/poc1b.lock.toml" \
    "$stage/poc2/poc2.lock.toml" "$stage/poc2/linux.img" "$archive"
  rmdir "$stage/poc2" "$stage" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

mkdir "$stage/poc2"
cp "$poc1a_lock" "$stage/poc2/poc1a.lock.toml"
cp "$poc1b_lock" "$stage/poc2/poc1b.lock.toml"
cp "$poc2_lock" "$stage/poc2/poc2.lock.toml"
cp "$dev_image" "$stage/poc2/linux.img"
"$repo/scripts/scan-poc1b-secrets.sh" "$stage/poc2"
COPYFILE_DISABLE=1 tar -czf "$archive" -C "$stage" \
  poc2/poc1a.lock.toml \
  poc2/poc1b.lock.toml \
  poc2/poc2.lock.toml \
  poc2/linux.img
members=$(tar -tzf "$archive")
expected='poc2/poc1a.lock.toml
poc2/poc1b.lock.toml
poc2/poc2.lock.toml
poc2/linux.img'
[ "$members" = "$expected" ] || fail 'local package allowlist changed'
"$repo/scripts/scan-poc1b-secrets.sh" "$stage/poc2"

remote_archive=/media/fat/linux/mister-remote-poc2.tar.gz
scp -- "$archive" "$target:$remote_archive"
ssh -- "$target" sh -s -- "$remote_archive" < "$repo/deploy/poc2/install-target.sh"
