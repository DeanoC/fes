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
      PATH="$fixture/path:$PATH" NATIVE_RUNTIME_SYSTEMS='megadrive pong snes' \
        "$fixture/platform/scripts/native-extra-cores.sh" verify "$fixture/cache"
      [ "$(cat "$SELECTOR_LOG")" = "$(printf '%s\n%s' "$expected" "$expected")" ]
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
export NATIVE_RUNTIME_SYSTEMS='megadrive pong snes'
for system in pong snes; do
  bundle=$fixture/$system
  mkdir "$bundle"
  printf 'synthetic %s core' "$system" > "$bundle/$system.rbf"
  digest=$(sha256sum "$bundle/$system.rbf" | awk '{print $1}')
  size=$(wc -c < "$bundle/$system.rbf" | tr -d ' ')
  case "$system" in
    pong) source=https://github.com/DeanoC/misteross; revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; recipe=scripts/build_pong.py ;;
    snes) source=https://github.com/MiSTer-devel/SNES_MiSTer; revision=93d359e6f23c734ae3928984e88bed1d9b53cbac; recipe=scripts/rebuild_core.py ;;
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
export PONG_RBF_BUNDLE=$fixture/pong SNES_RBF_BUNDLE=$fixture/snes
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
for arg in 'NATIVE_RUNTIME_SYSTEMS=megadrive pong snes' 'PONG_RBF_BUNDLE=/pong-rbf-bundle' 'SNES_RBF_BUNDLE=/snes-rbf-bundle' "$fixture/pong:/pong-rbf-bundle:ro" "$fixture/snes:/snes-rbf-bundle:ro"; do
  grep -Fqx -- "$arg" "$CORE_CONTAINER_LOG"
done
TARGET_IMAGE_DEV_CONTAINER=1 TARGET_IMAGE_CONTAINER_RUNTIME="$fixture/container" "$repo/scripts/target-image-container.sh" run /work/test-verify
grep -Fqx 'NATIVE_RUNTIME_SYSTEMS=megadrive pong snes' "$CORE_CONTAINER_LOG"
"$helper" fetch "$fixture/cache"
"$helper" verify "$fixture/cache"
NATIVE_RUNTIME_SYSTEMS=megadrive TARGET_IMAGE_EXTRA_CORE_CACHE="$fixture/cache" sh "$repo/scripts/tests/target-image_test.sh"
"$helper" install "$fixture/cache" "$fixture/target"
"$helper" copy-records "$fixture/cache" "$fixture/sidecars"
"$helper" verify-image "$fixture/sidecars" "$fixture/target"
reject() { if "$@" > "$fixture/rejected" 2>&1; then echo 'invalid extra core accepted' >&2; exit 1; fi; }
reject env NATIVE_RUNTIME_SYSTEMS=megadrive "$helper" verify-image "$fixture/sidecars" "$fixture/target"
reject env NATIVE_RUNTIME_SYSTEMS='pong snes' "$helper" validate
mv "$fixture/sidecars/snes.selection.toml" "$fixture/snes.saved"
reject "$helper" verify-image "$fixture/sidecars" "$fixture/target"
mv "$fixture/snes.saved" "$fixture/sidecars/snes.selection.toml"
printf 'corrupt' > "$fixture/target/usr/share/mister-runtime/cores/pong.rbf"
reject "$helper" verify-image "$fixture/sidecars" "$fixture/target"
NATIVE_RUNTIME_SYSTEMS=megadrive "$helper" install "$fixture/cache" "$fixture/target"
NATIVE_RUNTIME_SYSTEMS=megadrive "$helper" verify-image "$fixture/sidecars" "$fixture/target"
echo 'native additional core fetch/install/verify and deselection passed'
