#!/usr/bin/env bash
set -euo pipefail

# Simulation baseline, independent of the qualified synthesis toolchain locks.
revision=8ff77e9d47351b0a59114929880687839a51840b # Verilator v5.032
prefix=$(realpath -m "${1:?usage: ci_verilator.sh INSTALL_PREFIX BUILD_DIRECTORY}")
build_directory=$(realpath -m "${2:?usage: ci_verilator.sh INSTALL_PREFIX BUILD_DIRECTORY}")
if [[ -f "$prefix/source-revision" ]]; then
    if [[ $(cat "$prefix/source-revision") != "$revision" ]]; then
        echo 'Cached Verilator revision differs; choose a fresh installation prefix.' >&2
        exit 1
    fi
    "$prefix/bin/verilator" --version
    exit 0
fi

mkdir -p "$build_directory"
git init -q "$build_directory/source"
git -C "$build_directory/source" fetch --depth=1 --no-tags https://github.com/verilator/verilator.git "$revision"
git -C "$build_directory/source" checkout --detach FETCH_HEAD
[[ $(git -C "$build_directory/source" rev-parse HEAD) == "$revision" ]]
if [[ -n $(git -C "$build_directory/source" status --porcelain --untracked-files=no) ]]; then
    echo 'Verilator source has tracked modifications; choose a fresh build directory.' >&2
    exit 1
fi
cd "$build_directory/source"
autoconf
./configure --prefix="$prefix" --disable-tcmalloc
make -j"${FES_CI_JOBS:-2}"
make install
"$prefix/bin/verilator" --version
printf '%s\n' "$revision" > "$prefix/source-revision"
