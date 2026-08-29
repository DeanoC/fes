#!/bin/sh
set -eu

usage() {
	echo "usage: MISTER_TARGET=root@HOST $0 --install|--uninstall [PACKAGE]" >&2
}

dry_run=0
if [ "${1:-}" = "--dry-run" ]; then
	dry_run=1
	mode=${2:---install}
	package=${3:-${FOGCAST_PACKAGE:-}}
else
	mode=${1:-}
	package=${2:-${FOGCAST_PACKAGE:-}}
fi
case "$mode" in
	--install) command_name=install-profile ;;
	--uninstall) command_name=uninstall-profile ;;
	*) usage; exit 2 ;;
esac

target=${MISTER_TARGET:-}
case "$target" in
	root@?*) ;;
	*) echo 'MISTER_TARGET must be root@HOST' >&2; exit 2 ;;
esac
# Keep the remote selector a strict argv-safe host token. A positive allow
# list is portable to the target's POSIX /bin/sh and avoids shell-specific
# `$'...'` patterns in this root-only helper.
case "$target" in
	*[!A-Za-z0-9@._:-]*) echo 'MISTER_TARGET contains unsafe characters' >&2; exit 2 ;;
esac

if [ -z "$package" ]; then
	package=${DIST_DIR:-dist}/fogcast-fpgadev-${VERSION:-0.1.0}.tar.gz
fi
if [ "$dry_run" -eq 1 ]; then
	echo "dry-run: transfer package to $target"
	echo "dry-run: verify package hash on $target"
	echo "dry-run: invoke $command_name once on $target"
	exit 0
fi

if [ ! -f "$package" ]; then echo 'package does not exist' >&2; exit 2; fi

hash=$(sha256sum "$package" | awk '{print $1}')
case "$hash" in
	*[!0-9a-f]*) echo 'local package hash is invalid' >&2; exit 1 ;;
esac
if [ "${#hash}" -ne 64 ]; then echo 'local package hash is invalid' >&2; exit 1; fi

remote_stage=''
cleanup_remote_stage() {
	if [ -n "$remote_stage" ]; then
		ssh -q -- "$target" sh -s -- "$remote_stage" <<'FOGCAST_CLEANUP_REMOTE' >/dev/null 2>&1 || :
set -eu
stage=$1
case "$stage" in
  /*/fogcast-fpgadev-transfer.??????) rm -rf -- "$stage" ;;
esac
FOGCAST_CLEANUP_REMOTE
	fi
}
trap cleanup_remote_stage EXIT INT TERM

remote_stage=$(ssh -q -- "$target" sh -s -- <<'FOGCAST_CREATE_REMOTE'
set -eu
umask 077
stage=$(mktemp -d "${TMPDIR:-/var/tmp}/fogcast-fpgadev-transfer.XXXXXX")
chmod 700 "$stage"
printf '%s\n' "$stage"
FOGCAST_CREATE_REMOTE
)
case "$remote_stage" in
	/*) ;;
	*) echo 'remote transfer directory is invalid' >&2; exit 1 ;;
esac
case "$remote_stage" in
	*/fogcast-fpgadev-transfer.??????) ;;
	*) echo 'remote transfer directory name is invalid' >&2; exit 1 ;;
esac
case "$remote_stage" in
	*[!A-Za-z0-9/_.:-]*) echo 'remote transfer directory contains unsafe characters' >&2; exit 1 ;;
esac
case "$remote_stage" in
	*/../*|*/..|../*) echo 'remote transfer directory is not canonical' >&2; exit 1 ;;
esac
case "$remote_stage" in
	/|/tmp|/var/tmp|/var/tmp/) echo 'remote transfer directory is too broad' >&2; exit 1 ;;
esac

archive="$remote_stage/package.tar.gz"
scp -q -- "$package" "$target:$archive"
ssh -q -- "$target" sh -s -- "$remote_stage" "$hash" "$command_name" <<'FOGCAST_INSTALL_REMOTE'
set -eu
transfer=$1
expected=$2
command_name=$3
archive="$transfer/package.tar.gz"
stage="$transfer/payload"
cleanup() {
	rm -rf -- "$transfer"
}
trap cleanup EXIT INT TERM
printf '%s  %s\n' "$expected" "$archive" | sha256sum -c -
mkdir -m 700 "$stage"
expected_members=$(printf '%s\n' \
	bin/ \
	bin/fogcast-dev-supervisor \
	bin/mister-agent \
	bin/mister-fpga-dev \
	deploy/ \
	deploy/fpgadev/ \
	deploy/fpgadev/agent.toml.example \
	deploy/fpgadev/start.sh \
	manifest.sha256)
actual_members=$(tar -tzf "$archive")
[ "$actual_members" = "$expected_members" ]
# Validate archive entry types before extraction.  The member-name allowlist
# alone is insufficient: a same-name symlink or hard link could otherwise be
# followed by the staged command after the hash check.  Package headers are
# produced with numeric uid/gid zero; tar on the target may render that owner
# as either 0/0 or root/root, so type/mode/name checks remain independent of
# the local account-name database.
archive_listing=$(tar -tvzf "$archive")
printf '%s\n' "$archive_listing" | awk '
  NF < 6 { bad = 1; next }
  {
    mode = $1
    name = $6
    if (name == "bin/" || name == "deploy/" || name == "deploy/fpgadev/") {
      if (mode != "drwxr-xr-x") bad = 1
    } else if (name == "bin/fogcast-dev-supervisor" || name == "bin/mister-agent" || name == "bin/mister-fpga-dev" || name == "deploy/fpgadev/start.sh") {
      if (mode != "-rwxr-xr-x") bad = 1
    } else if (name == "deploy/fpgadev/agent.toml.example" || name == "manifest.sha256") {
      if (mode != "-rw-r--r--") bad = 1
    } else {
      bad = 1
    }
  }
  END { if (NR != 9) bad = 1; exit bad }
'
tar -xzf "$archive" -C "$stage"
(cd "$stage" && sha256sum -c manifest.sha256 >/dev/null)
case "$command_name" in
	install-profile)
		"$stage/bin/mister-fpga-dev" install-profile --package-root "$stage"
		;;
	uninstall-profile)
		"$stage/bin/mister-fpga-dev" uninstall-profile
		;;
	*)
		echo 'invalid install command' >&2
		exit 2
		;;
esac
FOGCAST_INSTALL_REMOTE
