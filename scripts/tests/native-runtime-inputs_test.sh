#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-native-runtime-inputs.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

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

write_lock() {
  write_path=$1
  write_runtime_commit=$2
  write_idle_sha=$3
  write_idle_size=$4
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
git -C "$runtime_source" add README
git -C "$runtime_source" -c user.name=Test -c user.email=test@example.invalid \
  commit -q -m fixture
runtime_commit=$(git -C "$runtime_source" rev-parse HEAD)

idle=$fixture/idle.rbf
printf '%s' 'fixture idle rbf' >"$idle"
idle_sha=$(sha256sum "$idle" | awk '{print $1}')
idle_size=$(wc -c <"$idle" | tr -d ' ')
lock=$fixture/native-runtime.inputs.lock.toml
write_lock "$lock" "$runtime_commit" "$idle_sha" "$idle_size"

verified_output=$(
  sh "$verifier" "$lock" "$runtime_source" "$idle"
)
printf '%s\n' "$verified_output" | grep -Fq "$runtime_commit"
printf '%s\n' "$verified_output" | grep -Fq "$idle_sha"
if printf '%s\n' "$verified_output" | grep -Fq "$runtime_source"; then
  fail 'verifier printed a local runtime checkout path'
fi

printf '%s\n' second >"$runtime_source/SECOND"
git -C "$runtime_source" add SECOND
git -C "$runtime_source" -c user.name=Test -c user.email=test@example.invalid \
  commit -q -m second
expect_rejected 'wrong runtime HEAD' \
  sh "$verifier" "$lock" "$runtime_source" "$idle"
git -C "$runtime_source" reset -q --hard "$runtime_commit"

printf '%s\n' dirty >"$runtime_source/untracked"
expect_rejected 'dirty runtime checkout' \
  sh "$verifier" "$lock" "$runtime_source" "$idle"
rm "$runtime_source/untracked"

wrong_digest_lock=$fixture/wrong-digest.lock.toml
write_lock "$wrong_digest_lock" "$runtime_commit" \
  aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  "$idle_size"
expect_rejected 'wrong idle digest' \
  sh "$verifier" "$wrong_digest_lock" "$runtime_source" "$idle"

wrong_size_lock=$fixture/wrong-size.lock.toml
write_lock "$wrong_size_lock" "$runtime_commit" "$idle_sha" "$((idle_size + 1))"
expect_rejected 'wrong idle size' \
  sh "$verifier" "$wrong_size_lock" "$runtime_source" "$idle"

expect_rejected 'non-regular idle path' \
  sh "$verifier" "$lock" "$runtime_source" "$fixture"

missing_commit_lock=$fixture/missing-commit.lock.toml
sed "/^commit = '$runtime_commit'$/d" "$lock" >"$missing_commit_lock"
expect_rejected 'missing runtime commit' \
  sh "$verifier" "$missing_commit_lock" "$runtime_source" "$idle"

short_commit_lock=$fixture/short-commit.lock.toml
sed "s/$runtime_commit/abc123/" "$lock" >"$short_commit_lock"
expect_rejected 'non-40-character runtime commit' \
  sh "$verifier" "$short_commit_lock" "$runtime_source" "$idle"

relative_mount_lock=$fixture/relative-mount.lock.toml
sed "s#mount_path = '/runtime-source'#mount_path = 'runtime-source'#" \
  "$lock" >"$relative_mount_lock"
expect_rejected 'relative runtime mount path' \
  sh "$verifier" "$relative_mount_lock" "$runtime_source" "$idle"

relative_install_lock=$fixture/relative-install.lock.toml
sed "s#install_path = '/usr/share/mister-runtime/idle.rbf'#install_path = 'idle.rbf'#" \
  "$lock" >"$relative_install_lock"
expect_rejected 'relative idle install path' \
  sh "$verifier" "$relative_install_lock" "$runtime_source" "$idle"

wrong_repository_lock=$fixture/wrong-repository.lock.toml
sed 's#https://github.com/MiSTer-devel/Distribution_MiSTer#https://example.invalid/mutable#' \
  "$lock" >"$wrong_repository_lock"
expect_rejected 'wrong idle repository' \
  sh "$verifier" "$wrong_repository_lock" "$runtime_source" "$idle"

wrong_idle_commit_lock=$fixture/wrong-idle-commit.lock.toml
sed 's/f7bde4becb452ca28f604ad9802bbed5c6b58e01/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/' \
  "$lock" >"$wrong_idle_commit_lock"
expect_rejected 'wrong idle commit' \
  sh "$verifier" "$wrong_idle_commit_lock" "$runtime_source" "$idle"

wrong_idle_path_lock=$fixture/wrong-idle-path.lock.toml
sed "s/path = 'menu.rbf'/path = 'latest.rbf'/" \
  "$lock" >"$wrong_idle_path_lock"
expect_rejected 'wrong idle source path' \
  sh "$verifier" "$wrong_idle_path_lock" "$runtime_source" "$idle"

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
cp "$NATIVE_RUNTIME_FAKE_DOWNLOAD" "$output"
EOF
chmod 0755 "$fake_bin/wget"

fetch_cache=$fixture/cache
fetch_log=$fixture/fetch.log
PATH="$fake_bin:$PATH" \
NATIVE_RUNTIME_INPUT_LOCK=$lock \
NATIVE_RUNTIME_CACHE=$fetch_cache \
NATIVE_RUNTIME_FAKE_DOWNLOAD=$idle \
NATIVE_RUNTIME_FETCH_LOG=$fetch_log \
  sh "$fetcher"
cmp "$idle" "$fetch_cache/idle.rbf"
grep -Fqx \
  "https://raw.githubusercontent.com/MiSTer-devel/Distribution_MiSTer/f7bde4becb452ca28f604ad9802bbed5c6b58e01/menu.rbf" \
  "$fetch_log"

printf '%s' prior >"$fetch_cache/idle.rbf"
bad_download=$fixture/bad-download.rbf
printf '%s' altered >"$bad_download"
expect_rejected 'fetch with wrong digest' \
  env PATH="$fake_bin:$PATH" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_CACHE="$fetch_cache" \
    NATIVE_RUNTIME_FAKE_DOWNLOAD="$bad_download" \
    NATIVE_RUNTIME_FETCH_LOG="$fetch_log" \
    sh "$fetcher"
test "$(cat "$fetch_cache/idle.rbf")" = prior || \
  fail 'failed fetch replaced the prior cached artifact'

wrong_fetch_size_lock=$fixture/wrong-fetch-size.lock.toml
write_lock "$wrong_fetch_size_lock" "$runtime_commit" "$idle_sha" "$((idle_size + 1))"
expect_rejected 'fetch with wrong size' \
  env PATH="$fake_bin:$PATH" \
    NATIVE_RUNTIME_INPUT_LOCK="$wrong_fetch_size_lock" \
    NATIVE_RUNTIME_CACHE="$fetch_cache" \
    NATIVE_RUNTIME_FAKE_DOWNLOAD="$idle" \
    NATIVE_RUNTIME_FETCH_LOG="$fetch_log" \
    sh "$fetcher"
test "$(cat "$fetch_cache/idle.rbf")" = prior || \
  fail 'wrong-size fetch replaced the prior cached artifact'

grep -Fq 'MISTER_RUNTIME_SITE = /runtime-source' "$package_mk"
grep -Fq 'MISTER_RUNTIME_SITE_METHOD = local' "$package_mk"
grep -Fq 'CXX="$(TARGET_CXX)" AR="$(TARGET_AR)" NM="$(TARGET_NM)"' "$package_mk"
grep -Fq 'CXXFILT="$(TARGET_CROSS)c++filt"' "$package_mk"
grep -Fq 'MISTER_RUNTIME_VERSION="git-$(FOGCAST_MISTER_RUNTIME_COMMIT)"' "$package_mk"
grep -Fq '$(@D)/build/mister-runtime' "$package_mk"
grep -Fq '$(TARGET_DIR)/usr/sbin/mister-runtime' "$package_mk"
if grep -Eqi '(fake|Main_MiSTer|git clone|https?://)' "$package_mk"; then
  fail 'Buildroot package contains a fake/Main/network source path'
fi
test "$(grep -c '\$(INSTALL)' "$package_mk")" -eq 1 || \
  fail 'Buildroot package installs more than the production daemon'
grep -Fq 'config BR2_PACKAGE_FOGCAST_MISTER_RUNTIME' "$package_config"
grep -Fq 'package/mister-runtime/Config.in' "$repo/buildroot/Config.in"
grep -Fq 'package/*/*.mk' "$repo/buildroot/external.mk"
grep -Fq 'sh scripts/tests/native-runtime-inputs_test.sh' "$repo/Makefile"

real_lock=$repo/build/native-runtime.inputs.lock.toml
grep -Fqx "commit = '1045306bf97d5e8f68fde7626cb954194f505ff5'" "$real_lock"
grep -Fqx "sha256 = '821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934'" "$real_lock"
grep -Fqx 'size = 2452588' "$real_lock"

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

expect_rejected 'relative runtime checkout path' \
  env NATIVE_RUNTIME_CONTAINER_LOG="$container_log" \
    NATIVE_RUNTIME_BASE_DIGEST="$base_digest" \
    NATIVE_RUNTIME_PACKAGE_DIGEST="$package_digest" \
    NATIVE_RUNTIME_INPUT_LOCK="$lock" \
    NATIVE_RUNTIME_IDLE_FILE="$idle" \
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
    LIBMISTER_RUNTIME_DIR="$runtime_source" \
    TARGET_IMAGE_CONTAINER_RUNTIME="$fake_container" \
    sh "$container" run true
rm "$runtime_source/untracked"

printf '%s\n' 'native runtime input tests passed'
