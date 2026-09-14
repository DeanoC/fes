#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
build_script=$repo/scripts/build-target-image.sh
container_script=$repo/scripts/target-image-container.sh
verify_script=$repo/scripts/verify-target-image.sh
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

for script in "$build_script" "$container_script" "$verify_script"; do
  test -x "$script"
done
grep -Fq 'native runtime mode must be package-only' "$build_script"
grep -Fq 'native runtime mode must be package-only' "$container_script"
grep -Fq 'native runtime mode must be package-only' "$verify_script"
grep -Fq 'unmanaged runtime directory' "$verify_script"
grep -Fq 'fes.pong' "$container_script"
grep -Fq 'fes.zx81' "$container_script"
grep -Fq 'fes.coleco' "$container_script"
! grep -Eq 'NATIVE_RUNTIME_MEGADRIVE_FILE|NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE|PONG_RBF_BUNDLE|SNES_RBF_BUNDLE|NES_RBF_BUNDLE' \
  "$build_script" "$container_script" "$verify_script"

for variant in prod dev native-dev; do
  TARGET_IMAGE_TEST_MODE=1 sh "$build_script" \
    --validate-inside-path "$variant" \
    "/target-image-output/work-1-$variant" \
    "/work/build/output/target-image/work-1-$variant/images/rootfs.ext4"
done
if TARGET_IMAGE_TEST_MODE=1 NATIVE_RUNTIME_MODE=format1 \
  sh "$build_script" --validate-inside-path prod \
  /target-image-output/work-1-prod \
  /work/build/output/target-image/work-1-prod/images/rootfs.ext4 \
  >/dev/null 2>&1; then
  echo 'target image builder accepted the retired format-1 mode' >&2
  exit 1
fi

for rejected_path in \
  /target-image-output/../work \
  /target-image-output/work-1-unknown \
  /target-image-output/work-2-prod; do
  if TARGET_IMAGE_TEST_MODE=1 sh "$build_script" \
    --validate-inside-path prod "$rejected_path" \
    /work/build/output/target-image/work-1-prod/images/rootfs.ext4 \
    >/dev/null 2>&1; then
    echo "inside path validator accepted $rejected_path" >&2
    exit 1
  fi
done

fake_build=$fixture/fake-build
cat >"$fake_build" <<'FAKE_BUILD'
#!/bin/sh
set -eu
variant=$1
output=$2
epoch=$3
printf '%s|%s|%s\n' "$variant" "$output" "$epoch" >>"$TARGET_IMAGE_BUILD_LOG"
mkdir -p "$output/images"
printf 'image-%s\n' "$variant" >"$output/images/rootfs.ext4"
FAKE_BUILD
chmod 0755 "$fake_build"

output_root=$fixture/output
build_log=$fixture/build.log
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_BUILD_ONCE="$fake_build" \
TARGET_IMAGE_BUILD_LOG="$build_log" \
TARGET_IMAGE_OUTPUT_ROOT="$output_root" \
  sh "$build_script" prod
test -f "$output_root/prod/linux.img"
test "$(wc -l <"$build_log" | tr -d ' ')" -eq 2
grep -Fq "prod|$output_root/work-1-prod|1751459412" "$build_log"
grep -Fq "prod|$output_root/work-2-prod|1751459412" "$build_log"
test "$(cat "$output_root/prod/linux.img")" = image-prod

before=$(wc -l <"$build_log" | tr -d ' ')
printf '%s\n' stale >"$output_root/prod/linux.img"
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_OUTPUT_ROOT="$output_root" \
  sh "$build_script" --promote-existing prod
test "$(cat "$output_root/prod/linux.img")" = image-prod
test "$(wc -l <"$build_log" | tr -d ' ')" -eq "$before"

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
