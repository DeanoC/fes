#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc2-install.XXXXXX")
test_pids=
cleanup() {
  for cleanup_pid in $test_pids; do
    kill "$cleanup_pid" 2>/dev/null || true
    wait "$cleanup_pid" 2>/dev/null || true
  done
  rm -rf "$fixture"
}
trap cleanup EXIT INT TERM

installer=$repo/deploy/poc2/install-target.sh
wrapper=$repo/scripts/install-poc2.sh
test -x "$installer"
test -x "$wrapper"

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

write_file() {
  write_path=$1
  write_value=$2
  mkdir -p "$(dirname "$write_path")"
  printf '%s\n' "$write_value" > "$write_path"
}

baseline=$fixture/baseline
fat=$baseline/media/fat
write_file "$fat/MiSTer" main-mister
write_file "$fat/menu.rbf" menu
write_file "$fat/linux/zImage_dtb" reproduced-kernel
write_file "$fat/linux/linux.img" poc1b-dev-root
write_file "$fat/_Console/MegaDrive_20260603.rbf" megadrive
write_file "$fat/_Console/SNES_20260603.rbf" snes
write_file "$fat/config/inputs/input_081f_e401_v3.map" controller
write_file "$fat/fogcast/cache/snes/retained.bin" retained-cache
write_file "$fat/linux/modules.poc1b/5.15.1-MiSTer/modules.tar.gz" retained-modules
write_file "$fat/games/SNES/retained.sfc" retained-rom
write_file "$fat/saves/retained.sav" retained-save
mkdir -p "$baseline/run" "$baseline/tmp" "$baseline/proc"

stage=$fixture/stage/poc2
mkdir -p "$stage"
write_file "$stage/linux.img" poc2-dev-root

cat > "$stage/poc1a.lock.toml" <<EOF
format = 1

[[artifacts]]
name = "main_mister"
path = "/media/fat/MiSTer"
sha256 = "$(sha256_file "$fat/MiSTer")"
size = 1
source = "fixture"

[[artifacts]]
name = "menu"
path = "/media/fat/menu.rbf"
sha256 = "$(sha256_file "$fat/menu.rbf")"
size = 1
source = "fixture"

[[artifacts]]
name = "kernel"
path = "/media/fat/linux/zImage_dtb"
sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
size = 1
source = "fixture"

[[artifacts]]
name = "linux_root"
path = "/media/fat/linux/linux.img"
sha256 = "$(printf '%s\n' stock-poc1a-root | shasum -a 256 | awk '{print $1}')"
size = 1
source = "fixture"

[[artifacts]]
name = "megadrive_core"
path = "/media/fat/_Console/MegaDrive_20260603.rbf"
sha256 = "$(sha256_file "$fat/_Console/MegaDrive_20260603.rbf")"
size = 1
source = "fixture"

[[artifacts]]
name = "snes_core"
path = "/media/fat/_Console/SNES_20260603.rbf"
sha256 = "$(sha256_file "$fat/_Console/SNES_20260603.rbf")"
size = 1
source = "fixture"

[[artifacts]]
name = "controller_input_081f_e401_v3.map"
path = "/media/fat/config/inputs/input_081f_e401_v3.map"
sha256 = "$(sha256_file "$fat/config/inputs/input_081f_e401_v3.map")"
size = 1
source = "fixture"

[runtime]
kernel_release = "5.15.1-MiSTer"
EOF

cat > "$stage/poc1b.lock.toml" <<EOF
format = 1

[outputs]
prod_rootfs_sha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
dev_rootfs_sha256 = "$(sha256_file "$fat/linux/linux.img")"
reproduced_kernel_sha256 = "$(sha256_file "$fat/linux/zImage_dtb")"
EOF

cat > "$stage/poc2.lock.toml" <<EOF
format = 1

[base]
poc1a_lock_sha256 = "$(sha256_file "$stage/poc1a.lock.toml")"
poc1b_lock_sha256 = "$(sha256_file "$stage/poc1b.lock.toml")"
accepted_dev_root_sha256 = "$(sha256_file "$fat/linux/linux.img")"
accepted_kernel_sha256 = "$(sha256_file "$fat/linux/zImage_dtb")"

[outputs]
prod_rootfs_sha256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
dev_rootfs_sha256 = "$(sha256_file "$stage/linux.img")"
EOF

archive=$fixture/poc2.tar.gz
COPYFILE_DISABLE=1 tar -czf "$archive" -C "$fixture/stage" \
  poc2/poc1a.lock.toml \
  poc2/poc1b.lock.toml \
  poc2/poc2.lock.toml \
  poc2/linux.img

