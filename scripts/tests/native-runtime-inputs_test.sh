#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-native-runtime-inputs.XXXXXX")
trap 'chmod -R u+w "$fixture" 2>/dev/null || :; rm -rf "$fixture"' EXIT INT TERM

fail() {
  printf 'native runtime input test: %s\n' "$*" >&2
  exit 1
}

expect_rejected() {
  reject_name=$1
  shift
  if "$@" >"$fixture/rejected.out" 2>"$fixture/rejected.err"; then
    fail "$reject_name was accepted"
  fi
}

active_once() {
  active_file=$1
  active_value=$2
  awk -v wanted="$active_value" '
    $0 !~ /^[[:space:]]*#/ && $0 == wanted { count++ }
    END { exit count == 1 ? 0 : 1 }
  ' "$active_file"
}

extract_command_block() {
  extract_file=$1
  extract_name=$2
  extract_output=$3
  awk -v wanted="define $extract_name" '
    $0 == wanted {
      found++
      active=1
      next
    }
    active && $0 == "endef" {
      ended++
      active=0
      next
    }
    active { print }
    END { if (found != 1 || ended != 1 || active) exit 1 }
  ' "$extract_file" >"$extract_output"
}

validate_package_semantics() {
  validate_mk=$1
  validate_package_config=$2
  validate_root_config=$3
  validate_external=$4

  active_once "$validate_mk" 'MISTER_RUNTIME_SITE = /runtime-source' &&
    active_once "$validate_mk" 'MISTER_RUNTIME_SITE_METHOD = local' &&
    active_once "$validate_mk" 'MISTER_RUNTIME_LICENSE = GPL-3.0-or-later' &&
    active_once "$validate_mk" 'MISTER_RUNTIME_LICENSE_FILES = LICENSE' &&
    active_once "$validate_mk" '$(eval $(generic-package))' || return 1

  extract_command_block "$validate_mk" MISTER_RUNTIME_BUILD_CMDS \
    "$fixture/actual-build-block" || return 1
  cmp "$expected_build_block" "$fixture/actual-build-block" >/dev/null 2>&1 || return 1
  extract_command_block "$validate_mk" MISTER_RUNTIME_INSTALL_TARGET_CMDS \
    "$fixture/actual-install-block" || return 1
  cmp "$expected_install_block" "$fixture/actual-install-block" >/dev/null 2>&1 || return 1

  awk '
    /^define MISTER_RUNTIME_.*_CMDS$/ { definitions[$0]++ }
    END {
      exit definitions["define MISTER_RUNTIME_BUILD_CMDS"] == 1 &&
           definitions["define MISTER_RUNTIME_INSTALL_TARGET_CMDS"] == 1 &&
           length(definitions) == 2 ? 0 : 1
    }
  ' "$validate_mk" || return 1

  awk '
    /^[[:space:]]*#/ { next }
    /^config / { section=$2; configs[$2]++; next }
    section == "BR2_PACKAGE_FOGCAST_MISTER_RUNTIME" &&
      $0 == "\tbool \"mister-runtime\"" { public_bool++ }
    section == "BR2_PACKAGE_MISTER_RUNTIME" &&
      $0 == "\tbool" { hidden_bool++ }
    section == "BR2_PACKAGE_MISTER_RUNTIME" &&
      $0 == "\tdefault y if BR2_PACKAGE_FOGCAST_MISTER_RUNTIME" { bridge++ }
    END {
      exit configs["BR2_PACKAGE_FOGCAST_MISTER_RUNTIME"] == 1 &&
           configs["BR2_PACKAGE_MISTER_RUNTIME"] == 1 &&
           public_bool == 1 && hidden_bool == 1 && bridge == 1 ? 0 : 1
    }
  ' "$validate_package_config" || return 1

  active_once "$validate_root_config" \
    'source "$BR2_EXTERNAL_FOGCAST_TARGET_PATH/package/mister-runtime/Config.in"' &&
    active_once "$validate_external" \
      'include $(sort $(wildcard $(BR2_EXTERNAL_FOGCAST_TARGET_PATH)/package/*/*.mk))'
}

expect_semantic_rejected() {
  semantic_name=$1
  shift
  if validate_package_semantics "$@"; then
    fail "package semantic validator accepted $semantic_name"
  fi
}

write_lock() {
  write_path=$1
  write_runtime_commit=$2
  write_idle_sha=$3
  write_idle_size=$4
  write_megadrive_sha=$5
  write_megadrive_size=$6
  cat >"$write_path" <<EOF
format = 1

[mister_runtime]
commit = '$write_runtime_commit'
mount_path = '/runtime-source'

[idle_rbf]
repository = 'https://github.com/MiSTer-devel/Distribution_MiSTer'
commit = 'f7bde4becb452ca28f604ad9802bbed5c6b58e01'
path = 'menu.rbf'
sha256 = '$write_idle_sha'
size = $write_idle_size
install_path = '/usr/share/mister-runtime/idle.rbf'

[megadrive_rbf]
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
commit = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
path = 'releases/MegaDrive_20260603.rbf'
sha256 = '$write_megadrive_sha'
size = $write_megadrive_size
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
EOF
}

verifier=$repo/scripts/verify-native-runtime-inputs.sh
fetcher=$repo/scripts/fetch-native-runtime-inputs.sh
container=$repo/scripts/target-image-container.sh
package_mk=$repo/buildroot/package/mister-runtime/mister-runtime.mk
package_config=$repo/buildroot/package/mister-runtime/Config.in

