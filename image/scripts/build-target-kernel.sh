#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
epoch=1751459412
kernel_commit=d7adb20b4ca595838289406c083fff78f004a8c3
defconfig_sha256=ab8d809efa286413bfc78c5209f63495b36c92449767f0a8d13b1f7747b4dc9d
kernel_release=5.15.1-MiSTer
dtb_target=socfpga_cyclone5_de10_nano.dtb

usage() {
  printf 'usage: build-target-kernel.sh | --inside OUTPUT EXPORT EPOCH\n' >&2
  exit 2
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    /usr/bin/shasum -a 256 "$1" | /usr/bin/awk '{print $1}'
  fi
}

file_size() {
  if stat -c %s "$1" >/dev/null 2>&1; then
    stat -c %s "$1"
  else
    /usr/bin/stat -f %z "$1"
  fi
}

toml_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

write_manifest() {
  manifest_dir=$1
  test "$(cat "$manifest_dir/release")" = "$kernel_release"
  test "$(cat "$manifest_dir/epoch")" = "$epoch"
  compiler=$(cat "$manifest_dir/compiler.txt")
  test -n "$compiler"
  manifest_tmp=$manifest_dir/manifest.toml.new.$$
  {
    printf 'format = 1\n'
    printf 'source_date_epoch = %s\n' "$epoch"
    printf 'kernel_commit = "%s"\n' "$kernel_commit"
    printf 'defconfig = "MiSTer_defconfig"\n'
    printf 'defconfig_sha256 = "%s"\n' "$defconfig_sha256"
    printf 'dtb = "%s"\n' "$dtb_target"
    printf 'release = "%s"\n' "$kernel_release"
    printf 'release_suffix = "-MiSTer"\n'
    printf 'release_suffix_source = "accepted-modules-tree"\n'
    printf 'compiler = "%s"\n' "$(toml_escape "$compiler")"
    for artifact in zImage MiSTer.dtb zImage_dtb modules.tar.gz config; do
      test -s "$manifest_dir/$artifact"
      printf '\n[[artifacts]]\n'
      printf 'name = "%s"\n' "$artifact"
      printf 'sha256 = "%s"\n' "$(sha256_file "$manifest_dir/$artifact")"
      printf 'size = %s\n' "$(file_size "$manifest_dir/$artifact")"
    done
  } > "$manifest_tmp"
  mv "$manifest_tmp" "$manifest_dir/manifest.toml"
}

validate_inside_paths() {
  validate_output=$1
  validate_export=$2
  case "$validate_output:$validate_export" in
    /target-image-output/kernel-work:/work/build/output/target-image/kernel-work-1|\
    /target-image-output/kernel-work:/work/build/output/target-image/kernel-work-2) : ;;
    *)
      printf 'build-target-kernel: unsafe or mismatched paths: %s -> %s\n' \
        "$validate_output" "$validate_export" >&2
      exit 2
      ;;
  esac
}

verify_materialized_source() {
  verify_bare=$1
  verify_kernel=$2
  verify_index=/target-image-output/kernel-source.index
  /bin/rm -f "$verify_index"
  GIT_INDEX_FILE=$verify_index git --git-dir="$verify_bare" read-tree "$kernel_commit"
  GIT_DIR="$verify_bare" GIT_WORK_TREE="$verify_kernel" \
    GIT_INDEX_FILE="$verify_index" git add --all --force
  verify_diff=$(GIT_DIR="$verify_bare" GIT_WORK_TREE="$verify_kernel" \
    GIT_INDEX_FILE="$verify_index" git diff-index --cached --name-status "$kernel_commit" --)
  /bin/rm -f "$verify_index"
  test -z "$verify_diff" || {
    printf '%s\n' 'build-target-kernel: materialized source differs from pinned commit' >&2
    printf '%s\n' "$verify_diff" | sed -n '1,50p' >&2
    exit 1
  }
}

