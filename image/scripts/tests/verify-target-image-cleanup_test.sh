#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-verify-cleanup.XXXXXX")
trap 'chmod -R u+w "$fixture" 2>/dev/null || :; rm -rf "$fixture"' EXIT INT TERM
package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

make_inspection() {
  root=$1
  package=$root/usr/share/mister-runtime/core-packages/$package_id
  mkdir -p "$package"
  printf manifest > "$package/manifest.toml"
  printf payload > "$package/core.rbf"
  chmod 0444 "$package/manifest.toml" "$package/core.rbf"
  chmod 0555 "$package"
}

for status in 0 23; do
  root=$fixture/inspect-native-dev-$status
  make_inspection "$root"
  set +e
  TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/verify-target-image.sh" \
    --cleanup-fixture native-dev "$root" "$package_id" "$status"
  actual=$?
  set -e
  test "$actual" -eq "$status"
  test ! -e "$root"
done

ambient=$fixture/ambient
root=$fixture/inspect-native-dev-malformed
mkdir -p "$ambient" \
  "$root/usr/share/mister-runtime/core-packages/not-a-package-id/unreadable" \
  "$root/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
printf unchanged > "$ambient/keep"
ln -s "$ambient" "$root/usr/share/mister-runtime/core-packages/$package_id"
chmod 0555 "$root/usr/share/mister-runtime/core-packages/not-a-package-id" \
  "$root/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
chmod 0000 "$root/usr/share/mister-runtime/core-packages/not-a-package-id/unreadable"
set +e
TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/verify-target-image.sh" \
  --cleanup-fixture native-dev "$root" "$package_id" 19
actual=$?
set -e
test "$actual" -eq 19
test ! -e "$root"
test "$(cat "$ambient/keep")" = unchanged

retry=$fixture/inspect-native-dev-retry
make_inspection "$retry"
mkdir -p "$retry/usr/share/mister-runtime/core-packages/not-a-package-id"
TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/verify-target-image.sh" \
  --prepare-cleanup-fixture native-dev "$retry" 0
test -d "$retry" && test ! -L "$retry"
test -z "$(find "$retry" -mindepth 1 -print -quit)"
rmdir "$retry"

symlink_root=$fixture/inspect-native-dev-symlink-root
ln -s "$ambient" "$symlink_root"
if TARGET_IMAGE_TEST_MODE=1 sh "$repo/scripts/verify-target-image.sh" \
  --prepare-cleanup-fixture native-dev "$symlink_root" 0 \
  >"$fixture/symlink-root.log" 2>&1; then
  echo 'verifier cleanup accepted a symlinked inspection root' >&2
  exit 1
fi
test -L "$symlink_root"
test "$(cat "$ambient/keep")" = unchanged

grep -Fq 'cleanup_inspection_root "$inspect_root"' \
  "$repo/scripts/verify-target-image.sh"
grep -Fq 'prepare_inspection_root "$inspect_root" "$variant"' \
  "$repo/scripts/verify-target-image.sh"
