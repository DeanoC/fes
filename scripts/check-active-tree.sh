#!/bin/bash
# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

root=${1:-"$(cd "$(dirname "$0")/.." && pwd -P)"}
build=$root/build
archive=$build/libmister-runtime.a
daemon=$build/mister-runtime
temporary=$(mktemp -d /tmp/libmister-active-tree.XXXXXX)
trap 'rm -rf -- "$temporary"' EXIT

for active_path in fogcast runtime support lib releases; do
	if [[ -e "$root/$active_path" ]]; then
		echo "obsolete root active path exists: $active_path" >&2
		exit 1
	fi
done

historic_pattern='stage[-_ ]?c0|poc[0-9]*|fogcast-runtime|native[-_ ]personality|mister_runtime_linux_v2|NativeLinuxV2|HardwareBroker|OperationLease|CapabilityBundle|AuthorityView|BackendFence|ReplayTracker|native_recovery|native_containment|native_peripheral_session|native_resources|native_audio|native_av_io|native_video|native_input|native_save|native_scheduler|native_offload|native_snes|v2'

if grep -ERni --include='*.[ch]' --include='*.cpp' --include='*.hpp' \
	--include='Makefile' "$historic_pattern" \
	"$root/include" "$root/src" "$root/Makefile" >"$temporary/source-names.log"; then
	echo "historic compatibility term remains in the active tree" >&2
	cat "$temporary/source-names.log" >&2
	exit 1
fi

archive_list=$(find "$build" -maxdepth 1 -type f -name '*.a' | sed 's|^.*/||' | LC_ALL=C sort)
[[ "$archive_list" == 'libmister-runtime.a' ]] || {
	echo "canonical build must contain exactly build/libmister-runtime.a" >&2
	printf '%s\n' "$archive_list" >&2
	exit 1
}
executable_list=$(find "$build" -maxdepth 1 -type f \
	\( -perm -0100 -o -perm -0010 -o -perm -0001 \) |
	sed 's|^.*/||' | LC_ALL=C sort)
[[ "$executable_list" == 'mister-runtime' ]] || {
	echo "canonical build must contain exactly build/mister-runtime" >&2
	printf '%s\n' "$executable_list" >&2
	exit 1
}
[[ -s "$archive" && -x "$daemon" ]] || {
	echo "canonical archive or executable is empty or unusable" >&2
	exit 1
}

built_name_pattern='fogcast|personality|stage[-_ ]?[a-z0-9]*|poc[0-9]*|broker|coordinator|fence|replay|(^|[^[:alnum:]])v2([^[:alnum:]]|$)'
for output in "$archive" "$daemon"; do
	strings "$output" >"$temporary/$(basename "$output").strings"
	grep -Fv '__gxx_personality_v0' "$temporary/$(basename "$output").strings" \
		>"$temporary/$(basename "$output").project-strings" || true
	if grep -Eai "$built_name_pattern" "$temporary/$(basename "$output").project-strings" \
		>"$temporary/built-names.log"; then
		echo "historic name remains in production output: ${output#$root/}" >&2
		cat "$temporary/built-names.log" >&2
		exit 1
	fi
done

cat >"$temporary/expected-members" <<'EOF'
artifacts.o
core_loader.o
fpga_manager.o
hardware.o
i2c.o
mmio.o
production_hardware.o
profile.o
runtime.o
spi.o
video.o
video_recipe.o
EOF
ar t "$archive" | grep -v '^__\.SYMDEF' >"$temporary/archive-members"
LC_ALL=C sort -o "$temporary/archive-members" "$temporary/archive-members"
cmp "$temporary/expected-members" "$temporary/archive-members" || {
	echo "archive members do not exactly match the production source manifest" >&2
	diff -u "$temporary/expected-members" "$temporary/archive-members" >&2 || true
	exit 1
}
if grep -Eai 'fake|test|fixture' "$temporary/archive-members" >/dev/null; then
	echo "archive contains a fake or test object" >&2
	exit 1
fi

nm -g "$archive" >"$temporary/archive-symbols.raw"
c++filt <"$temporary/archive-symbols.raw" >"$temporary/archive-symbols"
if grep -E 'FakeHardware|FakeMmio|FakeSpi|FakeI2c|LinuxI2cTestOperations|CartProfile|BiosProfile|mister_test' \
	"$temporary/archive-symbols" >/dev/null; then
	echo "archive contains fake hardware or profile symbols" >&2
	exit 1
fi
if grep -E '(^|[^[:alnum:]_])video_mode_adjust($|[^[:alnum:]_])' \
	"$temporary/archive-symbols" >/dev/null; then
	echo "archive contains Main mutation authority" >&2
	exit 1
fi

if strings "$daemon" | grep -Eai '(^|/)libi2c([.-]|$)' >/dev/null || \
	nm -g "$daemon" | c++filt | \
		grep -E '(^|[^[:alnum:]_])_?i2c_smbus_[[:alnum:]_]*($|[^[:alnum:]_])' \
		>/dev/null; then
	echo "executable links external libi2c" >&2
	exit 1
fi

nm -g "$daemon" >"$temporary/daemon-symbols.raw"
c++filt <"$temporary/daemon-symbols.raw" >"$temporary/daemon-symbols"
if grep -E '(^|[^[:alnum:]_])(fpga_load_rbf|user_io_|video_mode_adjust|scheduler_|offload_|Main|FakeHardware|FakeMmio|FakeSpi|FakeI2c|LinuxI2cTestOperations|CartProfile|BiosProfile|mister_test)($|[^[:alnum:]_])' \
	"$temporary/daemon-symbols" >/dev/null; then
	echo "executable contains Main mutation or fake symbols" >&2
	exit 1
fi

check_header_rebuilds() {
	local header=$1
	local label=$2
	local marker=$temporary/$label.marker
	local timestamp=$temporary/$label.timestamp
	local dependencies=$temporary/$label.dependencies
	touch -r "$root/$header" "$timestamp"
	find "$build/src" -type f -name '*.d' -exec grep -Fl -- "$header" {} + \
		| LC_ALL=C sort >"$dependencies"
	[[ -s "$dependencies" ]] || {
		echo "$label header has no recorded object dependencies" >&2
		exit 1
	}
	# GNU Make 3.81 compares modification times at one-second resolution.
	touch "$marker"
	sleep 1
	touch "$root/$header"
	make -C "$root" all >/dev/null
	touch -r "$timestamp" "$root/$header"
	while IFS= read -r dependency; do
		object=${dependency%.d}.o
		[[ -f "$object" && "$object" -nt "$marker" ]] || {
			echo "$label header did not rebuild dependent object: ${object#$root/}" >&2
			exit 1
		}
	done <"$dependencies"
}

check_header_rebuilds include/libmister-runtime/runtime.h public
check_header_rebuilds src/native/linux/mmio.hpp native

make -C "$root" clean >/dev/null
make -C "$root" all >/dev/null
first_hash=$(sha256sum "$archive" | cut -d' ' -f1)
make -C "$root" clean >/dev/null
make -C "$root" all >/dev/null
second_hash=$(sha256sum "$archive" | cut -d' ' -f1)
[[ "$first_hash" == "$second_hash" ]] || {
	echo "two clean archive builds are not deterministic" >&2
	printf 'first:  %s\nsecond: %s\n' "$first_hash" "$second_hash" >&2
	exit 1
}

printf 'active-tree checks passed; deterministic archive sha256: %s\n' "$second_hash"
