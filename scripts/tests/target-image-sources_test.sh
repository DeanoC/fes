#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-sources.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

grep -Fq 'TARGET_IMAGE_LOCK' "$repo/scripts/fetch-target-image-sources.sh"
grep -Fq 'TARGET_IMAGE_CONTAINER_RUNTIME' "$repo/scripts/target-image-container.sh"
grep -Fqx 'name: FOGCAST_TARGET' "$repo/buildroot/external.desc"
grep -Fq 'fogcast_target_dev_defconfig' "$repo/scripts/build-target-image.sh"
grep -Fq 'fogcast_target_native_dev_defconfig' "$repo/scripts/build-target-image.sh"
grep -Fq '/work/scripts/verify-native-runtime-inputs.sh' "$repo/scripts/build-target-image.sh"

native_make_root=$fixture/native-make
native_make_log=$fixture/native-make.log
native_runtime_path=$fixture/runtime-source
mkdir -p "$native_make_root/scripts" "$native_runtime_path"
cat >"$native_make_root/scripts/target-image-container.sh" <<'EOF'
#!/bin/sh
set -eu
printf 'container\truntime=%s\targc=%s\targ1=%s\targ2=%s\n' \
  "${LIBMISTER_RUNTIME_DIR:-}" "$#" "$1" "$2" >>"$NATIVE_FETCH_RECIPE_LOG"
EOF
cat >"$native_make_root/scripts/build-target-image.sh" <<'EOF'
#!/bin/sh
set -eu
printf 'build\truntime=%s\targc=%s\targ1=%s\targ2=%s\n' \
  "${LIBMISTER_RUNTIME_DIR:-}" "$#" "$1" "$2" >>"$NATIVE_FETCH_RECIPE_LOG"
EOF
chmod 0755 "$native_make_root/scripts/target-image-container.sh" \
  "$native_make_root/scripts/build-target-image.sh"
(
  cd "$native_make_root"
  NATIVE_FETCH_RECIPE_LOG=$native_make_log \
  LIBMISTER_RUNTIME_DIR=$native_runtime_path \
    make --no-print-directory -f "$repo/Makefile" \
      -o build-target-image-lock-container -o build-agent \
      target-image-native-fetch
)
{
  printf 'container\truntime=\targc=2\targ1=fetch\targ2=/work/scripts/fetch-native-runtime-inputs.sh\n'
  printf 'build\truntime=%s\targc=2\targ1=--fetch\targ2=native-dev\n' \
    "$native_runtime_path"
} >"$fixture/native-make.expected"
if ! cmp "$fixture/native-make.expected" "$native_make_log"; then
  printf '%s\n' \
    'native fetch recipe did not isolate idle bootstrap from runtime verification' >&2
  exit 1
fi

for legacy_target in target-image-fetch target-images target-image-dev target-image-verify target-image-qemu-smoke; do
  legacy_body=$(
    awk -v target="$legacy_target:" '
      $0 ~ "^" target { in_target=1; next }
      in_target && /^[^[:space:]]/ { exit }
      in_target { print }
    ' "$repo/Makefile"
  )
  if printf '%s\n' "$legacy_body" | grep -Fq 'native'; then
    echo "$legacy_target gained a native prerequisite or command" >&2
    exit 1
  fi
done

grep -Fq 'snapshot.debian.org/archive/debian/20260801T120000Z' \
  "$repo/containers/target-image/Dockerfile"
grep -Fq 'snapshot.debian.org/archive/debian-security/20260801T120000Z' \
  "$repo/containers/target-image/Dockerfile"
if grep -Fq 'deb.debian.org' "$repo/containers/target-image/Dockerfile"; then
  echo 'target image container still uses the mutable Debian mirror' >&2
  exit 1
fi

