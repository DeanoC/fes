# RAM Tester corresponding source

`fes.ramtest` RTL is licensed under GPL-2.0-or-later. The full GPL version 2
text is in [COPYING](COPYING); recipients may choose version 2 or a later version.

The OSS 100 MHz package sealed for PR #381 is
`2c0a2507b6cb6c6c913c65064750bd1b32568b842151857af7b1d4386b899d93`.
Its exact corresponding source is FES commit
`5290d6462dbddebe170714ef2245d45dad814e07` in
https://github.com/DeanoC/fes (the archived standalone misteross repository is
not the source of this artifact). Download the complete source, including RTL,
shared definitions, build scripts and compiler locks, at:
https://github.com/DeanoC/fes/archive/5290d6462dbddebe170714ef2245d45dad814e07.tar.gz

Alternatively, clone that repository and check out the exact commit. From
`sources/misteross`, prepare the pinned toolchain with
`make toolchain-fes-ramtest`, then build with `make build-fes-ramtest-100`.
The source paths and instructions are in `cores/fes-ramtest/README.md`,
`scripts/build_fes_ramtest.py` and `toolchains/ramtest.lock` at that commit.

For every image assembly, `image/scripts/ramtest-notices.py` generates a new
source notice from the installed package's sealed `manifest.toml`, rather than
from the image's selected commit. The image installs that notice and COPYING
under `/usr/share/mister-runtime/core-notices/fes.ramtest/<package-id>/`,
adjacent to `core-packages/<package-id>/core.rbf`. The notices remain outside
the closed two-member package, so its identity and runtime admission are unchanged.
Later bitstreams carry their own exact producing revision in that generated
notice; the package above remains the reviewed artifact reference.
