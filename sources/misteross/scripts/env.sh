#!/usr/bin/env bash

# Source this file to use only the repository-local OSS toolchain.
# It intentionally does not probe, locate, or export any proprietary FPGA tool.

_OPEN_MISTER_ENV_SCRIPT="${BASH_SOURCE[0]}"
OPEN_MISTER_ROOT="$(cd -- "$(dirname -- "$_OPEN_MISTER_ENV_SCRIPT")/.." && pwd -P)"

# The shared cache is selected explicitly by FES Python recipes.  A sourced
# shell environment cannot safely authenticate and export a complete immutable
# install, so fail before it silently falls back to the repository-local lane.
if [[ -n "${FES_TOOLCHAIN_CACHE_ROOT:-}" ]]; then
    printf '%s\n' 'misteross: shared toolchain cache is unsupported when sourcing scripts/env.sh; use an FES Python recipe or unset FES_TOOLCHAIN_CACHE_ROOT for the local lane.' >&2
    if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
        return 2
    fi
    exit 2
fi

TOOLCHAIN_ROOT="${FES_TOOLCHAIN_ROOT:-$OPEN_MISTER_ROOT/build/toolchain}"
TOOLCHAIN_INSTALL="$TOOLCHAIN_ROOT/install"

export OPEN_MISTER_ROOT TOOLCHAIN_ROOT TOOLCHAIN_INSTALL

if [[ -n "${PATH:-}" ]]; then
    PATH="$TOOLCHAIN_INSTALL/bin:$PATH"
else
    PATH="$TOOLCHAIN_INSTALL/bin"
fi
export PATH

if [[ -n "${LD_LIBRARY_PATH:-}" ]]; then
    LD_LIBRARY_PATH="$TOOLCHAIN_INSTALL/lib:$LD_LIBRARY_PATH"
else
    LD_LIBRARY_PATH="$TOOLCHAIN_INSTALL/lib"
fi
export LD_LIBRARY_PATH

if [[ -n "${PKG_CONFIG_PATH:-}" ]]; then
    PKG_CONFIG_PATH="$TOOLCHAIN_INSTALL/lib/pkgconfig:$PKG_CONFIG_PATH"
else
    PKG_CONFIG_PATH="$TOOLCHAIN_INSTALL/lib/pkgconfig"
fi
export PKG_CONFIG_PATH

if [[ -n "${CMAKE_PREFIX_PATH:-}" ]]; then
    CMAKE_PREFIX_PATH="$TOOLCHAIN_INSTALL:$CMAKE_PREFIX_PATH"
else
    CMAKE_PREFIX_PATH="$TOOLCHAIN_INSTALL"
fi
export CMAKE_PREFIX_PATH

unset _OPEN_MISTER_ENV_SCRIPT
