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

[splash_rbf]
repository = 'https://github.com/DeanoC/misteross'
commit = 'a2af7fdd58d8e5d288892aeda38e8dc226aaed07'
path = 'sealed/fes-splash.rbf'
sha256 = '$idle_sha'
size = $idle_size
fat_destination = '/menu.rbf'

[idle_rbf]
repository = 'https://github.com/DeanoC/misteross'
commit = 'a2af7fdd58d8e5d288892aeda38e8dc226aaed07'
path = 'sealed/fes-splash.rbf'
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
printf '%s\n' "$*" >> "$WGET_LOG"
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
export WGET_LOG=$fixture/wget.log
: > "$WGET_LOG"

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
unset GITHUB_TOKEN GH_TOKEN

local_root=$fixture/fes-local
mkdir -p "$local_root/sources/misteross/sealed"
cp "$idle" "$local_root/sources/misteross/sealed/fes-splash.rbf"
chmod 0444 "$local_root/sources/misteross/sealed/fes-splash.rbf"
local_lock=$fixture/local.lock
sed -e "s#https://github.com/DeanoC/misteross#sources/misteross#" "$lock" > "$local_lock"
: > "$WGET_LOG"
NATIVE_RUNTIME_MODE=package-only NATIVE_RUNTIME_INPUT_LOCK="$local_lock" \
  NATIVE_RUNTIME_CACHE="$cache" FES_ROOT="$local_root" \
  sh "$repo/scripts/fetch-native-runtime-inputs.sh"
test ! -s "$WGET_LOG" || fail 'in-tree splash/idle fetch used wget'
test -f "$cache/idle.rbf"
test -f "$cache/splash.rbf"
test "$(stat -c %a "$cache/idle.rbf")" = 444
test "$(stat -c %a "$cache/splash.rbf")" = 444
test "$(sha256sum "$cache/idle.rbf" | awk '{print $1}')" = "$(sha256sum "$cache/splash.rbf" | awk '{print $1}')"

github_cache=$fixture/github-cache
mkdir "$github_cache"
: > "$WGET_LOG"
NATIVE_RUNTIME_MODE=package-only NATIVE_RUNTIME_CACHE="$github_cache" \
  sh "$repo/scripts/fetch-native-runtime-inputs.sh"
grep -Fq 'https://raw.githubusercontent.com/DeanoC/misteross/a2af7fdd58d8e5d288892aeda38e8dc226aaed07/sealed/fes-splash.rbf' \
  "$WGET_LOG" || fail 'fetch did not wget the locked GitHub splash path'
test -f "$cache/idle.rbf"
test -f "$cache/splash.rbf"
test "$(stat -c %a "$cache/idle.rbf")" = 444
test "$(stat -c %a "$cache/splash.rbf")" = 444
test "$(sha256sum "$cache/idle.rbf" | awk '{print $1}')" = "$(sha256sum "$cache/splash.rbf" | awk '{print $1}')"
test -f "$cache/fes-pong.package-selection.toml"
test -d "$cache/core-packages/$package_id"

chmod u+w "$cache/splash.rbf"
printf '%s\n' 'stale splash bytes' >"$cache/splash.rbf"
chmod 0444 "$cache/splash.rbf"
NATIVE_RUNTIME_MODE=package-only NATIVE_RUNTIME_INPUT_LOCK="$local_lock" \
  FES_ROOT="$local_root" sh "$repo/scripts/fetch-native-runtime-inputs.sh"
test "$(sha256sum "$cache/idle.rbf" | awk '{print $1}')" = "$(sha256sum "$cache/splash.rbf" | awk '{print $1}')"
test "$(stat -c %a "$cache/splash.rbf")" = 444

auth_cache=$fixture/auth-cache
mkdir "$auth_cache"
: > "$WGET_LOG"
NATIVE_RUNTIME_MODE=package-only NATIVE_RUNTIME_CACHE="$auth_cache" \
  GITHUB_TOKEN=fixture-github-token \
  sh "$repo/scripts/fetch-native-runtime-inputs.sh"
grep -Fq 'https://api.github.com/repos/DeanoC/misteross/contents/sealed/fes-splash.rbf?ref=a2af7fdd58d8e5d288892aeda38e8dc226aaed07' \
  "$WGET_LOG" || fail 'authenticated fetch did not use the GitHub contents API'
grep -Fq 'Authorization: Bearer fixture-github-token' "$WGET_LOG" || \
  fail 'authenticated fetch did not send the GitHub token'
grep -Fq 'Accept: application/vnd.github.raw' "$WGET_LOG" || \
  fail 'authenticated fetch did not request raw GitHub contents'
! grep -Fq 'raw.githubusercontent.com' "$WGET_LOG" || \
  fail 'authenticated fetch still used unauthenticated raw.githubusercontent.com'
