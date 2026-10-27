#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
epoch=1751459412
test "$epoch" = "$(python3 "$repo/scripts/toolchain_cache.py" epoch)"
image_passes=${FES_IMAGE_PASSES:-1}
case "$image_passes" in
  1|2) : ;;
  *) printf '%s\n' 'build-target-image: FES_IMAGE_PASSES must be 1 or 2' >&2; exit 2 ;;
esac
expected_work=/target-image-output/work-$image_passes-native-dev
image_work=${FES_IMAGE_WORK:-$expected_work}
if [ "$image_work" != "$expected_work" ]; then
  printf 'build-target-image: FES_IMAGE_WORK must be %s when FES_IMAGE_PASSES=%s\n' "$expected_work" "$image_passes" >&2
  exit 2
fi
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
      fes.menu) printf '%s\n' menu ;;
      fes.pong) printf '%s\n' pong ;;
      fes.zx81) printf '%s\n' zx81 ;;
      fes.coleco) printf '%s\n' coleco ;;
      fes.sms) printf '%s\n' sms ;;
      fes.sg1000) printf '%s\n' sg1000 ;;
      fes.c64) printf '%s\n' c64 ;;
      fes.spectrum) printf '%s\n' spectrum ;;
      fes.ramtest) printf '%s\n' ramtest ;;
      fes.atari-st) printf '%s\n' atari-st ;;
      *) exit 2 ;;
    esac
    [ -n "$remaining" ] || break
  done
}

usage() {
  printf 'usage: build-target-image.sh native-dev|--promote-existing VARIANT|--fetch VARIANT|--inside VARIANT OUTPUT EPOCH EXPORT|--inside-toolchain EPOCH|--ensure-toolchain VARIANT|--inside-fetch VARIANT OUTPUT EPOCH|--inside-fetch-toolchain EPOCH|--inside-kit-sdk EPOCH|--ensure-kit-sdk|--validate-inside-path VARIANT OUTPUT EXPORT\n' >&2
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
  [ -n "${LIBMISTER_RUNTIME_DIR:-}${FES_PREBUILT_RUNTIME_SHA256:-}" ] || {
    printf '%s\n' 'build-target-image: LIBMISTER_RUNTIME_DIR is required for native-dev' >&2
    exit 2
  }
  NATIVE_RUNTIME_MODE=package-only \
    "$repo/scripts/verify-native-runtime-inputs.sh" \
    "${NATIVE_RUNTIME_INPUT_LOCK:-${FOGCAST_DIR:?FOGCAST_DIR is required}/build/native-runtime.inputs.lock.toml}" \
    "${LIBMISTER_RUNTIME_DIR:-/unused-prebuilt-runtime}" \
    "$repo/build/cache/target-image/native/idle.rbf" \
    "$repo/build/cache/target-image/native/splash.rbf"
}

run_target_container() {
  container_variant=$1
  shift
  if [ "$container_variant" = native-dev ]; then
    verify_native_inputs
    LIBMISTER_RUNTIME_DIR=${LIBMISTER_RUNTIME_DIR:-} \
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
  config=$inside_output/fogcast.generated.defconfig
  /bin/mkdir -p "$inside_output"
  if [ "$inside_mode" = fetch ]; then
    /work/scripts/toolchain_cache.py config internal "$config"
  else
    /work/scripts/toolchain_cache.py config external "$config"
    external_host=$(/work/scripts/toolchain_cache.py path)
    /work/scripts/toolchain_cache.py extract "$(dirname "$external_host")"
    test -x "$external_host/relocate-sdk.sh" || {
      printf '%s\n' 'build-target-image: SDK relocation script is missing' >&2; exit 1;
    }
    "$external_host/relocate-sdk.sh"
  fi
  make -C /work/build/cache/target-image/buildroot \
    O="$inside_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/target-image/dl \
    BR2_DEFCONFIG="$config" defconfig
  /work/scripts/toolchain_cache.py validate-config \
    "$(if [ "$inside_mode" = fetch ]; then printf internal; else printf external; fi)" \
    "$inside_output/.config"

  if [ "$inside_mode" = fetch ]; then
    make -C /work/build/cache/target-image/buildroot \
      O="$inside_output" \
      BR2_EXTERNAL=/work/buildroot \
      BR2_DL_DIR=/work/build/cache/target-image/dl \
      source
    if [ -n "${FES_TARGET_IMAGE_SHARED_CACHE:-}" ]; then
      # The image passes run without network; with ccache enabled they also need
      # host-ccache's source, which the internal fetch config does not select.
      make -C /work/build/cache/target-image/buildroot \
        O="$inside_output" \
        BR2_EXTERNAL=/work/buildroot \
        BR2_DL_DIR=/work/build/cache/target-image/dl \
        host-ccache-source
    fi
    return
  fi

  if [ "$inside_variant" = native-dev ]; then
    # The post-build installs btusb from the fetched image-creator archive.
    # A production image fails closed when that archive is absent.
    export FES_BLUETOOTH_MODULES_REQUIRED=1
  fi
  make -C /work/build/cache/target-image/buildroot \
    O="$inside_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/target-image/dl
  test -f "$inside_output/images/rootfs.ext4"
  /bin/mkdir -p "$(dirname "$inside_export")"
  /bin/cp "$inside_output/images/rootfs.ext4" "$inside_export"
}

