#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-install.XXXXXX")
test_pids=
cleanup() {
  for cleanup_pid in $test_pids; do
    kill "$cleanup_pid" 2>/dev/null || true
    wait "$cleanup_pid" 2>/dev/null || true
  done
  rm -rf "$fixture"
}
trap cleanup EXIT INT TERM

installer=$repo/deploy/poc1b/install-target.sh
wrapper=$repo/scripts/install-poc1b.sh
restore=$repo/scripts/restore-poc1a-sd.sh
test -x "$installer"
test -x "$wrapper"
test -x "$restore"

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
write_file "$fat/linux/zImage_dtb" accepted-kernel
write_file "$fat/linux/linux.img" stock-root
write_file "$fat/_Console/MegaDrive_20260603.rbf" megadrive
write_file "$fat/_Console/SNES_20260603.rbf" snes
write_file "$fat/config/inputs/input_081f_e401_v3.map" controller
mkdir -p "$baseline/tmp"

stage=$fixture/stage/poc1b
mkdir -p "$stage"
write_file "$stage/linux.img" poc1b-dev-root
write_file "$stage/zImage_dtb" reproduced-kernel

module_root=$fixture/module-root
write_file "$module_root/lib/modules/5.15.1-MiSTer/kernel/test.ko" module
COPYFILE_DISABLE=1 tar -czf "$stage/modules.tar.gz" -C "$module_root" .
modules_sha=$(sha256_file "$stage/modules.tar.gz")
cat > "$stage/kernel-manifest.toml" <<EOF
release = "5.15.1-MiSTer"

[[artifacts]]
name = "modules.tar.gz"
sha256 = "$modules_sha"
size = 1
EOF

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
sha256 = "$(sha256_file "$fat/linux/zImage_dtb")"
size = 1
source = "fixture"

[[artifacts]]
name = "linux_root"
path = "/media/fat/linux/linux.img"
sha256 = "$(sha256_file "$fat/linux/linux.img")"
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
prod_rootfs_sha256 = "$(sha256_file "$stage/linux.img")"
dev_rootfs_sha256 = "$(sha256_file "$stage/linux.img")"
reproduced_kernel_sha256 = "$(sha256_file "$stage/zImage_dtb")"
EOF

binary_archive=$fixture/binary-kernel.tar.gz
COPYFILE_DISABLE=1 tar -czf "$binary_archive" -C "$fixture/stage" \
  poc1b/poc1a.lock.toml \
  poc1b/poc1b.lock.toml \
  poc1b/linux.img

source_archive=$fixture/source-kernel.tar.gz
COPYFILE_DISABLE=1 tar -czf "$source_archive" -C "$fixture/stage" \
  poc1b/poc1a.lock.toml \
  poc1b/poc1b.lock.toml \
  poc1b/linux.img \
  poc1b/zImage_dtb \
  poc1b/modules.tar.gz \
  poc1b/kernel-manifest.toml

install_fixture() {
  install_root=$1
  shift
  MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ROOT=$install_root \
  MISTER_REMOTE_ALLOWED_TEST_ROOT=$install_root \
    sh "$installer" "$@"
}

expect_install_failure() {
  failure_root=$1
  shift
  if install_fixture "$failure_root" "$@" >/dev/null 2>&1; then
    echo 'POC 1B installer unexpectedly succeeded' >&2
    exit 1
  fi
}

expect_wrapper_target_failure() {
  bad_target=$1
  failure_log=$fixture/wrapper-target.log
  if MISTER_TARGET=$bad_target sh "$wrapper" binary-kernel \
      > /dev/null 2> "$failure_log"; then
    echo "POC 1B wrapper accepted invalid target: $bad_target" >&2
    exit 1
  fi
  grep -q 'MISTER_TARGET must be root@' "$failure_log"
}

expect_wrapper_target_failure user@192.0.2.1
bad_option_target=-p22
expect_wrapper_target_failure "$bad_option_target"
expect_wrapper_target_failure 'root@-option'

