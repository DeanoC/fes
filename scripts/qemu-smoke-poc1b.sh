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
  wait_count=$(grep -Fc 'mister-main: waiting for /media/fat payloads' "$smoke_log" || true)
  test "$wait_count" -eq 1 || {
    printf 'qemu-smoke-poc1b: %s did not enter exactly one bounded payload wait\n' "$smoke_variant" >&2
    exit 1
  }
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

    # The smoke kernel is shared test infrastructure. Use one canonical pinned
    # toolchain so switching rootfs variants cannot invalidate its cache.
    toolchain=/poc1b-output/work-2-prod/host/bin/arm-buildroot-linux-gnueabihf-
    test -x "${toolchain}gcc" || {
      printf 'qemu-smoke-poc1b: cross compiler is missing: %sgcc\n' "$toolchain" >&2
      exit 1
    }
    kernel_bare=/work/build/cache/poc1b/linux-kernel.git
    kernel_source=/poc1b-output/qemu-vexpress-source
    kernel_output=/poc1b-output/qemu-vexpress-kernel
    /work/scripts/verify-poc1b-source-cache.sh \
      /work/build/sources.poc1b.lock.toml \
      /work/build/cache/poc1b
    source_head=$(git --git-dir="$kernel_bare" rev-parse refs/poc1b/pinned)
    source_marker=$kernel_source/.poc1b-commit
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
    test -z "$(git -C "$kernel_source" status --porcelain --untracked-files=all -- ':!/.poc1b-commit')"
    compiler_sha=$(sha256sum "${toolchain}gcc" | awk '{print $1}')
    script_sha=$(sha256sum "$0" | awk '{print $1}')
    expected_key=$(printf '%s\n%s\n%s\n' "$source_head" "$compiler_sha" "$script_sha" | sha256sum | awk '{print $1}')
    provenance_key=$kernel_output/provenance.key
    actual_key=
    if [ -f "$provenance_key" ]; then
      IFS= read -r actual_key < "$provenance_key"
    fi

    if [ "$actual_key" != "$expected_key" ] || \
       [ ! -f "$kernel_output/arch/arm/boot/zImage" ] || \
       [ ! -f "$kernel_output/arch/arm/boot/dts/vexpress-v2p-ca9.dtb" ]; then
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
      printf '%s\n' "$expected_key" > "$provenance_key.new"
      /bin/mv "$provenance_key.new" "$provenance_key"
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