install_fixture() {
  install_root=$1
  shift
  MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ROOT=$install_root \
  MISTER_REMOTE_ALLOWED_TEST_ROOT=$install_root \
  MISTER_REMOTE_EXPECTED_POC2_LOCK_SHA256=$(sha256_file "$stage/poc2.lock.toml") \
    sh "$installer" "$@"
}

expect_install_failure() {
  failure_root=$1
  shift
  if install_fixture "$failure_root" "$@" >/dev/null 2>&1; then
    echo 'POC 2 installer unexpectedly succeeded' >&2
    exit 1
  fi
}

expect_wrapper_target_failure() {
  bad_target=$1
  failure_log=$fixture/wrapper-target.log
  if MISTER_TARGET=$bad_target sh "$wrapper" > /dev/null 2> "$failure_log"; then
    echo "POC 2 wrapper accepted invalid target: $bad_target" >&2
    exit 1
  fi
  grep -q 'MISTER_TARGET must be root@' "$failure_log"
}

expect_wrapper_target_failure user@192.0.2.1
bad_option_target=-p22
expect_wrapper_target_failure "$bad_option_target"
expect_wrapper_target_failure root@-option

if test "$(id -u)" -ne 0; then
  production_log=$fixture/production-uid.log
  if sh "$installer" /not-canonical > /dev/null 2> "$production_log"; then
    echo 'target installer accepted a non-root production invocation' >&2
    exit 1
  fi
  grep -q 'production installation requires UID 0' "$production_log"
fi

unsafe_root=$fixture/unsafe-root
cp -R "$baseline" "$unsafe_root"
if MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ROOT=$unsafe_root \
  MISTER_REMOTE_ALLOWED_TEST_ROOT=$baseline \
    sh "$installer" "$archive" >/dev/null 2>&1; then
  echo 'installer accepted a test root outside its explicit fixture' >&2
  exit 1
fi

for asset in \
  media/fat/MiSTer \
  media/fat/menu.rbf \
  media/fat/_Console/MegaDrive_20260603.rbf \
  media/fat/_Console/SNES_20260603.rbf \
  media/fat/config/inputs/input_081f_e401_v3.map; do
  changed_asset=$fixture/changed-$(basename "$asset")
  cp -R "$baseline" "$changed_asset"
  write_file "$changed_asset/$asset" changed-asset
  expect_install_failure "$changed_asset" "$archive"
done

poc1a_root=$fixture/poc1a-root
cp -R "$baseline" "$poc1a_root"
write_file "$poc1a_root/media/fat/linux/linux.img" stock-poc1a-root
expect_install_failure "$poc1a_root" "$archive"

unknown_kernel=$fixture/unknown-kernel
cp -R "$baseline" "$unknown_kernel"
write_file "$unknown_kernel/media/fat/linux/zImage_dtb" unknown-kernel
expect_install_failure "$unknown_kernel" "$archive"

altered_lock_stage=$fixture/altered-lock/poc2
mkdir -p "$altered_lock_stage"
cp "$stage"/* "$altered_lock_stage/"
sed 's/^poc1a_lock_sha256 = .*/poc1a_lock_sha256 = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"/' \
  "$stage/poc2.lock.toml" > "$altered_lock_stage/poc2.lock.toml"
altered_lock_archive=$fixture/altered-lock.tar.gz
COPYFILE_DISABLE=1 tar -czf "$altered_lock_archive" -C "$fixture/altered-lock" \
  poc2/poc1a.lock.toml poc2/poc1b.lock.toml poc2/poc2.lock.toml poc2/linux.img
altered_lock_root=$fixture/altered-lock-root
cp -R "$baseline" "$altered_lock_root"
expect_install_failure "$altered_lock_root" "$altered_lock_archive"

