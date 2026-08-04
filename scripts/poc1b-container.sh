#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${POC1B_LOCK:-$repo_root/build/sources.poc1b.lock.toml}
runtime=${POC1B_CONTAINER_RUNTIME:-docker}
output_volume=${POC1B_OUTPUT_VOLUME:-mister-remote-poc1b-output}
package_lock=$repo_root/build/poc1b-container-packages.sha256

case "$output_volume" in
  *[!a-zA-Z0-9_.-]*|'')
    printf 'poc1b-container: invalid output volume name: %s\n' "$output_volume" >&2
    exit 2
    ;;
esac

if ! command -v "$runtime" >/dev/null 2>&1; then
  printf 'poc1b-container: container runtime is not executable: %s\n' "$runtime" >&2
  exit 2
fi

[ "$#" -ge 2 ] || {
  printf 'usage: poc1b-container.sh fetch|run COMMAND [ARG...]\n' >&2
  exit 2
}
mode=$1
shift
case "$mode" in
  fetch|run) : ;;
  *)
    printf 'poc1b-container: mode must be fetch or run\n' >&2
    exit 2
    ;;
esac

if [ "${POC1B_DEV_CONTAINER:-0}" = 1 ]; then
  dev_platform=linux/arm64
  dev_image=mister-remote-poc1b-dev-build
  host_uid=$(id -u)
  host_gid=$(id -g)
  dev_context_digest=$(
    /usr/bin/shasum -a 256 \
      "$repo_root/containers/poc1b/Dockerfile.dev" \
      | /usr/bin/awk '{print substr($1, 1, 12)}'
  )
  dev_image="$dev_image:$dev_context_digest-$host_uid-$host_gid"
  if ! "$runtime" image inspect "$dev_image" >/dev/null 2>&1; then
    "$runtime" build \
      --platform "$dev_platform" \
      --build-arg "HOST_UID=$host_uid" \
      --build-arg "HOST_GID=$host_gid" \
      --tag "$dev_image" \
      --file "$repo_root/containers/poc1b/Dockerfile.dev" \
      "$repo_root"
  fi
  if [ "$mode" = run ]; then
    exec "$runtime" run --rm \
      --platform "$dev_platform" \
      --network none \
      --ulimit core=0:0 \
      --user "$host_uid:$host_gid" \
      --volume "$repo_root:/work" \
      --volume "$output_volume:/poc1b-output" \
      --workdir /work \
      "$dev_image" "$@"
  fi
  exec "$runtime" run --rm \
    --platform "$dev_platform" \
    --ulimit core=0:0 \
    --user "$host_uid:$host_gid" \
    --volume "$repo_root:/work" \
    --volume "$output_volume:/poc1b-output" \
    --workdir /work \
    "$dev_image" "$@"
fi

[ -f "$lock" ] || {
  printf 'poc1b-container: lock does not exist: %s\n' "$lock" >&2
  exit 2
}
[ -f "$package_lock" ] || {
  printf 'poc1b-container: package-set lock does not exist: %s\n' "$package_lock" >&2
  exit 2
}
package_set_sha=$(tr -d '[:space:]' < "$package_lock")
printf '%s\n' "$package_set_sha" | grep -Eq '^[0-9a-f]{64}$' || {
  printf '%s\n' 'poc1b-container: invalid package-set digest' >&2
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
  printf 'poc1b-container: invalid immutable container digest\n' >&2
  exit 2
fi

short_digest=$(printf '%s' "$digest" | cut -c8-19)
host_uid=$(id -u)
host_gid=$(id -g)
context_digest=$(
  /usr/bin/shasum -a 256 \
    "$repo_root/containers/poc1b/Dockerfile" \
    "$repo_root/containers/poc1b/create-builder-user.sh" \
    "$package_lock" \
    "$lock" |
    /usr/bin/shasum -a 256 |
    /usr/bin/awk '{print substr($1, 1, 12)}'
)
build_image=mister-remote-poc1b-build:$short_digest-$context_digest-$host_uid-$host_gid
base_ref=$base_image@$digest

if ! repo_digests=$("$runtime" image inspect "$base_ref" --format '{{join .RepoDigests "\n"}}' 2>/dev/null); then
  printf 'poc1b-container: pinned base image is unavailable; run make poc1b-resolve first\n' >&2
  exit 2
fi
if ! printf '%s\n' "$repo_digests" | grep -Fq -- "@$digest"; then
  printf 'poc1b-container: local base image does not match locked digest %s\n' "$digest" >&2
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
    --file "$repo_root/containers/poc1b/Dockerfile" \
    "$repo_root"
fi

build_metadata=$("$runtime" image inspect "$build_image" --format '{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "org.mister-remote.poc1b.base-digest"}}|{{index .Config.Labels "org.mister-remote.poc1b.context-digest"}}|{{index .Config.Labels "org.mister-remote.poc1b.package-set-sha256"}}|{{index .Config.Labels "org.mister-remote.poc1b.host-uid"}}|{{index .Config.Labels "org.mister-remote.poc1b.host-gid"}}')
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
  printf '%s\n' 'poc1b-container: cached build image provenance does not match locked inputs' >&2
  exit 1
fi

if [ "$mode" = run ]; then
  exec "$runtime" run --rm \
    --platform "$platform" \
    --network none \
    --ulimit core=0:0 \
    --user "$host_uid:$host_gid" \
    --volume "$repo_root:/work" \
    --volume "$output_volume:/poc1b-output" \
    --workdir /work \
    "$build_image_id" "$@"
fi

exec "$runtime" run --rm \
  --platform "$platform" \
  --ulimit core=0:0 \
  --user "$host_uid:$host_gid" \
  --volume "$repo_root:/work" \
  --volume "$output_volume:/poc1b-output" \
  --workdir /work \
  "$build_image_id" "$@"
