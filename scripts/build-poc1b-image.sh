#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
epoch=1751459412

usage() {
  printf 'usage: build-poc1b-image.sh prod|dev|--promote-existing VARIANT|--fetch VARIANT|--inside VARIANT OUTPUT EPOCH EXPORT|--inside-fetch VARIANT OUTPUT EPOCH|--validate-inside-path VARIANT OUTPUT EXPORT\n' >&2
  exit 2
}

validate_inside_paths() {
  path_variant=$1
  path_output=$2
  path_export=$3
  validate_variant "$path_variant"
  for path_run in 1 2; do
    if [ "$path_output" = "/poc1b-output/work-$path_run-$path_variant" ] && \
       [ "$path_export" = "/work/build/output/poc1b/work-$path_run-$path_variant/images/rootfs.ext4" ]; then
      return 0
    fi
  done
  printf 'build-poc1b-image: unsafe or mismatched build paths: %s -> %s\n' \
    "$path_output" "$path_export" >&2
  return 1
}

validate_variant() {
  case "$1" in
    prod|dev) : ;;
    *) usage ;;
  esac
}

defconfig_for() {
  printf 'mister_remote_poc1b_%s_defconfig\n' "$1"
}

inside_build() {
  inside_variant=$1
  inside_output=$2
  inside_epoch=$3
  inside_mode=${4:-build}
  inside_export=${5:--}
  validate_variant "$inside_variant"
  test "$(/usr/bin/id -u)" -ne 0 || {
    printf '%s\n' 'build-poc1b-image: refusing to run Buildroot as root' >&2
    exit 1
  }
  case "$inside_output" in
    /poc1b-output/*) : ;;
    *)
      printf 'build-poc1b-image: unsafe container output path: %s\n' "$inside_output" >&2
      exit 2
      ;;
  esac
  test "$inside_epoch" = "$epoch"
  if [ "$inside_mode" = fetch ]; then
    test "$inside_output" = "/poc1b-output/fetch-$inside_variant" || {
      printf 'build-poc1b-image: unsafe fetch output path: %s\n' "$inside_output" >&2
      exit 2
    }
  else
    validate_inside_paths "$inside_variant" "$inside_output" "$inside_export"
  fi

  /work/scripts/verify-poc1b-source-cache.sh \
    /work/build/sources.poc1b.lock.toml \
    /work/build/cache/poc1b
  /work/bin/poc1b-lock-linux-amd64 verify-inputs \
    --lock /work/build/sources.poc1b.lock.toml \
    --cache /work/build/cache/poc1b

  /bin/rm -rf "$inside_output"
  export SOURCE_DATE_EPOCH=$inside_epoch
  export E2FSPROGS_FAKE_TIME=$inside_epoch
  make -C /work/build/cache/poc1b/buildroot \
    O="$inside_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/poc1b/dl \
    "$(defconfig_for "$inside_variant")"

  if [ "$inside_mode" = fetch ]; then
    make -C /work/build/cache/poc1b/buildroot \
      O="$inside_output" \
      BR2_EXTERNAL=/work/buildroot \
      BR2_DL_DIR=/work/build/cache/poc1b/dl \
      source
    return
  fi

  make -C /work/build/cache/poc1b/buildroot \
    O="$inside_output" \
    BR2_EXTERNAL=/work/buildroot \
    BR2_DL_DIR=/work/build/cache/poc1b/dl
  test -f "$inside_output/images/rootfs.ext4"
  /bin/mkdir -p "$(dirname "$inside_export")"
  /bin/cp "$inside_output/images/rootfs.ext4" "$inside_export"
}

promote_existing=0
case "${1:-}" in
  --validate-inside-path)
    [ "$#" -eq 4 ] || usage
    test "${POC1B_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'build-poc1b-image: path validation interface requires test mode' >&2
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
    output=/poc1b-output/fetch-$variant
    exec "$repo/scripts/poc1b-container.sh" fetch \
      /work/scripts/build-poc1b-image.sh --inside-fetch "$variant" "$output" "$epoch"
    ;;
  prod|dev)
    [ "$#" -eq 1 ] || usage
    variant=$1
    ;;
  --promote-existing)
    [ "$#" -eq 2 ] || usage
    variant=$2
    validate_variant "$variant"
    test "${POC1B_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'build-poc1b-image: test mode is required for --promote-existing' >&2
      exit 2
    }
    promote_existing=1
    ;;
  *) usage ;;
esac

output_root=${POC1B_OUTPUT_ROOT:-$repo/build/output/poc1b}
if [ "${POC1B_TEST_MODE:-0}" != 1 ]; then
  test "$output_root" = "$repo/build/output/poc1b" || {
    printf '%s\n' 'build-poc1b-image: output override requires POC1B_TEST_MODE=1' >&2
    exit 2
  }
fi
case "$output_root" in
  /*) : ;;
  *)
    printf '%s\n' 'build-poc1b-image: output root must be absolute' >&2
    exit 2
    ;;
esac

/bin/mkdir -p "$output_root"
if [ -n "${POC1B_BUILD_ONCE:-}" ] && [ "${POC1B_TEST_MODE:-0}" != 1 ]; then
  printf '%s\n' 'build-poc1b-image: test mode is required for POC1B_BUILD_ONCE' >&2
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
    if [ -n "${POC1B_BUILD_ONCE:-}" ]; then
      "$POC1B_BUILD_ONCE" "$variant" "$work" "$epoch"
    else
      "$repo/scripts/poc1b-container.sh" run \
        /work/scripts/build-poc1b-image.sh --inside "$variant" "/poc1b-output/work-$run-$variant" "$epoch" "/work/build/output/poc1b/work-$run-$variant/images/rootfs.ext4"
    fi
    test -f "$work/images/rootfs.ext4" || {
      printf 'build-poc1b-image: build %s did not produce rootfs.ext4\n' "$run" >&2
      exit 1
    }
  done
fi

first=$output_root/work-1-$variant/images/rootfs.ext4
second=$output_root/work-2-$variant/images/rootfs.ext4
first_sha=$(/usr/bin/shasum -a 256 "$first" | /usr/bin/awk '{print $1}')
second_sha=$(/usr/bin/shasum -a 256 "$second" | /usr/bin/awk '{print $1}')
if [ "$first_sha" != "$second_sha" ]; then
  printf 'build-poc1b-image: %s is not reproducible: %s != %s\n' "$variant" "$first_sha" "$second_sha" >&2
  exit 1
fi

final_dir=$output_root/$variant
/bin/mkdir -p "$final_dir"
image_tmp=$final_dir/linux.img.new.$$
evidence_tmp=$final_dir/reproducibility.txt.new.$$
trap '/bin/rm -f "$image_tmp" "$evidence_tmp"' EXIT INT TERM
/bin/cp "$second" "$image_tmp"
printf 'source_date_epoch=%s\nrun_1_sha256=%s\nrun_2_sha256=%s\n' \
  "$epoch" "$first_sha" "$second_sha" > "$evidence_tmp"
/bin/mv "$image_tmp" "$final_dir/linux.img"
/bin/mv "$evidence_tmp" "$final_dir/reproducibility.txt"
trap - EXIT INT TERM
printf 'POC 1B %s image: %s\n' "$variant" "$second_sha"
