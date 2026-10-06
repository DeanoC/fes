#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
build_script=$repo/scripts/build-target-image.sh
container_script=$repo/scripts/target-image-container.sh
verify_script=$repo/scripts/verify-target-image.sh
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM
# The single-pass cases model a local scratch build, so clear the CI markers
# here; the CI refusal is asserted explicitly below.
unset CI GITHUB_ACTIONS

for script in "$build_script" "$container_script" "$verify_script"; do
  test -x "$script"
done
grep -Fq 'native runtime mode must be package-only' "$build_script"
grep -Fq 'native runtime mode must be package-only' "$container_script"
grep -Fq 'native runtime mode must be package-only' "$verify_script"
grep -Fq 'unmanaged runtime directory' "$verify_script"
grep -Fq 'runtime_root=$package_root/usr/share/mister-runtime' "$verify_script"
grep -Fq 'verify_rootfs_headroom "$image"' "$verify_script"
grep -Fq 'check-rootfs-headroom.sh' "$verify_script"
grep -Fq 'maximum is 85%' "$repo/scripts/check-rootfs-headroom.sh"
grep -Fq 'BR2_TARGET_ROOTFS_EXT2_SIZE="128M"' "$repo/buildroot/configs/fogcast_target_native_dev_defconfig"
headroom_script=$repo/scripts/check-rootfs-headroom.sh
if sh "$headroom_script" 32768 3276 4096 134217728 2>"$fixture/headroom.log"; then
  echo 'rootfs headroom accepted occupancy above 85%' >&2; exit 1
fi
grep -Fq 'populated rootfs uses' "$fixture/headroom.log"
sh "$headroom_script" 32768 9831 4096 134217728
grep -Fq 'for stale_dir in "$runtime_root/cores"; do' "$verify_script"
! grep -Fq '"$runtime_root/selections"' "$verify_script"
grep -Fq 'fes.pong' "$container_script"
grep -Fq 'fes.zx81' "$container_script"
grep -Fq 'fes.coleco' "$container_script"
! grep -Eq 'NATIVE_RUNTIME_MEGADRIVE_FILE|NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE|PONG_RBF_BUNDLE|SNES_RBF_BUNDLE|NES_RBF_BUNDLE' \
  "$build_script" "$container_script" "$verify_script"

TARGET_IMAGE_TEST_MODE=1 FES_IMAGE_PASSES=1 sh "$build_script" --validate-inside-path native-dev \
  /target-image-output/work-1-native-dev \
  /work/build/output/target-image/work-1-native-dev/images/rootfs.ext4
for variant in prod dev --fast-dev; do
  if sh "$build_script" "$variant" >"$fixture/rejected.log" 2>&1; then
    echo "retired image variant accepted: $variant" >&2; exit 1
  fi
  grep -Fq usage "$fixture/rejected.log"
done
for rejected_path in /target-image-output/../work /target-image-output/work-1-unknown /target-image-output/work-2-native-dev; do
  if TARGET_IMAGE_TEST_MODE=1 sh "$build_script" --validate-inside-path native-dev "$rejected_path" \
      /work/build/output/target-image/work-1-native-dev/images/rootfs.ext4 >/dev/null 2>&1; then
    echo "unsafe build path accepted: $rejected_path" >&2; exit 1
  fi
done