# One Buildroot invocation for the toolchain SDK. Image and kit-sdk builds both
# call it, so the published host tree stays the one toolchain_cache.py packages.
toolchain_make() {
  toolchain_output=$1
  shift
  make -C /work/build/cache/target-image/buildroot \
    O="$toolchain_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/target-image/dl \
    "$@"
}

prepare_toolchain_output() {
  toolchain_output=$1
  cleanup_inside_output "$toolchain_output"
  /bin/mkdir -p "$toolchain_output"
  toolchain_config=$toolchain_output/fogcast.generated.defconfig
  /work/scripts/toolchain_cache.py config toolchain "$toolchain_config"
  export SOURCE_DATE_EPOCH=$epoch
  export E2FSPROGS_FAKE_TIME=$epoch
  toolchain_make "$toolchain_output" BR2_DEFCONFIG="$toolchain_config" defconfig
  /work/scripts/toolchain_cache.py validate-config toolchain "$toolchain_output/.config"
}

publish_toolchain() {
  prepare_toolchain_output /target-image-output/toolchain-build
  toolchain_make /target-image-output/toolchain-build toolchain
  toolchain_make /target-image-output/toolchain-build sdk
  /work/scripts/toolchain_cache.py package /target-image-output/toolchain-build/host
}

fetch_toolchain_sources() {
  # The toolchain defconfig selects these downloads. This is not the image fetch.
  prepare_toolchain_output /target-image-output/toolchain-fetch
  toolchain_make /target-image-output/toolchain-fetch source
}

verify_buildroot_checkout() {
  /work/scripts/verify-target-image-source-cache.sh \
    /work/build/target-image.sources.lock.toml \
    /work/build/cache/target-image buildroot
}

verify_kit_sdk_sources() {
  /work/scripts/verify-target-image-source-cache.sh \
    /work/build/target-image.sources.lock.toml \
    /work/build/cache/target-image kit-sdk
}

inside_toolchain() {
  test "$1" = "$epoch"
  test "$(/usr/bin/id -u)" -ne 0
  /work/scripts/verify-target-image-source-cache.sh \
    /work/build/target-image.sources.lock.toml /work/build/cache/target-image
  /work/bin/target-image-lock-linux-amd64 verify-inputs \
    --lock /work/build/target-image.sources.lock.toml --cache /work/build/cache/target-image
  publish_toolchain
}

inside_fetch_toolchain() {
  test "$1" = "$epoch"
  test "$(/usr/bin/id -u)" -ne 0
  verify_buildroot_checkout
  fetch_toolchain_sources
}

inside_kit_sdk() {
  test "$1" = "$epoch"
  test "$(/usr/bin/id -u)" -ne 0
  verify_kit_sdk_sources
  publish_toolchain
}

