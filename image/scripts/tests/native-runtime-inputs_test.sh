#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-native-runtime-inputs.XXXXXX")
trap 'chmod -R u+w "$fixture" 2>/dev/null || :; rm -rf "$fixture"' EXIT INT TERM

fail() {
  printf 'native runtime input test: %s\n' "$*" >&2
  exit 1
}

fogcast=$fixture/FogCast
mkdir -p "$fogcast/build" "$fogcast/bin"
runtime_commit=1111111111111111111111111111111111111111
idle=$fixture/idle.rbf
printf '%s\n' 'fixture idle rbf' >"$idle"
idle_sha=$(sha256sum "$idle" | awk '{print $1}')
idle_size=$(wc -c <"$idle" | tr -d ' ')
lock=$fogcast/build/native-runtime.inputs.lock.toml
cat >"$lock" <<EOF
format = 1

[mister_runtime]
commit = '$runtime_commit'
mount_path = '/runtime-source'

[idle_rbf]
repository = 'https://github.com/MiSTer-devel/Distribution_MiSTer'
commit = 'f7bde4becb452ca28f604ad9802bbed5c6b58e01'
path = 'menu.rbf'
sha256 = '$idle_sha'
size = $idle_size
install_path = '/usr/share/mister-runtime/idle.rbf'
EOF

fake_bin=$fixture/bin
mkdir -p "$fake_bin"
cat >"$fake_bin/wget" <<'WGET'
#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -O) output=$2; shift 2 ;;
    *) shift ;;
  esac
done
test -n "$output"
cp "$FAKE_IDLE" "$output"
WGET
chmod +x "$fake_bin/wget"

selector=$fogcast/bin/target-image-lock-linux-amd64
cat >"$selector" <<'SELECTOR'
#!/bin/sh
set -eu
command=$1
shift
package=
selection=
cache=
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --package) package=$2; shift 2 ;;
    --selection) selection=$2; shift 2 ;;
    --cache) cache=$2; shift 2 ;;
    --output) output=$2; shift 2 ;;
    *) shift ;;
  esac
done
read_value() {
  awk -F"'" -v wanted="$1" \
    '$1 ~ "^[[:space:]]*" wanted "[[:space:]]*=" { print $2; exit }' "$selection"
}
case "$command" in
  select-package)
    package_id=$(read_value package_id)
    destination=$cache/core-packages/$package_id
    mkdir -p "$destination"
    cp "$package/manifest.toml" "$destination/manifest.toml"
    cp "$package/core.rbf" "$destination/core.rbf"
    chmod 0444 "$destination/manifest.toml" "$destination/core.rbf"
    chmod 0555 "$destination"
    cp "$selection" "$output"
    chmod 0444 "$output"
    ;;
  verify-package)
    test -d "$package"
    test -f "$selection"
    ;;
  *) exit 2 ;;
esac
SELECTOR
chmod +x "$selector"

package=$fixture/pong-package
mkdir "$package"
printf '%s\n' "core_id = 'fes.pong'" >"$package/manifest.toml"
printf '%s\n' 'pong package payload' >"$package/core.rbf"
chmod 0444 "$package/manifest.toml" "$package/core.rbf"
chmod 0555 "$package"
package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
selection=$fixture/fes-pong.package-selection.toml
cat >"$selection" <<EOF
format = 2
kind = 'core-package'
core_id = 'fes.pong'
package_id = '$package_id'
EOF
chmod 0444 "$selection"

cache=$fixture/cache
mkdir "$cache"
export PATH="$fake_bin:$PATH"
export FAKE_IDLE="$idle"
export FOGCAST_DIR="$fogcast"
export TARGET_IMAGE_LOCK_BIN="$selector"
export NATIVE_RUNTIME_INPUT_LOCK="$lock"
export NATIVE_RUNTIME_CACHE="$cache"
export FES_PACKAGE_IDS=fes.pong
export FES_PONG_PACKAGE_DIR="$package"
export FES_PONG_PACKAGE_SELECTION="$selection"

NATIVE_RUNTIME_MODE=package-only sh "$repo/scripts/fetch-native-runtime-inputs.sh"
test -f "$cache/idle.rbf"
test "$(stat -c %a "$cache/idle.rbf")" = 444
test -f "$cache/fes-pong.package-selection.toml"
test -d "$cache/core-packages/$package_id"

if NATIVE_RUNTIME_MODE=format1 sh "$repo/scripts/fetch-native-runtime-inputs.sh" \
  >"$fixture/legacy.out" 2>"$fixture/legacy.err"; then
  fail 'legacy format-1 mode was accepted'
fi
printf '%s\n' 'native runtime package-only inputs passed'
