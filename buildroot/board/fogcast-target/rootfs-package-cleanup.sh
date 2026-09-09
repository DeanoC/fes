#!/bin/sh
set -eu

[ "$#" -eq 1 ] || {
  printf '%s\n' 'rootfs-package-cleanup: expected target directory' >&2
  exit 1
}
target=$1
[ -d "$target" ] && [ ! -L "$target" ] || {
  printf '%s\n' 'rootfs-package-cleanup: target must be a non-symlink directory' >&2
  exit 1
}

# Check each retained path component before looking below it. Tests such as
# -d follow symlinks, so the explicit -L rejection must accompany every step.
path=$target
for component in usr share mister-runtime core-packages; do
  path=$path/$component
  if [ ! -e "$path" ] && [ ! -L "$path" ]; then
    exit 0
  fi
  [ -d "$path" ] && [ ! -L "$path" ] || {
    printf '%s\n' 'rootfs-package-cleanup: package path components must be non-symlink directories' >&2
    exit 1
  }
done
root=$path

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
