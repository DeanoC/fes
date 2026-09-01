#!/bin/bash
# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: active_tree_test.sh ROOT" >&2
	exit 2
fi

root=$1
"$root/scripts/check-active-tree.sh" "$root"

fixture=$(mktemp -d /tmp/libmister-active-tree-test.XXXXXX)
trap 'rm -rf -- "$fixture"' EXIT
mkdir -p "$fixture/include" "$fixture/src" "$fixture/build"
: >"$fixture/Makefile"

expect_guard_failure() {
	local expected=$1
	local log=$fixture/guard.log
	if "$root/scripts/check-active-tree.sh" "$fixture" >"$log" 2>&1; then
		echo "active-tree guard accepted invalid fixture: $expected" >&2
		exit 1
	fi
	if ! grep -Fqx "$expected" "$log"; then
		echo "active-tree guard failed for the wrong reason; expected: $expected" >&2
		cat "$log" >&2
		exit 1
	fi
}

mkdir "$fixture/runtime"
expect_guard_failure 'obsolete root active path exists: runtime'
rmdir "$fixture/runtime"
printf '%s\n' 'class HardwareBroker;' >"$fixture/src/legacy.hpp"
expect_guard_failure 'historic compatibility term remains in the active tree'
rm -f -- "$fixture/src/legacy.hpp"
expect_guard_failure 'canonical build must contain exactly build/libmister-runtime.a'

archive="$root/build/libmister-runtime.a"
[[ -s "$archive" ]] || {
	echo "production archive missing for active-tree test" >&2
	exit 1
}
if ar t "$archive" | grep -Ei 'fake|test|fixture' >/dev/null; then
	echo "production archive contains a test-only member" >&2
	exit 1
fi
if nm -g "$archive" | c++filt | \
	grep -E 'FakeHardware|FakeMmio|FakeSpi|CartProfile|BiosProfile|mister_test' \
	>/dev/null; then
	echo "production archive contains fake hardware or profile symbols" >&2
	exit 1
fi

echo "active_tree_test: passed"
