#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${TARGET_IMAGE_LOCK:-$repo_root/build/target-image.sources.lock.toml}
runtime=${TARGET_IMAGE_CONTAINER_RUNTIME:-docker}
output_volume=${TARGET_IMAGE_OUTPUT_VOLUME:-fogcast-target-image-output}
package_lock=$repo_root/build/target-image-container-packages.sha256
native_mode=${NATIVE_RUNTIME_MODE:-package-only}
case "$native_mode" in
  format1|package-only) : ;;
  *)
    printf '%s\n' 'target-image-container: native runtime mode must be format1 or package-only' >&2
    exit 2
    ;;
esac

case "$output_volume" in
  *[!a-zA-Z0-9_.-]*|'')
    printf 'target-image-container: invalid output volume name: %s\n' "$output_volume" >&2
    exit 2
    ;;
esac

if ! command -v "$runtime" >/dev/null 2>&1; then
  printf 'target-image-container: container runtime is not executable: %s\n' "$runtime" >&2
  exit 2
fi

[ "$#" -ge 2 ] || {
  printf 'usage: target-image-container.sh fetch|run COMMAND [ARG...]\n' >&2
  exit 2
}
mode=$1
shift
case "$mode" in
  fetch|run) : ;;
  *)
    printf 'target-image-container: mode must be fetch or run\n' >&2
    exit 2
    ;;
esac


"$repo_root/scripts/native-extra-cores.sh" validate
package_enabled=0
package_ids_reverse=
load_package_mount_order() {
  remaining=${FES_PACKAGE_IDS:-}
  while :; do
    case "$remaining" in
      *,*) package_id=${remaining%%,*}; remaining=${remaining#*,} ;;
      *) package_id=$remaining; remaining= ;;
    esac
    case "$package_id" in
      fes.pong) package_core=pong ;;
      fes.zx81) package_core=zx81 ;;
      fes.coleco) package_core=coleco ;;
      *) exit 2 ;;
    esac
    package_ids_reverse="$package_core $package_ids_reverse"
    [ -n "$remaining" ] || break
  done
}
if [ "$native_mode" = package-only ]; then
  load_package_mount_order
else
  if [ -n "${FES_PONG_PACKAGE_DIR:-}" ]; then package_enabled=1; fi
