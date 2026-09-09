#!/bin/sh
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d)
trap 'chmod -R u+w "$fixture"; rm -rf "$fixture"' EXIT INT TERM
# Host preflight and container verification must execute their own binary format.
mkdir -p "$fixture/platform/scripts" "$fixture/platform/bin" "$fixture/path"
cp "$repo/scripts/native-extra-cores.sh" "$fixture/platform/scripts/"
cat > "$fixture/path/uname" <<'UNAME'
#!/bin/sh
printf '%s\n' "$TEST_OS"
UNAME
for binary in target-image-lock target-image-lock-linux-amd64 override; do
  cat > "$fixture/platform/bin/$binary" <<'SELECTOR'
#!/bin/sh
printf '%s\n' "${0##*/}" >> "$SELECTOR_LOG"
SELECTOR
  chmod +x "$fixture/platform/bin/$binary"
done
chmod +x "$fixture/path/uname"
for os in Darwin Linux; do
  case "$os" in Darwin) expected=target-image-lock ;; Linux) expected=target-image-lock-linux-amd64 ;; esac
  for override in default explicit; do
    (
      unset TARGET_IMAGE_LOCK_BIN
      if [ "$override" = explicit ]; then
        export TARGET_IMAGE_LOCK_BIN=$fixture/platform/bin/override
        expected=override
      fi
      export TEST_OS=$os SELECTOR_LOG=$fixture/selector.log
      : > "$SELECTOR_LOG"
      PATH="$fixture/path:$PATH" NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes' \
        "$fixture/platform/scripts/native-extra-cores.sh" verify "$fixture/cache"
      [ "$(cat "$SELECTOR_LOG")" = "$(printf '%s\n%s\n%s' "$expected" "$expected" "$expected")" ]
    )
  done
done
# The macOS entry point must build its host selector before preflight.
for os in Darwin Linux; do
  TEST_OS=$os PATH="$fixture/path:$PATH" make -s -C "$repo" -n target-image-native-fetch > "$fixture/make-$os"
  grep -q 'GOOS=linux GOARCH=amd64.*target-image-lock-linux-amd64' "$fixture/make-$os"
  if [ "$os" = Darwin ]; then
    grep -q 'GOOS=darwin GOARCH=arm64.*target-image-lock' "$fixture/make-$os"
  elif grep -q 'GOOS=darwin GOARCH=arm64.*target-image-lock' "$fixture/make-$os"; then
    echo 'Linux native fetch unexpectedly builds a Darwin selector' >&2
    exit 1
  fi
done
selector=$fixture/selector
(cd "$repo" && go build -o "$selector" ./cmd/target-image-lock)
export TARGET_IMAGE_LOCK_BIN=$selector
helper=$repo/scripts/native-extra-cores.sh
reject() { if "$@" > "$fixture/rejected" 2>&1; then echo 'invalid extra core accepted' >&2; exit 1; fi; }
export NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes'
for system in pong snes nes; do
  bundle=$fixture/$system
  mkdir "$bundle"
  printf 'synthetic %s core' "$system" > "$bundle/$system.rbf"
  digest=$(sha256sum "$bundle/$system.rbf" | awk '{print $1}')
  size=$(wc -c < "$bundle/$system.rbf" | tr -d ' ')
  case "$system" in
    pong) source=https://github.com/DeanoC/misteross; revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; recipe=scripts/build_pong.py ;;
    snes) source=https://github.com/MiSTer-devel/SNES_MiSTer; revision=93d359e6f23c734ae3928984e88bed1d9b53cbac; recipe=scripts/rebuild_core.py ;;
    nes) source=https://github.com/MiSTer-devel/NES_MiSTer; revision=9a63821173b6da4d6e95dcbe2e2a322ec8171144; recipe=scripts/rebuild_core.py ;;
  esac
  cat > "$bundle/$system-rbf.toml" <<MANIFEST