# Drop image-only inputs before the SDK container. The publisher does not read them.
run_sdk_container() {
  env \
    -u LIBMISTER_RUNTIME_DIR \
    -u FOGCAST_DIR \
    -u NATIVE_RUNTIME_INPUT_LOCK \
    -u NATIVE_RUNTIME_IDLE_FILE \
    -u NATIVE_RUNTIME_SPLASH_FILE \
    -u FES_PREBUILT_RUNTIME_SHA256 \
    -u FES_PACKAGE_IDS \
    -u FES_VIDEO_PARTS_DIR \
    -u FES_UNAVAILABLE_CORES \
    -u FES_MENU_PACKAGE_DIR -u FES_MENU_PACKAGE_SELECTION \
    -u FES_PONG_PACKAGE_DIR -u FES_PONG_PACKAGE_SELECTION \
    -u FES_ZX81_PACKAGE_DIR -u FES_ZX81_PACKAGE_SELECTION \
    -u FES_COLECO_PACKAGE_DIR -u FES_COLECO_PACKAGE_SELECTION \
    -u FES_SMS_PACKAGE_DIR -u FES_SMS_PACKAGE_SELECTION \
    -u FES_SG1000_PACKAGE_DIR -u FES_SG1000_PACKAGE_SELECTION \
    -u FES_C64_PACKAGE_DIR -u FES_C64_PACKAGE_SELECTION \
    -u FES_RAMTEST_PACKAGE_DIR -u FES_RAMTEST_PACKAGE_SELECTION \
    -u FES_ATARI_ST_PACKAGE_DIR -u FES_ATARI_ST_PACKAGE_SELECTION \
    -u FES_SPECTRUM_PACKAGE_DIR -u FES_SPECTRUM_PACKAGE_SELECTION \
    "$repo/scripts/target-image-container.sh" "$@"
}

ensure_container_base() {
  container_runtime=${TARGET_IMAGE_CONTAINER_RUNTIME:-docker}
  container_lock=${TARGET_IMAGE_LOCK:-$repo/build/target-image.sources.lock.toml}
  container_values=$(python3 - "$container_lock" <<'PY'
import re
import sys
import tomllib

with open(sys.argv[1], "rb") as lock_file:
    container = tomllib.load(lock_file)["container"]
image = container["image"]
platform = container["platform"]
digest = container["digest"]
if image != "docker.io/library/debian:12.11-slim" or platform != "linux/amd64" or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
    raise SystemExit("build-target-image: invalid pinned container inputs")
print(image, platform, digest)
PY
  )
  set -- $container_values
  [ "$#" -eq 3 ] || {
    printf '%s\n' 'build-target-image: invalid pinned container inputs' >&2
    exit 2
  }
  container_ref=$1@$3
  if ! command -v "$container_runtime" >/dev/null 2>&1; then
    printf 'build-target-image: container runtime is not executable: %s\n' "$container_runtime" >&2
    exit 2
  fi
  if ! "$container_runtime" image inspect "$container_ref" >/dev/null 2>&1; then
    "$container_runtime" pull --platform "$2" "$container_ref"
  fi
}

fetch_kit_sdk_sources() {
  source_cache_lock=$(source_cache_lock_path)
  mkdir -p "$(dirname "$source_cache_lock")"
  flock "$source_cache_lock" "$0" --fetch-kit-sdk-sources-locked
}

source_cache_lock_path() {
  source_cache=${TARGET_IMAGE_CACHE:-$repo/build/cache/target-image}
  printf '%s/.kit-sdk-sources.lock\n' "$(dirname "$source_cache")"
}

fetch_image_sources() {
  image_variant=$1
  source_cache_lock=$(source_cache_lock_path)
  mkdir -p "$(dirname "$source_cache_lock")"
  flock "$source_cache_lock" "$0" --fetch-image-sources-locked "$image_variant"
}

fetch_kit_sdk_sources_locked() {
  source_lock=${TARGET_IMAGE_LOCK:-$repo/build/target-image.sources.lock.toml}
  source_cache=${TARGET_IMAGE_CACHE:-$repo/build/cache/target-image}
  if sh "$repo/scripts/verify-target-image-source-cache.sh" \
      "$source_lock" "$source_cache" kit-sdk >/dev/null 2>&1; then
    printf '%s\n' 'target image source cache provenance verified'
    return 0
  fi
  ensure_container_base
  run_sdk_container fetch \
    /work/scripts/fetch-target-image-sources.sh kit-sdk
  run_sdk_container run \
    /work/scripts/verify-target-image-source-cache.sh \
    /work/build/target-image.sources.lock.toml \
    /work/build/cache/target-image kit-sdk
}

build_kit_sdk() {
  run_sdk_container fetch \
    /work/scripts/build-target-image.sh --inside-fetch-toolchain "$epoch"
  run_sdk_container run \
    /work/scripts/build-target-image.sh --inside-kit-sdk "$epoch"
}

