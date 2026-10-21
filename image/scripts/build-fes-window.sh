#!/bin/sh
# build-fes-window.sh KERNEL_SOURCE BUILD_DIR OUTPUT_DIR CROSS_PREFIX HOST_DIR RELEASE
#
# Builds the fes-window driver (image/kernel/fes-window) against the pinned
# kernel configuration and writes fes_window.ko and version to OUTPUT_DIR.
# version is the digest of the driver sources and is also the module's
# MODULE_VERSION, which the runtime compares before reusing a loaded copy.
# HOST_DIR is an unrelocated Buildroot host: SDK relocation rewrites GCC's
# plugin configargs.h, and cc1 then refuses the kernel's stack protector plugin.
set -eu
[ "$#" -eq 6 ] || {
  printf '%s\n' 'usage: build-fes-window.sh KERNEL_SOURCE BUILD_DIR OUTPUT_DIR CROSS_PREFIX HOST_DIR RELEASE' >&2
  exit 2
}
kernel=$1
build=$2
output=$3
cross=$4
host=$5
release=$6
export LC_ALL=C
source_dir=$(CDPATH='' cd -- "$(dirname "$0")/../kernel/fes-window" && pwd)
version=$(cd "$source_dir" && for file in *; do printf '%s\0' "$file"; cat "$file"; printf '\0'; done |
  sha256sum | cut -c1-16)
/bin/rm -rf "$build"
/bin/mkdir -p "$build/kernel" "$build/module" "$output"
/bin/cp "$source_dir"/* "$build/module/"
printf '%s\n' -MiSTer > "$build/kernel/localversion.mister"
export KBUILD_BUILD_USER=fogcast KBUILD_BUILD_HOST=target-image KBUILD_BUILD_VERSION=1
export HOSTCXXFLAGS="-I$host/include"
# Run the kernel make as a top-level make: Buildroot's post-build passes its
# MAKEFLAGS down, and their w adds directory lines to the kernelrelease output.
unset MAKEFLAGS GNUMAKEFLAGS MAKELEVEL
kmake() {
  make -s -C "$kernel" O="$build/kernel" ARCH=arm CROSS_COMPILE="$cross" "$@"
}
kmake MiSTer_defconfig
actual=$(kmake kernelrelease)
[ "$actual" = "$release" ] || {
  printf 'build-fes-window: kernel release is %s, expected %s\n' "$actual" "$release" >&2
  exit 1
}
kmake -j2 modules_prepare
kmake M="$build/module" FES_WINDOW_VERSION="$version" modules
"${cross}strip" --strip-debug "$build/module/fes_window.ko"
/bin/cp "$build/module/fes_window.ko" "$output/fes_window.ko"
printf '%s\n' "$version" > "$output/version"
