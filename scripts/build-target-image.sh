#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
epoch=1751459412
"$repo/scripts/native-extra-cores.sh" validate

usage() {
  printf 'usage: build-target-image.sh prod|dev|native-dev|--fast-dev|--promote-existing VARIANT|--fetch VARIANT|--inside VARIANT OUTPUT EPOCH EXPORT|--inside-fast-dev OUTPUT EPOCH EXPORT|--inside-fetch VARIANT OUTPUT EPOCH|--validate-inside-path VARIANT OUTPUT EXPORT\n' >&2
  exit 2
}

validate_inside_paths() {
  path_variant=$1
  path_output=$2
  path_export=$3
  validate_variant "$path_variant"
  for path_run in 1 2; do
    if [ "$path_output" = "/target-image-output/work-$path_run-$path_variant" ] && \
       [ "$path_export" = "/work/build/output/target-image/work-$path_run-$path_variant/images/rootfs.ext4" ]; then
      return 0
    fi
  done
  printf 'build-target-image: unsafe or mismatched build paths: %s -> %s\n' \
    "$path_output" "$path_export" >&2
  return 1
}

validate_variant() {
  case "$1" in
    prod|dev|native-dev) : ;;
    *) usage ;;
  esac
}

defconfig_for() {
  case "$1" in
    native-dev) printf '%s\n' fogcast_target_native_dev_defconfig ;;
    prod|dev) printf 'fogcast_target_%s_defconfig\n' "$1" ;;
  esac
}

verify_native_inputs() {
  [ -n "${LIBMISTER_RUNTIME_DIR:-}" ] || {
    printf '%s\n' 'build-target-image: LIBMISTER_RUNTIME_DIR is required for native-dev' >&2
    exit 2
  }
  "$repo/scripts/verify-native-runtime-inputs.sh" \
    "$repo/build/native-runtime.inputs.lock.toml" \
    "$LIBMISTER_RUNTIME_DIR" \
    "$repo/build/cache/target-image/native/idle.rbf" \
    "$repo/build/cache/target-image/native/megadrive.rbf" \
    "${NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE:-$repo/build/cache/target-image/native/megadrive.selection.toml}"
}

run_target_container() {
  container_variant=$1
  shift
  if [ "$container_variant" = native-dev ]; then
    verify_native_inputs
    LIBMISTER_RUNTIME_DIR=$LIBMISTER_RUNTIME_DIR \
      "$repo/scripts/target-image-container.sh" "$@"
  else
    LIBMISTER_RUNTIME_DIR= "$repo/scripts/target-image-container.sh" "$@"
  fi
}

