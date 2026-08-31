#!/bin/bash
# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: native_link_closure_test.sh ROOT" >&2
	exit 2
fi

root=$1
build_dir="$root/build"
archive="$build_dir/libmister-runtime.a"
nm_tool=${NM:-nm}
cxxfilt_tool=${CXXFILT:-c++filt}

make -C "$root" clean
make -C "$root" all
make -C "$root" archive-audit

[[ -s "$archive" ]] || {
	echo "canonical runtime archive is missing or empty: $archive" >&2
	exit 1
}

archive_list="$(find "$build_dir" -type f -name '*.a' -printf '%P\n' | LC_ALL=C sort)"
[[ "$archive_list" == "libmister-runtime.a" ]] || {
	echo "canonical build did not produce exactly one archive" >&2
	printf '%s\n' "$archive_list" >&2
	exit 1
}

archive_members="$(ar t "$archive" | LC_ALL=C sort)"
object_members="$(find "$build_dir/src" -type f -name '*.o' -printf '%f\n' | LC_ALL=C sort)"
[[ "$archive_members" == "$object_members" ]] || {
	echo "canonical archive members differ from production objects" >&2
	diff -u <(printf '%s\n' "$object_members") \
		<(printf '%s\n' "$archive_members") >&2 || true
	exit 1
}

compiled_sources=""
while IFS= read -r object; do
	relative_object=${object#"$build_dir/"}
	source="$root/${relative_object%.o}.cpp"
	[[ -f "$source" ]] || {
		echo "production object has no source: $relative_object" >&2
		exit 1
	}
	case "${source#"$root/"}" in
		src/runtime.cpp|src/profile.cpp|src/native/*.cpp|src/native/linux/*.cpp|src/linux/production_hardware.cpp)
			;;
		*)
			echo "canonical archive contains a non-production source: ${source#"$root/"}" >&2
			exit 1
			;;
	esac
	compiled_sources+="${source#"$root/"}"$'\n'
done < <(find "$build_dir/src" -type f -name '*.o' | LC_ALL=C sort)
compiled_source_names="$(printf '%s' "$compiled_sources" | sed 's|.*/||')"

for forbidden_source in \
	main.cpp mister_runtime_legacy.cpp mister_runtime_linux_v2.cpp \
	scheduler.cpp offload.cpp input.cpp user_io.cpp fpga_io.cpp; do
	if grep -Fx "$forbidden_source" <<<"$compiled_source_names" >/dev/null; then
		echo "canonical archive contains forbidden compatibility source: $forbidden_source" >&2
		exit 1
	fi
done

for required_member in \
	native_core_artifact_adapter.o fpga_manager.o mmio.o spi.o; do
	grep -Fx "$required_member" <<<"$archive_members" >/dev/null || {
		echo "canonical archive omits required native member: $required_member" >&2
		exit 1
	}
done

raw_owners="$(
	while IFS= read -r object; do
		if "$nm_tool" -u "$object" | \
			grep -E '(^|[[:space:]])_?(close|ioctl|mmap|munmap|open|open64|openat|pread|pread64|pwrite|pwrite64)(@.*)?$' \
			>/dev/null; then
			printf '%s\n' "${object#"$build_dir/"}"
		fi
	done < <(find "$build_dir/src" -type f -name '*.o' | LC_ALL=C sort)
)"
expected_raw_owners=$'src/native/linux/mmio.o\nsrc/native/linux/native_core_artifact_adapter.o\nsrc/native/linux/native_input_adapter.o\nsrc/native/linux/spi.o'
[[ "$raw_owners" == "$expected_raw_owners" ]] || {
	echo "raw I/O ownership differs from the canonical native boundary" >&2
	diff -u <(printf '%s\n' "$expected_raw_owners") \
		<(printf '%s\n' "$raw_owners") >&2 || true
	exit 1
}

undefined_symbols="$("$nm_tool" -u "$archive" | "$cxxfilt_tool")"
if grep -E '(^|[^[:alnum:]_])(fpga_load_rbf|user_io_|scheduler_|offload_|reboot|reexec|execl|system)($|[^[:alnum:]_])' \
	<<<"$undefined_symbols" >/dev/null; then
	echo "canonical archive references compatibility mutation authority" >&2
	exit 1
fi

if "$nm_tool" -g "$archive" | "$cxxfilt_tool" | \
	grep -E 'Native(Mmio|CoreProtocolIo)(Test|LinuxOperations)|PosixOperations' \
	>/dev/null; then
	echo "canonical archive exports raw MMIO/SPI operations" >&2
	exit 1
fi

dependency_file="$build_dir/src/native/linux/native_core_artifact_adapter.d"
grep -F 'src/native/linux/native_core_artifact_adapter.hpp' "$dependency_file" \
	>/dev/null || {
		echo "canonical dependency closure omits the core artifact adapter" >&2
		exit 1
}
