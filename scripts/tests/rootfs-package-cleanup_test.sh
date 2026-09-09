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