inside_build() {
  inside_variant=$1
  inside_output=$2
  inside_epoch=$3
  inside_mode=${4:-build}
  inside_export=${5:--}
  validate_variant "$inside_variant"
  test "$(/usr/bin/id -u)" -ne 0 || {
    printf '%s\n' 'build-target-image: refusing to run Buildroot as root' >&2
    exit 1
  }
  case "$inside_output" in
    /target-image-output/*) : ;;
    *)
      printf 'build-target-image: unsafe container output path: %s\n' "$inside_output" >&2
      exit 2
      ;;
  esac
  test "$inside_epoch" = "$epoch"
  if [ "$inside_mode" = fetch ]; then
    test "$inside_output" = "/target-image-output/fetch-$inside_variant" || {
      printf 'build-target-image: unsafe fetch output path: %s\n' "$inside_output" >&2
      exit 2
    }
  else
    validate_inside_paths "$inside_variant" "$inside_output" "$inside_export"
  fi

  /work/scripts/verify-target-image-source-cache.sh \
    /work/build/target-image.sources.lock.toml \
    /work/build/cache/target-image
  /work/bin/target-image-lock-linux-amd64 verify-inputs \
    --lock /work/build/target-image.sources.lock.toml \
    --cache /work/build/cache/target-image

  if [ "$inside_variant" = native-dev ]; then
    /work/scripts/verify-native-runtime-inputs.sh \
      /work/build/native-runtime.inputs.lock.toml \
      /runtime-source \
      /work/build/cache/target-image/native/idle.rbf \
      /work/build/cache/target-image/native/megadrive.rbf \
      /work/build/cache/target-image/native/megadrive.selection.toml
  fi

  /bin/rm -rf "$inside_output"
  export SOURCE_DATE_EPOCH=$inside_epoch
  export E2FSPROGS_FAKE_TIME=$inside_epoch
  make -C /work/build/cache/target-image/buildroot \
    O="$inside_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/target-image/dl \
    "$(defconfig_for "$inside_variant")"

  if [ "$inside_mode" = fetch ]; then
    make -C /work/build/cache/target-image/buildroot \
      O="$inside_output" \
      BR2_EXTERNAL=/work/buildroot \
      BR2_DL_DIR=/work/build/cache/target-image/dl \
      source
    return
  fi

  make -C /work/build/cache/target-image/buildroot \
    O="$inside_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/target-image/dl
  test -f "$inside_output/images/rootfs.ext4"
  /bin/mkdir -p "$(dirname "$inside_export")"
  /bin/cp "$inside_output/images/rootfs.ext4" "$inside_export"
}

inside_fast_dev_build() {
  inside_output=$1
  inside_epoch=$2
  inside_export=$3
  test "$inside_output" = /target-image-output/dev-work-dev || {
    printf 'build-target-image: unsafe fast-development output path: %s\n' "$inside_output" >&2
    exit 2
  }
  test "$inside_export" = /work/build/output/target-image/dev/linux.img || {
    printf 'build-target-image: unsafe fast-development export path: %s\n' "$inside_export" >&2
    exit 2
  }
  test "$inside_epoch" = "$epoch"
  test "$(/usr/bin/id -u)" -ne 0 || {
    printf '%s\n' 'build-target-image: refusing to run Buildroot as root' >&2
    exit 1
  }

  /work/scripts/verify-target-image-source-cache.sh \
    /work/build/target-image.sources.lock.toml \
    /work/build/cache/target-image
  /work/bin/target-image-lock-linux-amd64 verify-inputs \
    --lock /work/build/target-image.sources.lock.toml \
    --cache /work/build/cache/target-image

  dev_config=/work/buildroot/configs/fogcast_target_dev_defconfig
  dev_config_sha=$(sha256sum "$dev_config" | awk '{print $1}')
  dev_fingerprint="$inside_output/.fogcast-dev-defconfig.sha256"
  stored_dev_config_sha=
  if [ -f "$dev_fingerprint" ]; then
    stored_dev_config_sha=$(tr -d '[:space:]' < "$dev_fingerprint")
  fi
  if [ "$stored_dev_config_sha" != "$dev_config_sha" ]; then
    /bin/rm -rf "$inside_output"
  fi

  export SOURCE_DATE_EPOCH=$inside_epoch
  export E2FSPROGS_FAKE_TIME=$inside_epoch
  make -C /work/build/cache/target-image/buildroot \
    O="$inside_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/target-image/dl \
    fogcast_target_dev_defconfig
  make -C /work/build/cache/target-image/buildroot \
    O="$inside_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/target-image/dl
  test -f "$inside_output/images/rootfs.ext4"
  printf '%s\n' "$dev_config_sha" > "$dev_fingerprint.new.$$"
  /bin/mv "$dev_fingerprint.new.$$" "$dev_fingerprint"
  /bin/mkdir -p "$(dirname "$inside_export")"
  /bin/cp "$inside_output/images/rootfs.ext4" "$inside_export.new.$$"
  /bin/mv "$inside_export.new.$$" "$inside_export"
}

promote_existing=0
case "${1:-}" in
  --validate-inside-path)
    [ "$#" -eq 4 ] || usage
    test "${TARGET_IMAGE_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'build-target-image: path validation interface requires test mode' >&2
      exit 2
    }
    validate_inside_paths "$2" "$3" "$4"
    exit
    ;;
  --inside)
    [ "$#" -eq 5 ] || usage
    inside_build "$2" "$3" "$4" build "$5"
    exit
    ;;
  --inside-fast-dev)
    [ "$#" -eq 4 ] || usage
    inside_fast_dev_build "$2" "$3" "$4"
    exit
    ;;
  --inside-fetch)
    [ "$#" -eq 4 ] || usage
    inside_build "$2" "$3" "$4" fetch
    exit
    ;;
  --fetch)
    [ "$#" -eq 2 ] || usage
    variant=$2
    validate_variant "$variant"
    output=/target-image-output/fetch-$variant
    run_target_container "$variant" fetch \
      /work/scripts/build-target-image.sh --inside-fetch "$variant" "$output" "$epoch"
    exit
    ;;
  --fast-dev)
    [ "$#" -eq 1 ] || usage
    output_root=${TARGET_IMAGE_OUTPUT_ROOT:-$repo/build/output/target-image}
    if [ "${TARGET_IMAGE_TEST_MODE:-0}" != 1 ]; then
      test "$output_root" = "$repo/build/output/target-image" || {
        printf '%s\n' 'build-target-image: output override requires TARGET_IMAGE_TEST_MODE=1' >&2
        exit 2
      }
    fi
    case "$output_root" in
      /*) : ;;
      *)
        printf '%s\n' 'build-target-image: output root must be absolute' >&2
        exit 2
        ;;
    esac
    /bin/mkdir -p "$output_root"
    if [ -n "${TARGET_IMAGE_BUILD_ONCE:-}" ]; then
      dev_work=$output_root/dev-work-dev
      dev_config="$repo/buildroot/configs/fogcast_target_dev_defconfig"
      dev_config_sha=$(/usr/bin/shasum -a 256 "$dev_config" | /usr/bin/awk '{print $1}')
      dev_fingerprint=$dev_work/.fogcast-dev-defconfig.sha256
      stored_dev_config_sha=
      if [ -f "$dev_fingerprint" ]; then
        stored_dev_config_sha=$(tr -d '[:space:]' < "$dev_fingerprint")
      fi
      if [ "$stored_dev_config_sha" != "$dev_config_sha" ]; then
        /bin/rm -rf "$dev_work"
      fi
      "$TARGET_IMAGE_BUILD_ONCE" dev "$dev_work" "$epoch"
      test -f "$dev_work/images/rootfs.ext4"
      /bin/mkdir -p "$dev_work"
      printf '%s\n' "$dev_config_sha" > "$dev_fingerprint.new.$$"
      /bin/mv "$dev_fingerprint.new.$$" "$dev_fingerprint"
      /bin/mkdir -p "$output_root/dev"
      /bin/cp "$dev_work/images/rootfs.ext4" "$output_root/dev/linux.img.new.$$"
      /bin/mv "$output_root/dev/linux.img.new.$$" "$output_root/dev/linux.img"
    else
      LIBMISTER_RUNTIME_DIR= exec "$repo/scripts/target-image-container.sh" run \
        /work/scripts/build-target-image.sh --inside-fast-dev \
        /target-image-output/dev-work-dev "$epoch" \
        /work/build/output/target-image/dev/linux.img
    fi
    printf 'target image fast development image: %s\n' \
      "$(/usr/bin/shasum -a 256 "$output_root/dev/linux.img" | /usr/bin/awk '{print $1}')"
    exit
    ;;
  prod|dev|native-dev)
    [ "$#" -eq 1 ] || usage
    variant=$1
    ;;
  --promote-existing)
    [ "$#" -eq 2 ] || usage
    variant=$2
    validate_variant "$variant"
    test "${TARGET_IMAGE_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'build-target-image: test mode is required for --promote-existing' >&2
      exit 2
    }
    promote_existing=1
    ;;
  *) usage ;;
esac

output_root=${TARGET_IMAGE_OUTPUT_ROOT:-$repo/build/output/target-image}
if [ "${TARGET_IMAGE_TEST_MODE:-0}" != 1 ]; then
  test "$output_root" = "$repo/build/output/target-image" || {
    printf '%s\n' 'build-target-image: output override requires TARGET_IMAGE_TEST_MODE=1' >&2
    exit 2
  }
fi
case "$output_root" in
  /*) : ;;
  *)
    printf '%s\n' 'build-target-image: output root must be absolute' >&2
    exit 2
    ;;
esac

/bin/mkdir -p "$output_root"
if [ -n "${TARGET_IMAGE_BUILD_ONCE:-}" ] && [ "${TARGET_IMAGE_TEST_MODE:-0}" != 1 ]; then
  printf '%s\n' 'build-target-image: test mode is required for TARGET_IMAGE_BUILD_ONCE' >&2
  exit 2
fi
if [ "$promote_existing" -ne 1 ]; then
  for run in 1 2; do
    work=$output_root/work-$run-$variant
    case "$work" in
      "$output_root"/work-[12]-"$variant") : ;;
      *) exit 2 ;;
    esac
    /bin/rm -rf "$work"
    if [ -n "${TARGET_IMAGE_BUILD_ONCE:-}" ]; then
      "$TARGET_IMAGE_BUILD_ONCE" "$variant" "$work" "$epoch"
    else
      run_target_container "$variant" run \
        /work/scripts/build-target-image.sh --inside "$variant" "/target-image-output/work-$run-$variant" "$epoch" "/work/build/output/target-image/work-$run-$variant/images/rootfs.ext4"
    fi
    test -f "$work/images/rootfs.ext4" || {
      printf 'build-target-image: build %s did not produce rootfs.ext4\n' "$run" >&2
      exit 1
    }
    if [ "$variant" = native-dev ]; then
      selection_source=${NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE:-$repo/build/cache/target-image/native/megadrive.selection.toml}
      [ -f "$selection_source" ] && [ ! -L "$selection_source" ] || {
        printf '%s\n' 'build-target-image: native Mega Drive selection record is unavailable' >&2
        exit 1
      }
      selection_mode=$(stat -c %a "$selection_source" 2>/dev/null || true)
      printf '%s\n' "$selection_mode" | grep -Eq '^[0145]{3,4}$' || {
        printf '%s\n' 'build-target-image: native Mega Drive selection record must not be writable' >&2
        exit 1
      }
      selection_tmp=$work/megadrive.selection.toml.new.$$
      /bin/cp "$selection_source" "$selection_tmp"
      /bin/chmod 0444 "$selection_tmp"
      /bin/mv "$selection_tmp" "$work/megadrive.selection.toml"
      "$repo/scripts/native-extra-cores.sh" copy-records "$(dirname "$selection_source")" "$work"
    fi
  done
fi

first=$output_root/work-1-$variant/images/rootfs.ext4
second=$output_root/work-2-$variant/images/rootfs.ext4
first_sha=$(/usr/bin/shasum -a 256 "$first" | /usr/bin/awk '{print $1}')
second_sha=$(/usr/bin/shasum -a 256 "$second" | /usr/bin/awk '{print $1}')
if [ "$first_sha" != "$second_sha" ]; then
  printf 'build-target-image: %s is not reproducible: %s != %s\n' "$variant" "$first_sha" "$second_sha" >&2
  exit 1
fi

if [ "$variant" = native-dev ]; then
  [ -f "$output_root/work-1-$variant/megadrive.selection.toml" ] &&
    [ -f "$output_root/work-2-$variant/megadrive.selection.toml" ] || {
    printf '%s\n' 'build-target-image: native Mega Drive selection is missing beside a reproducible output' >&2
    exit 1
  }
  cmp -s "$output_root/work-1-$variant/megadrive.selection.toml" \
    "$output_root/work-2-$variant/megadrive.selection.toml" || {
    printf '%s\n' 'build-target-image: native Mega Drive selection differs between reproducible outputs' >&2
    exit 1
  }
  if [ "${NATIVE_RUNTIME_SYSTEMS:-megadrive}" = 'megadrive pong snes nes' ]; then
    for system in pong snes nes; do
      cmp "$output_root/work-1-$variant/$system.selection.toml" "$output_root/work-2-$variant/$system.selection.toml"
    done
  fi
  if [ -n "${FES_PONG_PACKAGE_DIR:-}" ]; then
    cmp "$output_root/work-1-$variant/fes-pong.package-selection.toml" \
      "$output_root/work-2-$variant/fes-pong.package-selection.toml" || {
      printf '%s\n' 'build-target-image: FES Pong package selection differs between reproducible outputs' >&2
      exit 1
    }
  else
    for work in "$output_root/work-1-$variant" "$output_root/work-2-$variant"; do
      [ ! -e "$work/fes-pong.package-selection.toml" ] && [ ! -L "$work/fes-pong.package-selection.toml" ] || {
        printf '%s\n' 'build-target-image: unselected FES Pong package selection was retained' >&2
        exit 1
      }
    done
  fi
fi

final_dir=$output_root/$variant
/bin/mkdir -p "$final_dir"
image_tmp=$final_dir/linux.img.new.$$
evidence_tmp=$final_dir/reproducibility.txt.new.$$
selection_final_tmp=
trap '/bin/rm -f "$image_tmp" "$evidence_tmp" "$selection_final_tmp"' EXIT INT TERM
/bin/cp "$second" "$image_tmp"
printf 'source_date_epoch=%s\nrun_1_sha256=%s\nrun_2_sha256=%s\n' \
  "$epoch" "$first_sha" "$second_sha" > "$evidence_tmp"
if [ "$variant" = native-dev ]; then
  selection_final_tmp=$final_dir/megadrive.selection.toml.new.$$
  /bin/cp "$output_root/work-2-$variant/megadrive.selection.toml" "$selection_final_tmp"
  /bin/chmod 0444 "$selection_final_tmp"
  /bin/mv "$selection_final_tmp" "$final_dir/megadrive.selection.toml"
  selection_final_tmp=
  "$repo/scripts/native-extra-cores.sh" copy-records "$output_root/work-2-$variant" "$final_dir"
fi
/bin/mv "$evidence_tmp" "$final_dir/reproducibility.txt"
/bin/mv "$image_tmp" "$final_dir/linux.img"
trap - EXIT INT TERM
printf 'target image %s image: %s\n' "$variant" "$second_sha"
