#!/bin/sh
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d /tmp/fes-native-video-parts.XXXXXX)
trap 'find -P "$fixture" -type d -exec chmod u+w {} \;; rm -rf "$fixture"' EXIT INT TERM
source=$fixture/source
cache=$fixture/cache
target=$fixture/target
mkdir -p "$source/shell" "$cache/core-packages" "$target/usr/share/mister-runtime/core-packages"
printf 'direct producer bytes\n' >"$source/shell/direct.tar"
printf 'scanline producer bytes\n' >"$source/shell/scanlines.tar"
printf '{"fixture":"exact indexed producer archives"}\n' >"$source/index.json"
chmod 0444 "$source/index.json" "$source/shell/"*.tar
chmod 0555 "$source" "$source/shell"
cat >"$fixture/selector" <<'SELECTOR'
#!/bin/sh
set -eu
command=$1
shift
parts=
cache=
selection=
print_inputs=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --parts) parts=$2; shift 2 ;;
    --cache) cache=$2; shift 2 ;;
    --selection) selection=$2; shift 2 ;;
    --packages) shift 2 ;;
    --print-inputs) print_inputs=1; shift ;;
    *) exit 2 ;;
  esac
done
case "$command" in
  select-video-parts)
    cp -R "$parts" "$cache/core-video-parts"
    cp "$parts/index.json" "$cache/fes-core-video-parts.json"
    chmod 0444 "$cache/fes-core-video-parts.json"
    ;;
  verify-video-parts)
    cmp "$parts/index.json" "$selection"
    cmp "$FIXTURE_SOURCE/shell/direct.tar" "$parts/shell/direct.tar"
    cmp "$FIXTURE_SOURCE/shell/scanlines.tar" "$parts/shell/scanlines.tar"
    test "$(stat -c %a "$parts")" = 555
    test "$(stat -c %a "$parts/shell")" = 555
    test "$(stat -c %a "$parts/shell/direct.tar")" = 444
    test "$(find "$parts" -type f | wc -l | tr -d ' ')" = 3
    if [ "$print_inputs" -eq 1 ]; then printf 'factory_video_index_sha256=fixture\n'; fi
    ;;
  verify-video-coverage) test "${MARKED_SHELL:-0}" = 0 ;;
  *) exit 2 ;;
esac
SELECTOR
chmod 0755 "$fixture/selector"
export TARGET_IMAGE_LOCK_BIN=$fixture/selector FES_VIDEO_PARTS_DIR=$source FIXTURE_SOURCE=$source
helper=$repo/scripts/native-video-parts.sh
sh "$helper" fetch "$cache"
sh "$helper" verify "$cache"
# A restrictive umask must not strip the sealed 555/444 modes.
(umask 077; sh "$helper" install "$cache" "$target")
sh "$helper" verify-image "$cache" "$target"
sh "$helper" build-inputs "$cache" "$target" >"$fixture/inputs"
grep -Fqx 'factory_video_index_sha256=fixture' "$fixture/inputs"
mkdir "$fixture/records"
sh "$helper" copy-records "$cache" "$fixture/records"
cmp "$source/index.json" "$fixture/records/fes-core-video-parts.json"
# Altered installed bytes and missing indexes cannot pass image verification.
part=$target/usr/share/mister-runtime/core-video-parts/shell/direct.tar
chmod 0644 "$part"
printf altered >"$part"
chmod 0444 "$part"
if sh "$helper" verify-image "$cache" "$target" >/dev/null 2>&1; then
  echo 'altered installed video bytes accepted' >&2; exit 1
fi
rm "$target/usr/share/mister-runtime/selections/fes-core-video-parts.json"
if sh "$helper" verify-image "$cache" "$target" >/dev/null 2>&1; then
  echo 'missing installed video index accepted' >&2; exit 1
fi
# An unselected old tree is removed during fetch, and marked shell coverage
# still requires an explicit selected asset set.
unset FES_VIDEO_PARTS_DIR
sh "$helper" fetch "$cache"
test ! -e "$cache/core-video-parts"
test ! -e "$cache/fes-core-video-parts.json"
if MARKED_SHELL=1 sh "$helper" verify "$cache" >/dev/null 2>&1; then
  echo 'marked shell accepted missing factory parts' >&2; exit 1
fi
sh "$helper" install "$cache" "$target"
test ! -e "$target/usr/share/mister-runtime/core-video-parts"
sh "$helper" copy-records "$cache" "$fixture/records"
test ! -e "$fixture/records/fes-core-video-parts.json"
printf '%s\n' 'native factory video parts plumbing tests passed'
