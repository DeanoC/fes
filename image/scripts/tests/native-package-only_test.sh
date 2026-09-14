#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-native-package-only.XXXXXX")
trap 'chmod -R u+rwX "$fixture" 2>/dev/null || :; rm -rf "$fixture"' EXIT INT TERM

selector=$fixture/target-image-lock
cat >"$selector" <<'SELECTOR'
#!/bin/sh
printf '%s\n' "$*" >>"$SELECTOR_LOG"
exit 0
SELECTOR
chmod +x "$selector"

package=$fixture/package
mkdir "$package"
printf '%s\n' manifest >"$package/manifest.toml"
printf '%s\n' payload >"$package/core.rbf"
selection=$fixture/fes-pong.package-selection.toml
printf '%s\n' 'format = 2' >"$selection"
cache=$fixture/cache
mkdir "$cache"

export TARGET_IMAGE_LOCK_BIN=$selector SELECTOR_LOG=$fixture/selector.log
export NATIVE_RUNTIME_MODE=package-only
export FES_PONG_PACKAGE_DIR=$package FES_PONG_PACKAGE_SELECTION=$selection

test "$("$repo/scripts/native-extra-cores.sh" count)" = 2
"$repo/scripts/native-extra-cores.sh" fetch "$cache"
test ! -e "$cache/megadrive.rbf"
test ! -e "$cache/megadrive.selection.toml"

make -s -C "$repo" -n NATIVE_RUNTIME_MODE=package-only \
  FOGCAST_DIR="$repo/../sources/FogCast" target-image-native-fetch >"$fixture/make.log"
grep -Fq 'NATIVE_RUNTIME_MODE="package-only"' "$fixture/make.log"
if grep -Eq 'MEGADRIVE_RBF_|PONG_RBF_BUNDLE|SNES_RBF_BUNDLE|NES_RBF_BUNDLE|NATIVE_RUNTIME_SYSTEMS|fetch-core|rebuild-core|export-core-bundle|megadrive\.selection\.toml' "$fixture/make.log"; then
  printf '%s\n' 'package-only make graph still exposes format-1 inputs' >&2
  exit 1
fi
make -s -C "$repo" -n NATIVE_RUNTIME_MODE=package-only \
  FOGCAST_DIR="$repo/../sources/FogCast" target-image-native-verify >"$fixture/verify.log"
if grep -Eq 'MEGADRIVE_RBF_|PONG_RBF_BUNDLE|SNES_RBF_BUNDLE|NES_RBF_BUNDLE|NATIVE_RUNTIME_SYSTEMS|megadrive\.selection\.toml' "$fixture/verify.log"; then
  printf '%s\n' 'package-only verify graph still exposes format-1 inputs' >&2
  exit 1
fi
