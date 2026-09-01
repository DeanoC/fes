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
	local checked_root=${2:-$fixture}
	local log=$fixture/guard.log
	if "$root/scripts/check-active-tree.sh" "$checked_root" >"$log" 2>&1; then
		echo "active-tree guard accepted invalid fixture: $expected" >&2
		exit 1
	fi
	if ! grep -Fqx "$expected" "$log"; then
		echo "active-tree guard failed for the wrong reason; expected: $expected" >&2
		cat "$log" >&2
		exit 1
	fi
}

production_fixture() {
	local name=$1
	local destination=$fixture/$name
	mkdir -p "$destination"
	cp -a "$root/include" "$root/src" "$root/build" "$destination/"
	cp "$root/Makefile" "$destination/Makefile"
	printf '%s\n' "$destination"
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
	grep -E 'FakeHardware|FakeMmio|FakeSpi|FakeI2c|CartProfile|BiosProfile|mister_test' \
	>/dev/null; then
	echo "production archive contains fake hardware or profile symbols" >&2
	exit 1
fi

for member in video_recipe.o video.o i2c.o; do
	mutated=$(production_fixture "missing-${member%.o}")
	ar d "$mutated/build/libmister-runtime.a" "$member"
	expect_guard_failure \
		'archive members do not exactly match the production source manifest' \
		"$mutated"
done

mutated=$(production_fixture fake-i2c)
printf '%s\n' 'namespace mister_test { void FakeI2c() {} }' \
	>"$mutated/src/native/linux/fake_i2c.cpp"
${CXX:-c++} -c "$mutated/src/native/linux/fake_i2c.cpp" \
	-o "$mutated/build/src/native/linux/i2c.o"
ar r "$mutated/build/libmister-runtime.a" \
	"$mutated/build/src/native/linux/i2c.o"
expect_guard_failure 'archive contains fake hardware or profile symbols' "$mutated"

mutated=$(production_fixture main-video)
printf '%s\n' 'void video_mode_adjust(bool) {}' \
	>"$mutated/src/native/main_video.cpp"
${CXX:-c++} -c "$mutated/src/native/main_video.cpp" \
	-o "$mutated/build/src/native/video.o"
ar r "$mutated/build/libmister-runtime.a" "$mutated/build/src/native/video.o"
expect_guard_failure 'archive contains Main mutation authority' "$mutated"

mutated=$(production_fixture external-libi2c)
printf '%s\n' 'extern "C" int i2c_smbus_read_byte_data(int) { return 0; }' \
	>"$mutated/libi2c.cpp"
${CXX:-c++} -c "$mutated/libi2c.cpp" -o "$mutated/libi2c.o"
${AR:-ar} rcs "$mutated/libi2c.a" "$mutated/libi2c.o"
[[ -f "$mutated/libi2c.a" ]] || {
	echo "external-libi2c fixture must use a portable static archive" >&2
	exit 1
}
printf '%s\n' 'extern "C" int i2c_smbus_read_byte_data(int); int main() { return i2c_smbus_read_byte_data(0); }' \
	>"$mutated/daemon.cpp"
${CXX:-c++} "$mutated/daemon.cpp" "$mutated/libi2c.a" \
	-o "$mutated/build/mister-runtime"
expect_guard_failure 'executable links external libi2c' "$mutated"

echo "active_tree_test: passed"
