#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-dev.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

build_script=$repo/scripts/build-poc1b-image.sh
test -x "$build_script"

grep -Fq 'poc1b-dev-image:' "$repo/Makefile"
grep -Fq -- '--fast-dev' "$build_script"
grep -Fq 'dev-work-dev' "$build_script"

fake_build=$fixture/fake-build
cat > "$fake_build" <<'EOF'
#!/bin/sh
set -eu
variant=$1
output=$2
epoch=$3
printf '%s|%s|%s\n' "$variant" "$output" "$epoch" >> "$POC1B_BUILD_LOG"
mkdir -p "$output/images"
count=$(wc -l < "$POC1B_BUILD_LOG" | tr -d ' ')
printf 'image-%s-%s\n' "$variant" "$count" > "$output/images/rootfs.ext4"
EOF
chmod 0755 "$fake_build"

output_root=$fixture/output
build_log=$fixture/build.log
POC1B_TEST_MODE=1 \
POC1B_BUILD_ONCE=$fake_build \
POC1B_BUILD_LOG=$build_log \
POC1B_OUTPUT_ROOT=$output_root \
  sh "$build_script" --fast-dev

# A fast development build runs once and promotes the result.
test -f "$output_root/dev/linux.img"
test "$(wc -l < "$build_log" | tr -d ' ')" -eq 1
grep -Fq "dev|$output_root/dev-work-dev|1751459412" "$build_log"
test "$(cat "$output_root/dev/linux.img")" = image-dev-1

# Re-running uses the same persistent Buildroot output rather than a clean run-2 tree.
POC1B_TEST_MODE=1 \
POC1B_BUILD_ONCE=$fake_build \
POC1B_BUILD_LOG=$build_log \
POC1B_OUTPUT_ROOT=$output_root \
  sh "$build_script" --fast-dev
test "$(wc -l < "$build_log" | tr -d ' ')" -eq 2
grep -Fq "dev|$output_root/dev-work-dev|1751459412" "$build_log"
test "$(cat "$output_root/dev/linux.img")" = image-dev-2

# It does not accept extra arguments that could change the fixed dev scope.
if POC1B_TEST_MODE=1 \
  POC1B_BUILD_ONCE=$fake_build \
  POC1B_BUILD_LOG=$build_log \
  POC1B_OUTPUT_ROOT=$output_root \
    sh "$build_script" --fast-dev extra >/dev/null 2>&1; then
  echo 'fast development build accepted an extra argument' >&2
  exit 1
fi

# The public Make target is documented separately from the strict release target.
grep -Fq 'poc1b-images:' "$repo/Makefile"
grep -Fq 'poc1b-dev-image:' "$repo/Makefile"

echo 'poc1b development build tests passed'