#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
cd "$repo"
test -x scripts/package-fpgadev.sh
test -x scripts/install-fpgadev.sh
sh -n scripts/package-fpgadev.sh scripts/install-fpgadev.sh deploy/fpgadev/start.sh
grep -q 'GOOS=linux GOARCH=arm GOARM=7' scripts/package-fpgadev.sh
grep -q -- '--install' scripts/install-fpgadev.sh
grep -q -- '--uninstall' scripts/install-fpgadev.sh
grep -q -- '--dry-run' scripts/install-fpgadev.sh
grep -q 'sha256sum' scripts/package-fpgadev.sh scripts/install-fpgadev.sh
grep -q 'readelf -h' scripts/package-fpgadev.sh
grep -q 'mktemp -d' scripts/package-fpgadev.sh scripts/install-fpgadev.sh
grep -q 'sha256sum -c' scripts/install-fpgadev.sh
grep -q -- 'install-profile --package-root' scripts/install-fpgadev.sh
grep -q 'stage/bin/mister-fpga-dev' scripts/install-fpgadev.sh
if grep -Eq '(/usr/bin/mister-fpga-dev|exec[[:space:]]+mister-fpga-dev)' scripts/install-fpgadev.sh; then
	echo 'installer executes a pre-existing fixed-path helper' >&2
	exit 1
fi
if grep -Eq '/tmp/fogcast-fpgadev|remote=/tmp|tar -xzf "\$remote" -C /tmp' scripts/package-fpgadev.sh scripts/install-fpgadev.sh; then
	echo 'package/installer uses a shared fixed temporary path' >&2
	exit 1
fi

# Remote commands are supplied as fixed argv/positional parameters. A shell
# command assembled with eval/sh -c would permit a profile value to change the
# remote operation and is forbidden by the transport contract.
grep -q 'ssh -q -- "\$target" sh -s --' scripts/install-fpgadev.sh
if grep -Eq '(^|[[:space:]])(eval|sh[[:space:]]+-c|bash[[:space:]]+-c)([[:space:]]|$)' scripts/install-fpgadev.sh; then
		echo 'install helper contains a dynamically assembled remote shell command' >&2
	exit 1
fi