test -x "$verifier" || fail 'missing executable verifier'
test -x "$fetcher" || fail 'missing executable fetcher'
test -f "$package_mk" || fail 'missing Buildroot package makefile'
test -f "$package_config" || fail 'missing Buildroot package Config.in'

runtime_source=$fixture/runtime-source
mkdir -p "$runtime_source"
git -C "$runtime_source" init -q
printf '%s\n' runtime >"$runtime_source/README"
printf '%s\n' /build/ >"$runtime_source/.gitignore"
git -C "$runtime_source" add README .gitignore
git -C "$runtime_source" -c user.name=Test -c user.email=test@example.invalid \
  commit -q -m fixture
runtime_commit=$(git -C "$runtime_source" rev-parse HEAD)
mkdir -p "$runtime_source/build"
printf '%s\n' 'ignored x86-64 host object' >"$runtime_source/build/host-object.o"
test -z "$(git -C "$runtime_source" status --porcelain --untracked-files=all)"

idle=$fixture/idle.rbf
printf '%s' 'fixture idle rbf' >"$idle"
idle_sha=$(sha256sum "$idle" | awk '{print $1}')
idle_size=$(wc -c <"$idle" | tr -d ' ')
megadrive=$fixture/megadrive.rbf
printf '%s' 'fixture Mega Drive rbf' >"$megadrive"
chmod 0444 "$megadrive"
megadrive_sha=$(sha256sum "$megadrive" | awk '{print $1}')
megadrive_size=$(wc -c <"$megadrive" | tr -d ' ')
lock=$fixture/native-runtime.inputs.lock.toml
write_lock "$lock" "$runtime_commit" "$idle_sha" "$idle_size" \
  "$megadrive_sha" "$megadrive_size"

selection=$fixture/megadrive.selection.toml
cat >"$selection" <<EOF
format = 1
origin = 'source-built'
abi = 'mister'
system = 'megadrive'
repository = 'https://fixture.invalid/source-built-megadrive'
revision = '4444444444444444444444444444444444444444'
artifact = 'megadrive.rbf'
sha256 = '$megadrive_sha'
size = $megadrive_size
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
recipe = 'scripts/rebuild_core.py'
recipe_sha256 = '5555555555555555555555555555555555555555555555555555555555555555'
toolchain = 'fixture-toolchain'
EOF
chmod 0444 "$selection"
export NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE="$selection"

verified_output=$(
  sh "$verifier" "$lock" "$runtime_source" "$idle" "$megadrive"
)
printf '%s\n' "$verified_output" | grep -Fq "$runtime_commit"
printf '%s\n' "$verified_output" | grep -Fq "$idle_sha"
printf '%s\n' "$verified_output" | grep -Fq "$megadrive_sha"
if printf '%s\n' "$verified_output" | grep -Fq "$runtime_source"; then
  fail 'verifier printed a local runtime checkout path'
fi

wrong_selection=$fixture/wrong-selection.toml
sed "s/$megadrive_sha/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/" \
  "$selection" >"$wrong_selection"
expect_rejected 'selection identity mismatch' \
  env NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE="$wrong_selection" \
    sh "$verifier" "$lock" "$runtime_source" "$idle" "$megadrive"

upstream_selection=$fixture/upstream-selection.toml
cat >"$upstream_selection" <<EOF
format = 1
origin = 'upstream'
abi = 'mister'
system = 'megadrive'
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
revision = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
artifact = 'releases/MegaDrive_20260603.rbf'
sha256 = '$megadrive_sha'
size = $megadrive_size
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
EOF
chmod 0444 "$upstream_selection"
upstream_output=$(NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE="$upstream_selection" \
  sh "$verifier" "$lock" "$runtime_source" "$idle" "$megadrive")
printf '%s\n' "$upstream_output" | grep -Fq 'megadrive_origin=upstream'

printf '%s\n' second >"$runtime_source/SECOND"
git -C "$runtime_source" add SECOND
git -C "$runtime_source" -c user.name=Test -c user.email=test@example.invalid \
  commit -q -m second
expect_rejected 'wrong runtime HEAD' \
  sh "$verifier" "$lock" "$runtime_source" "$idle" "$megadrive"
git -C "$runtime_source" reset -q --hard "$runtime_commit"

printf '%s\n' dirty >"$runtime_source/untracked"
expect_rejected 'dirty runtime checkout' \
  sh "$verifier" "$lock" "$runtime_source" "$idle" "$megadrive"
rm "$runtime_source/untracked"

wrong_digest_lock=$fixture/wrong-digest.lock.toml
write_lock "$wrong_digest_lock" "$runtime_commit" \
  aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  "$idle_size" "$megadrive_sha" "$megadrive_size"
expect_rejected 'wrong idle digest' \
  sh "$verifier" "$wrong_digest_lock" "$runtime_source" "$idle" "$megadrive"

wrong_size_lock=$fixture/wrong-size.lock.toml
write_lock "$wrong_size_lock" "$runtime_commit" "$idle_sha" "$((idle_size + 1))" \
  "$megadrive_sha" "$megadrive_size"
expect_rejected 'wrong idle size' \
  sh "$verifier" "$wrong_size_lock" "$runtime_source" "$idle" "$megadrive"

expect_rejected 'non-regular idle path' \
  sh "$verifier" "$lock" "$runtime_source" "$fixture" "$megadrive"

