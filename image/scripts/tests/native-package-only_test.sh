#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "/tmp/fogcast-native-package-only.XXXXXX")
trap 'chmod -R u+rwX "$fixture" 2>/dev/null || :; rm -rf "$fixture"' EXIT INT TERM
fogcast_make=$fixture/fogcast-make
mkdir "$fogcast_make"
printf '%s\n' 'build-target-image-lock-container:' >"$fogcast_make/Makefile"

selector=$fixture/target-image-lock
cat >"$selector" <<'SELECTOR'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$SELECTOR_LOG"
command=$1
shift
package=
selection=
cache=
output=
print_inputs=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --package|--selection|--cache|--output)
      key=$1
      value=$2
      shift 2
      case "$key" in
        --package) package=$value ;;
        --selection) selection=$value ;;
        --cache) cache=$value ;;
        --output) output=$value ;;
      esac
      ;;
    --print-inputs) print_inputs=1; shift ;;
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
    core_id=$(read_value core_id)
    destination=$cache/core-packages/$package_id
    mkdir -p "$destination"
    cp "$package/manifest.toml" "$destination/manifest.toml"
    cp "$package/core.rbf" "$destination/core.rbf"
    if [ -f "$package/rom-map.json" ]; then
      cp "$package/rom-map.json" "$destination/rom-map.json"
      chmod 0444 "$destination/rom-map.json"
    fi
    chmod 0444 "$destination/manifest.toml" "$destination/core.rbf"
    chmod 0555 "$destination"
    cp "$selection" "$output"
    chmod 0444 "$output"
    printf 'selected %s\n' "$core_id" >>"$SELECTOR_LOG"
    ;;
  verify-package)
    package_id=$(read_value package_id)
    core_id=$(read_value core_id)
    test "$(basename "$package")" = "$package_id"
    grep -Fqx "core_id = '$core_id'" "$package/manifest.toml"
    grep -Fqx "package_id = '$package_id'" "$selection"
    if [ "$print_inputs" -eq 1 ]; then
      printf '%s_package_id=%s\n' "$core_id" "$package_id"
    else
      printf 'verify-package %s verified\n' "$core_id"
    fi
    ;;
  *)
    printf 'unsupported selector command: %s\n' "$command" >&2
    exit 2
    ;;
esac
SELECTOR
chmod +x "$selector"

package_ids='fes.pong,fes.zx81,fes.coleco'
package_words='fes.pong fes.zx81 fes.coleco'
package_id_for() {
  case "$1" in
    fes.pong) package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
    fes.zx81) package_id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
    fes.coleco) package_id=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc ;;
    *) return 1 ;;
  esac
}
for core_id in $package_words; do
  core=$(printf '%s' "$core_id" | sed 's/^fes\.//')
  package_id_for "$core_id"
  package=$fixture/$core-package
  mkdir "$package"
  printf "core_id = '%s'\n" "$core_id" >"$package/manifest.toml"
  printf '%s payload\n' "$core" >"$package/core.rbf"
  chmod 0444 "$package/manifest.toml" "$package/core.rbf"
  if [ "$core_id" = fes.zx81 ]; then
    printf 'sealed map fixture\n' >"$package/rom-map.json"
    chmod 0444 "$package/rom-map.json"
  fi
  chmod 0555 "$package"
  selection=$fixture/$core.package-selection.toml
  cat >"$selection" <<EOF
format = 2
kind = 'core-package'
core_id = '$core_id'
package_id = '$package_id'
EOF
  chmod 0444 "$selection"
  case "$core_id" in
    fes.pong)
      export FES_PONG_PACKAGE_DIR=$package FES_PONG_PACKAGE_SELECTION=$selection ;;
    fes.zx81)
      export FES_ZX81_PACKAGE_DIR=$package FES_ZX81_PACKAGE_SELECTION=$selection ;;
    fes.coleco)
      export FES_COLECO_PACKAGE_DIR=$package FES_COLECO_PACKAGE_SELECTION=$selection ;;
  esac