if test "$(id -u)" -ne 0; then
  production_log=$fixture/production-uid.log
  if sh "$installer" binary-kernel /not-canonical \
      > /dev/null 2> "$production_log"; then
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
    sh "$installer" binary-kernel "$binary_archive" >/dev/null 2>&1; then
  echo 'installer accepted a test root outside its explicit fixture' >&2
  exit 1
fi
if MISTER_REMOTE_TEST_MODE=1 MISTER_REMOTE_ALLOWED_TEST_ROOT=$baseline \
  sh "$installer" binary-kernel "$binary_archive" >/dev/null 2>&1; then
  echo 'installer accepted an empty test root' >&2
  exit 1
fi

missing_asset=$fixture/missing-asset
cp -R "$baseline" "$missing_asset"
rm "$missing_asset/media/fat/menu.rbf"
expect_install_failure "$missing_asset" binary-kernel "$binary_archive"

changed_kernel=$fixture/changed-kernel
cp -R "$baseline" "$changed_kernel"
write_file "$changed_kernel/media/fat/linux/zImage_dtb" changed-kernel
expect_install_failure "$changed_kernel" binary-kernel "$binary_archive"

changed_root=$fixture/changed-root
cp -R "$baseline" "$changed_root"
write_file "$changed_root/media/fat/linux/linux.img" changed-stock-root
expect_install_failure "$changed_root" binary-kernel "$binary_archive"

missing_output_stage=$fixture/missing-output/poc1b
mkdir -p "$missing_output_stage"
cp "$stage/poc1a.lock.toml" "$missing_output_stage/poc1a.lock.toml"
cp "$stage/linux.img" "$missing_output_stage/linux.img"
printf '%s\n' 'format = 1' > "$missing_output_stage/poc1b.lock.toml"
missing_output_archive=$fixture/missing-output.tar.gz
COPYFILE_DISABLE=1 tar -czf "$missing_output_archive" -C "$fixture/missing-output" \
  poc1b/poc1a.lock.toml poc1b/poc1b.lock.toml poc1b/linux.img
missing_output_root=$fixture/missing-output-root
cp -R "$baseline" "$missing_output_root"
expect_install_failure "$missing_output_root" binary-kernel "$missing_output_archive"

tainted_stage=$fixture/tainted
cp -R "$fixture/stage" "$tainted_stage"
write_file "$tainted_stage/poc1b/secret.txt" 'token = "must-not-install"'
write_file "$tainted_stage/poc1b/game.sfc" rom-data
tainted_archive=$fixture/tainted.tar.gz
COPYFILE_DISABLE=1 tar -czf "$tainted_archive" -C "$tainted_stage" \
  poc1b/poc1a.lock.toml poc1b/poc1b.lock.toml poc1b/linux.img \
  poc1b/secret.txt poc1b/game.sfc
tainted_root=$fixture/tainted-root
cp -R "$baseline" "$tainted_root"
expect_install_failure "$tainted_root" binary-kernel "$tainted_archive"

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
expect_install_failure "$traversal_root" binary-kernel "$traversal_archive"
test ! -e "$traversal_root/escape"

interrupted_root=$fixture/interrupted-root
cp -R "$baseline" "$interrupted_root"
stock_root_sha=$(sha256_file "$interrupted_root/media/fat/linux/linux.img")
if ( MISTER_REMOTE_TEST_INTERRUPT_BEFORE_RENAME=1 \
  install_fixture "$interrupted_root" binary-kernel "$binary_archive" ) \
    >/dev/null 2>&1; then
  echo 'interrupted installer unexpectedly succeeded' >&2
  exit 1
fi
test "$(sha256_file "$interrupted_root/media/fat/linux/linux.img")" = \
  "$stock_root_sha"

binary_root=$fixture/binary-root
cp -R "$baseline" "$binary_root"
install_fixture "$binary_root" binary-kernel "$binary_archive"
test "$(sha256_file "$binary_root/media/fat/linux/linux.img")" = \
  "$(sha256_file "$stage/linux.img")"
test "$(sha256_file "$binary_root/media/fat/linux/zImage_dtb")" = \
  "$(sha256_file "$fat/linux/zImage_dtb")"