missing_commit_lock=$fixture/missing-commit.lock.toml
sed "/^commit = '$runtime_commit'$/d" "$lock" >"$missing_commit_lock"
expect_rejected 'missing runtime commit' \
  sh "$verifier" "$missing_commit_lock" "$runtime_source" "$idle" "$megadrive"

short_commit_lock=$fixture/short-commit.lock.toml
sed "s/$runtime_commit/abc123/" "$lock" >"$short_commit_lock"
expect_rejected 'non-40-character runtime commit' \
  sh "$verifier" "$short_commit_lock" "$runtime_source" "$idle" "$megadrive"

relative_mount_lock=$fixture/relative-mount.lock.toml
sed "s#mount_path = '/runtime-source'#mount_path = 'runtime-source'#" \
  "$lock" >"$relative_mount_lock"
expect_rejected 'relative runtime mount path' \
  sh "$verifier" "$relative_mount_lock" "$runtime_source" "$idle" "$megadrive"

relative_install_lock=$fixture/relative-install.lock.toml
sed "s#install_path = '/usr/share/mister-runtime/idle.rbf'#install_path = 'idle.rbf'#" \
  "$lock" >"$relative_install_lock"
expect_rejected 'relative idle install path' \
  sh "$verifier" "$relative_install_lock" "$runtime_source" "$idle" "$megadrive"

wrong_repository_lock=$fixture/wrong-repository.lock.toml
sed 's#https://github.com/MiSTer-devel/Distribution_MiSTer#https://example.invalid/mutable#' \
  "$lock" >"$wrong_repository_lock"
expect_rejected 'wrong idle repository' \
  sh "$verifier" "$wrong_repository_lock" "$runtime_source" "$idle" "$megadrive"

wrong_idle_commit_lock=$fixture/wrong-idle-commit.lock.toml
sed 's/f7bde4becb452ca28f604ad9802bbed5c6b58e01/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/' \
  "$lock" >"$wrong_idle_commit_lock"
expect_rejected 'wrong idle commit' \
  sh "$verifier" "$wrong_idle_commit_lock" "$runtime_source" "$idle" "$megadrive"

wrong_idle_path_lock=$fixture/wrong-idle-path.lock.toml
sed "s/path = 'menu.rbf'/path = 'latest.rbf'/" \
  "$lock" >"$wrong_idle_path_lock"
expect_rejected 'wrong idle source path' \
  sh "$verifier" "$wrong_idle_path_lock" "$runtime_source" "$idle" "$megadrive"

wrong_megadrive_digest_lock=$fixture/wrong-megadrive-digest.lock.toml
sed "s/$megadrive_sha/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/" \
  "$lock" >"$wrong_megadrive_digest_lock"
expect_rejected 'wrong Mega Drive digest' \
  sh "$verifier" "$wrong_megadrive_digest_lock" "$runtime_source" "$idle" "$megadrive"

wrong_megadrive_size_lock=$fixture/wrong-megadrive-size.lock.toml
write_lock "$wrong_megadrive_size_lock" "$runtime_commit" "$idle_sha" "$idle_size" \
  "$megadrive_sha" 0
expect_rejected 'wrong Mega Drive size' \
  sh "$verifier" "$wrong_megadrive_size_lock" "$runtime_source" "$idle" "$megadrive"

wrong_megadrive_path_lock=$fixture/wrong-megadrive-path.lock.toml
sed "s#path = 'releases/MegaDrive_20260603.rbf'#path = 'releases/latest.rbf'#" \
  "$lock" >"$wrong_megadrive_path_lock"
expect_rejected 'wrong Mega Drive source path' \
  sh "$verifier" "$wrong_megadrive_path_lock" "$runtime_source" "$idle" "$megadrive"

fat_megadrive_install_lock=$fixture/fat-megadrive-install.lock.toml
sed "s#install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'#install_path = '/media/fat/_Console/MegaDrive.rbf'#" \
  "$lock" >"$fat_megadrive_install_lock"
expect_rejected 'FAT Mega Drive install path' \
  sh "$verifier" "$fat_megadrive_install_lock" "$runtime_source" "$idle" "$megadrive"

wrong_megadrive_repository_lock=$fixture/wrong-megadrive-repository.lock.toml
sed 's#https://github.com/MiSTer-devel/MegaDrive_MiSTer#https://example.invalid/MegaDrive#' \
  "$lock" >"$wrong_megadrive_repository_lock"
expect_rejected 'wrong Mega Drive repository' \
  sh "$verifier" "$wrong_megadrive_repository_lock" "$runtime_source" "$idle" "$megadrive"

wrong_megadrive_commit_lock=$fixture/wrong-megadrive-commit.lock.toml
sed 's/7365a137cfd8fa6f041e964d8b953159c0ec42d9/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/' \
  "$lock" >"$wrong_megadrive_commit_lock"
expect_rejected 'wrong Mega Drive commit' \
  sh "$verifier" "$wrong_megadrive_commit_lock" "$runtime_source" "$idle" "$megadrive"

expect_rejected 'non-regular Mega Drive path' \
  sh "$verifier" "$lock" "$runtime_source" "$idle" "$fixture"

duplicate_megadrive_lock=$fixture/duplicate-megadrive.lock.toml
cp "$lock" "$duplicate_megadrive_lock"
sed -n '/^\[megadrive_rbf\]$/,$p' "$lock" >>"$duplicate_megadrive_lock"
expect_rejected 'duplicate Mega Drive lock section' \
  sh "$verifier" "$duplicate_megadrive_lock" "$runtime_source" "$idle" "$megadrive"

