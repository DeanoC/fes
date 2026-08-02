#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)

usage() {
  printf 'usage: qemu-smoke-poc1b.sh prod|dev IMAGE | --inside VARIANT IMAGE | --verify-log VARIANT LOG\n' >&2
  exit 2
}

validate_variant() {
  case "$1" in
    prod|dev) : ;;
    *) usage ;;
  esac
}

verify_smoke_log() {
  smoke_variant=$1
  smoke_log=$2
  grep -Fq 'POC1B_SMOKE_READY' "$smoke_log" || {
    printf 'qemu-smoke-poc1b: %s did not reach the smoke sentinel\n' "$smoke_variant" >&2
    tail -n 80 "$smoke_log" >&2
    exit 1
  }
  grep -Eq 'POC1B_SMOKE_ROOT options=([^,[:space:]]+,)*ro(,|[[:space:]]|$)' "$smoke_log"
  grep -Fq 'POC1B_SMOKE_VOLATILE /run /tmp /var/log writable tmpfs' "$smoke_log"
}

case "${1:-}" in
  --verify-log)
    [ "$#" -eq 3 ] || usage
    test "${POC1B_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'qemu-smoke-poc1b: log fixtures require test mode' >&2
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
      /work/build/output/poc1b/*) : ;;
      *)
        printf '%s\n' 'qemu-smoke-poc1b: unsafe image path' >&2
        exit 2
        ;;
    esac
    test -f "$image"

    toolchain=/poc1b-output/work-2-$variant/host/bin/arm-buildroot-linux-gnueabihf-
    test -x "${toolchain}gcc" || {
      printf 'qemu-smoke-poc1b: cross compiler is missing: %sgcc\n' "$toolchain" >&2
      exit 1
    }
    kernel_source=/work/build/cache/poc1b/linux-kernel
    kernel_output=/poc1b-output/qemu-vexpress-kernel
    /bin/mkdir -p "$kernel_output"

    if [ ! -f "$kernel_output/arch/arm/boot/zImage" ] || \
       [ ! -f "$kernel_output/arch/arm/boot/dts/vexpress-v2p-ca9.dtb" ]; then
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
    fi

    log=/work/build/output/poc1b/$variant/qemu-smoke.log
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
      -append 'root=/dev/mmcblk0 ro rootwait console=ttyAMA0,115200 init=/sbin/init poc1b_smoke=1' \
      > "$log" 2>&1 || true
    verify_smoke_log "$variant" "$log"
    printf 'POC 1B %s QEMU smoke passed (vexpress-a9, not FPGA emulation)\n' "$variant"
    exit
    ;;
  prod|dev)
    [ "$#" -eq 2 ] || usage
    variant=$1
    image=$2
    exec "$repo/scripts/poc1b-container.sh" run \
      /work/scripts/qemu-smoke-poc1b.sh --inside "$variant" "$image"
    ;;
  *) usage ;;
esac