rebuild_toolchain_unless_cached() {
  toolchain_sha=$(python3 "$repo/scripts/toolchain_cache.py" status) || toolchain_sha=
  if [ "${TOOLCHAIN_REBUILD:-0}" = 1 ] || [ -z "$toolchain_sha" ]; then
    if [ -n "${TARGET_IMAGE_BUILD_ONCE:-}" ]; then
      [ -n "${TARGET_IMAGE_TOOLCHAIN_BUILD_ONCE:-}" ] || {
        printf '%s\n' 'build-target-image: fake toolchain builder is required' >&2; exit 2;
      }
      "$TARGET_IMAGE_TOOLCHAIN_BUILD_ONCE" "$repo"
    else
      "$@"
    fi
    toolchain_sha=$(python3 "$repo/scripts/toolchain_cache.py" status) || {
      printf '%s\n' 'build-target-image: toolchain cache failed validation' >&2; exit 1;
    }
  fi
}

ensure_toolchain() {
  rebuild_toolchain_unless_cached run_target_container "$variant" run \
    /work/scripts/build-target-image.sh --inside-toolchain "$epoch"
}

ensure_kit_sdk() {
  fetch_kit_sdk_sources
  rebuild_toolchain_unless_cached build_kit_sdk
}

promote_existing=0
ensure_only=0
kit_sdk=0
case "${1:-}" in
  --fetch-kit-sdk-sources-locked)
    [ "$#" -eq 1 ] || usage
    fetch_kit_sdk_sources_locked
    exit
    ;;
  --fetch-image-sources-locked)
    [ "$#" -eq 2 ] || usage
    variant=$2
    validate_variant "$variant"
    output=/target-image-output/fetch-$variant
    run_target_container "$variant" fetch \
      /work/scripts/build-target-image.sh --inside-fetch "$variant" "$output" "$epoch"
    exit
    ;;
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
  --inside-toolchain)
    [ "$#" -eq 2 ] || usage
    inside_toolchain "$2"
    exit
    ;;
  --inside-fetch-toolchain)
    [ "$#" -eq 2 ] || usage
    inside_fetch_toolchain "$2"
    exit
    ;;
  --inside-kit-sdk)
    [ "$#" -eq 2 ] || usage
    inside_kit_sdk "$2"
    exit
    ;;
  --fetch)
    [ "$#" -eq 2 ] || usage
    variant=$2
    validate_variant "$variant"
    fetch_image_sources "$variant"
    exit
    ;;
  --ensure-toolchain)
    # Used by the incremental `make dev` path so it reuses the same cached SDK.
    [ "$#" -eq 2 ] || usage
    variant=$2
    validate_variant "$variant"
    ensure_only=1
    ;;
  --ensure-kit-sdk)
    # Same publisher and key as --ensure-toolchain, without image inputs.
    [ "$#" -eq 1 ] || usage
    ensure_only=1
    kit_sdk=1
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
if [ "$ensure_only" = 1 ]; then
  if [ "$kit_sdk" = 1 ]; then
    ensure_kit_sdk
  else
    ensure_toolchain
  fi
  printf '%s\n' "$toolchain_sha"
  exit
fi
if [ "$promote_existing" -ne 1 ]; then
  ensure_toolchain
  if [ -n "${FES_TARGET_IMAGE_SHARED_CACHE:-}" ]; then
    unset CCACHE_DISABLE
  fi
  if [ "$image_passes" = 1 ]; then
    /bin/rm -rf "$output_root/work-2-$variant"
  fi
  for run in $(if [ "$image_passes" = 1 ]; then printf 1; else printf '1 2'; fi); do
    work=$output_root/work-$run-$variant
    case "$work" in
      "$output_root"/work-[12]-"$variant") : ;;
      *) exit 2 ;;
    esac
    /bin/rm -rf "$work"
    if [ -n "${TARGET_IMAGE_BUILD_ONCE:-}" ]; then
      if [ "$run" = 2 ] && [ -n "${FES_TARGET_IMAGE_SHARED_CACHE:-}" ]; then
        CCACHE_DISABLE=1 TARGET_IMAGE_TOOLCHAIN_PATH=$(python3 "$repo/scripts/toolchain_cache.py" path) \
          "$TARGET_IMAGE_BUILD_ONCE" "$variant" "$work" "$epoch"
      else
        TARGET_IMAGE_TOOLCHAIN_PATH=$(python3 "$repo/scripts/toolchain_cache.py" path) \
          "$TARGET_IMAGE_BUILD_ONCE" "$variant" "$work" "$epoch"
      fi
    else
      if [ "$run" = 2 ] && [ -n "${FES_TARGET_IMAGE_SHARED_CACHE:-}" ]; then
        CCACHE_DISABLE=1 run_target_container "$variant" run \
          /work/scripts/build-target-image.sh --inside "$variant" "/target-image-output/work-$run-$variant" "$epoch" "/work/build/output/target-image/work-$run-$variant/images/rootfs.ext4"
      else
        run_target_container "$variant" run \
          /work/scripts/build-target-image.sh --inside "$variant" "/target-image-output/work-$run-$variant" "$epoch" "/work/build/output/target-image/work-$run-$variant/images/rootfs.ext4"
      fi
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
toolchain_key=$(python3 "$repo/scripts/toolchain_cache.py" key)
toolchain_sha=$(python3 "$repo/scripts/toolchain_cache.py" status) || {
  printf '%s\n' 'build-target-image: toolchain cache failed validation' >&2; exit 1;
}