done
cache=$fixture/cache
mkdir "$cache"

export TARGET_IMAGE_LOCK_BIN=$selector SELECTOR_LOG=$fixture/selector.log
export NATIVE_RUNTIME_MODE=package-only FES_PACKAGE_IDS=$package_ids

test "$("$repo/scripts/native-extra-cores.sh" count)" = 4
"$repo/scripts/native-extra-cores.sh" fetch "$cache"
for core_id in $package_words; do
  core=$(printf '%s' "$core_id" | sed 's/^fes\.//')
  package_id_for "$core_id"
  test -d "$cache/core-packages/$package_id"
  test -f "$cache/fes-$core.package-selection.toml"
done
for core_id in $package_words; do
  grep -Fq -- "select-package --core-id $core_id" "$SELECTOR_LOG"
  grep -Fq -- "verify-package --core-id $core_id" "$SELECTOR_LOG"
done

target=$fixture/target
mkdir -p "$target/usr/share/mister-runtime/core-packages" \
  "$target/usr/share/mister-runtime/selections"
"$repo/scripts/native-extra-cores.sh" install "$cache" "$target"
test "$(find "$target/usr/share/mister-runtime/core-packages" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" = 3
test "$(find "$target/usr/share/mister-runtime/selections" -maxdepth 1 -type f -name '*.package.toml' | wc -l | tr -d ' ')" = 3
for core_id in $package_words; do
  core=$(printf '%s' "$core_id" | sed 's/^fes\.//')
  package_id_for "$core_id"
  installed=$target/usr/share/mister-runtime/core-packages/$package_id
  test "$(stat -c %a "$installed")" = 555
  test "$(stat -c %a "$installed/manifest.toml")" = 444
  test "$(stat -c %a "$installed/core.rbf")" = 444
  if [ "$core_id" = fes.zx81 ]; then
    cmp "$FES_ZX81_PACKAGE_DIR/rom-map.json" "$installed/rom-map.json"
    test "$(stat -c %a "$installed/rom-map.json")" = 444
  fi
  test "$(stat -c %a "$target/usr/share/mister-runtime/selections/fes-$core.package.toml")" = 444
done
"$repo/scripts/native-extra-cores.sh" verify-image "$cache" "$target"

build_inputs=$fixture/build-inputs
"$repo/scripts/native-extra-cores.sh" build-inputs "$cache" "$target" >"$build_inputs"
test "$(awk -F= 'NR == 1 { print $1 }' "$build_inputs")" = fes.pong_package_id
test "$(awk -F= 'NR == 2 { print $1 }' "$build_inputs")" = fes.zx81_package_id
test "$(awk -F= 'NR == 3 { print $1 }' "$build_inputs")" = fes.coleco_package_id
expected_build_inputs=$fixture/build-inputs.expected
cat >"$expected_build_inputs" <<EOF
fes.pong_package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
fes.zx81_package_id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
fes.coleco_package_id=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
EOF
cmp "$expected_build_inputs" "$build_inputs"

records=$fixture/records
"$repo/scripts/native-extra-cores.sh" copy-records "$cache" "$records"
for core in pong zx81 coleco; do
  test "$(stat -c %a "$records/fes-$core.package-selection.toml")" = 444
done

chmod -R u+rwX "$target/usr/share/mister-runtime/core-packages"
rm -rf "$target/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
if "$repo/scripts/native-extra-cores.sh" verify-image "$cache" "$target"; then
  echo 'package-only verifier accepted a missing selected package' >&2
  exit 1
fi
"$repo/scripts/native-extra-cores.sh" install "$cache" "$target"

mkdir "$target/usr/share/mister-runtime/core-packages/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
cp "$cache/core-packages/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/manifest.toml" \
  "$target/usr/share/mister-runtime/core-packages/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd/manifest.toml"