tmp=$(mktemp -d "${TMPDIR:-/dev/shm}/fogcast-fpgadev-package-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT TERM
mkdir -p "$tmp/dist" "$tmp/build-one" "$tmp/build-two" "$tmp/unpack"
version=5d-package-test
VERSION="$version" DIST_DIR="$tmp/dist" TMPDIR="$tmp/build-one" ./scripts/package-fpgadev.sh >"$tmp/first-output"
archive="$tmp/dist/fogcast-fpgadev-$version.tar.gz"
test -s "$archive"
archive_hash=$(sha256sum "$archive" | awk '{print $1}')
test "${#archive_hash}" -eq 64

expected=$(printf '%s\n' \
	'bin/' \
	'bin/fogcast-dev-supervisor' \
	'bin/mister-agent' \
	'bin/mister-fpga-dev' \
	'deploy/' \
	'deploy/fpgadev/' \
	'deploy/fpgadev/agent.toml.example' \
	'deploy/fpgadev/start.sh' \
	'manifest.sha256')
actual=$(tar -tzf "$archive")
test "$actual" = "$expected"
# Archive headers, not only the post-extraction view, carry the reproducible
# ownership/time contract.  GNU tar prints numeric owners here because the
# package deliberately uses --numeric-owner; force UTC so this remains stable
# on hosts whose local timezone is not UTC.
archive_listing=$(TZ=UTC tar --full-time -tvzf "$archive")
printf '%s\n' "$archive_listing" | awk '
  $2 != "0/0" || $4 != "1970-01-01" || $5 != "00:00:00" { bad = 1 }
  END { exit bad }
'
printf '%s\n' "$archive_listing" | awk '
  $6 == "bin/" || $6 == "deploy/" || $6 == "deploy/fpgadev/" {
    if ($1 != "drwxr-xr-x") bad = 1
  }
  $6 == "bin/fogcast-dev-supervisor" || $6 == "bin/mister-agent" || $6 == "bin/mister-fpga-dev" || $6 == "deploy/fpgadev/start.sh" {
    if ($1 != "-rwxr-xr-x") bad = 1
  }
  $6 == "deploy/fpgadev/agent.toml.example" || $6 == "manifest.sha256" {
    if ($1 != "-rw-r--r--") bad = 1
  }
  END { exit bad }
'
tar -xzf "$archive" -C "$tmp/unpack"
test "$(stat -c '%a' "$tmp/unpack/bin/mister-agent")" = 755
test "$(stat -c '%a' "$tmp/unpack/bin/mister-fpga-dev")" = 755
test "$(stat -c '%a' "$tmp/unpack/bin/fogcast-dev-supervisor")" = 755
test "$(stat -c '%a' "$tmp/unpack/deploy/fpgadev/start.sh")" = 755
test "$(stat -c '%a' "$tmp/unpack/deploy/fpgadev/agent.toml.example")" = 644
test "$(stat -c '%a' "$tmp/unpack/manifest.sha256")" = 644
(cd "$tmp/unpack" && sha256sum -c manifest.sha256 >/dev/null)
test "$(wc -l < "$tmp/unpack/manifest.sha256")" -eq 5
(cd "$tmp/unpack" && sha256sum -c manifest.sha256 >/dev/null)
# The package manifest has one canonical sorted order, shared with the target
# InstallManager.  A producer that lists the same files in another order is
# not a package the staged validator can safely consume.
expected_manifest_members=$(printf '%s\n' \
	bin/fogcast-dev-supervisor \
	bin/mister-agent \
	bin/mister-fpga-dev \
	deploy/fpgadev/agent.toml.example \
	deploy/fpgadev/start.sh)
actual_manifest_members=$(awk '{print $2}' "$tmp/unpack/manifest.sha256")
test "$actual_manifest_members" = "$expected_manifest_members"
if tar -tzf "$archive" | grep -Eiq 'signature|public[_-]?key|\.sig$'; then
	echo 'software package contains signing material' >&2
	exit 1
fi
for binary in mister-agent mister-fpga-dev fogcast-dev-supervisor; do
	file "$tmp/unpack/bin/$binary" | grep -Eiq 'ELF 32-bit.*ARM'
	readelf -h "$tmp/unpack/bin/$binary" | grep -Eq 'Class:[[:space:]]+ELF32'
	readelf -h "$tmp/unpack/bin/$binary" | grep -Eq 'Machine:[[:space:]]+ARM'
done

# The ordinary binaries and API must not accidentally retain the private
# development lifecycle surface. Build fresh untagged executables and inspect
# both exported documentation and literal protocol/command strings.
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o "$tmp/untagged-mister-fpga-dev" ./cmd/mister-fpga-dev
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o "$tmp/untagged-supervisor" ./cmd/fogcast-dev-supervisor
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o "$tmp/untagged-mister-agent" ./cmd/mister-agent
for binary in "$tmp/untagged-mister-fpga-dev" "$tmp/untagged-supervisor" "$tmp/untagged-mister-agent"; do
	if strings "$binary" | grep -Eq 'fpgadev-install|fpgadev-boot|install-profile|recover-install|uninstall-profile|approved_trampoline|trampoline'; then
		echo "untagged binary contains development lifecycle strings: $binary" >&2
		exit 1
	fi
done
if go doc ./internal/fpgadev | grep -Eq 'InstallJournal|BootProof|ReadinessReceipt|NewMaintenanceGate|NewInstallManager|Supervisor'; then
	echo 'untagged fpgadev API exposes private development lifecycle surface' >&2
	exit 1
fi

# A second clean build with the same inputs is the source-package hash gate:
# archive bytes, member list, modes, and the fixed payload manifest must be
# identical rather than merely each individual executable hashing correctly.
VERSION="$version" DIST_DIR="$tmp/dist" TMPDIR="$tmp/build-two" ./scripts/package-fpgadev.sh >"$tmp/second-output"
cmp "$archive" "$tmp/dist/fogcast-fpgadev-$version.tar.gz"
test "$(sha256sum "$archive" | awk '{print $1}')" = "$archive_hash"

# Exercise the real transport protocol through fake argv-safe ssh/scp commands.
# The fake ssh executes each fixed remote script locally, so this checks the
# stage creation, archive/member verification, staged invocation, and cleanup
# without contacting a target.
fake_bin="$tmp/fake-bin"
fake_remote="$tmp/fake-remote"
fake_capture="$tmp/fake-capture"
network_log="$tmp/network.log"
invocation_log="$tmp/invocation.log"
mkdir -p "$fake_bin" "$fake_remote" "$fake_capture"
chmod 700 "$fake_remote"
: > "$network_log"
: > "$invocation_log"

cat > "$fake_bin/ssh" <<'FAKE_SSH'
#!/bin/sh
set -eu
log=${FAKE_NETWORK_LOG:?}
capture=${FAKE_CAPTURE_DIR:?}
printf 'ssh' >> "$log"
for arg do
	printf ' %s' "$arg" >> "$log"
done
printf '\n' >> "$log"
[ "$1" = '-q' ] && [ "$2" = '--' ] && [ "$4" = 'sh' ] && [ "$5" = '-s' ] && [ "$6" = '--' ]
case "$#" in
	6)
		output="$capture/create-output"
		if [ "${FAKE_BAD_STAGE:-0}" = 1 ]; then
			printf '%s\n' /var/tmp/not-private > "$output"
			cat "$output"
			exit 0
		fi
		TMPDIR=${FAKE_REMOTE_ROOT:?} sh -s > "$output"
		test "$(wc -l < "$output")" -eq 1
		stage=$(sed -n '1p' "$output")
		case "$stage" in
			"$FAKE_REMOTE_ROOT"/fogcast-fpgadev-transfer.??????) ;;
			*) echo 'fake ssh received an unexpected transfer path' >&2; exit 97 ;;
		esac
		printf '%s\n' "$stage" >> "$capture/stages"
		stat -c '%a' "$stage" >> "$capture/stage-modes"
		cat "$output"
		;;
	7)
		stage=$7
		cleanup_script=$(mktemp "$capture/cleanup-script.XXXXXX")
		tee "$cleanup_script" > /dev/null
		if [ -e "$stage" ]; then
			before=yes
		else
			before=no
		fi
		if sh "$cleanup_script" "$stage"; then
			status=0
		else
			status=$?
		fi
		printf 'cleanup-stage=%s exists-before=%s status=%s\n' "$stage" "$before" "$status" >> "$log"
		exit "$status"
		;;
	9)
		transfer=$7
		expected=$8
		command_name=$9
		install_script="$capture/install-script"
		tee "$install_script" > /dev/null
		if TMPDIR=${FAKE_REMOTE_ROOT:?} sh "$install_script" "$transfer" "$expected" "$command_name"; then
			status=0
		else
			status=$?
		fi
		if [ -e "$transfer" ]; then
			removed=no
		else
			removed=yes
		fi
		printf 'install-transfer=%s removed-by-remote-trap=%s status=%s\n' "$transfer" "$removed" "$status" >> "$log"
		exit "$status"
		;;
	*)
		echo 'fake ssh received unexpected argv' >&2
		exit 97
		;;
