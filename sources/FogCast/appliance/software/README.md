# Kit software bundles

This package defines immutable kit userspace artifacts, independent of appliance
images and FPGA core packages. A release requires `bin/mister-agent`,
`bin/mister-runtime` and `bin/fogcast-tenfoot`, and may hold application assets
beneath `share/`. Releases also carry the `bin/fogcast-kit` stub for bases whose
supervisor still runs `fogcast-kit --print-kit-ui`. It does not contain kernel, drivers, system libraries,
mutable configuration or user data.

The canonical manifest includes board, ARM architecture, base ABI contract,
provided interface versions, source provenance, and each file's path, mode,
size and SHA-256. Provided interfaces include `menu.<interface>` keys with the
`major.minor` versions the bundle's runtime admits in an idle menu package.
`Compatibility.Requires` holds the menu interfaces a base image needs. A release
accepts a requirement at the same major and at least the required minor. When
it does not, or its base contract differs, `Compatible` returns an error
wrapping `ErrIncompatible`, so the supervisor can hold that release back. Its SHA-256 is the release ID. Source revisions are provenance,
not a same-commit compatibility gate. The deterministic uncompressed tar starts
with `manifest.json`; all following members must match it exactly. Links,
unlisted files, duplicate paths and oversized archives are rejected.

Canonical JSON is at most 1 MiB. A payload can satisfy the file-count and byte
limits and still encode a larger manifest; packing rejects that payload, and
readers reject a bundle that contains it. Readers also compare the original
manifest bytes with that canonical encoding. Whitespace, reordered fields and
reordered file records are a different byte string and are rejected, in both
archives and installed directories.

`fes-kit-bundle pack --manifest metadata.json --root payload --output kit.tar`
constructs a bundle; `fes-kit-bundle inspect --bundle kit.tar` verifies it. File
metadata is populated from the payload during packing. Executables are sealed
0555 and other files 0444.

`OpenPersistent` verifies the mounted FESDATA3 ext4 volume before opening
`/run/fogcast/fesdata3/software-releases`. `New` provides a POSIX store for tooling
and tests. The store root, `releases`, `incoming` and the lock file must be
owner-only (`0700` directories and a `0600` lock). Both open paths refuse a
group- or world-accessible store, and later reads refuse a store whose
permissions change after it was opened.

Opening the store takes the process-shared lock and removes recognizable
incomplete staging directories (`incoming/bundle-` plus a decimal suffix) left
by an interrupted install, then syncs `incoming`. Published releases are not
removed. Installation verifies all bytes, syncs files and directories, then
publishes atomically under that same lock. Repeated identical installation
is idempotent. Inventory authenticates canonical manifests and their release IDs,
without reading payloads. It describes installed releases, not payload readiness.
`Read`, `Path`, and activation still reverify all payload bytes and pinned storage
identities before use. Installation
alone never selects a release, restarts services or modifies user data.

Activation and its durable journal are implemented separately by the base-image
supervisor. This package deliberately provides no automatic selection and does
not delete published releases.
