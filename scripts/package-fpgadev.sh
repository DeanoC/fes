#!/bin/sh
set -eu

version=${VERSION:-0.1.0}
out_dir=${DIST_DIR:-dist}
tmp_parent=${TMPDIR:-/dev/shm}
work_dir=$(mktemp -d "$tmp_parent/fogcast-fpgadev-package.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT INT TERM
mkdir -p "$out_dir" "$work_dir/bin" "$work_dir/deploy/fpgadev"
out_dir=$(CDPATH='' cd -- "$out_dir" && pwd)
archive="$out_dir/fogcast-fpgadev-$version.tar.gz"

build() {
	name=$1
	package=$2
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -buildvcs=false -trimpath -tags fpgadev -ldflags "-s -w" -o "$work_dir/bin/$name" "$package"
	file_output=$(file "$work_dir/bin/$name")
	case "$file_output" in
		*'ELF 32-bit'*'ARM'*) ;;
		*) echo "unexpected architecture for $name: $file_output" >&2; exit 1 ;;
	esac
	header=$(readelf -h "$work_dir/bin/$name")
	printf '%s\n' "$header" | grep -Eq 'Class:[[:space:]]+ELF32'
	printf '%s\n' "$header" | grep -Eq 'Machine:[[:space:]]+ARM'
}

build mister-agent ./cmd/mister-agent
build mister-fpga-dev ./cmd/mister-fpga-dev
build fogcast-dev-supervisor ./cmd/fogcast-dev-supervisor

# Archive metadata is part of the package contract. Go's output mode follows
# the caller's umask, while the deployer needs stable executable and payload
# modes independent of the build host.
chmod 0755 "$work_dir/bin/mister-agent" "$work_dir/bin/mister-fpga-dev" "$work_dir/bin/fogcast-dev-supervisor"
chmod 0755 "$work_dir/bin" "$work_dir/deploy" "$work_dir/deploy/fpgadev"
cp deploy/fpgadev/start.sh "$work_dir/deploy/fpgadev/start.sh"
cp deploy/fpgadev/agent.toml.example "$work_dir/deploy/fpgadev/agent.toml.example"
chmod 0755 "$work_dir/deploy/fpgadev/start.sh"
chmod 0644 "$work_dir/deploy/fpgadev/agent.toml.example"

(cd "$work_dir" && sha256sum bin/fogcast-dev-supervisor bin/mister-agent bin/mister-fpga-dev deploy/fpgadev/agent.toml.example deploy/fpgadev/start.sh > manifest.sha256)
(cd "$work_dir" && chmod 0644 manifest.sha256)
(cd "$work_dir" && sha256sum -c manifest.sha256 >/dev/null)
(cd "$work_dir" && tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner -cf - bin deploy manifest.sha256 | gzip -n > "$archive")
printf '%s\n' "$archive"
