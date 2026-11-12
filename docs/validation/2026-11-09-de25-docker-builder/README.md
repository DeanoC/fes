# DE25 SDK image-store addressing

Build-host repair for #1580, based on518089bb9. The kernel, SDK archive and
compiler config pin remain unchanged. No board, deployed runtime, firmware,
FPGA source, shared contract or Docker daemon setting changes.

Docker26 on Powerboat addresses the locked image by config digest
ea200655b92ab1211bc8b326c6e799815f1d648b2d5e5a6c982f597cb2b9650d.
Docker29 on the preferred3090 host addresses that same content by OCI manifest
5105c04336c362cf47cf7ae4b83d079d5f9b848e08e5f53c38ad2cd0be94bf8a.
The earlier read-only export independently authenticated the raw config and
all seven uncompressed layer payloads (993422848bytes). The committed1308-byte
manifest reproduces that exact exported manifest digest and binds the config
and ordered layers. Address metadata lives separately from the compiler lock,
so existing kernel/SDK receipts retain their original content identity.

The resolver considers only the config and this authenticated manifest address.
It checks Linux/amd64, the complete ordered rootfs layers and, for the manifest
address, its descriptor type/digest/size. Missing, malformed, substituted,
wrong-platform, changed-layer and mutable-tag inputs fail before workspace
creation. Build receipts keep content provenance separate from the actual
execution reference. Producer recipe hashing includes the new inputs while
reading older authenticated Git archives as data; it never executes old code.

The first actual Docker29 writeability gate exposes a second issue: the pinned
image's original UID1000 differs from the3090 account's UID1001. An initial
empty-directory ownership fix alone still fails at the next mount. Both failures
are retained. The final workspace initialization changes only an empty volume's
top-level owner. Every subsequent workspace mount uses `volume-nocopy`, avoiding
another image-metadata copy over an empty directory. A populated wrong-owner
volume is refused without recursive ownership changes.

Actual corrected gates pass on Docker26 and Docker29. The Docker29 negative
control populates its own private volume, changes that directory's owner, then
observes refusal with both owner and file bytes preserved. All diagnostic
containers finish and the three failed/corrected probe volumes are removed.
No compiler runs in these address/workspace probes. Seventeen focused tests
cover resolver rejection, both addressing forms, runtime receipt/command
separation, immutable image/named-volume validation and existing-volume/failed-
build cleanup. Full affected checks and real3090 source-workflow qualification
remain separate from this initial gate evidence.

Private receipts and retained failed controls:
`/data/fes-de25-main-integration-20261105/partial-start-investigation-20261108/docker29-builder-20261109/`.
In particular, remote-gate.json and remote-workspace-gate.json remain failed;
remote-nocopy-gate.json and local-gate.json pass. The successful gate does not
close the issue's real compiler/source-workflow requirement or any hardware
failed-upload/recovery requirement.
