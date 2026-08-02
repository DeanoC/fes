#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-image.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

grep -Fq 'export E2FSPROGS_FAKE_TIME=$inside_epoch' \
  "$repo/scripts/build-poc1b-image.sh"
grep -Fq '/bin/rm -rf "$inside_output"' \
  "$repo/scripts/build-poc1b-image.sh"
grep -Fq 'image=$(readlink -f "$image")' \
  "$repo/scripts/verify-poc1b-image.sh"
grep -Fq -- '-d GCC_PLUGINS' \
  "$repo/scripts/qemu-smoke-poc1b.sh"
grep -Fq -- '-e BLK_DEV_LOOP' \
  "$repo/scripts/qemu-smoke-poc1b.sh"
if grep -Fq 'readonly=on' "$repo/scripts/qemu-smoke-poc1b.sh"; then
  echo 'QEMU smoke config uses unsupported read-only SD backing' >&2
  exit 1
fi

fake_build=$fixture/fake-build
cat > "$fake_build" <<'EOF'
#!/bin/sh
set -eu
variant=$1
output=$2
epoch=$3
printf '%s|%s|%s\n' "$variant" "$output" "$epoch" >> "$POC1B_BUILD_LOG"
mkdir -p "$output/images"
payload="image-$variant"
case "$output:${POC1B_FAKE_DIFFER:-0}" in
  *work-2*:1) payload="$payload-different" ;;
esac
printf '%s\n' "$payload" > "$output/images/rootfs.ext4"
EOF
chmod 0755 "$fake_build"

build_log=$fixture/build.log
output_root=$fixture/output
POC1B_TEST_MODE=1 \
POC1B_BUILD_ONCE=$fake_build \
POC1B_BUILD_LOG=$build_log \
POC1B_OUTPUT_ROOT=$output_root \
  sh "$repo/scripts/build-poc1b-image.sh" prod

test -f "$output_root/prod/linux.img"
test "$(wc -l < "$build_log" | tr -d ' ')" -eq 2
grep -Fq "prod|$output_root/work-1-prod|1751459412" "$build_log"
grep -Fq "prod|$output_root/work-2-prod|1751459412" "$build_log"

build_count=$(wc -l < "$build_log" | tr -d ' ')
printf '%s\n' prior > "$output_root/prod/linux.img"
POC1B_TEST_MODE=1 \
POC1B_OUTPUT_ROOT=$output_root \
  sh "$repo/scripts/build-poc1b-image.sh" --promote-existing prod
test "$(cat "$output_root/prod/linux.img")" = image-prod
test "$(wc -l < "$build_log" | tr -d ' ')" -eq "$build_count"

printf '%s\n' prior > "$output_root/prod/linux.img"
if POC1B_TEST_MODE=1 \
  POC1B_BUILD_ONCE=$fake_build \
  POC1B_BUILD_LOG=$build_log \
  POC1B_FAKE_DIFFER=1 \
  POC1B_OUTPUT_ROOT=$output_root \
    sh "$repo/scripts/build-poc1b-image.sh" prod >/dev/null 2>&1; then
  echo 'image build accepted non-reproducible output' >&2
  exit 1
fi
test "$(cat "$output_root/prod/linux.img")" = prior

fake_bin=$fixture/fake-bin
mkdir -p "$fake_bin"
cat > "$fake_bin/file" <<'EOF'
#!/bin/sh
case "$*" in
  *mister-agent*) printf '%s\n' 'ELF 32-bit LSB executable, ARM, EABI5 version 1 (SYSV), statically linked, stripped' ;;
  *) /usr/bin/file "$@" ;;
esac
EOF
cat > "$fake_bin/readelf" <<'EOF'
#!/bin/sh
printf '%s\n' '  Class:                             ELF32' '  Machine:                           ARM'
EOF
chmod 0755 "$fake_bin/file" "$fake_bin/readelf"

