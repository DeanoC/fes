#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d)
trap 'chmod -R u+w "$fixture" 2>/dev/null || :; rm -rf "$fixture"' EXIT INT TERM
package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

make_target() {
  target=$1
  package=$target/usr/share/mister-runtime/core-packages/$package_id
  mkdir -p "$package"
  printf 'format=1\nfes_pong_package_id=%s\n' "$package_id" \
    > "$target/usr/share/mister-runtime/build-inputs"
  printf manifest > "$package/manifest.toml"
  printf payload > "$package/core.rbf"
  chmod 0444 "$package/manifest.toml" "$package/core.rbf"
  chmod 0555 "$package"
}

emit_fakeroot() {
  target=$1
  status=$2
  script=$3
  capture=$4
  cat > "$fixture/harness.mk" <<EOF
BR2_EXTERNAL_FOGCAST_TARGET_PATH := $repo/buildroot
TARGET_DIR := $target
include \$(BR2_EXTERNAL_FOGCAST_TARGET_PATH)/external.mk
.PHONY: emit
emit:
	\$(file >$script,#!/bin/sh)
	\$(file >>$script,set -eu)
	\$(file >>$script,\$(FOGCAST_PACKAGE_ROOTFS_CLEANUP))
	\$(file >>$script,printf '%s %s %s' "\$\$(stat -c %a '$target/usr/share/mister-runtime/core-packages/$package_id')" "\$\$(stat -c %a '$target/usr/share/mister-runtime/core-packages/$package_id/manifest.toml')" "\$\$(stat -c %a '$target/usr/share/mister-runtime/core-packages/$package_id/core.rbf')" > '$capture')
	\$(file >>$script,exit $status)
	@chmod 0755 $script
EOF
  make -s -f "$fixture/harness.mk" emit
}

emit_exit_only() {
  target=$1
  status=$2
  script=$3
  cat > "$fixture/harness-exit.mk" <<EOF
BR2_EXTERNAL_FOGCAST_TARGET_PATH := $repo/buildroot
TARGET_DIR := $target
include \$(BR2_EXTERNAL_FOGCAST_TARGET_PATH)/external.mk
.PHONY: emit
emit:
	\$(file >$script,#!/bin/sh)
	\$(file >>$script,set -eu)
	\$(file >>$script,\$(FOGCAST_PACKAGE_ROOTFS_CLEANUP))
	\$(file >>$script,exit $status)
	@chmod 0755 $script
EOF
  make -s -f "$fixture/harness-exit.mk" emit
}

# This is the real unprivileged failure seen after mkfs when no hook repairs
# the disposable filesystem copy.
without=$fixture/without
make_target "$without"
if rm -rf "$without" >"$fixture/rm-without.log" 2>&1; then
  echo 'read-only package directory unexpectedly allowed unprivileged cleanup' >&2
  exit 1
fi
chmod u+w "$without/usr/share/mister-runtime/core-packages/$package_id"
rm -rf "$without"

for status in 0 23; do
  target=$fixture/target-$status
  script=$fixture/fakeroot-$status
  capture=$fixture/modes-$status
  make_target "$target"
  emit_fakeroot "$target" "$status" "$script" "$capture"
  if "$script"; then
    actual=0
  else
    actual=$?
  fi
  test "$actual" -eq "$status"
  test "$(cat "$capture")" = '555 444 444'
  test "$(stat -c %a "$target/usr/share/mister-runtime/core-packages/$package_id")" = 755
  test "$(stat -c %a "$target/usr/share/mister-runtime/core-packages/$package_id/manifest.toml")" = 444
  rm -rf "$target"
done

# Package-only native images retain the complete selected FES package tuple.
# The cleanup hook must relax every selected package directory before the
# disposable Buildroot copy is removed, while still preserving sealed files.
multi=$fixture/multi
multi_package_id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
mkdir -p "$multi/usr/share/mister-runtime/core-packages/$package_id" \
  "$multi/usr/share/mister-runtime/core-packages/$multi_package_id"
printf 'format=1\nfes_pong_package_id=%s\nfes_zx81_package_id=%s\n' \
  "$package_id" "$multi_package_id" \
  > "$multi/usr/share/mister-runtime/build-inputs"
for package in "$package_id" "$multi_package_id"; do
  printf manifest > "$multi/usr/share/mister-runtime/core-packages/$package/manifest.toml"
  printf payload > "$multi/usr/share/mister-runtime/core-packages/$package/core.rbf"
  chmod 0444 "$multi/usr/share/mister-runtime/core-packages/$package/manifest.toml" \
    "$multi/usr/share/mister-runtime/core-packages/$package/core.rbf"
  chmod 0555 "$multi/usr/share/mister-runtime/core-packages/$package"
done
emit_exit_only "$multi" 0 "$fixture/fakeroot-multi"
"$fixture/fakeroot-multi"
for package in "$package_id" "$multi_package_id"; do
  test "$(stat -c %a "$multi/usr/share/mister-runtime/core-packages/$package")" = 755
  test "$(stat -c %a "$multi/usr/share/mister-runtime/core-packages/$package/manifest.toml")" = 444
  test "$(stat -c %a "$multi/usr/share/mister-runtime/core-packages/$package/core.rbf")" = 444
done

# A duplicate package key must fail closed even when the duplicate is the
# final token in the key accumulator and both payload directories exist.
ambiguous_key=$fixture/ambiguous-key
ambiguous_key_first=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
ambiguous_key_second=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
mkdir -p "$ambiguous_key/usr/share/mister-runtime/core-packages/$ambiguous_key_first" \
  "$ambiguous_key/usr/share/mister-runtime/core-packages/$ambiguous_key_second"
printf 'format=1\nfes_pong_package_id=%s\nfes_pong_package_id=%s\n' \
  "$ambiguous_key_first" "$ambiguous_key_second" \
  > "$ambiguous_key/usr/share/mister-runtime/build-inputs"
for package in "$ambiguous_key_first" "$ambiguous_key_second"; do
  printf manifest > "$ambiguous_key/usr/share/mister-runtime/core-packages/$package/manifest.toml"
  printf payload > "$ambiguous_key/usr/share/mister-runtime/core-packages/$package/core.rbf"
  chmod 0444 "$ambiguous_key/usr/share/mister-runtime/core-packages/$package/manifest.toml" \
    "$ambiguous_key/usr/share/mister-runtime/core-packages/$package/core.rbf"
  chmod 0555 "$ambiguous_key/usr/share/mister-runtime/core-packages/$package"
done
emit_exit_only "$ambiguous_key" 0 "$fixture/fakeroot-ambiguous-key"
if "$fixture/fakeroot-ambiguous-key" >"$fixture/ambiguous-key.log" 2>&1; then
  echo 'duplicate package identity key unexpectedly accepted' >&2
  exit 1
fi

# Absent packages are a no-op, while malformed/symlinked destinations make a
# successful image command fail and never affect the symlink target.
absent=$fixture/absent
mkdir -p "$absent"
emit_exit_only "$absent" 0 "$fixture/fakeroot-absent"
"$fixture/fakeroot-absent"
"$repo/buildroot/board/fogcast-target/rootfs-package-cleanup.sh" "$absent"

ambient=$fixture/ambient
bad=$fixture/bad
mkdir -p "$ambient" "$bad/usr/share/mister-runtime"
printf unchanged > "$ambient/keep"
ln -s "$ambient" "$bad/usr/share/mister-runtime/core-packages"
if "$repo/buildroot/board/fogcast-target/rootfs-package-cleanup.sh" "$bad" \
    >"$fixture/bad-root.log" 2>&1; then
  echo 'symlinked temporary package root unexpectedly accepted' >&2
  exit 1
fi
test "$(cat "$ambient/keep")" = unchanged

assert_ambient_unchanged() {
  ambient_target=$1
  expected_modes=$2
  expected_manifest=$3
  expected_payload=$4
  package=$ambient_target/usr/share/mister-runtime/core-packages/$package_id
  actual_modes=$(stat -c '%a %a %a' \
    "$ambient_target/usr/share/mister-runtime" \
    "$ambient_target/usr/share/mister-runtime/core-packages" \
    "$package")
  test "$actual_modes" = "$expected_modes"
  test "$(sha256sum "$package/manifest.toml" | awk '{print $1}')" = \
    "$expected_manifest"
  test "$(sha256sum "$package/core.rbf" | awk '{print $1}')" = \
    "$expected_payload"
}

# Every retained path component must be a real directory. A stale target or
# intermediate symlink must never redirect chmod into an ambient tree.
for redirect in target usr share mister-runtime; do
  ambient_target=$fixture/ambient-$redirect
  redirected_output=$fixture/redirected-$redirect
  make_target "$ambient_target"
  package=$ambient_target/usr/share/mister-runtime/core-packages/$package_id
  expected_modes=$(stat -c '%a %a %a' \
    "$ambient_target/usr/share/mister-runtime" \
    "$ambient_target/usr/share/mister-runtime/core-packages" \
    "$package")
  expected_manifest=$(sha256sum "$package/manifest.toml" | awk '{print $1}')
  expected_payload=$(sha256sum "$package/core.rbf" | awk '{print $1}')
  case "$redirect" in
    target)
      ln -s "$ambient_target" "$redirected_output"
      redirected_target=$redirected_output
      ;;
    usr)
      mkdir -p "$redirected_output"
      ln -s "$ambient_target/usr" "$redirected_output/usr"
      redirected_target=$redirected_output
      ;;
    share)
      mkdir -p "$redirected_output/usr"
      ln -s "$ambient_target/usr/share" "$redirected_output/usr/share"
      redirected_target=$redirected_output
      ;;
    mister-runtime)
      mkdir -p "$redirected_output/usr/share"
      ln -s "$ambient_target/usr/share/mister-runtime" \
        "$redirected_output/usr/share/mister-runtime"
      redirected_target=$redirected_output
      ;;
  esac
  if "$repo/buildroot/board/fogcast-target/rootfs-package-cleanup.sh" \
      "$redirected_target" >"$fixture/redirected-$redirect.log" 2>&1; then
    echo "redirected $redirect cleanup unexpectedly accepted" >&2
    exit 1
  fi
  assert_ambient_unchanged "$ambient_target" "$expected_modes" \
    "$expected_manifest" "$expected_payload"
