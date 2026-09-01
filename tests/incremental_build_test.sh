#!/bin/bash
# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

if [[ $# -lt 2 ]]; then
	echo "usage: incremental_build_test.sh ROOT TEST_BINARY..." >&2
	exit 2
fi

root=$1
shift
temporary=$(mktemp -d /tmp/libmister-incremental-build.XXXXXX)
headers=(
	include/libmister-runtime/runtime.h
	src/native/artifacts.hpp
	src/native/hardware.hpp
	src/native/linux/mmio.hpp
	src/daemon/protocol.hpp
	src/linux/production_hardware.hpp
	tests/support/fake_hardware.hpp
)

restore_timestamps() {
	for header in "${headers[@]}"; do
		if [[ -e "$temporary/${header//\//_}" ]]; then
			touch -r "$temporary/${header//\//_}" "$root/$header"
		fi
	done
	rm -rf -- "$temporary"
}
trap restore_timestamps EXIT

for binary in "$@"; do
	[[ -x "$root/$binary" ]] || {
		echo "test binary is missing before dependency check: $binary" >&2
		exit 1
	}
done
for header in "${headers[@]}"; do
	touch -r "$root/$header" "$temporary/${header//\//_}"
done
touch "$temporary/rebuild-marker"
for header in "${headers[@]}"; do
	touch "$root/$header"
done

MAKEFLAGS= make -C "$root" -j16 run-tests >/dev/null
for binary in "$@"; do
	[[ "$root/$binary" -nt "$temporary/rebuild-marker" ]] || {
		echo "relevant headers did not rebuild test binary: $binary" >&2
		exit 1
	}
done

echo "incremental_build_test: all test binaries rebuilt"
