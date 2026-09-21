#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-dev-container.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

# Native development must use the host platform; the locked release wrapper
# remains linux/amd64. An explicit override is available for cross-host builds.
grep -Fq 'TARGET_IMAGE_DEV_CONTAINER' "$repo/scripts/target-image-container.sh"
grep -Fq 'TARGET_IMAGE_DEV_PLATFORM' "$repo/scripts/target-image-container.sh"
grep -Fq -- '--platform "$dev_platform"' "$repo/scripts/target-image-container.sh"
grep -Fq 'TARGET_IMAGE_OUTPUT_VOLUME=fogcast-target-image-output' "$repo/Makefile"

docker_log=$fixture/docker.log
fake_docker=$fixture/docker
cat > "$fake_docker" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$TARGET_IMAGE_DOCKER_LOG"
case "$1 $2" in
  "image inspect")
    printf 'sha256:2222222222222222222222222222222222222222222222222222222222222222\n'
    ;;
  build) : ;;
  run) : ;;
esac
EOF
chmod 0755 "$fake_docker"

fake_bin=$fixture/bin
mkdir -p "$fake_bin"
cat > "$fake_bin/uname" <<'EOF'
#!/bin/sh
test "$1" = -m
printf '%s\n' x86_64
EOF
chmod 0755 "$fake_bin/uname"

TARGET_IMAGE_CONTAINER_RUNTIME=$fake_docker \
TARGET_IMAGE_DEV_CONTAINER=1 \
TARGET_IMAGE_DOCKER_LOG=$docker_log \
PATH="$fake_bin:$PATH" \
  sh "$repo/scripts/target-image-container.sh" fetch true

grep -Fq -- '--platform linux/amd64' "$docker_log"
grep -Fq -- 'fogcast-target-image-dev-build' "$docker_log"
! grep -Fq -- '--network none' "$docker_log"
! grep -Fq -- 'GITHUB_TOKEN' "$docker_log"
! grep -Fq -- 'GH_TOKEN' "$docker_log"

: > "$docker_log"
GITHUB_TOKEN=fixture-github-token \
TARGET_IMAGE_CONTAINER_RUNTIME=$fake_docker \
TARGET_IMAGE_DEV_CONTAINER=1 \
TARGET_IMAGE_DOCKER_LOG=$docker_log \
PATH="$fake_bin:$PATH" \
  sh "$repo/scripts/target-image-container.sh" fetch true
grep -Fq -- '--env GITHUB_TOKEN=fixture-github-token' "$docker_log" || {
  echo 'fetch container did not receive GITHUB_TOKEN' >&2
  exit 1
}

: > "$docker_log"
TARGET_IMAGE_CONTAINER_RUNTIME=$fake_docker \
TARGET_IMAGE_DEV_CONTAINER=1 \
TARGET_IMAGE_DOCKER_LOG=$docker_log \
TARGET_IMAGE_DEV_PLATFORM=linux/arm64 \
  sh "$repo/scripts/target-image-container.sh" run true

grep -Fq -- '--platform linux/arm64' "$docker_log"
grep -Fq -- '--network none' "$docker_log"
grep -Fq -- 'fogcast-target-image-dev-build' "$docker_log"
! grep -Fq -- 'GITHUB_TOKEN' "$docker_log"
! grep -Fq -- 'GH_TOKEN' "$docker_log"

echo 'target image native development container tests passed'
