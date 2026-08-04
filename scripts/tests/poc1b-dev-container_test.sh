#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-dev-container.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

# Native development must be a separate arm64 container path; the locked
# release wrapper remains linux/amd64.
grep -Fq 'POC1B_DEV_CONTAINER' "$repo/scripts/poc1b-container.sh"
grep -Fq 'dev_platform=linux/arm64' "$repo/scripts/poc1b-container.sh"
grep -Fq -- '--platform "$dev_platform"' "$repo/scripts/poc1b-container.sh"
grep -Fq 'mister-remote-poc1b-dev-output' "$repo/Makefile"

docker_log=$fixture/docker.log
fake_docker=$fixture/docker
cat > "$fake_docker" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$POC1B_DOCKER_LOG"
case "$1 $2" in
  "image inspect")
    printf 'sha256:2222222222222222222222222222222222222222222222222222222222222222\n'
    ;;
  build) : ;;
  run) : ;;
esac
EOF
chmod 0755 "$fake_docker"

POC1B_CONTAINER_RUNTIME=$fake_docker \
POC1B_DEV_CONTAINER=1 \
POC1B_DOCKER_LOG=$docker_log \
  sh "$repo/scripts/poc1b-container.sh" fetch true

grep -Fq -- '--platform linux/arm64' "$docker_log"
grep -Fq -- 'mister-remote-poc1b-dev-build' "$docker_log"
! grep -Fq -- '--network none' "$docker_log"

: > "$docker_log"
POC1B_CONTAINER_RUNTIME=$fake_docker \
POC1B_DEV_CONTAINER=1 \
POC1B_DOCKER_LOG=$docker_log \
  sh "$repo/scripts/poc1b-container.sh" run true

grep -Fq -- '--platform linux/arm64' "$docker_log"
grep -Fq -- '--network none' "$docker_log"
grep -Fq -- 'mister-remote-poc1b-dev-build' "$docker_log"

echo 'poc1b native development container tests passed'