grep -Fq 'PACKAGE_SET_SHA256' "$repo/containers/target-image/Dockerfile"
grep -Fq 'build_image_id' "$repo/scripts/target-image-container.sh"
grep -Fq 'org.fogcast.target-image.context-digest' "$repo/scripts/target-image-container.sh"

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
lock=$fixture/target-image.sources.lock.toml
{
  printf '%s\n' 'format = 1' ''
  printf '%s\n' '[container]' "image = 'docker.io/library/debian:12.11-slim'" "platform = 'linux/amd64'" "digest = 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'" ''
  printf '%s\n' '[buildroot]' "version = '2021.02.4'" "commit = '$buildroot_commit'" ''
  printf '%s\n' '[image_creator]' "commit = '$creator_commit'" "rootfs_sha256 = '$rootfs_sha'" "modules_sha256 = '$modules_sha'" "kernel_sha256 = '$kernel_sha'" ''
  printf '%s\n' '[kernel]' "commit = '$kernel_commit'" "defconfig = 'MiSTer_defconfig'" "dtb = 'socfpga_cyclone5_de10_nano.dtb'" "release = '5.15.1-MiSTer'"
} > "$lock"

lock_bin=$fixture/target-image-lock
(
  cd "$repo"
  go build -o "$lock_bin" ./cmd/target-image-lock
  make build-target-image-lock-container
)
file "$repo/bin/target-image-lock-linux-amd64" | grep -Eq 'ELF 64-bit.*(x86-64|x86_64)'

cache=$fixture/cache
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_LOCK=$lock \
TARGET_IMAGE_LOCK_BIN=$lock_bin \
TARGET_IMAGE_CACHE=$cache \
TARGET_IMAGE_BUILDROOT_REPO=$buildroot_upstream \
TARGET_IMAGE_IMAGE_CREATOR_REPO=$creator_upstream \
TARGET_IMAGE_KERNEL_REPO=$kernel_upstream \
  sh "$repo/scripts/fetch-target-image-sources.sh"

test "$(git -C "$cache/buildroot" rev-parse HEAD)" = "$buildroot_commit"
test "$(git -C "$cache/image-creator" rev-parse HEAD)" = "$creator_commit"
test "$(git --git-dir="$cache/linux-kernel.git" rev-parse refs/target-image/pinned)" = "$kernel_commit"
test "$(git --git-dir="$cache/linux-kernel.git" rev-parse --is-bare-repository)" = true
test "$(git -C "$cache/buildroot" symbolic-ref -q HEAD || true)" = ""
test "$(git -C "$cache/image-creator" symbolic-ref -q HEAD || true)" = ""
test -d "$cache/linux-kernel.git/objects"

sh "$repo/scripts/verify-target-image-source-cache.sh" "$lock" "$cache"
printf '%s\n' dirty > "$cache/buildroot/dirty.untracked"
if sh "$repo/scripts/verify-target-image-source-cache.sh" "$lock" "$cache" >/dev/null 2>&1; then
  echo 'source-cache verifier accepted an untracked file' >&2
  exit 1
fi
rm "$cache/buildroot/dirty.untracked"
git -C "$cache/buildroot" checkout -q -B attached-test
if sh "$repo/scripts/verify-target-image-source-cache.sh" "$lock" "$cache" >/dev/null 2>&1; then
  echo 'source-cache verifier accepted an attached HEAD' >&2
  exit 1
fi
git -C "$cache/buildroot" checkout -q --detach "$buildroot_commit"
sh "$repo/scripts/verify-target-image-source-cache.sh" "$lock" "$cache"
grep -Fq '/work/scripts/verify-target-image-source-cache.sh' \
  "$repo/scripts/build-target-image.sh"

bad_lock=$fixture/bad.lock.toml
sed "s/rootfs_sha256 = '$rootfs_sha'/rootfs_sha256 = 'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'/" "$lock" > "$bad_lock"
if TARGET_IMAGE_TEST_MODE=1 \
  TARGET_IMAGE_LOCK=$bad_lock \
  TARGET_IMAGE_LOCK_BIN=$lock_bin \
  TARGET_IMAGE_CACHE=$cache \
  TARGET_IMAGE_BUILDROOT_REPO=$buildroot_upstream \
  TARGET_IMAGE_IMAGE_CREATOR_REPO=$creator_upstream \
  TARGET_IMAGE_KERNEL_REPO=$kernel_upstream \
    sh "$repo/scripts/fetch-target-image-sources.sh" >/dev/null 2>&1; then
  echo 'fetch accepted a changed image-creator artifact' >&2
  exit 1
fi

