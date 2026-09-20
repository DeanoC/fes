#!/bin/bash
# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: version_build_test.sh ROOT" >&2
	exit 2
fi

root=$1
temporary=$(mktemp -d /tmp/libmister-version-build.XXXXXX)
trap 'rm -rf -- "$temporary"' EXIT
build=$temporary/build
first=version-regression-one
second=version-regression-two

MAKEFLAGS= make -C "$root" -j16 BUILD_DIR="$build" MISTER_RUNTIME_VERSION="$first" all \
	>/dev/null
strings "$build/mister-runtime" >"$temporary/first.strings"
grep -Fqx "$first" "$temporary/first.strings" || {
	echo "first version input did not reach mister-runtime" >&2
	exit 1
}
MAKEFLAGS= make -C "$root" -j16 BUILD_DIR="$build" MISTER_RUNTIME_VERSION="$second" all \
	>/dev/null
strings "$build/mister-runtime" >"$temporary/second.strings"
grep -Fqx "$second" "$temporary/second.strings" || {
	echo "incremental version input did not reach mister-runtime" >&2
	exit 1
}
if grep -Fqx "$first" "$temporary/second.strings"; then
	echo "stale version remains embedded after incremental rebuild" >&2
	exit 1
fi

echo "version_build_test: incremental version updated"