cp "$cache/core-packages/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/core.rbf" \
  "$target/usr/share/mister-runtime/core-packages/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd/core.rbf"
chmod 0444 "$target/usr/share/mister-runtime/core-packages/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"/*
chmod 0555 "$target/usr/share/mister-runtime/core-packages/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
if "$repo/scripts/native-extra-cores.sh" verify-image "$cache" "$target"; then
  echo 'package-only verifier accepted an extra package' >&2
  exit 1
fi
"$repo/scripts/native-extra-cores.sh" install "$cache" "$target"

chmod -R u+rwX "$target/usr/share/mister-runtime/core-packages"
rm -rf "$target/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
mkdir "$target/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
cp "$cache/core-packages/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/manifest.toml" \
  "$target/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/manifest.toml"
cp "$cache/core-packages/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/core.rbf" \
  "$target/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/core.rbf"
chmod 0444 "$target/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"/*
chmod 0555 "$target/usr/share/mister-runtime/core-packages/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
if "$repo/scripts/native-extra-cores.sh" verify-image "$cache" "$target"; then
  echo 'package-only verifier accepted a misidentified package' >&2
  exit 1
fi
"$repo/scripts/native-extra-cores.sh" install "$cache" "$target"

if FES_PACKAGE_IDS='fes.pong,fes.pong' "$repo/scripts/native-extra-cores.sh" validate; then
  echo 'package-only validator accepted duplicate package IDs' >&2
  exit 1
fi
if FES_PACKAGE_IDS='fes.pong,fes.unknown' "$repo/scripts/native-extra-cores.sh" validate; then
  echo 'package-only validator accepted an unknown package ID' >&2
  exit 1
fi
if FES_PACKAGE_IDS='fes.pong,,fes.coleco' "$repo/scripts/native-extra-cores.sh" validate; then
  echo 'package-only validator accepted an empty package ID' >&2
  exit 1
fi
if env -u FES_ZX81_PACKAGE_DIR -u FES_ZX81_PACKAGE_SELECTION \
  FES_PACKAGE_IDS="$package_ids" "$repo/scripts/native-extra-cores.sh" validate; then
  echo 'package-only validator accepted missing package inputs' >&2
  exit 1
fi
if FES_PACKAGE_IDS='fes.pong,fes.zx81' "$repo/scripts/native-extra-cores.sh" validate; then
  echo 'package-only validator accepted extra package inputs' >&2
  exit 1
fi

chmod 0644 "$fixture/zx81.package-selection.toml"
sed -i "s/core_id = 'fes.zx81'/core_id = 'fes.pong'/" "$fixture/zx81.package-selection.toml"
chmod 0444 "$fixture/zx81.package-selection.toml"
if "$repo/scripts/native-extra-cores.sh" validate; then
  echo 'package-only validator accepted a misidentified selection' >&2
  exit 1
fi
chmod 0644 "$fixture/zx81.package-selection.toml"
sed -i "s/core_id = 'fes.pong'/core_id = 'fes.zx81'/" "$fixture/zx81.package-selection.toml"
chmod 0444 "$fixture/zx81.package-selection.toml"

container=$fixture/container
export IMAGE_TEST_REPO=$repo
cat >"$container" <<'CONTAINER'
#!/bin/sh
set -eu
printf '%s\n' "$@" >>"$CONTAINER_LOG"
sh "$IMAGE_TEST_REPO/scripts/tests/fake-image-container.sh" "$@"
case "$1" in
  image|build) exit 0 ;;
  run) exit 0 ;;
  *) exit 1 ;;
esac
CONTAINER
chmod +x "$container"
export CONTAINER_LOG=$fixture/container.log
TARGET_IMAGE_CONTAINER_RUNTIME="$container" \
  sh "$repo/scripts/target-image-container.sh" fetch /work/test-fetch
grep -Fqx -- 'FES_PACKAGE_IDS=fes.pong,fes.zx81,fes.coleco' "$CONTAINER_LOG"
previous_line=0
for core in pong zx81 coleco; do
  grep -Fqx -- "$fixture/$core-package:/fes-$core-package:ro" "$CONTAINER_LOG"
  grep -Fqx -- "$fixture/$core.package-selection.toml:/fes-$core-package-selection.toml:ro" "$CONTAINER_LOG"
  grep -Fqx -- "FES_$(printf '%s' "$core" | tr '[:lower:]' '[:upper:]')_PACKAGE_DIR=/fes-$core-package" "$CONTAINER_LOG"
  grep -Fqx -- "FES_$(printf '%s' "$core" | tr '[:lower:]' '[:upper:]')_PACKAGE_SELECTION=/fes-$core-package-selection.toml" "$CONTAINER_LOG"
  package_line=$(grep -nF -- "$fixture/$core-package:/fes-$core-package:ro" "$CONTAINER_LOG" | cut -d: -f1)
  test "$package_line" -gt "$previous_line"
  previous_line=$package_line
done

grep -Fq 'FES_PACKAGE_IDS' "$repo/Makefile"
for variable in FES_PONG_PACKAGE_DIR FES_PONG_PACKAGE_SELECTION \
  FES_ZX81_PACKAGE_DIR FES_ZX81_PACKAGE_SELECTION \
  FES_COLECO_PACKAGE_DIR FES_COLECO_PACKAGE_SELECTION; do
  grep -Fq "$variable" "$repo/Makefile"
done
make -s -C "$repo" -n NATIVE_RUNTIME_MODE=package-only \
  FES_PACKAGE_IDS="$package_ids" \
  LIBMISTER_RUNTIME_DIR="$fixture/runtime" \
  FES_PONG_PACKAGE_DIR="$fixture/pong-package" \
  FES_PONG_PACKAGE_SELECTION="$fixture/pong.package-selection.toml" \
  FES_ZX81_PACKAGE_DIR="$fixture/zx81-package" \
  FES_ZX81_PACKAGE_SELECTION="$fixture/zx81.package-selection.toml" \
  FES_COLECO_PACKAGE_DIR="$fixture/coleco-package" \
  FES_COLECO_PACKAGE_SELECTION="$fixture/coleco.package-selection.toml" \
  FOGCAST_DIR="$fogcast_make" target-image-native-fetch >"$fixture/make.log"
grep -Fq 'LIBMISTER_RUNTIME_DIR= ' "$fixture/make.log"
grep -Fq "LIBMISTER_RUNTIME_DIR=\"$fixture/runtime\"" "$fixture/make.log"
grep -Fq 'NATIVE_RUNTIME_MODE="package-only"' "$fixture/make.log"
if grep -Eq 'MEGADRIVE_RBF_|PONG_RBF_BUNDLE|SNES_RBF_BUNDLE|NES_RBF_BUNDLE|NATIVE_RUNTIME_SYSTEMS|fetch-core|rebuild-core|export-core-bundle|megadrive\.selection\.toml' "$fixture/make.log"; then
  printf '%s\n' 'package-only make graph still exposes format-1 inputs' >&2
  exit 1
fi
make -s -C "$repo" -n NATIVE_RUNTIME_MODE=package-only \
  FES_PACKAGE_IDS="$package_ids" \
  FOGCAST_DIR="$fogcast_make" target-image-native-verify >"$fixture/verify.log"
if grep -Eq 'MEGADRIVE_RBF_|PONG_RBF_BUNDLE|SNES_RBF_BUNDLE|NES_RBF_BUNDLE|NATIVE_RUNTIME_SYSTEMS|megadrive\.selection\.toml' "$fixture/verify.log"; then
  printf '%s\n' 'package-only verify graph still exposes format-1 inputs' >&2
  exit 1
fi