wrong_root_stage=$fixture/wrong-root/poc2
mkdir -p "$wrong_root_stage"
cp "$stage"/* "$wrong_root_stage/"
write_file "$wrong_root_stage/linux.img" wrong-new-root
wrong_root_archive=$fixture/wrong-root.tar.gz
COPYFILE_DISABLE=1 tar -czf "$wrong_root_archive" -C "$fixture/wrong-root" \
  poc2/poc1a.lock.toml poc2/poc1b.lock.toml poc2/poc2.lock.toml poc2/linux.img
wrong_root_target=$fixture/wrong-root-target
cp -R "$baseline" "$wrong_root_target"
expect_install_failure "$wrong_root_target" "$wrong_root_archive"

linked_current=$fixture/linked-current
cp -R "$baseline" "$linked_current"
mv "$linked_current/media/fat/linux/linux.img" "$linked_current/media/fat/linux/linux.real"
ln -s linux.real "$linked_current/media/fat/linux/linux.img"
expect_install_failure "$linked_current" "$archive"

linked_linux=$fixture/linked-linux
cp -R "$baseline" "$linked_linux"
mv "$linked_linux/media/fat/linux" "$fixture/linked-linux-outside"
ln -s "$fixture/linked-linux-outside" "$linked_linux/media/fat/linux"
expect_install_failure "$linked_linux" "$archive"

extra_stage=$fixture/extra
cp -R "$fixture/stage" "$extra_stage"
write_file "$extra_stage/poc2/extra" extra
extra_archive=$fixture/extra.tar.gz
COPYFILE_DISABLE=1 tar -czf "$extra_archive" -C "$extra_stage" \
  poc2/poc1a.lock.toml poc2/poc1b.lock.toml poc2/poc2.lock.toml poc2/linux.img poc2/extra
extra_root=$fixture/extra-root
cp -R "$baseline" "$extra_root"
expect_install_failure "$extra_root" "$extra_archive"

traversal_archive=$fixture/traversal.tar.gz
python3 - "$traversal_archive" <<'PY'
import io
import sys
import tarfile

with tarfile.open(sys.argv[1], "w:gz") as archive:
    member = tarfile.TarInfo("../escape")
    payload = b"escape\n"
    member.size = len(payload)
    archive.addfile(member, io.BytesIO(payload))
PY
traversal_root=$fixture/traversal-root
cp -R "$baseline" "$traversal_root"
expect_install_failure "$traversal_root" "$traversal_archive"
test ! -e "$traversal_root/escape"

linked_stage_root=$fixture/linked-stage-root
cp -R "$baseline" "$linked_stage_root"
ln -s "$fixture" "$linked_stage_root/media/fat/linux/.mister-remote-poc2-install"
expect_install_failure "$linked_stage_root" "$archive"
test -L "$linked_stage_root/media/fat/linux/.mister-remote-poc2-install"

for unsafe_temp in \
  poc2-checkpoint.state.poc2.new \
  linux.img.pre-poc2.poc2.new \
  linux.img.poc2.new; do
  unsafe_name=$(printf '%s' "$unsafe_temp" | tr '.-' '__')
  unsafe_temp_root=$fixture/unsafe-temp-$unsafe_name
  unsafe_outside=$fixture/unsafe-outside-$unsafe_name
  cp -R "$baseline" "$unsafe_temp_root"
  case "$unsafe_temp" in
    poc2-checkpoint.state.poc2.new)
      unsafe_outside=$unsafe_temp_root/media/fat/linux/zImage_dtb
      unsafe_before=$(sha256_file "$unsafe_outside")
      ;;
    *)
      write_file "$unsafe_outside" must-not-change
      unsafe_before=$(sha256_file "$unsafe_outside")
      ;;
  esac
  ln -s "$unsafe_outside" "$unsafe_temp_root/media/fat/linux/$unsafe_temp"
  expect_install_failure "$unsafe_temp_root" "$archive"
  test "$(sha256_file "$unsafe_outside")" = "$unsafe_before"
done

interrupted_root=$fixture/interrupted-root
cp -R "$baseline" "$interrupted_root"
poc1b_sha=$(sha256_file "$interrupted_root/media/fat/linux/linux.img")
if ( MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME=1 \
  install_fixture "$interrupted_root" "$archive" ) >/dev/null 2>&1; then
  echo 'pre-rename interrupted installer unexpectedly succeeded' >&2
  exit 1
fi
test "$(sha256_file "$interrupted_root/media/fat/linux/linux.img")" = "$poc1b_sha"
test -f "$interrupted_root/media/fat/linux/linux.img.pre-poc2"
test -f "$interrupted_root/media/fat/linux/poc2-checkpoint.state"
install_fixture "$interrupted_root" "$archive"

post_rename_root=$fixture/post-rename-root
cp -R "$baseline" "$post_rename_root"
if ( MISTER_REMOTE_TEST_INTERRUPT_AFTER_RENAME=1 \
  install_fixture "$post_rename_root" "$archive" ) >/dev/null 2>&1; then
  echo 'post-rename interrupted installer unexpectedly succeeded' >&2
  exit 1
fi
test "$(sha256_file "$post_rename_root/media/fat/linux/linux.img")" = \
  "$(sha256_file "$stage/linux.img")"
grep -q '^checkpoint=prepared$' "$post_rename_root/media/fat/linux/poc2-checkpoint.state"
install_fixture "$post_rename_root" "$archive"

installed=$fixture/installed
cp -R "$baseline" "$installed"
kernel_before=$(sha256_file "$installed/media/fat/linux/zImage_dtb")
main_before=$(sha256_file "$installed/media/fat/MiSTer")
menu_before=$(sha256_file "$installed/media/fat/menu.rbf")
megadrive_before=$(sha256_file "$installed/media/fat/_Console/MegaDrive_20260603.rbf")
snes_before=$(sha256_file "$installed/media/fat/_Console/SNES_20260603.rbf")
controller_before=$(sha256_file "$installed/media/fat/config/inputs/input_081f_e401_v3.map")
cache_before=$(sha256_file "$installed/media/fat/fogcast/cache/snes/retained.bin")
modules_before=$(sha256_file "$installed/media/fat/linux/modules.poc1b/5.15.1-MiSTer/modules.tar.gz")
rom_before=$(sha256_file "$installed/media/fat/games/SNES/retained.sfc")
save_before=$(sha256_file "$installed/media/fat/saves/retained.sav")
install_fixture "$installed" "$archive"
test "$(sha256_file "$installed/media/fat/linux/linux.img")" = "$(sha256_file "$stage/linux.img")"
test "$(sha256_file "$installed/media/fat/linux/linux.img.pre-poc2")" = "$poc1b_sha"
test "$kernel_before" = "$(sha256_file "$installed/media/fat/linux/zImage_dtb")"
test "$main_before" = "$(sha256_file "$installed/media/fat/MiSTer")"
test "$menu_before" = "$(sha256_file "$installed/media/fat/menu.rbf")"
test "$megadrive_before" = "$(sha256_file "$installed/media/fat/_Console/MegaDrive_20260603.rbf")"
test "$snes_before" = "$(sha256_file "$installed/media/fat/_Console/SNES_20260603.rbf")"
test "$controller_before" = "$(sha256_file "$installed/media/fat/config/inputs/input_081f_e401_v3.map")"
test "$cache_before" = "$(sha256_file "$installed/media/fat/fogcast/cache/snes/retained.bin")"
test "$modules_before" = "$(sha256_file "$installed/media/fat/linux/modules.poc1b/5.15.1-MiSTer/modules.tar.gz")"
test "$rom_before" = "$(sha256_file "$installed/media/fat/games/SNES/retained.sfc")"
test "$save_before" = "$(sha256_file "$installed/media/fat/saves/retained.sav")"
grep -q '^checkpoint=installed$' "$installed/media/fat/linux/poc2-checkpoint.state"
install_fixture "$installed" "$archive"

tampered_state=$fixture/tampered-state
cp -R "$installed" "$tampered_state"
sed 's/^current_root_sha256=.*/current_root_sha256=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd/' \
  "$installed/media/fat/linux/poc2-checkpoint.state" > \
  "$tampered_state/media/fat/linux/poc2-checkpoint.state"