# Exercise native two-pass publication with a deterministic package-record helper.
mkdir -p "$fixture/recipe/scripts"
cp "$build_script" "$fixture/recipe/scripts/build-target-image.sh"
cp "$repo/scripts/toolchain_cache.py" "$fixture/recipe/scripts/toolchain_cache.py"
mkdir -p "$fixture/recipe/buildroot/configs" "$fixture/recipe/build"
cp "$repo/buildroot/configs/fogcast_toolchain.fragment" "$fixture/recipe/buildroot/configs/"
cp "$repo/buildroot/configs/fogcast_toolchain_only_defconfig" "$fixture/recipe/buildroot/configs/"
cp "$repo/buildroot/configs/fogcast_target_native_dev_defconfig" "$fixture/recipe/buildroot/configs/"
cp "$repo/build/target-image.sources.lock.toml" "$fixture/recipe/build/"
cp "$repo/build/target-image-container-packages.sha256" "$fixture/recipe/build/"
cat > "$fixture/fake-toolchain" <<'TOOLCHAIN'
#!/bin/sh
set -eu
repo=$1
mkdir -p "$repo/fake-host/bin"
printf 'fake-gcc\n' > "$repo/fake-host/bin/arm-buildroot-linux-gnueabihf-gcc"
printf 'built\n' >> "$repo/toolchain-builds.log"
python3 "$repo/scripts/toolchain_cache.py" package "$repo/fake-host" >/dev/null
TOOLCHAIN
chmod +x "$fixture/fake-toolchain"
cat > "$fixture/recipe/scripts/native-extra-cores.sh" <<'HELPER'
#!/bin/sh
set -eu
[ "$1" = copy-records ]
mkdir -p "$3"
printf 'format = 2\n' > "$3/fes-pong.package-selection.toml"
if [ -n "${FES_VIDEO_PARTS_DIR:-}" ]; then
  printf 'exact-factory-index\n' > "$3/fes-core-video-parts.json"
  case "$3:${DIFFER_VIDEO_INDEX:-0}" in *work-2-native-dev:1) printf changed >> "$3/fes-core-video-parts.json" ;; esac
fi
HELPER
chmod +x "$fixture/recipe/scripts/native-extra-cores.sh"
cat > "$fixture/fake-build" <<'BUILD'
#!/bin/sh
set -eu
printf '%s\n' "$2" >> "$BUILD_LOG"
mkdir -p "$2/images"
printf 'native-image\n' > "$2/images/rootfs.ext4"
printf '%s\n' "$TARGET_IMAGE_TOOLCHAIN_PATH" >> "${TARGET_IMAGE_TOOLCHAIN_PATH_LOG:?}"
printf '%s\n' "${CCACHE_DISABLE-unset}" >> "${CCACHE_LOG:?}"
case "$2:${DIFFER:-0}" in *work-2-native-dev:1) printf changed >> "$2/images/rootfs.ext4" ;; esac
BUILD
chmod +x "$fixture/fake-build"
export TARGET_IMAGE_TEST_MODE=1 TARGET_IMAGE_BUILD_ONCE="$fixture/fake-build" BUILD_LOG="$fixture/build.log"
export TARGET_IMAGE_TOOLCHAIN_BUILD_ONCE="$fixture/fake-toolchain"
export TARGET_IMAGE_OUTPUT_ROOT="$fixture/output" FES_PACKAGE_IDS=fes.pong
export TARGET_IMAGE_TOOLCHAIN_PATH_LOG="$fixture/toolchain-paths.log"
export CCACHE_LOG="$fixture/ccache.log"
sh "$fixture/recipe/scripts/build-target-image.sh" native-dev
test "$(cat "$fixture/output/native-dev/linux.img")" = native-image
test "$(head -n 1 "$fixture/toolchain-paths.log")" = /target-image-output/external-toolchain/host
test "$(sed -n '2p' "$fixture/toolchain-paths.log")" = /target-image-output/external-toolchain/host
test "$(wc -l < "$fixture/recipe/toolchain-builds.log")" -eq 1
grep -Eq '^toolchain_key=[0-9a-f]{64}$' "$fixture/output/native-dev/reproducibility.txt"
grep -Eq '^toolchain_sha256=[0-9a-f]{64}$' "$fixture/output/native-dev/reproducibility.txt"
test "$(cat "$fixture/ccache.log")" = "$(printf 'unset\nunset')"
! grep -Fq 'shared_cache=' "$fixture/output/native-dev/reproducibility.txt"
mkdir -p "$fixture/output/work-2-native-dev"
printf stale > "$fixture/output/work-2-native-dev/stale"
: > "$fixture/build.log"
FES_IMAGE_PASSES=1 sh "$fixture/recipe/scripts/build-target-image.sh" native-dev >"$fixture/single-pass.log"
grep -Fq "target image native-dev image: $(/usr/bin/shasum -a 256 "$fixture/output/native-dev/linux.img" | awk '{print $1}')" "$fixture/single-pass.log"
test "$(wc -l < "$fixture/build.log" | tr -d ' ')" = 1
test ! -e "$fixture/output/work-2-native-dev"
grep -Fq 'image_passes=1' "$fixture/output/native-dev/reproducibility.txt"
grep -Fq 'single_pass_scratch=1' "$fixture/output/native-dev/reproducibility.txt"
! grep -Fq 'run_2_sha256=' "$fixture/output/native-dev/reproducibility.txt"
grep -Fq 'linux_img_sha256=' "$fixture/output/native-dev/SINGLE-PASS-SCRATCH.txt"
if FES_IMAGE_PASSES=3 sh "$fixture/recipe/scripts/build-target-image.sh" native-dev >"$fixture/invalid-passes.log" 2>&1; then
  echo 'invalid IMAGE_PASSES accepted' >&2; exit 1