test -f "$binary_root/media/fat/linux/linux.img.pre-poc1b"
test -f "$binary_root/media/fat/linux/zImage_dtb.pre-poc1b"
install_fixture "$binary_root" binary-kernel "$binary_archive"

write_file "$binary_root/media/fat/linux/linux.img.pre-poc1b" tampered-backup
expect_install_failure "$binary_root" binary-kernel "$binary_archive"

service_root=$fixture/service-root
cp -R "$baseline" "$service_root"
cat > "$fixture/start-agent.sh" <<'EOF'
#!/bin/sh
while :; do sleep 1; done
EOF
cat > "$fixture/mister-supervise" <<'EOF'
#!/bin/sh
while :; do sleep 1; done
EOF
chmod 0755 "$fixture/start-agent.sh" "$fixture/mister-supervise"
"$fixture/start-agent.sh" >/dev/null 2>&1 &
poc1a_supervisor_pid=$!
test_pids="$test_pids $poc1a_supervisor_pid"
"$fixture/mister-supervise" mister-main >/dev/null 2>&1 &
main_supervisor_pid=$!
test_pids="$test_pids $main_supervisor_pid"
"$fixture/mister-supervise" mister-agent >/dev/null 2>&1 &
poc1b_agent_supervisor_pid=$!
test_pids="$test_pids $poc1b_agent_supervisor_pid"
mkdir -p "$service_root/run" "$service_root/tmp"
mkdir -p \
  "$service_root/proc/$poc1a_supervisor_pid" \
  "$service_root/proc/$poc1b_agent_supervisor_pid" \
  "$service_root/proc/$main_supervisor_pid"
printf '%s\n' "$fixture/start-agent.sh" > \
  "$service_root/proc/$poc1a_supervisor_pid/cmdline"
printf '%s\n' "$fixture/mister-supervise mister-main" > \
  "$service_root/proc/$main_supervisor_pid/cmdline"
printf '%s\n' "$fixture/mister-supervise mister-agent" > \
  "$service_root/proc/$poc1b_agent_supervisor_pid/cmdline"
printf '%s\n' "$poc1a_supervisor_pid" > \
  "$service_root/tmp/mister-agent-supervisor.pid"
printf '%s\n' "$poc1b_agent_supervisor_pid" > \
  "$service_root/run/mister-agent-supervisor.pid"
printf '%s\n' "$main_supervisor_pid" > \
  "$service_root/run/mister-main-supervisor.pid"
install_fixture "$service_root" binary-kernel "$binary_archive"
if kill -0 "$poc1a_supervisor_pid" 2>/dev/null; then
  echo 'checkpoint install left the POC 1A supervisor running' >&2
  exit 1
fi
if kill -0 "$main_supervisor_pid" 2>/dev/null; then
  echo 'checkpoint install left the Main supervisor running' >&2
  exit 1
fi
if kill -0 "$poc1b_agent_supervisor_pid" 2>/dev/null; then
  echo 'checkpoint install left the POC 1B agent supervisor running' >&2
  exit 1
fi

missing_pidof_root=$fixture/missing-pidof-root
cp -R "$baseline" "$missing_pidof_root"
missing_pidof_log=$fixture/missing-pidof.log
if ( MISTER_REMOTE_TEST_MISSING_PIDOF=1 \
  install_fixture "$missing_pidof_root" binary-kernel "$binary_archive" ) \
    > /dev/null 2> "$missing_pidof_log"; then
  echo 'checkpoint install proceeded without child-process verification' >&2
  exit 1
fi
grep -q 'pidof is required to verify runtime shutdown' "$missing_pidof_log"

wrong_identity_root=$fixture/wrong-identity-root
cp -R "$baseline" "$wrong_identity_root"
"$fixture/mister-supervise" unrelated-service >/dev/null 2>&1 &
unrelated_pid=$!
test_pids="$test_pids $unrelated_pid"
mkdir -p "$wrong_identity_root/proc/$unrelated_pid" \
  "$wrong_identity_root/run"
printf '%s\n' "$fixture/mister-supervise unrelated-service" > \
  "$wrong_identity_root/proc/$unrelated_pid/cmdline"
printf '%s\n' "$unrelated_pid" > \
  "$wrong_identity_root/run/mister-main-supervisor.pid"
