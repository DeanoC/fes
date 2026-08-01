#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
lock=${POC1B_LOCK:-$repo_root/build/sources.poc1b.lock.toml}
runtime=${POC1B_CONTAINER_RUNTIME:-docker}

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

[ -f "$lock" ] || {
  printf 'poc1b-container: lock does not exist: %s\n' "$lock" >&2
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
build_image=mister-remote-poc1b-build:$short_digest
host_uid=$(id -u)
host_gid=$(id -g)
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
    --tag "$build_image" \
    --file "$repo_root/containers/poc1b/Dockerfile" \
    "$repo_root"
fi

actual_platform=$("$runtime" image inspect "$build_image" --format '{{.Os}}/{{.Architecture}}')
if [ "$actual_platform" != "$platform" ]; then
  printf 'poc1b-container: build image platform is %s, expected %s\n' "$actual_platform" "$platform" >&2
  exit 2
fi

if [ "$mode" = run ]; then
  exec "$runtime" run --rm \
    --platform "$platform" \
    --network none \
    --user "$host_uid:$host_gid" \
    --volume "$repo_root:/work" \
    --workdir /work \
    "$build_image" "$@"
fi

exec "$runtime" run --rm \
  --platform "$platform" \
  --user "$host_uid:$host_gid" \
  --volume "$repo_root:/work" \
  --workdir /work \
  "$build_image" "$@"