fi
grep -Fq 'must be 1 or 2' "$fixture/invalid-passes.log"
for marker in CI GITHUB_ACTIONS; do
  if env "$marker=true" FES_IMAGE_PASSES=1 sh "$fixture/recipe/scripts/build-target-image.sh" native-dev \
      >"$fixture/ci-refusal.log" 2>&1; then
    echo "single-pass image accepted with $marker=true" >&2; exit 1
  fi
  grep -Fq 'single-pass images are disabled in CI' "$fixture/ci-refusal.log"
done
env CI=true sh "$fixture/recipe/scripts/build-target-image.sh" native-dev >/dev/null
if FES_IMAGE_PASSES=1 FES_IMAGE_WORK=/target-image-output/work-2-native-dev \
    sh "$fixture/recipe/scripts/build-target-image.sh" native-dev >"$fixture/mismatched-work.log" 2>&1; then
  echo 'mismatched image work directory was accepted' >&2; exit 1
fi
grep -Fq 'FES_IMAGE_WORK must be /target-image-output/work-1-native-dev' "$fixture/mismatched-work.log"
grep -Fq 'selected_work=${FES_IMAGE_WORK:-/target-image-output/work-2-native-dev}' "$repo/scripts/build-target-kernel.sh"
grep -Fq 'selected_work=${FES_IMAGE_WORK:-/target-image-output/work-2-native-dev}' "$repo/scripts/qemu-smoke-target-image.sh"
# Only image pass 1 and make dev may use the shared ccache; kernel builds after the passes must not.
grep -Fq 'export CCACHE_DISABLE=1' "$repo/scripts/build-target-kernel.sh"
grep -Fq 'export CCACHE_DISABLE=1' "$repo/scripts/qemu-smoke-target-image.sh"
! grep -Fq 'FES_IMAGE_WORK = ' "$repo/Makefile"
cat > "$fixture/fake-container-runtime" <<'RUNTIME'
#!/bin/sh
printf '%s\n' "$*" >> "$CONTAINER_ARGS"
RUNTIME
chmod +x "$fixture/fake-container-runtime"
for passes in 1 2; do
  : > "$fixture/container-args"
  env -u FES_PACKAGE_IDS TARGET_IMAGE_TEST_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/fake-container-runtime" \
    CONTAINER_ARGS="$fixture/container-args" FES_IMAGE_PASSES=$passes \
    sh "$container_script" run true
  grep -Fq "FES_IMAGE_PASSES=$passes" "$fixture/container-args"
  grep -Fq "FES_IMAGE_WORK=/target-image-output/work-$passes-native-dev" "$fixture/container-args"
  ! grep -Fq '/target-image-shared-cache' "$fixture/container-args"