format = 1
abi = 'mister'
system = '$system'
artifact = '$system.rbf'
sha256 = '$digest'
size = $size
repository = '$source'
revision = '$revision'
recipe = '$recipe'
recipe_sha256 = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
toolchain = 'Quartus 17.0.2 Lite'
MANIFEST
  chmod 0444 "$bundle"/*
  chmod 0555 "$bundle"
done
export PONG_RBF_BUNDLE=$fixture/pong SNES_RBF_BUNDLE=$fixture/snes NES_RBF_BUNDLE=$fixture/nes
package_id=b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0
package=$fixture/fes-pong-package
package_selection=$fixture/fes-pong.package-selection.toml
mkdir "$package"
cp "$repo/internal/corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml" "$package/manifest.toml"
cp "$repo/internal/corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf" "$package/core.rbf"
chmod 0444 "$package"/*
chmod 0555 "$package"
cat > "$package_selection" <<SELECTION
format = 2
kind = 'core-package'
core_id = 'fes.pong'
package_id = '$package_id'
payload_sha256 = 'e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1'
misteross_revision = '1111111111111111111111111111111111111111'
mister_packages_revision = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
install_path = '/usr/share/mister-runtime/core-packages/$package_id'
SELECTION
chmod 0444 "$package_selection"
export FES_PONG_PACKAGE_DIR=$package FES_PONG_PACKAGE_SELECTION=$package_selection
cat > "$fixture/container" <<'CONTAINER'
#!/bin/sh
case "$1" in
 image) exit 0 ;;
 run) printf '%s\n' "$@" > "$CORE_CONTAINER_LOG" ;;
 *) exit 1 ;;
esac
CONTAINER
chmod +x "$fixture/container"
export CORE_CONTAINER_LOG=$fixture/container.log
TARGET_IMAGE_DEV_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/container" "$repo/scripts/target-image-container.sh" fetch /work/test-fetch
for arg in 'NATIVE_RUNTIME_SYSTEMS=megadrive pong snes nes' 'PONG_RBF_BUNDLE=/pong-rbf-bundle' 'SNES_RBF_BUNDLE=/snes-rbf-bundle' 'NES_RBF_BUNDLE=/nes-rbf-bundle' 'FES_PONG_PACKAGE_DIR=/fes-pong-package' 'FES_PONG_PACKAGE_SELECTION=/fes-pong-package-selection.toml' "$fixture/pong:/pong-rbf-bundle:ro" "$fixture/snes:/snes-rbf-bundle:ro" "$fixture/nes:/nes-rbf-bundle:ro" "$package:/fes-pong-package:ro" "$package_selection:/fes-pong-package-selection.toml:ro"; do
  grep -Fqx -- "$arg" "$CORE_CONTAINER_LOG"
done
TARGET_IMAGE_DEV_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/container" "$repo/scripts/target-image-container.sh" run /work/test-verify
grep -Fqx 'NATIVE_RUNTIME_SYSTEMS=megadrive pong snes nes' "$CORE_CONTAINER_LOG"
grep -Fqx 'FES_PONG_PACKAGE_DIR=/fes-pong-package' "$CORE_CONTAINER_LOG"
grep -Fqx 'FES_PONG_PACKAGE_SELECTION=/fes-pong-package-selection.toml' "$CORE_CONTAINER_LOG"
"$helper" fetch "$fixture/cache"
"$helper" verify "$fixture/cache"
bad_package_selection=$fixture/bad-fes-pong.package-selection.toml
sed 's/e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1/ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff/' \
  "$package_selection" > "$bad_package_selection"
chmod 0444 "$bad_package_selection"
reject env FES_PONG_PACKAGE_DIR="$package" FES_PONG_PACKAGE_SELECTION="$bad_package_selection" \
  "$helper" fetch "$fixture/cache"
"$helper" verify "$fixture/cache"
touch "$fixture/cache/core-packages/.stale"
reject "$helper" verify "$fixture/cache"
rm "$fixture/cache/core-packages/.stale"
[ "$($helper count)" = 6 ]
NATIVE_RUNTIME_SYSTEMS=megadrive TARGET_IMAGE_EXTRA_CORE_CACHE="$fixture/cache" sh "$repo/scripts/tests/target-image_test.sh"
NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes' TARGET_IMAGE_EXTRA_CORE_CACHE="$fixture/cache" sh "$repo/scripts/tests/target-image_test.sh"
"$helper" install "$fixture/cache" "$fixture/target"
"$helper" copy-records "$fixture/cache" "$fixture/sidecars"
"$helper" verify-image "$fixture/sidecars" "$fixture/target"
mkdir "$fixture/target/usr/share/mister-runtime/core-packages/.stale"
reject "$helper" verify-image "$fixture/sidecars" "$fixture/target"
rmdir "$fixture/target/usr/share/mister-runtime/core-packages/.stale"
"$helper" build-inputs "$fixture/sidecars" "$fixture/target" > "$fixture/package-build-inputs"
test -f "$fixture/target/usr/share/mister-runtime/core-packages/$package_id/manifest.toml"
test -f "$fixture/target/usr/share/mister-runtime/core-packages/$package_id/core.rbf"
test "$(stat -c %a "$fixture/target/usr/share/mister-runtime/core-packages/$package_id")" = 555
cmp "$package_selection" "$fixture/sidecars/fes-pong.package-selection.toml"
grep -Fqx "fes_pong_package_selection_sha256=$(sha256sum "$package_selection" | awk '{print $1}')" "$fixture/package-build-inputs"
grep -Fqx "fes_pong_package_id=$package_id" "$fixture/package-build-inputs"
grep -Fqx 'fes_pong_payload_sha256=e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1' "$fixture/package-build-inputs"
grep -Fqx 'fes_pong_misteross_revision=1111111111111111111111111111111111111111' "$fixture/package-build-inputs"
grep -Fqx 'fes_pong_mister_packages_revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "$fixture/package-build-inputs"
grep -Fqx "fes_pong_install_path=/usr/share/mister-runtime/core-packages/$package_id" "$fixture/package-build-inputs"
test "$(wc -l < "$fixture/package-build-inputs" | tr -d ' ')" -eq 6
(
  unset FES_PONG_PACKAGE_SELECTION
  reject "$helper" validate
)
reject env NATIVE_RUNTIME_SYSTEMS=megadrive "$helper" verify-image "$fixture/sidecars" "$fixture/target"
reject env NATIVE_RUNTIME_SYSTEMS='pong snes' "$helper" validate
mv "$fixture/sidecars/snes.selection.toml" "$fixture/snes.saved"
reject "$helper" verify-image "$fixture/sidecars" "$fixture/target"
mv "$fixture/snes.saved" "$fixture/sidecars/snes.selection.toml"
printf 'corrupt' > "$fixture/target/usr/share/mister-runtime/cores/pong.rbf"
reject "$helper" verify-image "$fixture/sidecars" "$fixture/target"
NATIVE_RUNTIME_SYSTEMS=megadrive "$helper" install "$fixture/cache" "$fixture/target"
NATIVE_RUNTIME_SYSTEMS=megadrive "$helper" verify-image "$fixture/sidecars" "$fixture/target"
[ "$(NATIVE_RUNTIME_SYSTEMS=megadrive "$helper" count)" = 3 ]
unset FES_PONG_PACKAGE_DIR FES_PONG_PACKAGE_SELECTION
"$helper" fetch "$fixture/cache"
"$helper" install "$fixture/cache" "$fixture/target"
"$helper" verify-image "$fixture/sidecars" "$fixture/target"
[ "$($helper count)" = 5 ]
echo 'native additional core fetch/install/verify and deselection passed'
