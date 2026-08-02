#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-sources.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

grep -Fq 'snapshot.debian.org/archive/debian/20260801T120000Z' \
  "$repo/containers/poc1b/Dockerfile"
grep -Fq 'snapshot.debian.org/archive/debian-security/20260801T120000Z' \
  "$repo/containers/poc1b/Dockerfile"
if grep -Fq 'deb.debian.org' "$repo/containers/poc1b/Dockerfile"; then
  echo 'POC 1B container still uses the mutable Debian mirror' >&2
  exit 1
fi

grep -Fq 'PACKAGE_SET_SHA256' "$repo/containers/poc1b/Dockerfile"
grep -Fq 'build_image_id' "$repo/scripts/poc1b-container.sh"
grep -Fq 'org.mister-remote.poc1b.context-digest' "$repo/scripts/poc1b-container.sh"

make_repo() {
  make_repo_path=$1
  mkdir -p "$make_repo_path"
  git -C "$make_repo_path" init -q
  git -C "$make_repo_path" add .
  git -C "$make_repo_path" -c user.name=Test -c user.email=test@example.invalid commit -q -m fixture
  git -C "$make_repo_path" rev-parse HEAD
}

buildroot_upstream=$fixture/upstream-buildroot
mkdir -p "$buildroot_upstream"
printf '%s\n' 'Buildroot fixture' > "$buildroot_upstream/README"
buildroot_commit=$(make_repo "$buildroot_upstream")

creator_upstream=$fixture/upstream-image-creator
mkdir -p "$creator_upstream"
printf '%s' rootfs > "$creator_upstream/rootfs.tar.bz2"
printf '%s' modules > "$creator_upstream/modules.tar.gz"
printf '%s' kernel > "$creator_upstream/zImage_dtb"
creator_commit=$(make_repo "$creator_upstream")

kernel_upstream=$fixture/upstream-kernel
mkdir -p "$kernel_upstream/arch/arm/configs"
printf '%s\n' 'CONFIG_LOCALVERSION="-MiSTer"' > "$kernel_upstream/arch/arm/configs/MiSTer_defconfig"
kernel_commit=$(make_repo "$kernel_upstream")

rootfs_sha=$(sha256sum "$creator_upstream/rootfs.tar.bz2" | awk '{print $1}')
modules_sha=$(sha256sum "$creator_upstream/modules.tar.gz" | awk '{print $1}')
kernel_sha=$(sha256sum "$creator_upstream/zImage_dtb" | awk '{print $1}')
lock=$fixture/sources.poc1b.lock.toml
{
  printf '%s\n' 'format = 1' ''
  printf '%s\n' '[container]' "image = 'docker.io/library/debian:12.11-slim'" "platform = 'linux/amd64'" "digest = 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'" ''
  printf '%s\n' '[buildroot]' "version = '2021.02.4'" "commit = '$buildroot_commit'" ''
  printf '%s\n' '[image_creator]' "commit = '$creator_commit'" "rootfs_sha256 = '$rootfs_sha'" "modules_sha256 = '$modules_sha'" "kernel_sha256 = '$kernel_sha'" ''
  printf '%s\n' '[kernel]' "commit = '$kernel_commit'" "defconfig = 'MiSTer_defconfig'" "dtb = 'socfpga_cyclone5_de10_nano.dtb'" "release = '5.15.1-MiSTer'"
} > "$lock"

lock_bin=$fixture/poc1b-lock
(
  cd "$repo"
  mise exec go@1.26.5 -- go build -o "$lock_bin" ./cmd/poc1b-lock
  mise exec go@1.26.5 -- make build-lock-container
)
file "$repo/bin/poc1b-lock-linux-amd64" | grep -Eq 'ELF 64-bit.*(x86-64|x86_64)'

cache=$fixture/cache
POC1B_TEST_MODE=1 \
POC1B_LOCK=$lock \
POC1B_LOCK_BIN=$lock_bin \
POC1B_CACHE=$cache \
POC1B_BUILDROOT_REPO=$buildroot_upstream \
POC1B_IMAGE_CREATOR_REPO=$creator_upstream \
POC1B_KERNEL_REPO=$kernel_upstream \
  sh "$repo/scripts/fetch-poc1b-sources.sh"

test "$(git -C "$cache/buildroot" rev-parse HEAD)" = "$buildroot_commit"
test "$(git -C "$cache/image-creator" rev-parse HEAD)" = "$creator_commit"
test "$(git --git-dir="$cache/linux-kernel.git" rev-parse refs/poc1b/pinned)" = "$kernel_commit"
test "$(git --git-dir="$cache/linux-kernel.git" rev-parse --is-bare-repository)" = true
test "$(git -C "$cache/buildroot" symbolic-ref -q HEAD || true)" = ""
test "$(git -C "$cache/image-creator" symbolic-ref -q HEAD || true)" = ""
test -d "$cache/linux-kernel.git/objects"

sh "$repo/scripts/verify-poc1b-source-cache.sh" "$lock" "$cache"
printf '%s\n' dirty > "$cache/buildroot/dirty.untracked"
if sh "$repo/scripts/verify-poc1b-source-cache.sh" "$lock" "$cache" >/dev/null 2>&1; then
  echo 'source-cache verifier accepted an untracked file' >&2
  exit 1
fi
rm "$cache/buildroot/dirty.untracked"
git -C "$cache/buildroot" checkout -q -B attached-test
if sh "$repo/scripts/verify-poc1b-source-cache.sh" "$lock" "$cache" >/dev/null 2>&1; then
  echo 'source-cache verifier accepted an attached HEAD' >&2
  exit 1