first=$output_root/work-1-$variant/images/rootfs.ext4
second=$output_root/work-2-$variant/images/rootfs.ext4
first_sha=$(/usr/bin/shasum -a 256 "$first" | /usr/bin/awk '{print $1}')
if [ "$image_passes" = 2 ]; then
  second_sha=$(/usr/bin/shasum -a 256 "$second" | /usr/bin/awk '{print $1}')
  if [ "$first_sha" != "$second_sha" ]; then
    printf 'build-target-image: %s is not reproducible: %s != %s\n' "$variant" "$first_sha" "$second_sha" >&2
    exit 1
  fi
  selected_work=$output_root/work-2-$variant
  selected_image=$second
  selected_sha=$second_sha
else
  second_sha=
  selected_work=$output_root/work-1-$variant
  selected_image=$first
  selected_sha=$first_sha
fi

if [ "$variant" = native-dev ]; then
  package_cores=$(selected_package_cores)
  for package_core in $package_cores; do
    if [ "$image_passes" = 2 ]; then
      cmp "$output_root/work-1-$variant/fes-$package_core.package-selection.toml" \
        "$output_root/work-2-$variant/fes-$package_core.package-selection.toml" || {
        printf 'build-target-image: FES %s package selection differs between outputs\n' "$package_core" >&2
        exit 1
      }
    fi
    [ -f "$output_root/work-1-$variant/fes-$package_core.package-selection.toml" ] || {
      printf 'build-target-image: missing FES %s package selection\n' "$package_core" >&2
      exit 1
    }
  done
  if [ -n "${FES_VIDEO_PARTS_DIR:-}" ]; then
    if [ "$image_passes" = 2 ]; then cmp "$output_root/work-1-$variant/fes-core-video-parts.json" \
      "$output_root/work-2-$variant/fes-core-video-parts.json" || {
      echo 'build-target-image: factory video index differs between reproducible outputs' >&2
      exit 1
    }; fi
  fi
  for work in "$output_root/work-1-$variant" $(if [ "$image_passes" = 2 ]; then printf '%s' "$output_root/work-2-$variant"; fi); do
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
/bin/cp "$selected_image" "$image_tmp"
if [ "$image_passes" = 1 ]; then
  printf 'source_date_epoch=%s\nimage_passes=1\nrun_1_sha256=%s\ntoolchain_key=%s\ntoolchain_sha256=%s\n' \
    "$epoch" "$first_sha" "$toolchain_key" "$toolchain_sha" > "$evidence_tmp"
else
  printf 'source_date_epoch=%s\nimage_passes=2\nrun_1_sha256=%s\nrun_2_sha256=%s\ntoolchain_key=%s\ntoolchain_sha256=%s\n' \
    "$epoch" "$first_sha" "$second_sha" "$toolchain_key" "$toolchain_sha" > "$evidence_tmp"
fi
if [ -n "${FES_TARGET_IMAGE_SHARED_CACHE:-}" ]; then
  printf 'shared_cache=1\nccache_pass_1=1\nccache_pass_2=0\n' >> "$evidence_tmp"
fi
if [ "$variant" = native-dev ]; then
  "$repo/scripts/native-extra-cores.sh" copy-records "$selected_work" "$final_dir"
fi
/bin/mv "$evidence_tmp" "$final_dir/reproducibility.txt"
/bin/mv "$image_tmp" "$final_dir/linux.img"
/bin/rm -f "$final_dir/SINGLE-PASS-SCRATCH.txt"

trap - EXIT INT TERM
printf 'target image %s image: %s\n' "$variant" "$selected_sha"