inside_build() {
  inside_output=$1
  inside_export=$2
  inside_epoch=$3
  validate_inside_paths "$inside_output" "$inside_export"
  test "$inside_epoch" = "$epoch"
  test "$(/usr/bin/id -u)" -ne 0 || {
    printf '%s\n' 'build-target-kernel: refusing to build as root' >&2
    exit 1
  }

  /work/scripts/verify-target-image-source-cache.sh \
    /work/build/target-image.sources.lock.toml \
    /work/build/cache/target-image
  /work/bin/target-image-lock-linux-amd64 verify-inputs \
    --lock /work/build/target-image.sources.lock.toml \
    --cache /work/build/cache/target-image

  locked_defconfig=$(tr -d '[:space:]' < /work/build/target-image-kernel-defconfig.sha256)
  test "$locked_defconfig" = "$defconfig_sha256"
  bare=/work/build/cache/target-image/linux-kernel.git
  test "$(git --git-dir="$bare" rev-parse refs/target-image/pinned)" = "$kernel_commit"
  bare_defconfig=$(git --git-dir="$bare" show \
    "$kernel_commit:arch/arm/configs/MiSTer_defconfig" | sha256sum | awk '{print $1}')
  test "$bare_defconfig" = "$defconfig_sha256" || {
    printf '%s\n' 'build-target-kernel: committed MiSTer_defconfig digest differs from lock' >&2
    exit 1
  }

  kernel=/target-image-output/kernel-source
  /bin/rm -rf "$kernel"
  /bin/mkdir -p "$kernel"
  git --git-dir="$bare" archive "$kernel_commit" | tar -x -C "$kernel"
  verify_materialized_source "$bare" "$kernel"
  checkout_defconfig=$(sha256sum "$kernel/arch/arm/configs/MiSTer_defconfig" | awk '{print $1}')
  test "$checkout_defconfig" = "$defconfig_sha256"

  cross=/target-image-output/work-2-prod/host/bin/arm-buildroot-linux-gnueabihf-
  toolchain_host=/target-image-output/work-2-prod/host
  test -x "${cross}gcc" || {
    printf '%s\n' 'build-target-kernel: canonical production Buildroot toolchain is missing' >&2
    exit 1
  }

  /bin/rm -rf "$inside_output" "$inside_export"
  /bin/mkdir -p "$inside_output" "$inside_export"
  export SOURCE_DATE_EPOCH=$inside_epoch
  export KBUILD_BUILD_TIMESTAMP
  KBUILD_BUILD_TIMESTAMP=$(date -u -d "@$inside_epoch" '+%a %b %e %T UTC %Y')
  export KBUILD_BUILD_USER=fogcast
  export KBUILD_BUILD_HOST=target-image
  export KBUILD_BUILD_VERSION=1
  export HOSTCXXFLAGS="-I$toolchain_host/include"

  printf '%s\n' '-MiSTer' > "$inside_output/localversion.mister"
  make -C "$kernel" O="$inside_output" ARCH=arm CROSS_COMPILE="$cross" MiSTer_defconfig
  /bin/cp "$inside_output/.config" "$inside_output/.config.after-defconfig"
  actual_release=$(make -s -C "$kernel" O="$inside_output" ARCH=arm \
    CROSS_COMPILE="$cross" kernelrelease)
  test "$actual_release" = "$kernel_release" || {
    printf 'build-target-kernel: release is %s, expected %s\n' \
      "$actual_release" "$kernel_release" >&2
    exit 1
  }
  make -C "$kernel" O="$inside_output" ARCH=arm CROSS_COMPILE="$cross" \
    -j"$(nproc)" zImage "$dtb_target" modules
  verify_materialized_source "$bare" "$kernel"
  cmp "$inside_output/.config.after-defconfig" "$inside_output/.config" || {
    printf '%s\n' 'build-target-kernel: build changed generated MiSTer_defconfig' >&2
    exit 1
  }

  modules=$inside_output/modules
  make -C "$kernel" O="$inside_output" ARCH=arm CROSS_COMPILE="$cross" \
    INSTALL_MOD_PATH="$modules" modules_install
  find "$modules/lib/modules/$kernel_release" -type l \
    \( -name build -o -name source \) -delete

  /bin/cp "$inside_output/arch/arm/boot/zImage" "$inside_export/zImage"
  /bin/cp "$inside_output/arch/arm/boot/dts/$dtb_target" "$inside_export/MiSTer.dtb"
  /bin/cp "$inside_output/.config.after-defconfig" "$inside_export/config"
  cat "$inside_export/zImage" "$inside_export/MiSTer.dtb" > "$inside_export/zImage_dtb"
  tar --sort=name --mtime="@$inside_epoch" --owner=0 --group=0 --numeric-owner \
    -cf "$inside_output/modules.tar" -C "$modules" .
  gzip -n -9 "$inside_output/modules.tar"
  /bin/mv "$inside_output/modules.tar.gz" "$inside_export/modules.tar.gz"
  printf '%s\n' "$kernel_release" > "$inside_export/release"
  "${cross}gcc" --version | sed -n '1p' > "$inside_export/compiler.txt"
  printf '%s\n' "$inside_epoch" > "$inside_export/epoch"
  write_manifest "$inside_export"
}