done
if env -u FES_PACKAGE_IDS TARGET_IMAGE_TEST_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/fake-container-runtime" \
    FES_IMAGE_PASSES=1 FES_IMAGE_WORK=/target-image-output/work-2-native-dev \
    sh "$container_script" run true >/dev/null 2>&1; then
  echo 'invalid selected image passes accepted by container wrapper' >&2; exit 1
fi
FES_VIDEO_PARTS_DIR="$fixture" sh "$fixture/recipe/scripts/build-target-image.sh" native-dev
test "$(wc -l < "$fixture/recipe/toolchain-builds.log")" -eq 1
printf corrupt >> "$fixture/recipe/build/cache/target-image/toolchains/$(python3 "$fixture/recipe/scripts/toolchain_cache.py" key)/host.tar"
sh "$fixture/recipe/scripts/build-target-image.sh" native-dev
test "$(wc -l < "$fixture/recipe/toolchain-builds.log")" -eq 2
TOOLCHAIN_REBUILD=1 sh "$fixture/recipe/scripts/build-target-image.sh" native-dev
test "$(wc -l < "$fixture/recipe/toolchain-builds.log")" -eq 3
ensured=$(sh "$fixture/recipe/scripts/build-target-image.sh" --ensure-toolchain native-dev)
test "$ensured" = "$(python3 "$fixture/recipe/scripts/toolchain_cache.py" status)"
test "$(wc -l < "$fixture/recipe/toolchain-builds.log")" -eq 3
printf corrupt >> "$fixture/recipe/build/cache/target-image/toolchains/$(python3 "$fixture/recipe/scripts/toolchain_cache.py" key)/host.tar"
sh "$fixture/recipe/scripts/build-target-image.sh" --ensure-toolchain native-dev >/dev/null
test "$(wc -l < "$fixture/recipe/toolchain-builds.log")" -eq 4
test "$(cat "$fixture/output/native-dev/fes-core-video-parts.json")" = exact-factory-index
if FES_VIDEO_PARTS_DIR="$fixture" DIFFER_VIDEO_INDEX=1 sh "$fixture/recipe/scripts/build-target-image.sh" native-dev >"$fixture/video-differ.log" 2>&1; then
  echo 'native image accepted differing factory video indexes' >&2; exit 1
fi
grep -Fq 'factory video index differs between reproducible outputs' "$fixture/video-differ.log"
test "$(cat "$fixture/output/native-dev/fes-core-video-parts.json")" = exact-factory-index
sh "$fixture/recipe/scripts/build-target-image.sh" --promote-existing native-dev
if DIFFER=1 sh "$fixture/recipe/scripts/build-target-image.sh" native-dev >"$fixture/differ.log" 2>&1; then
  echo 'native image accepted differing two-pass bytes' >&2; exit 1
fi
grep -Fq 'not reproducible' "$fixture/differ.log"
test "$(cat "$fixture/output/native-dev/linux.img")" = native-image
shared=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-shared-cache.XXXXXX")
trap 'rm -rf "$fixture" "$shared"' EXIT INT TERM
export FES_TARGET_IMAGE_SHARED_CACHE=$shared
if FES_TARGET_IMAGE_SHARED_CACHE=relative python3 "$fixture/recipe/scripts/toolchain_cache.py" status >/dev/null 2>&1; then
  echo 'relative shared cache accepted' >&2; exit 1
fi
if FES_TARGET_IMAGE_SHARED_CACHE="$fixture/recipe/subdir" python3 "$fixture/recipe/scripts/toolchain_cache.py" status >/dev/null 2>&1; then
  echo 'in-worktree shared cache accepted' >&2; exit 1
fi
for invalid in relative "$repo/build/cache/target-image/shared"; do
  if env -u FES_PACKAGE_IDS FES_TARGET_IMAGE_SHARED_CACHE="$invalid" \
      TARGET_IMAGE_TEST_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/fake-container-runtime" \
      sh "$container_script" run true >/dev/null 2>&1; then
    echo "container accepted invalid shared cache: $invalid" >&2; exit 1
  fi
