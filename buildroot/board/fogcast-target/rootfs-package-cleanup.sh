#!/bin/sh
set -eu

[ "$#" -eq 1 ] || {
  printf '%s\n' 'rootfs-package-cleanup: expected target directory' >&2
  exit 1
}
target=$1
root=$target/usr/share/mister-runtime/core-packages

if [ ! -e "$root" ] && [ ! -L "$root" ]; then
  exit 0
fi
[ -d "$root" ] && [ ! -L "$root" ] || {
  printf '%s\n' 'rootfs-package-cleanup: package root must be a non-symlink directory' >&2
  exit 1
}

build_inputs=$target/usr/share/mister-runtime/build-inputs
[ -f "$build_inputs" ] && [ ! -L "$build_inputs" ] || {
  printf '%s\n' 'rootfs-package-cleanup: package identity record is unavailable' >&2
  exit 1
}
identity_count=$(grep -c '^fes_pong_package_id=' "$build_inputs" || :)
[ "$identity_count" -eq 1 ] || {
  printf '%s\n' 'rootfs-package-cleanup: package identity record is ambiguous' >&2
  exit 1
}
identity=$(sed -n 's/^fes_pong_package_id=//p' "$build_inputs")
[ "${#identity}" -eq 64 ] || {
  printf '%s\n' 'rootfs-package-cleanup: package identity is invalid' >&2
  exit 1
}
case "$identity" in
  *[!0-9a-f]*)
    printf '%s\n' 'rootfs-package-cleanup: package identity is invalid' >&2
    exit 1
    ;;
esac

directory=$root/$identity
[ -d "$directory" ] && [ ! -L "$directory" ] || {
  printf '%s\n' 'rootfs-package-cleanup: selected package must be a non-symlink directory' >&2
  exit 1
}
[ "$(find "$root" -mindepth 1 -maxdepth 1 -print | wc -l | tr -d ' ')" -eq 1 ] || {
  printf '%s\n' 'rootfs-package-cleanup: package root differs from selected identity' >&2
  exit 1
}

# Unlink permission belongs to directories. Keep manifest and RBF at 0444 so
# the filesystem image and the copied tree retain the same sealed file modes.
chmod u+w "$directory" "$root"
