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
grep -Fq 'BR2_TARGET_ROOTFS_EXT2_SIZE="160M"' "$repo/buildroot/configs/fogcast_target_native_dev_defconfig"
headroom_script=$repo/scripts/check-rootfs-headroom.sh
if sh "$headroom_script" 40960 4096 4096 167772160 2>"$fixture/headroom.log"; then
  echo 'rootfs headroom accepted occupancy above 85%' >&2; exit 1
fi
grep -Fq 'populated rootfs uses' "$fixture/headroom.log"
grep -Fq 'configured 167772160 bytes (142606336 bytes)' "$fixture/headroom.log"
sh "$headroom_script" 40960 12288 4096 167772160
# The observed nine-package ST population fits with the unchanged 85% guard.
sh "$headroom_script" 40960 8897 4096 167772160
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
case "$2:${DIFFER:-0}" in *work-2-native-dev:1) printf changed >> "$2/images/rootfs.ext4" ;; esac
BUILD
chmod +x "$fixture/fake-build"
export TARGET_IMAGE_TEST_MODE=1 TARGET_IMAGE_BUILD_ONCE="$fixture/fake-build" BUILD_LOG="$fixture/build.log"
export TARGET_IMAGE_OUTPUT_ROOT="$fixture/output" FES_PACKAGE_IDS=fes.pong
sh "$fixture/recipe/scripts/build-target-image.sh" native-dev
test "$(cat "$fixture/output/native-dev/linux.img")" = native-image
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
done
if env -u FES_PACKAGE_IDS TARGET_IMAGE_TEST_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/fake-container-runtime" \
    FES_IMAGE_PASSES=1 FES_IMAGE_WORK=/target-image-output/work-2-native-dev \
    sh "$container_script" run true >/dev/null 2>&1; then
  echo 'invalid selected image passes accepted by container wrapper' >&2; exit 1
fi
FES_VIDEO_PARTS_DIR="$fixture" sh "$fixture/recipe/scripts/build-target-image.sh" native-dev
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
unset TARGET_IMAGE_BUILD_ONCE TARGET_IMAGE_OUTPUT_ROOT FES_PACKAGE_IDS

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