done

emit_exit_only "$bad" 0 "$fixture/fakeroot-bad-success"
if "$fixture/fakeroot-bad-success" >"$fixture/bad-success.log" 2>&1; then
  echo 'cleanup failure after successful image command was hidden' >&2
  exit 1
fi
emit_exit_only "$bad" 23 "$fixture/fakeroot-bad-failure"
if "$fixture/fakeroot-bad-failure" >"$fixture/bad-failure.log" 2>&1; then
  echo 'failed image command unexpectedly succeeded' >&2
  exit 1
else
  test "$?" -eq 23
fi

rm "$bad/usr/share/mister-runtime/core-packages"
mkdir "$bad/usr/share/mister-runtime/core-packages"
printf 'format=1\nfes_pong_package_id=%s\n' "$package_id" \
  > "$bad/usr/share/mister-runtime/build-inputs"
ln -s "$ambient" "$bad/usr/share/mister-runtime/core-packages/$package_id"
if "$repo/buildroot/board/fogcast-target/rootfs-package-cleanup.sh" "$bad" \
    >"$fixture/bad-package.log" 2>&1; then
  echo 'symlinked selected package directory unexpectedly accepted' >&2
  exit 1
fi
test "$(cat "$ambient/keep")" = unchanged

grep -Fq 'ROOTFS_PRE_CMD_HOOKS += FOGCAST_PACKAGE_ROOTFS_CLEANUP' \
  "$repo/buildroot/external.mk"