fake_bin=$fixture/bin
mkdir -p "$fake_bin"
cat >"$fake_bin/wget" <<'EOF'
#!/bin/sh
set -eu
output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -O)
      shift
      output=$1
      ;;
    https://*) url=$1 ;;
  esac
  shift
done
test -n "$output"
test -n "$url"
printf '%s\n' "$url" >>"$NATIVE_RUNTIME_FETCH_LOG"
case "$url" in
  *MiSTer-devel/Distribution_MiSTer/*) source=$NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD ;;
  *MiSTer-devel/MegaDrive_MiSTer/*) source=$NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD ;;
  *) exit 91 ;;
esac
cp "$source" "$output"
EOF
chmod 0755 "$fake_bin/wget"

source_bundle=$fixture/source-built-bundle
mkdir -p "$source_bundle"
printf '%s' 'fixture source-built Mega Drive rbf' >"$source_bundle/megadrive.rbf"
source_bundle_sha=$(sha256sum "$source_bundle/megadrive.rbf" | awk '{print $1}')
source_bundle_size=$(wc -c <"$source_bundle/megadrive.rbf" | tr -d ' ')
cat >"$source_bundle/megadrive-rbf.toml" <<EOF
format = 1
abi = 'mister'
system = 'megadrive'
artifact = 'megadrive.rbf'
sha256 = '$source_bundle_sha'
size = $source_bundle_size
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
revision = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
recipe = 'scripts/rebuild_core.py'
recipe_sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
toolchain = 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'
EOF
chmod 0444 "$source_bundle/megadrive.rbf" "$source_bundle/megadrive-rbf.toml"
chmod 0555 "$source_bundle"

selector=$fixture/bin/target-image-lock
selector_log=$fixture/selector.log
cat >"$selector" <<'EOF'
#!/bin/sh
set -eu
source=
bundle=
artifact=
cache=
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --source) source=$2; shift ;;
    --bundle) bundle=$2; shift ;;
    --artifact) artifact=$2; shift ;;
    --cache) cache=$2; shift ;;
    --output) output=$2; shift ;;
  esac
  shift
done
printf '%s\n' "$source|$bundle|$artifact|$cache|$output" >>"$NATIVE_RUNTIME_SELECTOR_LOG"
if [ "${NATIVE_RUNTIME_SELECTOR_FAIL:-0}" = 1 ]; then
  exit 31
fi
mkdir -p "$cache"
if [ "$source" = source-built ]; then
  cp "$bundle/megadrive.rbf" "$cache/megadrive.rbf"
else
  cp "$artifact" "$cache/megadrive.rbf"
fi
printf 'origin = %s\n' "$source" >"$output"
EOF
chmod 0755 "$selector"

source_cache=$fixture/source-cache
source_fetch_log=$fixture/source-fetch.log
PATH="$fake_bin:$PATH" \
NATIVE_RUNTIME_INPUT_LOCK=$lock \
NATIVE_RUNTIME_CACHE=$source_cache \
NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD=$idle \
NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD=$megadrive \
NATIVE_RUNTIME_FETCH_LOG=$source_fetch_log \
NATIVE_RUNTIME_SELECTOR_LOG=$selector_log \
TARGET_IMAGE_LOCK_BIN=$selector \
MEGADRIVE_RBF_BUNDLE=$source_bundle \
  sh "$fetcher"
cmp "$source_bundle/megadrive.rbf" "$source_cache/megadrive.rbf"
test -f "$source_cache/megadrive.selection.toml"
grep -Fqx -- 'source-built|'"$source_bundle"'||'"$source_cache"'|'"$source_cache"'/megadrive.selection.toml' "$selector_log"
grep -Fqx -- \
  "https://raw.githubusercontent.com/MiSTer-devel/Distribution_MiSTer/f7bde4becb452ca28f604ad9802bbed5c6b58e01/menu.rbf" \
  "$source_fetch_log"
if grep -Fq 'MegaDrive_MiSTer' "$source_fetch_log"; then
  fail 'default source-built selection invoked the upstream Mega Drive download'
fi

upstream_cache=$fixture/upstream-cache
upstream_selector_log=$fixture/upstream-selector.log
fetch_log=$fixture/upstream-fetch.log
PATH="$fake_bin:$PATH" \
NATIVE_RUNTIME_INPUT_LOCK=$lock \
NATIVE_RUNTIME_CACHE=$upstream_cache \
NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD=$idle \
NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD=$megadrive \
NATIVE_RUNTIME_FETCH_LOG=$fetch_log \
NATIVE_RUNTIME_SELECTOR_LOG=$upstream_selector_log \
TARGET_IMAGE_LOCK_BIN=$selector \
MEGADRIVE_RBF_SOURCE=upstream \
  sh "$fetcher"
cmp "$megadrive" "$upstream_cache/megadrive.rbf"
test -f "$upstream_cache/megadrive.selection.toml"
grep -Fq 'upstream||' "$upstream_selector_log"

expect_rejected 'missing default source-built bundle' \
  env PATH="$fake_bin:$PATH" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_CACHE="$fixture/missing-cache" \
    NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD="$idle" \
    NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD="$megadrive" \
    NATIVE_RUNTIME_FETCH_LOG="$fetch_log" \
    NATIVE_RUNTIME_SELECTOR_LOG="$selector_log" \
    TARGET_IMAGE_LOCK_BIN="$selector" \
    MEGADRIVE_RBF_BUNDLE="$fixture/does-not-exist" \
    sh "$fetcher"

expect_rejected 'unknown Mega Drive source' \
  env PATH="$fake_bin:$PATH" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_CACHE="$fixture/unknown-cache" \
    NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD="$idle" \
    NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD="$megadrive" \
    NATIVE_RUNTIME_FETCH_LOG="$fetch_log" \
    NATIVE_RUNTIME_SELECTOR_LOG="$selector_log" \
    TARGET_IMAGE_LOCK_BIN="$selector" \
    MEGADRIVE_RBF_SOURCE=other \
    sh "$fetcher"

expect_rejected 'source-built selector failure without upstream fallback' \
  env PATH="$fake_bin:$PATH" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_CACHE="$fixture/failing-cache" \
    NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD="$idle" \
    NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD="$megadrive" \
    NATIVE_RUNTIME_FETCH_LOG="$fixture/failing-fetch.log" \
    NATIVE_RUNTIME_SELECTOR_LOG="$fixture/failing-selector.log" \
    TARGET_IMAGE_LOCK_BIN="$selector" \
    NATIVE_RUNTIME_SELECTOR_FAIL=1 \
    MEGADRIVE_RBF_BUNDLE="$source_bundle" \
    sh "$fetcher"
if grep -Fq 'MegaDrive_MiSTer' "$fixture/failing-fetch.log" 2>/dev/null; then
  fail 'source-built selector failure fell back to upstream download'
fi

fetch_cache=$fixture/cache
fetch_log=$fixture/fetch.log
PATH="$fake_bin:$PATH" \
NATIVE_RUNTIME_INPUT_LOCK=$lock \
NATIVE_RUNTIME_CACHE=$fetch_cache \
NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD=$idle \
NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD=$megadrive \
NATIVE_RUNTIME_FETCH_LOG=$fetch_log \
NATIVE_RUNTIME_SELECTOR_LOG=$fixture/legacy-selector.log \
TARGET_IMAGE_LOCK_BIN=$selector \
MEGADRIVE_RBF_SOURCE=upstream \
  sh "$fetcher"
cmp "$idle" "$fetch_cache/idle.rbf"
cmp "$megadrive" "$fetch_cache/megadrive.rbf"
grep -Fqx -- \
  "https://raw.githubusercontent.com/MiSTer-devel/Distribution_MiSTer/f7bde4becb452ca28f604ad9802bbed5c6b58e01/menu.rbf" \
  "$fetch_log"
grep -Fqx -- \
  "https://raw.githubusercontent.com/MiSTer-devel/MegaDrive_MiSTer/7365a137cfd8fa6f041e964d8b953159c0ec42d9/releases/MegaDrive_20260603.rbf" \
  "$fetch_log"

printf '%s' prior >"$fetch_cache/idle.rbf"
bad_download=$fixture/bad-download.rbf
printf '%s' altered >"$bad_download"
expect_rejected 'fetch with wrong digest' \
  env PATH="$fake_bin:$PATH" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_CACHE="$fetch_cache" \
    NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD="$bad_download" \
    NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD="$megadrive" \
    NATIVE_RUNTIME_FETCH_LOG="$fetch_log" \
    NATIVE_RUNTIME_SELECTOR_LOG="$fixture/legacy-selector.log" \
    TARGET_IMAGE_LOCK_BIN="$selector" \
    MEGADRIVE_RBF_SOURCE=upstream \
    sh "$fetcher"
test "$(cat "$fetch_cache/idle.rbf")" = prior || \
  fail 'failed fetch replaced the prior cached artifact'

wrong_fetch_size_lock=$fixture/wrong-fetch-size.lock.toml
write_lock "$wrong_fetch_size_lock" "$runtime_commit" "$idle_sha" "$((idle_size + 1))" \
  "$megadrive_sha" "$megadrive_size"
expect_rejected 'fetch with wrong size' \
  env PATH="$fake_bin:$PATH" \
    NATIVE_RUNTIME_INPUT_LOCK="$wrong_fetch_size_lock" \
    NATIVE_RUNTIME_CACHE="$fetch_cache" \
    NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD="$idle" \
    NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD="$megadrive" \
    NATIVE_RUNTIME_FETCH_LOG="$fetch_log" \
    NATIVE_RUNTIME_SELECTOR_LOG="$fixture/legacy-selector.log" \
    TARGET_IMAGE_LOCK_BIN="$selector" \
    MEGADRIVE_RBF_SOURCE=upstream \
    sh "$fetcher"
test "$(cat "$fetch_cache/idle.rbf")" = prior || \
  fail 'wrong-size fetch replaced the prior cached artifact'

printf '%s' prior-megadrive >"$fetch_cache/megadrive.rbf"
bad_megadrive=$fixture/bad-megadrive.rbf
printf '%s' altered-megadrive >"$bad_megadrive"
expect_rejected 'fetch with wrong Mega Drive digest' \
  env PATH="$fake_bin:$PATH" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_CACHE="$fetch_cache" \
    NATIVE_RUNTIME_FAKE_IDLE_DOWNLOAD="$idle" \
    NATIVE_RUNTIME_FAKE_MEGADRIVE_DOWNLOAD="$bad_megadrive" \
    NATIVE_RUNTIME_FETCH_LOG="$fetch_log" \
    NATIVE_RUNTIME_SELECTOR_LOG="$fixture/legacy-selector.log" \
    TARGET_IMAGE_LOCK_BIN="$selector" \
    MEGADRIVE_RBF_SOURCE=upstream \
    sh "$fetcher"
test "$(cat "$fetch_cache/megadrive.rbf")" = prior-megadrive || \
  fail 'failed fetch replaced the prior cached Mega Drive artifact'

expected_build_block=$fixture/expected-build-block
cat >"$expected_build_block" <<'EOF'
	/bin/rm -rf "$(@D)/build"
	$(TARGET_MAKE_ENV) $(MAKE) -C $(@D) \
		CXX="$(TARGET_CXX)" AR="$(TARGET_AR)" NM="$(TARGET_NM)" \
		CXXFILT="$(TARGET_CROSS)c++filt" \
		MISTER_RUNTIME_VERSION="git-$(FOGCAST_MISTER_RUNTIME_COMMIT)" \
		all
EOF
expected_install_block=$fixture/expected-install-block
cat >"$expected_install_block" <<'EOF'
	$(INSTALL) -D -m 0755 $(@D)/build/mister-runtime \
		$(TARGET_DIR)/usr/sbin/mister-runtime
EOF

# Prove the active package surface first, then pressure-test the same validator
# against realistic mutations of that accepted source.
candidate_mk=$package_mk
validate_package_semantics "$candidate_mk" "$package_config" \
  "$repo/buildroot/Config.in" "$repo/buildroot/external.mk" ||
  fail 'semantic validator rejected the intended package shape'

commented_root_config=$fixture/commented-root-Config.in
sed 's/^source /# source /' "$repo/buildroot/Config.in" >"$commented_root_config"
expect_semantic_rejected 'commented Config.in wiring' \
  "$candidate_mk" "$package_config" "$commented_root_config" \
  "$repo/buildroot/external.mk"

commented_external=$fixture/commented-external.mk
sed 's/^include /# include /' "$repo/buildroot/external.mk" >"$commented_external"
expect_semantic_rejected 'commented external.mk wiring' \
  "$candidate_mk" "$package_config" "$repo/buildroot/Config.in" \
  "$commented_external"

missing_bridge_config=$fixture/missing-bridge-Config.in
sed '/^config BR2_PACKAGE_MISTER_RUNTIME$/d' "$package_config" \
  >"$missing_bridge_config"
expect_semantic_rejected 'missing hidden Kconfig bridge' \
  "$candidate_mk" "$missing_bridge_config" "$repo/buildroot/Config.in" \
  "$repo/buildroot/external.mk"

missing_default_config=$fixture/missing-default-Config.in
sed '/default y if BR2_PACKAGE_FOGCAST_MISTER_RUNTIME/d' "$package_config" \
  >"$missing_default_config"
expect_semantic_rejected 'missing Kconfig bridge default' \
  "$candidate_mk" "$missing_default_config" "$repo/buildroot/Config.in" \
  "$repo/buildroot/external.mk"

missing_all_mk=$fixture/missing-all.mk
sed '/^[[:space:]]*all$/d' "$candidate_mk" >"$missing_all_mk"
expect_semantic_rejected 'missing production all target' \
  "$missing_all_mk" "$package_config" "$repo/buildroot/Config.in" \
  "$repo/buildroot/external.mk"

extra_target_mk=$fixture/extra-target.mk
sed 's/^[[:space:]]*all$/\t\tall test/' "$candidate_mk" >"$extra_target_mk"
expect_semantic_rejected 'extra build target' \
  "$extra_target_mk" "$package_config" "$repo/buildroot/Config.in" \
  "$repo/buildroot/external.mk"

wrong_tool_mk=$fixture/wrong-tool.mk
sed 's/CXX="$(TARGET_CXX)"/CXX="$(HOSTCXX)"/' "$candidate_mk" \
  >"$wrong_tool_mk"
expect_semantic_rejected 'host compiler substitution' \
  "$wrong_tool_mk" "$package_config" "$repo/buildroot/Config.in" \
  "$repo/buildroot/external.mk"

extra_build_command_mk=$fixture/extra-build-command.mk
awk '
  { print }
  $0 == "define MISTER_RUNTIME_BUILD_CMDS" { print "\t/bin/true" }
' "$candidate_mk" >"$extra_build_command_mk"
expect_semantic_rejected 'extra build command' \
  "$extra_build_command_mk" "$package_config" "$repo/buildroot/Config.in" \
  "$repo/buildroot/external.mk"

extra_install_mk=$fixture/extra-install.mk
awk '
  $0 == "define MISTER_RUNTIME_INSTALL_TARGET_CMDS" { install=1 }
  install && $0 == "endef" {
    print "\t$(INSTALL) -D -m 0644 $(@D)/build/libmister-runtime.a $(TARGET_DIR)/usr/lib/libmister-runtime.a"
    install=0
  }
  { print }
' "$candidate_mk" >"$extra_install_mk"
expect_semantic_rejected 'extra installed file and destination' \
  "$extra_install_mk" "$package_config" "$repo/buildroot/Config.in" \
  "$repo/buildroot/external.mk"

package_source=$fixture/package-source
mkdir -p "$package_source"
cp -R "$runtime_source/." "$package_source/"
test -f "$package_source/build/host-object.o"
fake_make=$fixture/fake-runtime-make
fake_make_log=$fixture/fake-runtime-make.log
cat >"$fake_make" <<'EOF'
#!/bin/sh
set -eu
test "$#" -eq 8
test "$1" = -C
source=$2
test "$3" = CXX=target-c++
test "$4" = AR=target-ar
test "$5" = NM=target-nm
test "$6" = CXXFILT=target-c++filt
test "$7" = "MISTER_RUNTIME_VERSION=git-$EXPECTED_RUNTIME_COMMIT"
test "$8" = all
printf '%s\n' "$*" >"$PACKAGE_FAKE_MAKE_LOG"
if [ -e "$source/build/host-object.o" ]; then
  printf '%s\n' 'ignored host build artifact reached package build' >&2
  exit 90
fi
mkdir -p "$source/build"
printf '%s\n' target-arm-daemon >"$source/build/mister-runtime"
chmod 0755 "$source/build/mister-runtime"
EOF
chmod 0755 "$fake_make"

package_target=$fixture/target
package_harness=$fixture/package-harness.mk
cat >"$package_harness" <<EOF
PACKAGE_SOURCE := $package_source
FAKE_MAKE := $fake_make
TARGET_MAKE_ENV :=
MAKE := \$(FAKE_MAKE)
TARGET_CXX := target-c++
TARGET_AR := target-ar
TARGET_NM := target-nm
TARGET_CROSS := target-
FOGCAST_MISTER_RUNTIME_COMMIT := $runtime_commit
INSTALL := /usr/bin/install
TARGET_DIR := $package_target
generic-package :=
include $package_mk

\$(PACKAGE_SOURCE)/.build-stamp:
	\$(MISTER_RUNTIME_BUILD_CMDS)

\$(PACKAGE_SOURCE)/.install-stamp:
	\$(MISTER_RUNTIME_INSTALL_TARGET_CMDS)
EOF
EXPECTED_RUNTIME_COMMIT=$runtime_commit PACKAGE_FAKE_MAKE_LOG=$fake_make_log \
  make -f "$package_harness" "$package_source/.build-stamp"
test ! -e "$package_source/build/host-object.o" || \
  fail 'ignored host build artifact survived package build preparation'
grep -Fqx -- \
  "-C $package_source CXX=target-c++ AR=target-ar NM=target-nm CXXFILT=target-c++filt MISTER_RUNTIME_VERSION=git-$runtime_commit all" \
  "$fake_make_log"
make -f "$package_harness" "$package_source/.install-stamp"
test "$(find "$package_target" -type f -print)" = \
  "$package_target/usr/sbin/mister-runtime" ||
  fail 'package installed something other than the production daemon'
test "$(cat "$package_target/usr/sbin/mister-runtime")" = target-arm-daemon

if grep -Eqi '(fake|Main_MiSTer|git clone|https?://)' "$package_mk"; then
  fail 'Buildroot package contains a fake/Main/network source path'
fi
grep -Fq 'sh scripts/tests/native-runtime-inputs_test.sh' "$repo/Makefile"

real_lock=$repo/build/native-runtime.inputs.lock.toml
grep -Fqx "commit = '960e61ece108d996eae4e09566b9fdc56c4ce952'" "$real_lock"
grep -Fqx "sha256 = '821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934'" "$real_lock"
grep -Fqx 'size = 2452588' "$real_lock"
grep -Fqx '[megadrive_rbf]' "$real_lock"
grep -Fqx "repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'" "$real_lock"
grep -Fqx "commit = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'" "$real_lock"
grep -Fqx "path = 'releases/MegaDrive_20260603.rbf'" "$real_lock"
grep -Fqx "sha256 = '0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839'" "$real_lock"
grep -Fqx 'size = 4296864' "$real_lock"
grep -Fqx "install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'" "$real_lock"

container_log=$fixture/container.log
fake_container=$fixture/fake-container
package_digest=$(tr -d '[:space:]' <"$repo/build/target-image-container-packages.sha256")
cat >"$fake_container" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$NATIVE_RUNTIME_CONTAINER_LOG"
if [ "$1 $2" = 'image inspect' ]; then
  case "$3" in
    docker.io/*@*)
      printf '%s\n' "debian@$NATIVE_RUNTIME_BASE_DIGEST"
      ;;
    fogcast-target-image-build:*)
      case "$*" in
        *org.fogcast.target-image.context-digest*)
          tag=${3#*:}
          old_ifs=$IFS
          IFS=-
          set -- $tag
          IFS=$old_ifs
          printf 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc|linux/amd64|%s|%s|%s|%s|%s\n' \
            "$NATIVE_RUNTIME_BASE_DIGEST" "$2" "$NATIVE_RUNTIME_PACKAGE_DIGEST" "$3" "$4"
          ;;
      esac
      ;;
  esac
  exit 0
fi
if [ "$1" = run ] && [ "${NATIVE_RUNTIME_REQUIRE_MOUNT:-0}" = 1 ]; then
  case " $* " in
    *" --volume $NATIVE_RUNTIME_EXPECTED_SOURCE:/runtime-source:ro "*) : ;;
    *) printf '%s\n' 'missing read-only /runtime-source mount' >&2; exit 97 ;;
  esac
  case " $* " in
    *" --env FOGCAST_MISTER_RUNTIME_COMMIT=$NATIVE_RUNTIME_EXPECTED_COMMIT "*) : ;;
    *) printf '%s\n' 'missing locked runtime version export' >&2; exit 98 ;;
  esac
fi
EOF
chmod 0755 "$fake_container"
base_digest=$(awk -F "'" '/^digest = / { print $2; exit }' \
  "$repo/build/target-image.sources.lock.toml")

NATIVE_RUNTIME_CONTAINER_LOG=$container_log \
NATIVE_RUNTIME_BASE_DIGEST=$base_digest \
NATIVE_RUNTIME_PACKAGE_DIGEST=$package_digest \
NATIVE_RUNTIME_REQUIRE_MOUNT=1 \
NATIVE_RUNTIME_EXPECTED_SOURCE=$runtime_source \
NATIVE_RUNTIME_EXPECTED_COMMIT=$runtime_commit \
NATIVE_RUNTIME_INPUT_LOCK=$lock \
NATIVE_RUNTIME_IDLE_FILE=$idle \
NATIVE_RUNTIME_MEGADRIVE_FILE=$megadrive \
LIBMISTER_RUNTIME_DIR=$runtime_source \
TARGET_IMAGE_CONTAINER_RUNTIME=$fake_container \
  sh "$container" run true
grep -Fq -- "--volume $runtime_source:/runtime-source:ro" "$container_log"
grep -Fq -- "--env FOGCAST_MISTER_RUNTIME_COMMIT=$runtime_commit" "$container_log"

: >"$container_log"
NATIVE_RUNTIME_CONTAINER_LOG=$container_log \
NATIVE_RUNTIME_BASE_DIGEST=$base_digest \
NATIVE_RUNTIME_PACKAGE_DIGEST=$package_digest \
TARGET_IMAGE_CONTAINER_RUNTIME=$fake_container \
  sh "$container" run true
if grep -Fq '/runtime-source' "$container_log"; then
  fail 'legacy container command gained a runtime-source mount'
fi
if grep -Fq 'FOGCAST_MISTER_RUNTIME_COMMIT' "$container_log"; then
  fail 'legacy container command gained a runtime version environment value'
fi

: >"$container_log"
NATIVE_RUNTIME_CONTAINER_LOG=$container_log \
NATIVE_RUNTIME_BASE_DIGEST=$base_digest \
NATIVE_RUNTIME_PACKAGE_DIGEST=$package_digest \
TARGET_IMAGE_CONTAINER_RUNTIME=$fake_container \
MEGADRIVE_RBF_SOURCE=upstream \
  sh "$container" fetch true
grep -Fq -- '--env MEGADRIVE_RBF_SOURCE=upstream' "$container_log"
if grep -Fq '/runtime-source' "$container_log"; then
  fail 'upstream fetch without runtime checkout gained a runtime-source mount'
fi

: >"$container_log"
NATIVE_RUNTIME_CONTAINER_LOG=$container_log \
NATIVE_RUNTIME_BASE_DIGEST=$base_digest \
NATIVE_RUNTIME_PACKAGE_DIGEST=$package_digest \
NATIVE_RUNTIME_REQUIRE_MOUNT=1 \
NATIVE_RUNTIME_EXPECTED_SOURCE=$runtime_source \
NATIVE_RUNTIME_EXPECTED_COMMIT=$runtime_commit \
NATIVE_RUNTIME_INPUT_LOCK=$lock \
NATIVE_RUNTIME_IDLE_FILE=$idle \
NATIVE_RUNTIME_MEGADRIVE_FILE=$megadrive \
NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE=$upstream_selection \
LIBMISTER_RUNTIME_DIR=$runtime_source \
TARGET_IMAGE_CONTAINER_RUNTIME=$fake_container \
MEGADRIVE_RBF_SOURCE=upstream \
  sh "$container" fetch true
grep -Fq -- "--volume $runtime_source:/runtime-source:ro" "$container_log"
grep -Fq -- "--env FOGCAST_MISTER_RUNTIME_COMMIT=$runtime_commit" "$container_log"
grep -Fq -- '--env MEGADRIVE_RBF_SOURCE=upstream' "$container_log"

expect_rejected 'relative runtime checkout path' \
  env NATIVE_RUNTIME_CONTAINER_LOG="$container_log" \
    NATIVE_RUNTIME_BASE_DIGEST="$base_digest" \
    NATIVE_RUNTIME_PACKAGE_DIGEST="$package_digest" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_IDLE_FILE="$idle" \
    NATIVE_RUNTIME_MEGADRIVE_FILE="$megadrive" \
    LIBMISTER_RUNTIME_DIR=relative/runtime \
    TARGET_IMAGE_CONTAINER_RUNTIME="$fake_container" \
    sh "$container" run true

printf '%s\n' dirty >"$runtime_source/untracked"
expect_rejected 'container with dirty runtime checkout' \
  env NATIVE_RUNTIME_CONTAINER_LOG="$container_log" \
    NATIVE_RUNTIME_BASE_DIGEST="$base_digest" \
    NATIVE_RUNTIME_PACKAGE_DIGEST="$package_digest" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_IDLE_FILE="$idle" \
    NATIVE_RUNTIME_MEGADRIVE_FILE="$megadrive" \
    LIBMISTER_RUNTIME_DIR="$runtime_source" \
    TARGET_IMAGE_CONTAINER_RUNTIME="$fake_container" \
    sh "$container" run true
rm "$runtime_source/untracked"

printf '%s\n' 'native runtime input tests passed'