esac
FAKE_SSH

cat > "$fake_bin/scp" <<'FAKE_SCP'
#!/bin/sh
set -eu
log=${FAKE_NETWORK_LOG:?}
printf 'scp' >> "$log"
for arg do
	printf ' %s' "$arg" >> "$log"
done
printf '\n' >> "$log"
[ "$#" -eq 4 ] && [ "$1" = '-q' ] && [ "$2" = '--' ]
source=$3
destination=$4
case "$destination" in
	${FAKE_TARGET:?}:/*) ;;
	*) echo 'fake scp received an unexpected destination' >&2; exit 97 ;;
esac
archive=${destination#*:}
cp -- "$source" "$archive"
printf '%s\n' "$destination" > "${FAKE_CAPTURE_DIR:?}/scp-destination"
FAKE_SCP
chmod 755 "$fake_bin/ssh" "$fake_bin/scp"

# Dry-run must succeed even for a missing package and must not execute either
# fake network command.
dry_log="$tmp/dry-network.log"
: > "$dry_log"
if ! FAKE_NETWORK_LOG="$dry_log" FAKE_CAPTURE_DIR="$fake_capture" FAKE_REMOTE_ROOT="$fake_remote" FAKE_TARGET=root@dry-host \
	PATH="$fake_bin:$PATH" MISTER_TARGET=root@dry-host FOGCAST_PACKAGE=/definitely/missing \
		scripts/install-fpgadev.sh --dry-run > "$tmp/dry-output"; then
	echo 'installer dry-run failed' >&2
	exit 1
fi
test ! -s "$dry_log"
: > "$dry_log"
if ! FAKE_NETWORK_LOG="$dry_log" FAKE_CAPTURE_DIR="$fake_capture" FAKE_REMOTE_ROOT="$fake_remote" FAKE_TARGET=root@dry-host \
	PATH="$fake_bin:$PATH" MISTER_TARGET=root@dry-host FOGCAST_PACKAGE="$archive" \
		scripts/install-fpgadev.sh --dry-run > "$tmp/dry-valid-output"; then
	echo 'installer dry-run rejected a valid package' >&2
	exit 1
fi
test ! -s "$dry_log"
hostile_marker="$tmp/hostile-target-marker"
if MISTER_TARGET="root@bad;touch $hostile_marker" FOGCAST_PACKAGE="$archive" \
	PATH="$fake_bin:$PATH" scripts/install-fpgadev.sh --dry-run > "$tmp/hostile-output" 2>&1; then
	echo 'installer accepted a hostile target' >&2
	exit 1
fi
test ! -e "$hostile_marker"
bad_stage_log="$tmp/bad-stage-network.log"
: > "$bad_stage_log"
if FAKE_NETWORK_LOG="$bad_stage_log" FAKE_CAPTURE_DIR="$fake_capture" FAKE_REMOTE_ROOT="$fake_remote" FAKE_TARGET=root@fake-host FAKE_BAD_STAGE=1 \
	PATH="$fake_bin:$PATH" MISTER_TARGET=root@fake-host FOGCAST_PACKAGE="$archive" \
		scripts/install-fpgadev.sh --install > "$tmp/bad-stage-output" 2>&1; then
	echo 'installer accepted a hostile remote staging path' >&2
	exit 1
fi
if grep -q '^scp ' "$bad_stage_log"; then
	echo 'installer copied an archive to a rejected remote staging path' >&2
	exit 1
fi

# Use an x86 shell helper only for the local fake target; the package above is
# still the real ARMv7 artifact. This lets the fake remote execute and assert
# the exact staged argv without emulation.
live_root="$tmp/live-package"
mkdir -p "$live_root/bin" "$live_root/deploy/fpgadev"
cp "$tmp/unpack/bin/fogcast-dev-supervisor" "$live_root/bin/fogcast-dev-supervisor"
cp "$tmp/unpack/bin/mister-agent" "$live_root/bin/mister-agent"
cp "$tmp/unpack/deploy/fpgadev/agent.toml.example" "$live_root/deploy/fpgadev/agent.toml.example"
cp "$tmp/unpack/deploy/fpgadev/start.sh" "$live_root/deploy/fpgadev/start.sh"
cat > "$live_root/bin/mister-fpga-dev" <<'FAKE_STAGED_HELPER'
#!/bin/sh
set -eu
log=${FAKE_INSTALL_INVOCATION:?}
: > "$log"
printf '%s\n' "$@" > "$log"
test "$#" -eq 3
test "$1" = install-profile
test "$2" = --package-root
stage=$3
case "$stage" in
	*/fogcast-fpgadev-transfer.??????/payload) ;;
	*) exit 41 ;;