docker_log=$fixture/docker.log
fake_docker=$fixture/docker
TARGET_IMAGE_FAKE_PACKAGE_DIGEST=$(tr -d '[:space:]' < \
  "$repo/build/target-image-container-packages.sha256")
export TARGET_IMAGE_FAKE_PACKAGE_DIGEST
cat > "$fake_docker" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$TARGET_IMAGE_DOCKER_LOG"
if [ "$1 $2" = "image inspect" ]; then
  case "$3" in
    docker.io/*@*)
      printf 'debian@%s\n' "${TARGET_IMAGE_FAKE_BASE_DIGEST:-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}"
      ;;
    fogcast-target-image-build:*)
      case "$*" in
        *org.fogcast.target-image.context-digest*)
          tag=${3#*:}
          old_ifs=$IFS
          IFS=-
          set -- $tag
          IFS=$old_ifs
          context=$2
          if [ "${TARGET_IMAGE_FAKE_BAD_CONTEXT:-0}" = 1 ]; then context=bad; fi
          printf 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc|linux/amd64|sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|%s|%s|%s|%s\n' "$context" "$TARGET_IMAGE_FAKE_PACKAGE_DIGEST" "$3" "$4"
          ;;
      esac
      ;;
  esac
fi
exit 0
EOF
chmod 0755 "$fake_docker"

TARGET_IMAGE_CONTAINER_RUNTIME=$fake_docker \
TARGET_IMAGE_DOCKER_LOG=$docker_log \
TARGET_IMAGE_LOCK=$lock \
MISTER_TOKEN=must-not-cross-container-boundary \
  sh "$repo/scripts/target-image-container.sh" run true
grep -q -- '--network none' "$docker_log"
grep -q -- '--ulimit core=0:0' "$docker_log"
grep -q -- 'fogcast-target-image-output:/target-image-output' "$docker_log"
! grep -q 'must-not-cross-container-boundary' "$docker_log"
grep -q 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc true' "$docker_log"

: > "$docker_log"
TARGET_IMAGE_CONTAINER_RUNTIME=$fake_docker \
TARGET_IMAGE_DOCKER_LOG=$docker_log \
TARGET_IMAGE_LOCK=$lock \
  sh "$repo/scripts/target-image-container.sh" fetch true
! grep -q -- '--network none' "$docker_log"

if TARGET_IMAGE_CONTAINER_RUNTIME=$fake_docker \
  TARGET_IMAGE_DOCKER_LOG=$docker_log \
  TARGET_IMAGE_FAKE_BASE_DIGEST=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  TARGET_IMAGE_LOCK=$lock \
    sh "$repo/scripts/target-image-container.sh" fetch true >/dev/null 2>&1; then
  echo 'container wrapper accepted a base image at the wrong digest' >&2
  exit 1
fi

if TARGET_IMAGE_CONTAINER_RUNTIME=$fake_docker \
  TARGET_IMAGE_DOCKER_LOG=$docker_log \
  TARGET_IMAGE_FAKE_BAD_CONTEXT=1 \
  TARGET_IMAGE_LOCK=$lock \
    sh "$repo/scripts/target-image-container.sh" run true >/dev/null 2>&1; then
  echo 'container wrapper accepted a retagged build image' >&2
  exit 1
fi

if missing_runtime_output=$(TARGET_IMAGE_CONTAINER_RUNTIME=$fixture/not-installed TARGET_IMAGE_LOCK=$lock sh "$repo/scripts/target-image-container.sh" run true 2>&1); then
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
printf 'groupadd %s\n' "$*" >> "$TARGET_IMAGE_USER_LOG"
EOF
cat > "$fake_bin/useradd" <<'EOF'
#!/bin/sh
printf 'useradd %s\n' "$*" >> "$TARGET_IMAGE_USER_LOG"
EOF
chmod 0755 "$fake_bin/getent" "$fake_bin/groupadd" "$fake_bin/useradd"

user_log=$fixture/user.log
PATH="$fake_bin:$PATH" TARGET_IMAGE_USER_LOG=$user_log HOST_UID=501 HOST_GID=20 \
  sh "$repo/containers/target-image/create-builder-user.sh"
! grep -q '^groupadd ' "$user_log"
grep -q '^useradd --uid 501 --gid 20 --create-home builder$' "$user_log"
