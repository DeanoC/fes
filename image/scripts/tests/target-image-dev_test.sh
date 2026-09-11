#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-image-dev.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

build_script=$repo/scripts/build-target-image.sh
test -x "$build_script"

grep -Fq 'target-image-dev:' "$repo/Makefile"
grep -Fq -- '--fast-dev' "$build_script"
grep -Fq 'dev-work-dev' "$build_script"

fake_build=$fixture/fake-build
cat > "$fake_build" <<'EOF'
#!/bin/sh
set -eu
variant=$1
output=$2
epoch=$3
printf '%s|%s|%s\n' "$variant" "$output" "$epoch" >> "$TARGET_IMAGE_BUILD_LOG"
mkdir -p "$output/images"
count=$(wc -l < "$TARGET_IMAGE_BUILD_LOG" | tr -d ' ')
if [ -e "$output/keep.marker" ]; then
  printf '%s\n' retained >> "$TARGET_IMAGE_PERSISTENCE_LOG"
fi
printf 'image-%s-%s\n' "$variant" "$count" > "$output/images/rootfs.ext4"
EOF
chmod 0755 "$fake_build"

output_root=$fixture/output
build_log=$fixture/build.log
persistence_log=$fixture/persistence.log
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_BUILD_ONCE=$fake_build \
TARGET_IMAGE_BUILD_LOG=$build_log \
TARGET_IMAGE_PERSISTENCE_LOG=$persistence_log \
TARGET_IMAGE_OUTPUT_ROOT=$output_root \
  sh "$build_script" --fast-dev

# A fast development build runs once and promotes the result.
test -f "$output_root/dev/linux.img"
test "$(wc -l < "$build_log" | tr -d ' ')" -eq 1
grep -Fq "dev|$output_root/dev-work-dev|1751459412" "$build_log"
test "$(cat "$output_root/dev/linux.img")" = image-dev-1
dev_config_hash=$(shasum -a 256 "$repo/buildroot/configs/fogcast_target_dev_defconfig" | awk '{print $1}')
test "$(cat "$output_root/dev-work-dev/.fogcast-dev-defconfig.sha256")" = "$dev_config_hash"

# Re-running uses the same persistent Buildroot output rather than a clean run-2 tree.
touch "$output_root/dev-work-dev/keep.marker"
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_BUILD_ONCE=$fake_build \
TARGET_IMAGE_BUILD_LOG=$build_log \
TARGET_IMAGE_PERSISTENCE_LOG=$persistence_log \
TARGET_IMAGE_OUTPUT_ROOT=$output_root \
  sh "$build_script" --fast-dev
test "$(wc -l < "$build_log" | tr -d ' ')" -eq 2
grep -Fq "dev|$output_root/dev-work-dev|1751459412" "$build_log"
test "$(cat "$output_root/dev/linux.img")" = image-dev-2
test -e "$output_root/dev-work-dev/keep.marker"
grep -Fqx retained "$persistence_log"

# A changed config invalidates the persistent output before the next build.
printf '%s\n' stale > "$output_root/dev-work-dev/.fogcast-dev-defconfig.sha256"
touch "$output_root/dev-work-dev/stale.marker"
TARGET_IMAGE_TEST_MODE=1 \
TARGET_IMAGE_BUILD_ONCE=$fake_build \
TARGET_IMAGE_BUILD_LOG=$build_log \
TARGET_IMAGE_PERSISTENCE_LOG=$persistence_log \
TARGET_IMAGE_OUTPUT_ROOT=$output_root \
  sh "$build_script" --fast-dev
test ! -e "$output_root/dev-work-dev/stale.marker"
test "$(cat "$output_root/dev-work-dev/.fogcast-dev-defconfig.sha256")" = "$dev_config_hash"

# It does not accept extra arguments that could change the fixed dev scope.
if TARGET_IMAGE_TEST_MODE=1 \
  TARGET_IMAGE_BUILD_ONCE=$fake_build \
  TARGET_IMAGE_BUILD_LOG=$build_log \
  TARGET_IMAGE_OUTPUT_ROOT=$output_root \
    sh "$build_script" --fast-dev extra >/dev/null 2>&1; then
  echo 'fast development build accepted an extra argument' >&2
  exit 1
fi

# The public Make target is documented separately from the strict release target.
grep -Fq 'target-images:' "$repo/Makefile"
grep -Fq 'target-image-dev:' "$repo/Makefile"

echo 'target image development build tests passed'
