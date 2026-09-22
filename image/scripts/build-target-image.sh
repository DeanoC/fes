#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
epoch=1751459412
native_mode=${NATIVE_RUNTIME_MODE:-package-only}
case "$native_mode" in
  package-only) : ;;
  *)
    printf '%s\n' 'build-target-image: native runtime mode must be package-only' >&2
    exit 2
    ;;
esac
selected_package_cores() {
  remaining=${FES_PACKAGE_IDS:-}
  while :; do
    case "$remaining" in
      *,*) package_id=${remaining%%,*}; remaining=${remaining#*,} ;;
      *) package_id=$remaining; remaining= ;;
    esac
    case "$package_id" in
      fes.pong) printf '%s\n' pong ;;
      fes.zx81) printf '%s\n' zx81 ;;
      fes.coleco) printf '%s\n' coleco ;;
      *) exit 2 ;;
    esac
    [ -n "$remaining" ] || break
  done
}

usage() {
  printf 'usage: build-target-image.sh native-dev|--promote-existing VARIANT|--fetch VARIANT|--inside VARIANT OUTPUT EPOCH EXPORT|--inside-fetch VARIANT OUTPUT EPOCH|--validate-inside-path VARIANT OUTPUT EXPORT\n' >&2
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
    native-dev) : ;;
    *) usage ;;
  esac
}

defconfig_for() {
  case "$1" in
    native-dev) printf '%s\n' fogcast_target_native_dev_defconfig ;;
  esac
}

cleanup_inside_output() {
	cleanup_output=$1
	if [ ! -e "$cleanup_output" ] && [ ! -L "$cleanup_output" ]; then
		return 0
	fi
	[ -d "$cleanup_output" ] && [ ! -L "$cleanup_output" ] || {
		printf '%s\n' 'build-target-image: retained output must be a non-symlink directory' >&2
		return 1
	}
	cleanup_target=$cleanup_output/target
	if [ -e "$cleanup_target" ] || [ -L "$cleanup_target" ]; then
		[ -d "$cleanup_target" ] && [ ! -L "$cleanup_target" ] || {
			printf '%s\n' 'build-target-image: retained target must be a non-symlink directory' >&2
			return 1
		}
		"$repo/buildroot/board/fogcast-target/rootfs-package-cleanup.sh" \
			"$cleanup_target"
	fi
	/bin/rm -rf "$cleanup_output"
}

verify_native_inputs() {
  [ -n "${LIBMISTER_RUNTIME_DIR:-}" ] || {
    printf '%s\n' 'build-target-image: LIBMISTER_RUNTIME_DIR is required for native-dev' >&2
    exit 2
  }
  NATIVE_RUNTIME_MODE=package-only \
    "$repo/scripts/verify-native-runtime-inputs.sh" \
    "${NATIVE_RUNTIME_INPUT_LOCK:-${FOGCAST_DIR:?FOGCAST_DIR is required}/build/native-runtime.inputs.lock.toml}" \
    "$LIBMISTER_RUNTIME_DIR" \
    "$repo/build/cache/target-image/native/idle.rbf" \
    "$repo/build/cache/target-image/native/splash.rbf"
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
    case "${FES_RUNTIME_SOURCE_PATH:-.}" in
      .|sources/libmister-runtime) : ;;
      *) printf '%s\n' 'build-target-image: unsupported runtime source path' >&2; exit 2 ;;
    esac
    NATIVE_RUNTIME_MODE=package-only \
      /work/scripts/verify-native-runtime-inputs.sh \
      "${FOGCAST_DIR}/build/native-runtime.inputs.lock.toml" \
      "/runtime-source/${FES_RUNTIME_SOURCE_PATH:-.}" \
      /work/build/cache/target-image/native/idle.rbf \
      /work/build/cache/target-image/native/splash.rbf
  fi

	cleanup_inside_output "$inside_output"
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

promote_existing=0
case "${1:-}" in
  --cleanup-inside-output)
    [ "$#" -eq 2 ] || usage
    test "${TARGET_IMAGE_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'build-target-image: cleanup test interface requires test mode' >&2
      exit 2
    }
    test "${TARGET_IMAGE_CLEANUP_TEST_PATH:-}" = "$2" || {
      printf '%s\n' 'build-target-image: cleanup test path was not authorized' >&2
      exit 2
    }
    cleanup_inside_output "$2"
    exit
    ;;
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
  native-dev)
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
      "$repo/scripts/native-extra-cores.sh" copy-records \
        "$repo/build/cache/target-image/native" "$work"
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
  package_cores=$(selected_package_cores)
  for package_core in $package_cores; do
    cmp "$output_root/work-1-$variant/fes-$package_core.package-selection.toml" \
      "$output_root/work-2-$variant/fes-$package_core.package-selection.toml" || {
      printf 'build-target-image: FES %s package selection differs between reproducible outputs\n' "$package_core" >&2
      exit 1
    }
  done
  for work in "$output_root/work-1-$variant" "$output_root/work-2-$variant"; do
    for stale in "$work"/*.rbf "$work"/*-rbf.toml "$work"/*.selection.toml; do
      [ ! -e "$stale" ] && [ ! -L "$stale" ] || {
        printf 'build-target-image: package-only output retains stale artifact: %s\n' "$stale" >&2
        exit 1
      }
    done
  done
fi

final_dir=$output_root/$variant
/bin/mkdir -p "$final_dir"
image_tmp=$final_dir/linux.img.new.$$
evidence_tmp=$final_dir/reproducibility.txt.new.$$
trap '/bin/rm -f "$image_tmp" "$evidence_tmp"' EXIT INT TERM
/bin/cp "$second" "$image_tmp"
printf 'source_date_epoch=%s\nrun_1_sha256=%s\nrun_2_sha256=%s\n' \
  "$epoch" "$first_sha" "$second_sha" > "$evidence_tmp"
if [ "$variant" = native-dev ]; then
  "$repo/scripts/native-extra-cores.sh" copy-records "$output_root/work-2-$variant" "$final_dir"
fi
/bin/mv "$evidence_tmp" "$final_dir/reproducibility.txt"
/bin/mv "$image_tmp" "$final_dir/linux.img"
trap - EXIT INT TERM
printf 'target image %s image: %s\n' "$variant" "$second_sha"
