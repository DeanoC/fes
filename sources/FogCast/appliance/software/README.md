# Kit software bundles

This package defines immutable kit userspace artifacts, independent of appliance
images and FPGA core packages. A release contains `bin/mister-agent`,
`bin/mister-runtime`, `bin/fogcast-kit`, `bin/fogcast-tenfoot` and any application
assets beneath `share/`. It does not contain kernel, drivers, system libraries,
mutable configuration or user data.

The canonical manifest includes board, ARM architecture, base ABI contract,
provided interface versions, source provenance, and each file's path, mode,
size and SHA-256. Its SHA-256 is the release ID. Source revisions are provenance,
not a same-commit compatibility gate. The deterministic uncompressed tar starts
with `manifest.json`; all following members must match it exactly. Links,
unlisted files, duplicate paths and oversized archives are rejected.

`fes-kit-bundle pack --manifest metadata.json --root payload --output kit.tar`
constructs a bundle; `fes-kit-bundle inspect --bundle kit.tar` verifies it. File
metadata is populated from the payload during packing. Executables are sealed
0555 and other files 0444.

`OpenPersistent` verifies the mounted FESDATA3 ext4 volume before opening
`/run/fogcast/fesdata3/software-releases`. `New` provides a POSIX store for tooling
and tests. Installation verifies all bytes, syncs files and directories, then
publishes atomically under a process-shared lock. Repeated identical installation
is idempotent. Reads reverify bytes and pinned storage identities. Installation
alone never selects a release, restarts services or modifies user data.

Activation and its durable journal are implemented separately by the base-image
supervisor. This package deliberately provides no automatic selection or cleanup.