esac
test "$(stat -c '%a' "$stage")" = 700
for member in \
	bin/fogcast-dev-supervisor \
	bin/mister-agent \
	bin/mister-fpga-dev \
	deploy/fpgadev/agent.toml.example \
	deploy/fpgadev/start.sh \
	manifest.sha256; do
	test -f "$stage/$member"
done
if [ "${FAKE_INSTALL_FAIL:-0}" = 1 ]; then
	exit 42
fi
FAKE_STAGED_HELPER
chmod 755 "$live_root/bin/mister-fpga-dev" "$live_root/bin/fogcast-dev-supervisor" "$live_root/bin/mister-agent" "$live_root/deploy/fpgadev/start.sh"
chmod 755 "$live_root/bin" "$live_root/deploy" "$live_root/deploy/fpgadev"
chmod 644 "$live_root/deploy/fpgadev/agent.toml.example"
(cd "$live_root" && sha256sum \
	bin/fogcast-dev-supervisor \
	bin/mister-agent \
	bin/mister-fpga-dev \
	deploy/fpgadev/agent.toml.example \
	deploy/fpgadev/start.sh > manifest.sha256)
chmod 644 "$live_root/manifest.sha256"
live_archive="$tmp/live-package.tar.gz"
(cd "$live_root" && tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner -cf - bin deploy manifest.sha256 | gzip -n > "$live_archive")