fi
git -C "$cache/buildroot" checkout -q --detach "$buildroot_commit"
sh "$repo/scripts/verify-poc1b-source-cache.sh" "$lock" "$cache"
grep -Fq '/work/scripts/verify-poc1b-source-cache.sh' \
  "$repo/scripts/build-poc1b-image.sh"

bad_lock=$fixture/bad.lock.toml
sed "s/rootfs_sha256 = '$rootfs_sha'/rootfs_sha256 = 'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'/" "$lock" > "$bad_lock"
if POC1B_TEST_MODE=1 \
  POC1B_LOCK=$bad_lock \
  POC1B_LOCK_BIN=$lock_bin \
  POC1B_CACHE=$cache \
  POC1B_BUILDROOT_REPO=$buildroot_upstream \
  POC1B_IMAGE_CREATOR_REPO=$creator_upstream \
  POC1B_KERNEL_REPO=$kernel_upstream \
    sh "$repo/scripts/fetch-poc1b-sources.sh" >/dev/null 2>&1; then
  echo 'fetch accepted a changed image-creator artifact' >&2
  exit 1
fi

docker_log=$fixture/docker.log
fake_docker=$fixture/docker
POC1B_FAKE_PACKAGE_DIGEST=$(tr -d '[:space:]' < \
  "$repo/build/poc1b-container-packages.sha256")
export POC1B_FAKE_PACKAGE_DIGEST
cat > "$fake_docker" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$POC1B_DOCKER_LOG"
if [ "$1 $2" = "image inspect" ]; then
  case "$3" in
    docker.io/*@*)
      printf 'debian@%s\n' "${POC1B_FAKE_BASE_DIGEST:-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}"
      ;;
    mister-remote-poc1b-build:*)
      case "$*" in
        *org.mister-remote.poc1b.context-digest*)
          tag=${3#*:}
          old_ifs=$IFS
          IFS=-
          set -- $tag
          IFS=$old_ifs
          context=$2
          if [ "${POC1B_FAKE_BAD_CONTEXT:-0}" = 1 ]; then context=bad; fi
          printf 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc|linux/amd64|sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|%s|%s|%s|%s\n' "$context" "$POC1B_FAKE_PACKAGE_DIGEST" "$3" "$4"
          ;;
      esac
      ;;
  esac
fi
exit 0
EOF
chmod 0755 "$fake_docker"

POC1B_CONTAINER_RUNTIME=$fake_docker \
POC1B_DOCKER_LOG=$docker_log \
POC1B_LOCK=$lock \
MISTER_TOKEN=must-not-cross-container-boundary \
  sh "$repo/scripts/poc1b-container.sh" run true
grep -q -- '--network none' "$docker_log"
grep -q -- '--ulimit core=0:0' "$docker_log"
grep -q -- 'mister-remote-poc1b-output:/poc1b-output' "$docker_log"
! grep -q 'must-not-cross-container-boundary' "$docker_log"
grep -q 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc true' "$docker_log"

: > "$docker_log"
POC1B_CONTAINER_RUNTIME=$fake_docker \
POC1B_DOCKER_LOG=$docker_log \
POC1B_LOCK=$lock \
  sh "$repo/scripts/poc1b-container.sh" fetch true
! grep -q -- '--network none' "$docker_log"

if POC1B_CONTAINER_RUNTIME=$fake_docker \
  POC1B_DOCKER_LOG=$docker_log \
  POC1B_FAKE_BASE_DIGEST=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  POC1B_LOCK=$lock \
    sh "$repo/scripts/poc1b-container.sh" fetch true >/dev/null 2>&1; then
  echo 'container wrapper accepted a base image at the wrong digest' >&2
  exit 1
fi

if POC1B_CONTAINER_RUNTIME=$fake_docker \
  POC1B_DOCKER_LOG=$docker_log \
  POC1B_FAKE_BAD_CONTEXT=1 \
  POC1B_LOCK=$lock \
    sh "$repo/scripts/poc1b-container.sh" run true >/dev/null 2>&1; then
  echo 'container wrapper accepted a retagged build image' >&2
  exit 1
fi

if missing_runtime_output=$(POC1B_CONTAINER_RUNTIME=$fixture/not-installed POC1B_LOCK=$lock sh "$repo/scripts/poc1b-container.sh" run true 2>&1); then
  echo 'missing container runtime succeeded' >&2
  exit 1
fi
printf '%s\n' "$missing_runtime_output" | grep -q 'container runtime is not executable'

fake_bin=$fixture/fake-bin
mkdir -p "$fake_bin"
cat > "$fake_bin/getent" <<'EOF'
#!/bin/sh
if [ "$1" = group ] && [ "$2" = 20 ]; then
  printf '%s\n' 'dialout:x:20:'
  exit 0
fi
exit 2
EOF
cat > "$fake_bin/groupadd" <<'EOF'
#!/bin/sh
printf 'groupadd %s\n' "$*" >> "$POC1B_USER_LOG"
EOF
cat > "$fake_bin/useradd" <<'EOF'
#!/bin/sh
printf 'useradd %s\n' "$*" >> "$POC1B_USER_LOG"
EOF
chmod 0755 "$fake_bin/getent" "$fake_bin/groupadd" "$fake_bin/useradd"

user_log=$fixture/user.log
PATH="$fake_bin:$PATH" POC1B_USER_LOG=$user_log HOST_UID=501 HOST_GID=20 \
  sh "$repo/containers/poc1b/create-builder-user.sh"
! grep -q '^groupadd ' "$user_log"
grep -q '^useradd --uid 501 --gid 20 --create-home builder$' "$user_log"