test "$(sha256sum "$auth_cache/idle.rbf" | awk '{print $1}')" = "$idle_sha"

failing_wget=$fixture/fail-wget
mkdir "$failing_wget"
cat >"$failing_wget/wget" <<'WGET'
#!/bin/sh
printf '%s\n' "$*" >> "$WGET_LOG"
exit 8
WGET
chmod +x "$failing_wget/wget"
empty_cache=$fixture/empty-cache
mkdir "$empty_cache"
: > "$WGET_LOG"
if PATH="$failing_wget:$PATH" NATIVE_RUNTIME_MODE=package-only \
  NATIVE_RUNTIME_CACHE="$empty_cache" \
  sh "$repo/scripts/fetch-native-runtime-inputs.sh" \
  >"$fixture/missing-token.out" 2>"$fixture/missing-token.err"; then
  fail 'unauthenticated private fetch succeeded'
fi
grep -Fq 'Private GitHub pins require GITHUB_TOKEN or GH_TOKEN' \
  "$fixture/missing-token.err" || \
  fail 'missing credential did not fail with a hard GitHub token error'
! grep -Fq 'fixture-github-token' "$fixture/missing-token.err" || \
  fail 'credential failure printed a token'

awk 'BEGIN { skip=0 } /^\[splash_rbf\]/ { skip=1; next } /^\[/ { skip=0 } skip { next } { print }' \
  "$lock" > "$fixture/idle-only.lock"
if NATIVE_RUNTIME_MODE=package-only NATIVE_RUNTIME_INPUT_LOCK="$fixture/idle-only.lock" \
  sh "$repo/scripts/fetch-native-runtime-inputs.sh" \
  >"$fixture/idle-only.out" 2>"$fixture/idle-only.err"; then
  fail 'fetch accepted a lock without splash_rbf'
fi

if NATIVE_RUNTIME_MODE=format1 sh "$repo/scripts/fetch-native-runtime-inputs.sh" \
  >"$fixture/legacy.out" 2>"$fixture/legacy.err"; then
  fail 'legacy format-1 mode was accepted'
fi
if NATIVE_RUNTIME_MODE=format1 sh "$repo/scripts/verify-native-runtime-inputs.sh" \
  >"$fixture/verify-legacy.out" 2>"$fixture/verify-legacy.err"; then
  fail 'verifier accepted the retired format-1 mode'
fi
if NATIVE_RUNTIME_MODE=unsupported sh "$repo/scripts/verify-native-runtime-inputs.sh" \
  >"$fixture/verify-unsupported.out" 2>"$fixture/verify-unsupported.err"; then
  fail 'verifier accepted an unsupported runtime mode'
fi
if NATIVE_RUNTIME_MODE=package-only sh "$repo/scripts/verify-native-runtime-inputs.sh" \
  "$lock" "$fogcast" "$cache/idle.rbf" \
  >"$fixture/verify-idle-only.out" 2>"$fixture/verify-idle-only.err"; then
  fail 'verifier accepted idle-only arguments'
fi
grep -Fq 'IDLE_FILE SPLASH_FILE' "$fixture/verify-idle-only.err" || \
  fail 'idle-only rejection did not require SPLASH_FILE'
printf '%s\n' 'native runtime package-only inputs passed'

# Real Git provenance: standalone checkouts and full-FES module snapshots.
standalone=$fixture/runtime
mono=$fixture/fes
mkdir -p "$standalone" "$mono/sources/libmister-runtime" "$mono/sources/FogCast/build"
printf '%s\n' runtime > "$standalone/README"
printf '%s\n' runtime > "$mono/sources/libmister-runtime/README"
printf '%s\n' host > "$mono/sources/FogCast/README"
for tree in "$standalone" "$mono"; do
  git -C "$tree" init -q
  git -C "$tree" -c user.name=Test -c user.email=test@example.invalid add .
  git -C "$tree" -c user.name=Test -c user.email=test@example.invalid commit -qm fixture