# A retained Buildroot output has the same sealed BASE_TARGET_DIR. The next
# unprivileged cold build must relax that exact package before removing the
# otherwise-disposable output tree.
inside_without=$fixture/work-without-native-dev
make_target "$inside_without/target"
if rm -rf "$inside_without" >"$fixture/inside-without.log" 2>&1; then
  echo 'sealed prior Buildroot output unexpectedly allowed plain cleanup' >&2
  exit 1
fi
chmod u+w "$inside_without/target/usr/share/mister-runtime/core-packages/$package_id"
rm -rf "$inside_without"
inside_output=$fixture/work-1-native-dev
make_target "$inside_output/target"
TARGET_IMAGE_TEST_MODE=1 TARGET_IMAGE_CLEANUP_TEST_PATH=$inside_output \
  "$repo/scripts/build-target-image.sh" \
  --cleanup-inside-output "$inside_output"
test ! -e "$inside_output"

# The caller rejects an output symlink before it can hand an ambient target to
# the package cleanup helper.
ambient_output=$fixture/ambient-output
make_target "$ambient_output/target"
ambient_package=$ambient_output/target/usr/share/mister-runtime/core-packages/$package_id
ambient_modes=$(stat -c '%a %a %a' \
  "$ambient_output/target/usr/share/mister-runtime" \
  "$ambient_output/target/usr/share/mister-runtime/core-packages" \
  "$ambient_package")