expect_install_failure "$wrong_identity_root" binary-kernel "$binary_archive"
kill -0 "$unrelated_pid" 2>/dev/null || {
  echo 'identity rejection stopped an unrelated process' >&2
  exit 1
}

checkpoint2_bad_root=$fixture/checkpoint2-bad-root
cp -R "$baseline" "$checkpoint2_bad_root"
install_fixture "$checkpoint2_bad_root" binary-kernel "$binary_archive"
write_file "$checkpoint2_bad_root/media/fat/linux/linux.img" changed-root
expect_install_failure "$checkpoint2_bad_root" source-kernel "$source_archive"

source_root=$fixture/source-root
cp -R "$baseline" "$source_root"
install_fixture "$source_root" binary-kernel "$binary_archive"
install_fixture "$source_root" source-kernel "$source_archive"
test "$(sha256_file "$source_root/media/fat/linux/zImage_dtb")" = \
  "$(sha256_file "$stage/zImage_dtb")"
installed_modules=$source_root/media/fat/linux/modules.poc1b/5.15.1-MiSTer/modules.tar.gz
test "$(sha256_file "$installed_modules")" = "$modules_sha"
install_fixture "$source_root" source-kernel "$source_archive"

post_rename_root=$fixture/post-rename-root
cp -R "$baseline" "$post_rename_root"
install_fixture "$post_rename_root" binary-kernel "$binary_archive"
if ( MISTER_REMOTE_TEST_INTERRUPT_AFTER_RENAME='reproduced kernel' \
  install_fixture "$post_rename_root" source-kernel "$source_archive" ) \
    >/dev/null 2>&1; then
  echo 'post-kernel-rename interruption unexpectedly succeeded' >&2
  exit 1
fi
test "$(sha256_file "$post_rename_root/media/fat/linux/zImage_dtb")" = \
  "$(sha256_file "$stage/zImage_dtb")"
grep -q '^checkpoint=binary-kernel$' \
  "$post_rename_root/media/fat/linux/poc1b-checkpoint.state"
install_fixture "$post_rename_root" source-kernel "$source_archive"
grep -q '^checkpoint=source-kernel$' \
  "$post_rename_root/media/fat/linux/poc1b-checkpoint.state"

post_state_root=$fixture/post-state-root
cp -R "$baseline" "$post_state_root"
install_fixture "$post_state_root" binary-kernel "$binary_archive"
if ( MISTER_REMOTE_TEST_INTERRUPT_AFTER_STATE=source-kernel \
  install_fixture "$post_state_root" source-kernel "$source_archive" ) \
    >/dev/null 2>&1; then
  echo 'post-state interruption unexpectedly succeeded' >&2
  exit 1
fi
grep -q '^checkpoint=source-kernel$' \
  "$post_state_root/media/fat/linux/poc1b-checkpoint.state"
install_fixture "$post_state_root" source-kernel "$source_archive"

missing_backup_root=$fixture/missing-backup-root
cp -R "$source_root" "$missing_backup_root"
rm "$missing_backup_root/media/fat/linux/zImage_dtb.pre-poc1b"
if MISTER_REMOTE_TEST_MODE=1 \
  MISTER_REMOTE_ALLOWED_TEST_VOLUME=$missing_backup_root/media/fat \
    sh "$restore" "$missing_backup_root/media/fat" >/dev/null 2>&1; then
  echo 'offline restore accepted a missing backup' >&2
  exit 1
fi

root_backup_sha=$(sha256_file "$source_root/media/fat/linux/linux.img.pre-poc1b")
kernel_backup_sha=$(sha256_file "$source_root/media/fat/linux/zImage_dtb.pre-poc1b")
MISTER_REMOTE_TEST_MODE=1 \
MISTER_REMOTE_ALLOWED_TEST_VOLUME=$source_root/media/fat \
  sh "$restore" "$source_root/media/fat"
test "$(sha256_file "$source_root/media/fat/linux/linux.img")" = \
  "$root_backup_sha"
test "$(sha256_file "$source_root/media/fat/linux/zImage_dtb")" = \
  "$kernel_backup_sha"