case "${1:-}" in
  --inside)
    [ "$#" -eq 4 ] || usage
    inside_build "$2" "$3" "$4"
    exit
    ;;
  '')
    [ "$#" -eq 0 ] || usage
    ;;
  *) usage ;;
esac

output_root=${TARGET_IMAGE_KERNEL_OUTPUT_ROOT:-$repo/build/output/target-image}
if [ "${TARGET_IMAGE_TEST_MODE:-0}" != 1 ]; then
  test "$output_root" = "$repo/build/output/target-image" || {
    printf '%s\n' 'build-target-kernel: output override requires test mode' >&2
    exit 2
  }
fi
case "$output_root" in
  /*) : ;;
  *)
    printf '%s\n' 'build-target-kernel: output root must be absolute' >&2
    exit 2
    ;;
esac
if [ -n "${TARGET_IMAGE_KERNEL_BUILD_ONCE:-}" ] && [ "${TARGET_IMAGE_TEST_MODE:-0}" != 1 ]; then
  printf '%s\n' 'build-target-kernel: fake builder requires test mode' >&2
  exit 2
fi

/bin/mkdir -p "$output_root"
for run in 1 2; do
  work=$output_root/kernel-work-$run
  case "$work" in
    "$output_root"/kernel-work-1|"$output_root"/kernel-work-2) : ;;
    *) exit 2 ;;
  esac
  /bin/rm -rf "$work"
  if [ -n "${TARGET_IMAGE_KERNEL_BUILD_ONCE:-}" ]; then
    "$TARGET_IMAGE_KERNEL_BUILD_ONCE" "$work" "$epoch"
    write_manifest "$work"
  else
    "$repo/scripts/target-image-container.sh" run \
      /work/scripts/build-target-kernel.sh --inside \
      "/target-image-output/kernel-work" \
      "/work/build/output/target-image/kernel-work-$run" "$epoch"
  fi
done

for artifact in zImage MiSTer.dtb zImage_dtb modules.tar.gz config manifest.toml; do
  test -s "$output_root/kernel-work-1/$artifact"
  test -s "$output_root/kernel-work-2/$artifact"
  cmp "$output_root/kernel-work-1/$artifact" "$output_root/kernel-work-2/$artifact" || {
    printf 'build-target-kernel: %s is not reproducible\n' "$artifact" >&2
    exit 1
  }
done

final_dir=$output_root/kernel
/bin/mkdir -p "$final_dir"
publish_tmp=$final_dir/.publish.$$
trap '/bin/rm -rf "$publish_tmp"' EXIT INT TERM
/bin/mkdir "$publish_tmp"
for artifact in zImage MiSTer.dtb zImage_dtb modules.tar.gz config manifest.toml; do
  /bin/cp "$output_root/kernel-work-2/$artifact" "$publish_tmp/$artifact"
done
for artifact in zImage MiSTer.dtb zImage_dtb modules.tar.gz config; do
  /bin/mv "$publish_tmp/$artifact" "$final_dir/$artifact"
done
/bin/mv "$publish_tmp/manifest.toml" "$final_dir/manifest.toml"
/bin/rmdir "$publish_tmp"
trap - EXIT INT TERM
printf 'target image kernel: %s\n' "$(sha256_file "$final_dir/zImage_dtb")"
