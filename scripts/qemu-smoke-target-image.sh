#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)

usage() {
  printf 'usage: qemu-smoke-target-image.sh prod|dev|native-dev IMAGE | --inside VARIANT IMAGE | --verify-log VARIANT LOG | --verify-kernel-cache KEY OUTPUT\n' >&2
  exit 2
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    /usr/bin/shasum -a 256 "$1" | awk '{print $1}'
  fi
}

kernel_cache_valid() {
  cache_expected=$1
  cache_output=$2
  cache_provenance=$cache_output/provenance.txt
  cache_kernel=$cache_output/arch/arm/boot/zImage
  cache_dtb=$cache_output/arch/arm/boot/dts/vexpress-v2p-ca9.dtb
  [ -f "$cache_provenance" ] && [ -f "$cache_kernel" ] && [ -f "$cache_dtb" ] || return 1
  [ "$(wc -l < "$cache_provenance" | tr -d ' ')" -eq 4 ] || return 1
  grep -Fqx 'format=1' "$cache_provenance" || return 1
  cache_key=$(awk -F= '$1 == "base_key" { print $2 }' "$cache_provenance")
  cache_kernel_sha=$(awk -F= '$1 == "zimage_sha256" { print $2 }' "$cache_provenance")
  cache_dtb_sha=$(awk -F= '$1 == "dtb_sha256" { print $2 }' "$cache_provenance")
  [ "$cache_key" = "$cache_expected" ] || return 1
  [ "$cache_kernel_sha" = "$(sha256_file "$cache_kernel")" ] || return 1
  [ "$cache_dtb_sha" = "$(sha256_file "$cache_dtb")" ] || return 1
}

validate_variant() {
  case "$1" in
    prod|dev|native-dev) : ;;
    *) usage ;;
  esac
}

verify_smoke_log() {
  smoke_variant=$1
  smoke_log=$2
  grep -Fq 'TARGET_IMAGE_SMOKE_READY' "$smoke_log" || {
    printf 'qemu-smoke-target-image: %s did not reach the smoke sentinel\n' "$smoke_variant" >&2
    tail -n 80 "$smoke_log" >&2
    exit 1
  }
  grep -Eq 'TARGET_IMAGE_SMOKE_ROOT options=([^,[:space:]]+,)*ro(,|[[:space:]]|$)' "$smoke_log"
  grep -Fq 'TARGET_IMAGE_SMOKE_VOLATILE /run /tmp /var/log writable tmpfs' "$smoke_log"
  wait_count=$(grep -Fc 'mister-main: waiting for /media/fat payloads' "$smoke_log" || true)
  if [ "$smoke_variant" = native-dev ]; then
    test "$wait_count" -eq 0 || {
      printf '%s\n' 'qemu-smoke-target-image: native-dev entered a Main payload wait' >&2
      exit 1
    }
  else
    test "$wait_count" -eq 1 || {
      printf 'qemu-smoke-target-image: %s did not enter exactly one bounded payload wait\n' "$smoke_variant" >&2
      exit 1
    }
  fi
}