make_root() {
  root=$1
  variant=$2
  mkdir -p "$root/sbin" "$root/usr/bin" "$root/usr/sbin" "$root/etc/init.d" "$root/lib" "$root/run" "$root/tmp" "$root/var/log"
  : > "$root/sbin/init"
  : > "$root/usr/bin/busybox"
  : > "$root/usr/sbin/mister-agent"
  chmod 0755 "$root/sbin/init" "$root/usr/bin/busybox" "$root/usr/sbin/mister-agent"
  for service in S20mister-network S40mister-main S50mister-agent S49poc1b-smoke; do
    : > "$root/etc/init.d/$service"
    chmod 0755 "$root/etc/init.d/$service"
  done
  cat > "$root/etc/fstab" <<'EOF'
/dev/root / ext4 ro,noatime,noauto 0 1
tmpfs /run tmpfs nosuid,nodev,mode=0755 0 0
tmpfs /tmp tmpfs nosuid,nodev,mode=1777 0 0
tmpfs /var/log tmpfs nosuid,nodev,noexec,mode=0755 0 0
EOF
  awk '
    /^\[\[libraries\]\]$/ { in_library=1; next }
    /^\[/ { in_library=0 }
    in_library && /^path = / {
      value=$0
      sub(/^[^=]*=[[:space:]]*"/, "", value)
      sub(/"[[:space:]]*$/, "", value)
      print value
    }
  ' "$repo/build/sources.poc1a.lock.toml" | while IFS= read -r library; do
    mkdir -p "$root$(dirname "$library")"
    : > "$root$library"
  done
  if [ "$variant" = dev ]; then
    : > "$root/usr/sbin/dropbear"
    chmod 0755 "$root/usr/sbin/dropbear"
  fi
}

verify_fixture() {
  verify_variant=$1
  verify_root=$2
  verify_manifest=$3
  PATH="$fake_bin:$PATH" POC1B_TEST_MODE=1 \
    sh "$repo/scripts/verify-poc1b-image.sh" --root-fixture "$verify_variant" "$verify_root" "$verify_manifest"
}

prod_root=$fixture/prod-root
dev_root=$fixture/dev-root
make_root "$prod_root" prod
make_root "$dev_root" dev
verify_fixture prod "$prod_root" "$fixture/prod.manifest"
verify_fixture dev "$dev_root" "$fixture/dev.manifest"
LC_ALL=C sort -c "$fixture/prod.manifest"

missing_root=$fixture/missing-root
cp -R "$prod_root" "$missing_root"
rm "$missing_root/lib/libz.so.1"
if verify_fixture prod "$missing_root" "$fixture/missing.manifest" >/dev/null 2>&1; then
  echo 'image verifier accepted a missing closure library' >&2
  exit 1
fi

prod_dropbear=$fixture/prod-dropbear
cp -R "$prod_root" "$prod_dropbear"
: > "$prod_dropbear/usr/sbin/dropbear"
chmod 0755 "$prod_dropbear/usr/sbin/dropbear"
if verify_fixture prod "$prod_dropbear" "$fixture/prod-dropbear.manifest" >/dev/null 2>&1; then
  echo 'production verifier accepted Dropbear' >&2
  exit 1
fi

dev_no_dropbear=$fixture/dev-no-dropbear
cp -R "$prod_root" "$dev_no_dropbear"
if verify_fixture dev "$dev_no_dropbear" "$fixture/dev-no-dropbear.manifest" >/dev/null 2>&1; then
  echo 'development verifier accepted a missing Dropbear server' >&2
  exit 1
fi

rw_root=$fixture/rw-root
cp -R "$prod_root" "$rw_root"
sed 's/ro,noatime/rw,noatime/' "$rw_root/etc/fstab" > "$rw_root/etc/fstab.new"
mv "$rw_root/etc/fstab.new" "$rw_root/etc/fstab"
if verify_fixture prod "$rw_root" "$fixture/rw.manifest" >/dev/null 2>&1; then
  echo 'image verifier accepted a writable root policy' >&2
  exit 1
fi

symlink_mount_root=$fixture/symlink-mount-root
cp -R "$prod_root" "$symlink_mount_root"
rm -rf "$symlink_mount_root/var/log"
ln -s ../tmp "$symlink_mount_root/var/log"
if verify_fixture prod "$symlink_mount_root" "$fixture/symlink-mount.manifest" >/dev/null 2>&1; then
  echo 'image verifier accepted a symlinked volatile mount point' >&2
  exit 1
fi

token_root=$fixture/token-root
cp -R "$prod_root" "$token_root"
printf '%s\n' 'token = "must-not-ship"' > "$token_root/etc/secret.conf"
if verify_fixture prod "$token_root" "$fixture/token.manifest" >/dev/null 2>&1; then
  echo 'image verifier accepted a token assignment' >&2
  exit 1
fi

rom_root=$fixture/rom-root
cp -R "$prod_root" "$rom_root"
: > "$rom_root/game.sfc"
if verify_fixture prod "$rom_root" "$fixture/rom.manifest" >/dev/null 2>&1; then
  echo 'image verifier accepted a ROM payload' >&2
  exit 1
fi

gdb_root=$fixture/gdb-root
cp -R "$prod_root" "$gdb_root"
mkdir -p "$gdb_root/usr/lib"
: > "$gdb_root/usr/lib/libstdc++.so.6.0.28-gdb.py"
if verify_fixture prod "$gdb_root" "$fixture/gdb.manifest" >/dev/null 2>&1; then
  echo 'image verifier accepted a path-bearing GDB auto-load helper' >&2
  exit 1
fi

smoke_log=$fixture/qemu-smoke.log
cat > "$smoke_log" <<'EOF'
POC1B_SMOKE_ROOT options=relatime,ro,data=ordered
POC1B_SMOKE_VOLATILE /run /tmp /var/log writable tmpfs
POC1B_SMOKE_READY
EOF
POC1B_TEST_MODE=1 sh "$repo/scripts/qemu-smoke-poc1b.sh" --verify-log prod "$smoke_log"

sed 's/relatime,ro,data/relatime,rw,road/' "$smoke_log" > "$smoke_log.bad"
if POC1B_TEST_MODE=1 \
  sh "$repo/scripts/qemu-smoke-poc1b.sh" --verify-log prod "$smoke_log.bad" >/dev/null 2>&1; then
  echo 'QEMU smoke verifier accepted a writable root option list' >&2
  exit 1
fi