expect_install_failure "$tampered_state" "$archive"

tampered_backup=$fixture/tampered-backup
cp -R "$installed" "$tampered_backup"
write_file "$tampered_backup/media/fat/linux/linux.img.pre-poc2" tampered
expect_install_failure "$tampered_backup" "$archive"

service_root=$fixture/service-root
cp -R "$baseline" "$service_root"
cat > "$fixture/mister-supervise" <<'EOF'
#!/bin/sh
while :; do sleep 1; done
EOF
chmod 0755 "$fixture/mister-supervise"
"$fixture/mister-supervise" mister-main >/dev/null 2>&1 &
main_pid=$!
test_pids="$test_pids $main_pid"
"$fixture/mister-supervise" mister-agent >/dev/null 2>&1 &
agent_pid=$!
test_pids="$test_pids $agent_pid"
mkdir -p "$service_root/proc/$main_pid" "$service_root/proc/$agent_pid"
printf '%s\n' "$fixture/mister-supervise mister-main" > "$service_root/proc/$main_pid/cmdline"
printf '%s\n' "$fixture/mister-supervise mister-agent" > "$service_root/proc/$agent_pid/cmdline"
printf '%s\n' "$main_pid" > "$service_root/run/mister-main-supervisor.pid"
printf '%s\n' "$agent_pid" > "$service_root/run/mister-agent-supervisor.pid"
install_fixture "$service_root" "$archive"
if kill -0 "$main_pid" 2>/dev/null || kill -0 "$agent_pid" 2>/dev/null; then
  echo 'POC 2 install left a recognized supervisor running' >&2
  exit 1
fi

printf '%s\n' 'POC 2 target installer policy passed'
