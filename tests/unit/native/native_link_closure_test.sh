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