done
: > "$fixture/ccache.log"
sh "$fixture/recipe/scripts/build-target-image.sh" native-dev
test -f "$shared/toolchains/$(python3 "$fixture/recipe/scripts/toolchain_cache.py" key)/host.tar"
test "$(cat "$fixture/ccache.log")" = "$(printf 'unset\n1')"
shared_toolchain=$shared/toolchains/$(python3 "$fixture/recipe/scripts/toolchain_cache.py" key)/host.tar
builds_before=$(wc -l < "$fixture/recipe/toolchain-builds.log")
printf corrupt >> "$shared_toolchain"
sh "$fixture/recipe/scripts/build-target-image.sh" --ensure-toolchain native-dev >/dev/null
test "$(wc -l < "$fixture/recipe/toolchain-builds.log")" -eq "$((builds_before + 1))"
grep -Fq 'shared_cache=1' "$fixture/output/native-dev/reproducibility.txt"
grep -Fq 'ccache_pass_1=1' "$fixture/output/native-dev/reproducibility.txt"
grep -Fq 'ccache_pass_2=0' "$fixture/output/native-dev/reproducibility.txt"
: > "$fixture/container-args"
env -u FES_PACKAGE_IDS TARGET_IMAGE_TEST_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/fake-container-runtime" \
  CONTAINER_ARGS="$fixture/container-args" sh "$container_script" run true
grep -Fq "$shared/dl:/work/build/cache/target-image/dl" "$fixture/container-args"
test -d "$repo/build/cache/target-image/dl" && test -O "$repo/build/cache/target-image"
grep -Fq "$shared:/target-image-shared-cache" "$fixture/container-args"
grep -Fq 'FES_TARGET_IMAGE_SHARED_CACHE=/target-image-shared-cache' "$fixture/container-args"
env -u FES_PACKAGE_IDS CCACHE_DISABLE=1 TARGET_IMAGE_TEST_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/fake-container-runtime" \
  CONTAINER_ARGS="$fixture/container-args" sh "$container_script" run true
grep -Fq 'CCACHE_DISABLE=1' "$fixture/container-args"
# Mutation check: removing pass-2 disable must trip the independence assertion.
cp "$fixture/recipe/scripts/build-target-image.sh" "$fixture/mutated-build"
sed 's/CCACHE_DISABLE=1 TARGET_IMAGE_TOOLCHAIN_PATH=/TARGET_IMAGE_TOOLCHAIN_PATH=/' \
  "$fixture/mutated-build" > "$fixture/recipe/scripts/build-target-image.sh"
: > "$fixture/ccache.log"
sh "$fixture/recipe/scripts/build-target-image.sh" native-dev >/dev/null
if test "$(cat "$fixture/ccache.log")" = "$(printf 'unset\n1')"; then
  echo 'independence assertion did not catch pass-2 ccache mutation' >&2; exit 1
fi
unset FES_TARGET_IMAGE_SHARED_CACHE
unset TARGET_IMAGE_BUILD_ONCE TARGET_IMAGE_TOOLCHAIN_BUILD_ONCE TARGET_IMAGE_OUTPUT_ROOT FES_PACKAGE_IDS TARGET_IMAGE_TOOLCHAIN_PATH_LOG

make_log=$fixture/make.log
make -s -C "$repo" -n \
  NATIVE_RUNTIME_MODE=package-only \
  FES_PACKAGE_IDS=fes.pong,fes.zx81,fes.coleco \
  FOGCAST_DIR="$repo/../sources/FogCast" \
  target-image-native-verify >"$make_log"
if grep -Eq 'NATIVE_RUNTIME_SYSTEMS|MEGADRIVE_RBF_|PONG_RBF_BUNDLE|SNES_RBF_BUNDLE|NES_RBF_BUNDLE|megadrive\.selection\.toml' "$make_log"; then
  echo 'native image make graph still exposes retired format-1 inputs' >&2
  exit 1
fi

printf '%s\n' 'target image package-only tests passed'