ambient_manifest=$(sha256sum "$ambient_package/manifest.toml" | awk '{print $1}')
ambient_payload=$(sha256sum "$ambient_package/core.rbf" | awk '{print $1}')
redirected_output=$fixture/redirected-output
ln -s "$ambient_output" "$redirected_output"
if TARGET_IMAGE_TEST_MODE=1 TARGET_IMAGE_CLEANUP_TEST_PATH=$redirected_output \
    "$repo/scripts/build-target-image.sh" \
    --cleanup-inside-output "$redirected_output" \
    >"$fixture/redirected-output.log" 2>&1; then
  echo 'symlinked retained output unexpectedly accepted' >&2
  exit 1
fi
test -L "$redirected_output"
assert_ambient_unchanged "$ambient_output/target" "$ambient_modes" \
  "$ambient_manifest" "$ambient_payload"

ambient_target=$fixture/ambient-caller-target
redirected_output=$fixture/redirected-caller-target
make_target "$ambient_target"
mkdir "$redirected_output"
ln -s "$ambient_target" "$redirected_output/target"
ambient_package=$ambient_target/usr/share/mister-runtime/core-packages/$package_id
ambient_modes=$(stat -c '%a %a %a' \
  "$ambient_target/usr/share/mister-runtime" \
  "$ambient_target/usr/share/mister-runtime/core-packages" \
  "$ambient_package")
ambient_manifest=$(sha256sum "$ambient_package/manifest.toml" | awk '{print $1}')
ambient_payload=$(sha256sum "$ambient_package/core.rbf" | awk '{print $1}')
if TARGET_IMAGE_TEST_MODE=1 TARGET_IMAGE_CLEANUP_TEST_PATH=$redirected_output \
    "$repo/scripts/build-target-image.sh" \
    --cleanup-inside-output "$redirected_output" \
    >"$fixture/redirected-caller-target.log" 2>&1; then
  echo 'symlinked retained target unexpectedly accepted by caller' >&2
  exit 1
fi
test -L "$redirected_output/target"
assert_ambient_unchanged "$ambient_target" "$ambient_modes" \
  "$ambient_manifest" "$ambient_payload"
cleanup_line=$(grep -n 'cleanup_inside_output "$inside_output"' \
  "$repo/scripts/build-target-image.sh" | head -1 | cut -d: -f1)
remove_line=$(grep -n '/bin/rm -rf "$cleanup_output"' \
  "$repo/scripts/build-target-image.sh" | head -1 | cut -d: -f1)
test -n "$cleanup_line" && test -n "$remove_line"
grep -Fq '$repo/buildroot/board/fogcast-target/rootfs-package-cleanup.sh' \
  "$repo/scripts/build-target-image.sh"