fi
fogcast_dir=
if [ -n "${FOGCAST_DIR:-}" ]; then
  case "$FOGCAST_DIR" in
    /*) : ;;
    *)
      printf '%s\n' 'target-image-container: FOGCAST_DIR must be absolute' >&2
      exit 2
      ;;
  esac
  [ -d "$FOGCAST_DIR" ] || {
    printf '%s\n' 'target-image-container: FOGCAST_DIR is not a directory' >&2
    exit 2
  }
  fogcast_dir=$(CDPATH='' cd -- "$FOGCAST_DIR" && pwd -P)
fi
docker_run() {
  if [ "$native_mode" = package-only ]; then
    set -- --env "FES_PACKAGE_IDS=$FES_PACKAGE_IDS" "$@"
    for package_core in $package_ids_reverse; do
      case "$package_core" in
        pong)
          package_dir=$FES_PONG_PACKAGE_DIR
          package_selection=$FES_PONG_PACKAGE_SELECTION
          package_dir_env=FES_PONG_PACKAGE_DIR
          package_selection_env=FES_PONG_PACKAGE_SELECTION
          ;;
        zx81)
          package_dir=$FES_ZX81_PACKAGE_DIR
          package_selection=$FES_ZX81_PACKAGE_SELECTION
          package_dir_env=FES_ZX81_PACKAGE_DIR
          package_selection_env=FES_ZX81_PACKAGE_SELECTION
          ;;
        coleco)
          package_dir=$FES_COLECO_PACKAGE_DIR
          package_selection=$FES_COLECO_PACKAGE_SELECTION
          package_dir_env=FES_COLECO_PACKAGE_DIR
          package_selection_env=FES_COLECO_PACKAGE_SELECTION
          ;;
        *) exit 2 ;;
      esac
      set -- --volume "$package_dir:/fes-$package_core-package:ro" \
        --env "$package_dir_env=/fes-$package_core-package" \
        --volume "$package_selection:/fes-$package_core-package-selection.toml:ro" \
        --env "$package_selection_env=/fes-$package_core-package-selection.toml" "$@"
    done
  fi
  if [ -n "$fogcast_dir" ]; then
    exec "$runtime" run --volume "$fogcast_dir:/fogcast:ro" \
      --env FOGCAST_DIR=/fogcast --env "NATIVE_RUNTIME_MODE=$native_mode" "$@"
  fi
  exec "$runtime" run --env "NATIVE_RUNTIME_MODE=$native_mode" "$@"
}
run_container() {
  if [ "${NATIVE_RUNTIME_SYSTEMS:-megadrive}" = 'megadrive pong snes nes' ]; then
    # Bundles are needed only for fetch; run/verify consume the sealed cache.
    if [ "$mode" = fetch ]; then
      for bundle in "${PONG_RBF_BUNDLE:-}" "${SNES_RBF_BUNDLE:-}" "${NES_RBF_BUNDLE:-}"; do
        case "$bundle" in /*) ;; *) echo 'absolute native extra-core bundles required' >&2; exit 2 ;; esac
        [ -d "$bundle" ] && [ ! -L "$bundle" ] || exit 2
      done
      if [ "$package_enabled" -eq 1 ]; then
        docker_run --env 'NATIVE_RUNTIME_SYSTEMS=megadrive pong snes nes' \
          --volume "$PONG_RBF_BUNDLE:/pong-rbf-bundle:ro" --env PONG_RBF_BUNDLE=/pong-rbf-bundle \
          --volume "$SNES_RBF_BUNDLE:/snes-rbf-bundle:ro" --env SNES_RBF_BUNDLE=/snes-rbf-bundle \
          --volume "$NES_RBF_BUNDLE:/nes-rbf-bundle:ro" --env NES_RBF_BUNDLE=/nes-rbf-bundle \
          --volume "$FES_PONG_PACKAGE_DIR:/fes-pong-package:ro" --env FES_PONG_PACKAGE_DIR=/fes-pong-package \
          --volume "$FES_PONG_PACKAGE_SELECTION:/fes-pong-package-selection.toml:ro" --env FES_PONG_PACKAGE_SELECTION=/fes-pong-package-selection.toml "$@"
      fi
      docker_run --env 'NATIVE_RUNTIME_SYSTEMS=megadrive pong snes nes' \
        --volume "$PONG_RBF_BUNDLE:/pong-rbf-bundle:ro" --env PONG_RBF_BUNDLE=/pong-rbf-bundle \
        --volume "$SNES_RBF_BUNDLE:/snes-rbf-bundle:ro" --env SNES_RBF_BUNDLE=/snes-rbf-bundle \
        --volume "$NES_RBF_BUNDLE:/nes-rbf-bundle:ro" --env NES_RBF_BUNDLE=/nes-rbf-bundle "$@"
    fi
    if [ "$package_enabled" -eq 1 ]; then
      docker_run --env 'NATIVE_RUNTIME_SYSTEMS=megadrive pong snes nes' \
        --volume "$FES_PONG_PACKAGE_DIR:/fes-pong-package:ro" --env FES_PONG_PACKAGE_DIR=/fes-pong-package \
        --volume "$FES_PONG_PACKAGE_SELECTION:/fes-pong-package-selection.toml:ro" --env FES_PONG_PACKAGE_SELECTION=/fes-pong-package-selection.toml "$@"
    fi
    docker_run --env 'NATIVE_RUNTIME_SYSTEMS=megadrive pong snes nes' "$@"
  fi
  if [ "$native_mode" != package-only ] && [ "$package_enabled" -eq 1 ]; then
    docker_run \
      --volume "$FES_PONG_PACKAGE_DIR:/fes-pong-package:ro" --env FES_PONG_PACKAGE_DIR=/fes-pong-package \
      --volume "$FES_PONG_PACKAGE_SELECTION:/fes-pong-package-selection.toml:ro" --env FES_PONG_PACKAGE_SELECTION=/fes-pong-package-selection.toml "$@"
  fi
  docker_run "$@"
}
native_runtime_source=
native_runtime_commit=
if [ -n "${LIBMISTER_RUNTIME_DIR:-}" ]; then
  case "$LIBMISTER_RUNTIME_DIR" in
    /*) : ;;
    *)
      printf '%s\n' 'target-image-container: LIBMISTER_RUNTIME_DIR must be absolute' >&2
      exit 2
      ;;
  esac
  [ -d "$LIBMISTER_RUNTIME_DIR" ] || {
    printf '%s\n' 'target-image-container: runtime source is not a directory' >&2
    exit 2
  }
  native_runtime_source=$(CDPATH='' cd -- "$LIBMISTER_RUNTIME_DIR" && pwd -P)
  [ -n "$fogcast_dir" ] || {
    printf '%s\n' 'target-image-container: FOGCAST_DIR is required for native runtime' >&2
    exit 2
  }
  native_lock=${NATIVE_RUNTIME_INPUT_LOCK:-$fogcast_dir/build/native-runtime.inputs.lock.toml}
  native_idle=${NATIVE_RUNTIME_IDLE_FILE:-$repo_root/build/cache/target-image/native/idle.rbf}
  if [ "$native_mode" = package-only ]; then
    NATIVE_RUNTIME_MODE=package-only \
      "$repo_root/scripts/verify-native-runtime-inputs.sh" \
      "$native_lock" "$native_runtime_source" "$native_idle"
  else
    native_megadrive=${NATIVE_RUNTIME_MEGADRIVE_FILE:-$repo_root/build/cache/target-image/native/megadrive.rbf}
    native_selection=${NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE:-$repo_root/build/cache/target-image/native/megadrive.selection.toml}
    "$repo_root/scripts/verify-native-runtime-inputs.sh" \
      "$native_lock" "$native_runtime_source" "$native_idle" "$native_megadrive" \
      "$native_selection"
  fi
  native_runtime_commit=$(git -C "$native_runtime_source" rev-parse --verify HEAD)
fi

native_megadrive_source=${MEGADRIVE_RBF_SOURCE:-}
native_megadrive_bundle=
if [ -n "$native_megadrive_source" ]; then
  case "$native_megadrive_source" in
    source-built)
      [ -n "${MEGADRIVE_RBF_BUNDLE:-}" ] || {
        printf '%s\n' 'target-image-container: source-built requires MEGADRIVE_RBF_BUNDLE' >&2
        exit 2
      }
      case "$MEGADRIVE_RBF_BUNDLE" in
        /*) : ;;
        *)
          printf '%s\n' 'target-image-container: source-built bundle must be absolute' >&2
          exit 2
          ;;
      esac
      [ -d "$MEGADRIVE_RBF_BUNDLE" ] && [ ! -L "$MEGADRIVE_RBF_BUNDLE" ] || {
        printf '%s\n' 'target-image-container: source-built bundle is not a directory' >&2
        exit 2
      }
      native_megadrive_bundle=$(CDPATH='' cd -- "$MEGADRIVE_RBF_BUNDLE" && pwd -P)
      ;;
    upstream)
      [ -z "${MEGADRIVE_RBF_BUNDLE:-}" ] || {
        printf '%s\n' 'target-image-container: upstream forbids MEGADRIVE_RBF_BUNDLE' >&2
        exit 2
      }
      ;;
    *)
      printf 'target-image-container: unsupported Mega Drive source: %s\n' "$native_megadrive_source" >&2
      exit 2
      ;;
  esac
fi

if [ "${TARGET_IMAGE_DEV_CONTAINER:-0}" = 1 ]; then
  dev_platform=${TARGET_IMAGE_DEV_PLATFORM:-}
  if [ -z "$dev_platform" ]; then
    case "$(uname -m)" in
      x86_64|amd64) dev_platform=linux/amd64 ;;
      aarch64|arm64) dev_platform=linux/arm64 ;;
      armv7l|armv7*) dev_platform=linux/arm/v7 ;;
      *)
        printf 'target-image-container: unsupported development host architecture: %s\n' "$(uname -m)" >&2
        exit 2
        ;;
    esac
  fi
  case "$dev_platform" in
    linux/amd64|linux/arm64|linux/arm/v7) : ;;
    *)
      printf 'target-image-container: unsupported development platform: %s\n' "$dev_platform" >&2
      exit 2
      ;;
  esac
  dev_image=fogcast-target-image-dev-build
  host_uid=$(id -u)
  host_gid=$(id -g)
  dev_context_digest=$(
    /usr/bin/shasum -a 256 \
      "$repo_root/containers/target-image/Dockerfile.dev" \
      | /usr/bin/awk '{print substr($1, 1, 12)}'
  )
  dev_image="$dev_image:$dev_context_digest-$host_uid-$host_gid"
  if ! "$runtime" image inspect "$dev_image" >/dev/null 2>&1; then
    "$runtime" build \
      --platform "$dev_platform" \
      --build-arg "HOST_UID=$host_uid" \
      --build-arg "HOST_GID=$host_gid" \
      --tag "$dev_image" \
      --file "$repo_root/containers/target-image/Dockerfile.dev" \
      "$repo_root"
  fi
  if [ "$mode" = run ]; then
    if [ -n "$native_runtime_source" ]; then
      run_container --rm \
        --platform "$dev_platform" \
        --network none \
        --ulimit core=0:0 \
        --user "$host_uid:$host_gid" \
        --volume "$repo_root:/work" \
        --volume "$output_volume:/target-image-output" \
        --volume "$native_runtime_source:/runtime-source:ro" \
        --env "FOGCAST_MISTER_RUNTIME_COMMIT=$native_runtime_commit" \
        --workdir /work \
        "$dev_image" "$@"
    fi
    run_container --rm \
      --platform "$dev_platform" \
      --network none \
      --ulimit core=0:0 \
      --user "$host_uid:$host_gid" \
      --volume "$repo_root:/work" \
      --volume "$output_volume:/target-image-output" \
      --workdir /work \
      "$dev_image" "$@"
  fi
  if [ -n "$native_runtime_source" ]; then
    run_container --rm \
      --platform "$dev_platform" \
      --ulimit core=0:0 \
      --user "$host_uid:$host_gid" \
      --volume "$repo_root:/work" \
      --volume "$output_volume:/target-image-output" \
      --volume "$native_runtime_source:/runtime-source:ro" \
      --env "FOGCAST_MISTER_RUNTIME_COMMIT=$native_runtime_commit" \
      --workdir /work \
      "$dev_image" "$@"
  fi
  run_container --rm \
    --platform "$dev_platform" \
    --ulimit core=0:0 \
    --user "$host_uid:$host_gid" \
    --volume "$repo_root:/work" \
    --volume "$output_volume:/target-image-output" \
    --workdir /work \
    "$dev_image" "$@"
fi

[ -f "$lock" ] || {
  printf 'target-image-container: lock does not exist: %s\n' "$lock" >&2
  exit 2
}
[ -f "$package_lock" ] || {
  printf 'target-image-container: package-set lock does not exist: %s\n' "$package_lock" >&2
  exit 2
}
package_set_sha=$(tr -d '[:space:]' < "$package_lock")
printf '%s\n' "$package_set_sha" | grep -Eq '^[0-9a-f]{64}$' || {
  printf '%s\n' 'target-image-container: invalid package-set digest' >&2
  exit 2
}

read_container_key() {
  key=$1
  awk -v wanted="$key" '
    /^\[/ { section=$0; gsub(/^\[|\]$/, "", section); next }
    section == "container" && $0 ~ "^" wanted "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) && substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$lock"
}

base_image=$(read_container_key image)
platform=$(read_container_key platform)
digest=$(read_container_key digest)
test "$base_image" = docker.io/library/debian:12.11-slim
test "$platform" = linux/amd64
if ! printf '%s\n' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$'; then
  printf 'target-image-container: invalid immutable container digest\n' >&2
  exit 2
fi

short_digest=$(printf '%s' "$digest" | cut -c8-19)
host_uid=$(id -u)
host_gid=$(id -g)
# Hash ordered content digests only; checkout paths do not affect the builder.
context_digest=$(
  /usr/bin/shasum -a 256 \
    "$repo_root/containers/target-image/Dockerfile" \
    "$repo_root/containers/target-image/create-builder-user.sh" \
    "$package_lock" \
    "$lock" |
    /usr/bin/awk '{print $1}' |
    /usr/bin/shasum -a 256 |
    /usr/bin/awk '{print substr($1, 1, 12)}'
)
build_image=fogcast-target-image-build:$short_digest-$context_digest-$host_uid-$host_gid
base_ref=$base_image@$digest

if ! repo_digests=$("$runtime" image inspect "$base_ref" --format '{{join .RepoDigests "\n"}}' 2>/dev/null); then
  printf 'target-image-container: pinned base image is unavailable; run make target-image-resolve first\n' >&2
  exit 2
fi
if ! printf '%s\n' "$repo_digests" | grep -Fq -- "@$digest"; then
  printf 'target-image-container: local base image does not match locked digest %s\n' "$digest" >&2
  exit 2
fi

if ! "$runtime" image inspect "$build_image" >/dev/null 2>&1; then
  "$runtime" build \
    --platform "$platform" \
    --build-arg "BASE_IMAGE=$base_image@$digest" \
    --build-arg "HOST_UID=$host_uid" \
    --build-arg "HOST_GID=$host_gid" \
    --build-arg "BASE_DIGEST=$digest" \
    --build-arg "CONTEXT_DIGEST=$context_digest" \
    --build-arg "PACKAGE_SET_SHA256=$package_set_sha" \
    --tag "$build_image" \
    --file "$repo_root/containers/target-image/Dockerfile" \
    "$repo_root"
fi

build_metadata=$("$runtime" image inspect "$build_image" --format '{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "org.fogcast.target-image.base-digest"}}|{{index .Config.Labels "org.fogcast.target-image.context-digest"}}|{{index .Config.Labels "org.fogcast.target-image.package-set-sha256"}}|{{index .Config.Labels "org.fogcast.target-image.host-uid"}}|{{index .Config.Labels "org.fogcast.target-image.host-gid"}}')
old_ifs=$IFS
IFS='|' read -r build_image_id actual_platform label_base label_context label_packages label_uid label_gid <<EOF
$build_metadata
EOF
IFS=$old_ifs
if ! printf '%s\n' "$build_image_id" | grep -Eq '^sha256:[0-9a-f]{64}$' || \
   [ "$actual_platform" != "$platform" ] || \
   [ "$label_base" != "$digest" ] || \
   [ "$label_context" != "$context_digest" ] || \
   [ "$label_packages" != "$package_set_sha" ] || \
   [ "$label_uid" != "$host_uid" ] || \
   [ "$label_gid" != "$host_gid" ]; then
  printf '%s\n' 'target-image-container: cached build image provenance does not match locked inputs' >&2
  exit 1
fi

if [ "$mode" = run ]; then
  if [ -n "$native_runtime_source" ]; then
    if [ -n "$native_megadrive_source" ] && [ -n "$native_megadrive_bundle" ]; then
      run_container --rm \
        --platform "$platform" \
        --network none \
        --ulimit core=0:0 \
        --user "$host_uid:$host_gid" \
        --volume "$repo_root:/work" \
        --volume "$output_volume:/target-image-output" \
        --volume "$native_runtime_source:/runtime-source:ro" \
        --volume "$native_megadrive_bundle:/megadrive-rbf-bundle:ro" \
        --env "FOGCAST_MISTER_RUNTIME_COMMIT=$native_runtime_commit" \
        --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
        --env 'MEGADRIVE_RBF_BUNDLE=/megadrive-rbf-bundle' \
        --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
        --workdir /work \
        "$build_image_id" "$@"
    fi
    run_container --rm \
      --platform "$platform" \
      --network none \
      --ulimit core=0:0 \
      --user "$host_uid:$host_gid" \
      --volume "$repo_root:/work" \
      --volume "$output_volume:/target-image-output" \
      --volume "$native_runtime_source:/runtime-source:ro" \
      --env "FOGCAST_MISTER_RUNTIME_COMMIT=$native_runtime_commit" \
      --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
      --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
      --workdir /work \
      "$build_image_id" "$@"
  fi
  if [ -n "$native_megadrive_source" ] && [ -n "$native_megadrive_bundle" ]; then
    run_container --rm \
      --platform "$platform" \
      --network none \
      --ulimit core=0:0 \
      --user "$host_uid:$host_gid" \
      --volume "$repo_root:/work" \
      --volume "$output_volume:/target-image-output" \
      --volume "$native_megadrive_bundle:/megadrive-rbf-bundle:ro" \
      --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
      --env 'MEGADRIVE_RBF_BUNDLE=/megadrive-rbf-bundle' \
      --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
      --workdir /work \
      "$build_image_id" "$@"
  fi
  if [ -n "$native_megadrive_source" ]; then
    run_container --rm \
      --platform "$platform" \
      --network none \
      --ulimit core=0:0 \
      --user "$host_uid:$host_gid" \
      --volume "$repo_root:/work" \
      --volume "$output_volume:/target-image-output" \
      --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
      --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
      --workdir /work \
      "$build_image_id" "$@"
  fi
  run_container --rm \
    --platform "$platform" \
    --network none \
    --ulimit core=0:0 \
    --user "$host_uid:$host_gid" \
    --volume "$repo_root:/work" \
    --volume "$output_volume:/target-image-output" \
    --workdir /work \
    "$build_image_id" "$@"
fi

if [ -n "$native_runtime_source" ]; then
  if [ -n "$native_megadrive_source" ] && [ -n "$native_megadrive_bundle" ]; then
    run_container --rm \
      --platform "$platform" \
      --ulimit core=0:0 \
      --user "$host_uid:$host_gid" \
      --volume "$repo_root:/work" \
      --volume "$output_volume:/target-image-output" \
      --volume "$native_runtime_source:/runtime-source:ro" \
      --volume "$native_megadrive_bundle:/megadrive-rbf-bundle:ro" \
      --env "FOGCAST_MISTER_RUNTIME_COMMIT=$native_runtime_commit" \
      --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
      --env 'MEGADRIVE_RBF_BUNDLE=/megadrive-rbf-bundle' \
      --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
      --workdir /work \
      "$build_image_id" "$@"
  fi
  if [ -n "$native_megadrive_source" ]; then
    run_container --rm \
      --platform "$platform" \
      --ulimit core=0:0 \
      --user "$host_uid:$host_gid" \
      --volume "$repo_root:/work" \
      --volume "$output_volume:/target-image-output" \
      --volume "$native_runtime_source:/runtime-source:ro" \
      --env "FOGCAST_MISTER_RUNTIME_COMMIT=$native_runtime_commit" \
      --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
      --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
      --workdir /work \
      "$build_image_id" "$@"
  fi
  run_container --rm \
    --platform "$platform" \
    --ulimit core=0:0 \
    --user "$host_uid:$host_gid" \
    --volume "$repo_root:/work" \
    --volume "$output_volume:/target-image-output" \
    --volume "$native_runtime_source:/runtime-source:ro" \
    --env "FOGCAST_MISTER_RUNTIME_COMMIT=$native_runtime_commit" \
    --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
    --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
    --workdir /work \
    "$build_image_id" "$@"
fi

if [ -n "$native_megadrive_source" ] && [ -n "$native_megadrive_bundle" ]; then
  run_container --rm \
    --platform "$platform" \
    --ulimit core=0:0 \
    --user "$host_uid:$host_gid" \
    --volume "$repo_root:/work" \
    --volume "$output_volume:/target-image-output" \
    --volume "$native_megadrive_bundle:/megadrive-rbf-bundle:ro" \
    --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
    --env 'MEGADRIVE_RBF_BUNDLE=/megadrive-rbf-bundle' \
    --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
    --workdir /work \
    "$build_image_id" "$@"
fi

if [ -n "$native_megadrive_source" ]; then
  run_container --rm \
    --platform "$platform" \
    --ulimit core=0:0 \
    --user "$host_uid:$host_gid" \
    --volume "$repo_root:/work" \
    --volume "$output_volume:/target-image-output" \
    --env "MEGADRIVE_RBF_SOURCE=$native_megadrive_source" \
    --env 'NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=/work/build/cache/target-image/native/megadrive.selection.toml' \
    --workdir /work \
    "$build_image_id" "$@"
fi

run_container --rm \
  --platform "$platform" \
  --ulimit core=0:0 \
  --user "$host_uid:$host_gid" \
  --volume "$repo_root:/work" \
  --volume "$output_volume:/target-image-output" \
  --workdir /work \
  "$build_image_id" "$@"
