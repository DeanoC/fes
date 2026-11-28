#!/usr/bin/env -S python3 -I
"""Start the keyed shared-tool build in a clean Bash environment.

The cache coordinator is not a build recipe.  In particular, its shell
variables, exported functions and BASH_ENV must not execute in the build
shell before the driver's own environment checks.  Keep only the inputs
needed to recompute the planned request; the driver checks the key and lock
again before building and publishes under that same lock.
"""
import os
import sys
from pathlib import Path

ALLOW = frozenset({
    "PATH", "HOME", "USER", "LOGNAME", "TMPDIR", "SHELL", "TERM", "LANG", "LC_ALL", "TZ",
    "JOBS", "PYTHON", "FES_TOOLCHAIN_ROOT", "FES_TOOLCHAIN_LOCKFILE", "FES_TOOLCHAIN_CACHE_ROOT",
    "FES_TOOLCHAIN_GPU_ROUTER", "FES_TOOLCHAIN_HIP_ARCHITECTURES", "FES_TOOLCHAIN_CUDA_ARCHITECTURES",
    "FES_TOOLCHAIN_CACHE_MAX_GB", "ROCM_PATH", "HIPCC", "CUDA_HOME", "CUDACXX", "CUDA_PATH",
})

def main():
    environment = {name: value for name, value in os.environ.items() if name in ALLOW}
    driver = Path(__file__).with_suffix('.sh')
    os.execve('/bin/bash', ['bash', '--noprofile', '--norc', str(driver), *sys.argv[1:]], environment)

if __name__ == '__main__':
    main()