done
cat > "$fake_bin/container" <<'CONTAINER'
#!/bin/sh
set -eu
if [ "$1" = run ]; then printf '%s\n' "$@" > "$CONTAINER_ARGS"; fi
CONTAINER
chmod +x "$fake_bin/container"
export CONTAINER_ARGS="$fixture/container-args"
printf '%s\n' 'tampered splash' >"$fixture/tampered.splash"
chmod 0444 "$fixture/tampered.splash"
for source in "$standalone" "$mono/sources/libmister-runtime"; do
  actual_commit=$(git -C "$source" rev-parse HEAD)
  sed "s/$runtime_commit/$actual_commit/" "$lock" > "$fixture/selected.lock"
  sh "$repo/scripts/verify-native-runtime-inputs.sh" "$fixture/selected.lock" "$source" "$cache/idle.rbf" "$cache/splash.rbf"
  if [ "$source" = "$standalone" ]; then
    if sh "$repo/scripts/verify-native-runtime-inputs.sh" \
      "$fixture/selected.lock" "$source" "$cache/idle.rbf" "$fixture/tampered.splash" \
      >"$fixture/verify-tampered.out" 2>"$fixture/verify-tampered.err"; then
      fail 'verifier accepted a tampered splash payload'
    fi
    grep -Fq 'splash SHA-256 does not match the lock' "$fixture/verify-tampered.err" || \
      fail 'tampered splash was not rejected for SHA-256'
    selected_fogcast=$fogcast
    expected_mount=$standalone
    expected_prefix=.
    expected_fogcast_mount=$fogcast
    expected_fogcast_path=/fogcast
  else
    selected_fogcast=$mono/sources/FogCast
    expected_mount=$mono
    expected_prefix=sources/libmister-runtime
    expected_fogcast_mount=$mono
    expected_fogcast_path=/fogcast/sources/FogCast
    # Generated host overlay does not dirty runtime scope.
    printf '%s\n' overlay > "$mono/sources/FogCast/build/generated"
    sh "$repo/scripts/verify-native-runtime-inputs.sh" "$fixture/selected.lock" "$source" "$cache/idle.rbf" "$cache/splash.rbf"
  fi
  FOGCAST_DIR="$selected_fogcast" LIBMISTER_RUNTIME_DIR="$source" \
    NATIVE_RUNTIME_INPUT_LOCK="$fixture/selected.lock" NATIVE_RUNTIME_IDLE_FILE="$cache/idle.rbf" \
    TARGET_IMAGE_CONTAINER_RUNTIME="$fake_bin/container" TARGET_IMAGE_DEV_CONTAINER=1 \
    sh "$repo/scripts/target-image-container.sh" run /bin/true
  grep -Fxq "$expected_mount:/runtime-source:ro" "$CONTAINER_ARGS" || fail 'runtime Git root was not mounted'
  grep -Fxq "FES_RUNTIME_SOURCE_PATH=$expected_prefix" "$CONTAINER_ARGS" || fail 'runtime module path missing'
  grep -Fxq "$expected_fogcast_mount:/fogcast:ro" "$CONTAINER_ARGS" || fail 'FogCast Git root was not mounted'
  grep -Fxq "FOGCAST_DIR=$expected_fogcast_path" "$CONTAINER_ARGS" || fail 'FogCast module path missing'
  printf '%s\n' dirty > "$source/README"
  if sh "$repo/scripts/verify-native-runtime-inputs.sh" \
    "$fixture/selected.lock" "$source" "$cache/idle.rbf" "$cache/splash.rbf" \
    > /dev/null 2>&1; then
    fail 'dirty runtime module was accepted'
  fi
  git -C "$source" restore README
done
mkdir -p "$mono/other-runtime"
if sh "$repo/scripts/verify-native-runtime-inputs.sh" \
  "$fixture/selected.lock" "$mono/other-runtime" "$cache/idle.rbf" "$cache/splash.rbf" \
  > /dev/null 2>&1; then
  fail 'arbitrary nested runtime path was accepted'
fi
python3 - "$repo/build/native-inputs.toml" <<'PY' || fail 'production splash/idle lock schema is wrong'
import sys, tomllib
policy = tomllib.load(open(sys.argv[1], 'rb'))
expected = {
    'repository': 'sources/misteross',
    'commit': 'a2af7fdd58d8e5d288892aeda38e8dc226aaed07',
    'path': 'sealed/fes-splash.rbf',
    'sha256': 'f165fdb841c16cb75e27ad518ff689802890008422abe51b5a788b42d8bd33d6',
    'size': 1961783,
}
for section in ('splash_rbf', 'idle_rbf'):
    for key, value in expected.items():
        if policy[section][key] != value:
            raise SystemExit(f'{section}.{key}={policy[section][key]!r}')
if policy['splash_rbf']['fat_destination'] != '/menu.rbf':
    raise SystemExit('splash fat_destination')
if policy['idle_rbf']['install_path'] != '/usr/share/mister-runtime/idle.rbf':
    raise SystemExit('idle install_path')
PY
printf '%s\n' 'standalone and monorepo runtime verification/container mounts passed'
cat > "$fixture/site.mk" <<EOF
include $repo/buildroot/package/mister-runtime/mister-runtime.mk
print-site:
	@printf '%s\n' '\$(MISTER_RUNTIME_SITE)'
EOF
for prefix in . sources/libmister-runtime; do
  # Parent make may export -w; capture the value without directory messages.
  site=$(FES_RUNTIME_SOURCE_PATH="$prefix" make --no-print-directory -s -f "$fixture/site.mk" print-site)
  test "$site" = "/runtime-source/$prefix" || fail 'Buildroot SITE differs from mounted module'
done
