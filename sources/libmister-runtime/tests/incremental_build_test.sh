#!/bin/bash
# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

check_only=false
if [[ ${1:-} == --check-only ]]; then
	check_only=true
	shift
fi

if [[ $# -lt 2 ]]; then
	echo "usage: incremental_build_test.sh [--check-only] ROOT TEST_BINARY..." >&2
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
if $check_only; then
	assumed=()
	for header in "${headers[@]}"; do
		assumed+=(-W "$header")
	done
	for binary in "$@"; do
		# Establish a clean baseline so an unrelated stale input cannot make
		# a missing header dependency appear to pass.
		MAKEFLAGS= make -C "$root" -q "$binary" || {
			echo "test binary is stale before dependency check: $binary" >&2
			exit 1
		}
		status=0
		MAKEFLAGS= make -C "$root" -q "${assumed[@]}" "$binary" || status=$?
		[[ $status == 1 ]] || {
			echo "header dependency check expected rebuild (status $status): $binary" >&2
			exit 1
		}
	done
	echo "incremental_build_test: all test binaries would rebuild (no compilation)"
	exit 0
fi

for header in "${headers[@]}"; do
	touch -r "$root/$header" "$temporary/${header//\//_}"
done
touch "$temporary/rebuild-marker"
# GNU Make 3.81 compares modification times at one-second resolution.
sleep 1
for header in "${headers[@]}"; do
	touch "$root/$header"
done

# The initial run-tests prerequisite already executed every functional case.
# Header contents are unchanged here: rebuild every binary to exercise the
# dependency edges, without running the same functional cases a second time.
MAKEFLAGS= make -C "$root" -j16 "$@" >/dev/null
for binary in "$@"; do
	[[ "$root/$binary" -nt "$temporary/rebuild-marker" ]] || {
		echo "relevant headers did not rebuild test binary: $binary" >&2
		exit 1
	}
done

echo "incremental_build_test: all test binaries rebuilt"