fake_env="FAKE_NETWORK_LOG=$network_log FAKE_CAPTURE_DIR=$fake_capture FAKE_REMOTE_ROOT=$fake_remote FAKE_TARGET=root@fake-host FAKE_INSTALL_INVOCATION=$invocation_log FAKE_SYMLINK_EXECUTION=$tmp/symlink-execution PATH=$fake_bin:$PATH"
env $fake_env MISTER_TARGET=root@fake-host FOGCAST_PACKAGE="$live_archive" scripts/install-fpgadev.sh --install > "$tmp/live-install-output"
test "$(sed -n '1p' "$invocation_log")" = install-profile
test "$(sed -n '2p' "$invocation_log")" = --package-root
invocation_stage=$(sed -n '3p' "$invocation_log")
case "$invocation_stage" in
	"$fake_remote"/fogcast-fpgadev-transfer.??????/payload) ;;
	*) echo 'staged helper received an unexpected package root' >&2; exit 1 ;;
esac
test "$(wc -l < "$fake_capture/stages")" -eq 1
test "$(sed -n '1p' "$fake_capture/stage-modes")" = 700
stage_one=$(sed -n '1p' "$fake_capture/stages")
test ! -e "$stage_one"
test "$(cat "$fake_capture/scp-destination")" = "root@fake-host:$stage_one/package.tar.gz"
grep -q "cleanup-stage=$stage_one exists-before=no status=0" "$network_log"
grep -q "install-transfer=$stage_one removed-by-remote-trap=yes status=0" "$network_log"
grep -Fq 'sha256sum -c -' "$fake_capture/install-script"
grep -Fq 'mkdir -m 700 "$stage"' "$fake_capture/install-script"
grep -Fq '"$stage/bin/mister-fpga-dev" install-profile --package-root "$stage"' "$fake_capture/install-script"

# A failing staged helper must still remove the remote transfer directory and
# must not cause a fallback to any fixed-path helper.
if env $fake_env FAKE_INSTALL_FAIL=1 MISTER_TARGET=root@fake-host FOGCAST_PACKAGE="$live_archive" \
	scripts/install-fpgadev.sh --install > "$tmp/live-install-failure-output" 2>&1; then
	echo 'installer unexpectedly succeeded after staged helper failure' >&2
	exit 1
fi
test "$(wc -l < "$fake_capture/stages")" -eq 2
stage_two=$(sed -n '2p' "$fake_capture/stages")
test "$stage_one" != "$stage_two"
test "$(sed -n '2p' "$fake_capture/stage-modes")" = 700
test ! -e "$stage_two"
grep -q "install-transfer=$stage_two removed-by-remote-trap=yes status=42" "$network_log"
grep -q "cleanup-stage=$stage_two exists-before=no status=0" "$network_log"

# A same-name archive symlink must be rejected before the staged command can
# follow it.  The symlink target is itself a valid shell executable and its
# hash is included in both manifest entries, making a hash-only check
# insufficient.
hostile_root="$tmp/hostile-package"
cp -a "$live_root" "$hostile_root"
rm "$hostile_root/bin/mister-fpga-dev"
cat > "$hostile_root/bin/mister-agent" <<'FAKE_SYMLINK_TARGET'
#!/bin/sh
set -eu
printf '%s\n' symlink-followed > "${FAKE_SYMLINK_EXECUTION:?}"
FAKE_SYMLINK_TARGET
chmod 755 "$hostile_root/bin/mister-agent"
ln -s mister-agent "$hostile_root/bin/mister-fpga-dev"
(cd "$hostile_root" && sha256sum \
	bin/fogcast-dev-supervisor \
	bin/mister-agent \
	bin/mister-fpga-dev \
	deploy/fpgadev/agent.toml.example \
	deploy/fpgadev/start.sh > manifest.sha256)
chmod 644 "$hostile_root/manifest.sha256"
hostile_archive="$tmp/hostile-package.tar.gz"
(cd "$hostile_root" && tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner -cf - bin deploy manifest.sha256 | gzip -n > "$hostile_archive")
: > "$invocation_log"
if env $fake_env MISTER_TARGET=root@fake-host FOGCAST_PACKAGE="$hostile_archive" \
	scripts/install-fpgadev.sh --install > "$tmp/hostile-package-output" 2>&1; then
	echo 'installer accepted a symlinked staged helper' >&2
	exit 1
fi
test ! -e "$tmp/symlink-execution"
test ! -s "$invocation_log"
test "$(wc -l < "$fake_capture/stages")" -eq 3
stage_three=$(sed -n '3p' "$fake_capture/stages")
test ! -e "$stage_three"
grep -q "install-transfer=$stage_three removed-by-remote-trap=yes" "$network_log"