case "${1:-}" in
  --verify-kernel-cache)
    [ "$#" -eq 3 ] || usage
    test "${TARGET_IMAGE_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'qemu-smoke-target-image: kernel cache fixtures require test mode' >&2
      exit 2
    }
    printf '%s\n' "$2" | grep -Eq '^[0-9a-f]{64}$' || usage
    kernel_cache_valid "$2" "$3"
    exit
    ;;
  --verify-log)
    [ "$#" -eq 3 ] || usage
    test "${TARGET_IMAGE_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'qemu-smoke-target-image: log fixtures require test mode' >&2
      exit 2
    }
    validate_variant "$2"
    test -f "$3"
    verify_smoke_log "$2" "$3"
    exit
    ;;
  --inside)
    [ "$#" -eq 3 ] || usage
    variant=$2
    image=$3
    validate_variant "$variant"
    image=$(readlink -f "$image")
    case "$image" in
      /work/build/output/target-image/*) : ;;
      *)
        printf '%s\n' 'qemu-smoke-target-image: unsafe image path' >&2
        exit 2
        ;;
    esac
    test -f "$image"

    # The smoke kernel is shared test infrastructure. Legacy variants retain
    # the canonical production toolchain; native-dev uses its own target
    # toolchain so the native build is independently sufficient for smoke.
    if [ "$variant" = native-dev ]; then
      toolchain_root=/target-image-output/work-2-native-dev/host
      toolchain=/target-image-output/work-2-native-dev/host/bin/arm-buildroot-linux-gnueabihf-
    else
      toolchain_root=/target-image-output/work-2-prod/host
      toolchain=/target-image-output/work-2-prod/host/bin/arm-buildroot-linux-gnueabihf-
    fi
    test -x "${toolchain}gcc" || {
      printf 'qemu-smoke-target-image: cross compiler is missing: %sgcc\n' "$toolchain" >&2
      exit 1
    }
    kernel_bare=/work/build/cache/target-image/linux-kernel.git
    kernel_source=/target-image-output/qemu-vexpress-source
    kernel_output=/target-image-output/qemu-vexpress-kernel
    /work/scripts/verify-target-image-source-cache.sh \
      /work/build/target-image.sources.lock.toml \
      /work/build/cache/target-image
    source_head=$(git --git-dir="$kernel_bare" rev-parse refs/target-image/pinned)
    source_marker=$kernel_source/.target-image-commit
    actual_source=
    if [ -f "$source_marker" ]; then
      IFS= read -r actual_source < "$source_marker"
    fi
    if [ "$actual_source" != "$source_head" ] || \
       [ ! -d "$kernel_source/.git" ]; then
      /bin/rm -rf "$kernel_source"
      /bin/mkdir -p "$kernel_source"
      git -C "$kernel_source" init
      git -C "$kernel_source" fetch --depth=1 "$kernel_bare" "$source_head"
      git -C "$kernel_source" checkout --detach FETCH_HEAD
      printf '%s\n' "$source_head" > "$source_marker"
    fi
    test "$(git -C "$kernel_source" rev-parse HEAD)" = "$source_head"
    test -z "$(git -C "$kernel_source" status --porcelain --untracked-files=all -- ':!/.target-image-commit')"
    toolchain_sha=$(
      cd "$toolchain_root"
      find . \( -type f -o -type l \) -print | LC_ALL=C sort | while IFS= read -r relative; do
        if [ -L "$relative" ]; then
          printf '%s\tsymlink\t%s\n' "$relative" "$(readlink "$relative")"
        else
          printf '%s\tfile\t%s\n' "$relative" "$(sha256sum "$relative" | awk '{print $1}')"
        fi
      done | sha256sum | awk '{print $1}'
    )
    script_sha=$(sha256sum "$0" | awk '{print $1}')
    expected_key=$(printf '%s\n%s\n%s\n' "$source_head" "$toolchain_sha" "$script_sha" | sha256sum | awk '{print $1}')

    if ! kernel_cache_valid "$expected_key" "$kernel_output"; then
      /bin/rm -rf "$kernel_output"
      /bin/mkdir -p "$kernel_output"
      make -C "$kernel_source" O="$kernel_output" ARCH=arm CROSS_COMPILE="$toolchain" vexpress_defconfig
      "$kernel_source/scripts/config" --file "$kernel_output/.config" \
        -e DEVTMPFS \
        -e DEVTMPFS_MOUNT \
        -e EXT4_FS \
        -d GCC_PLUGINS \
        -e BLK_DEV_LOOP \
        -e MMC \
        -e MMC_ARMMMCI \
        -d STACKPROTECTOR_PER_TASK \
        -e TMPFS
      make -C "$kernel_source" O="$kernel_output" ARCH=arm CROSS_COMPILE="$toolchain" olddefconfig
      make -C "$kernel_source" O="$kernel_output" ARCH=arm CROSS_COMPILE="$toolchain" -j4 zImage dtbs
      provenance=$kernel_output/provenance.txt
      kernel_sha=$(sha256_file "$kernel_output/arch/arm/boot/zImage")
      dtb_sha=$(sha256_file "$kernel_output/arch/arm/boot/dts/vexpress-v2p-ca9.dtb")
      printf 'format=1\nbase_key=%s\nzimage_sha256=%s\ndtb_sha256=%s\n' \
        "$expected_key" "$kernel_sha" "$dtb_sha" > "$provenance.new"
      /bin/mv "$provenance.new" "$provenance"
    fi

    log=/work/build/output/target-image/$variant/qemu-smoke.log
    /bin/rm -f "$log"
    timeout -s TERM 45 qemu-system-arm \
      -M vexpress-a9 \
      -m 256M \
      -nographic \
      -no-reboot \
      -nic none \
      -audiodev none,id=noaudio \
      -kernel "$kernel_output/arch/arm/boot/zImage" \
      -dtb "$kernel_output/arch/arm/boot/dts/vexpress-v2p-ca9.dtb" \
      -drive "file=$image,if=sd,format=raw" \
      -append 'root=/dev/mmcblk0 ro rootwait console=ttyAMA0,115200 init=/sbin/init fogcast_target_smoke=1' \
      > "$log" 2>&1 || true
    verify_smoke_log "$variant" "$log"
    printf 'target image %s QEMU smoke passed (vexpress-a9, not FPGA emulation)\n' "$variant"
    exit
    ;;
  prod|dev|native-dev)
    [ "$#" -eq 2 ] || usage
    variant=$1
    image=$2
    exec "$repo/scripts/target-image-container.sh" run \
      /work/scripts/qemu-smoke-target-image.sh --inside "$variant" "$image"
    ;;
  *) usage ;;
esac
