# DE25 retained SD-package configuration preparation, 2026-11-02

The native SD-to-FPGA path is implemented, software-validated and cross-built.
The bounded hardware attempt **stopped before native launch**: the second
upload found live U-Boot reservations over the fixed scratch RAM and correctly
refused reuse. The native firmware candidate was never programmed or executed.
This record supplies no new physical package-loading acceptance.

Native source: `12a0d0caf487f3984b49b5af6aed629868a41572`. Candidate boot: `3cd79bb0c7e60ae1`.
See the [observations](evidence.json), [two upload receipts](upload-package.json)
and [independent cleanup audit](cleanup.json). The earlier
[built-in-payload cold load](../2026-11-02-de25-large-blocks/README.md) remains
a separate passing source/artifact; its hardware evidence does not transfer.

## Implemented path

`fpgaload` now requires the exact controlled SD profile and a sealed HPS-first
project. The SDK emits a separate canonical `package.fcore`; the
[built image](package-artifact.json) contains no RBF. It selects
`FES/PKGTEST.BIN` by the compiled package ID, retains it with the actual package
validator and read-only FAT/store path, and contains SD before extracting RAM
bytes into the boot-pinned configuration arena.

`NewStoredConfigurationTransfer` compares package, build, target, profile, ABI,
payload size and hash against the compiled source. It compares the contained
SD receipt with fresh secure owner/generation observations before reservation,
then rechecks that handoff immediately before preparation. Resuming storage,
changed ownership, malformed source, reservation errors or changed bytes cannot
reach START. Failed and uncertain transfers retain their bytes and original
error; no filesystem access resumes behind configuration.

The secure gate still initializes at generation 1, but preparation can capture
the current never-enabled, closed generation after secure device containment.
One SD resume/prepare/stop cycle reaches generation 4. Every configuration call
then requires that captured generation. Active leases, stale requests, changed
owner/context, open bridges and post-preparation generation changes reject.
The identity probe opens control only and recloses at generation 6. Neither
this version-1.6 eligibility extension nor package recognition grants general
runtime compatibility, active-master drain, data release or memory reclaim.
The arena remains uncached, 8 MiB at `0x94800000`; transfer blocks remain
1,048,512 bytes and the native transfer deadline remains 20 seconds.

## Validation

All twelve [affected groups](software.json) pass on the exact source, including
parent regressions, generated consistency, SD lease/PIO/FAT/store tests, native
Tenfoot launch/return/failure integration, contract/schema fixtures and the
actual firmware/identity/drain RTL integrations. Generated consumers match;
all six [matched build stages](composition.json) pass. The
[linked audit](linked-firmware.json) checks eleven strong symbols, combined SD
and configuration selection, and the unchanged 2,912-word scalar SHA transform.

The actual strong secure provider fixture now executes SD resume/prepare/stop,
configuration, readiness and control identity/reclose in one image, with
independent modeled register/PHY events. Seven joined cases cover success,
active SD, stale generation, attempted resume, lost owner, generation drift and
retirement at generation 4. Go integration uses real canonical archive
validation, retained storage and the shared transfer engine across fifteen
success/failure cases. The identity fixture covers 57 cases, including the
contained generation and wrap rejection. Stored trace checks require the SD
containment record and exact generation-4/6 progression. Host boot/cleanup
helpers pass 36/32 cases; retirement capture checks pass 56 cases. These are
software/model checks, not hardware acceptance.

## Stopped upload and next bounded attempt

The [boot upload](upload-boot.json) wrote and independently read back only
`FES/UPLOAD.BIN`, preserving protected boot files and recovery inputs. On the
same recovery boot, U-Boot retained reservations at `0x82000000`, `0x82800000`
and `0x83000000`. The second [upload](upload-package.json) issued only version
and memory-map observations, then stopped before download or any package SD
write. No `fesboot`, native startup, SD-package consumption or FPGA transfer
occurred. The guard was not relaxed and no live reservation was cleared.

The next attempt must use the established fresh qualified recovery boot before
each upload, retaining this first failure. No source rebuild or repeat of the
passing software campaign is needed for that host orchestration repair. The
current SD driver uses its conservative 400 kHz, one-bit PIO mode; a roughly
2 MiB package needs a separate bounded storage-observation stage. Keep that
stage distinct from the existing 20-second configuration deadline rather than
misclassifying storage time as a configuration timeout. Actual read throughput
and the complete joined path remain hardware unqualified.

The [cleanup audit](cleanup.json) confirms the original 15 MHz clock readback,
restored USB permissions and a free UART. The board is parked in the known
recovery loader with the spare card inserted. The factory card remains safe,
and the exclusive top-board claim is retained. The
[matched-build archive](build-retention.json) retains all frozen components;
every member was independently read back. Runtime admission, repeated physical
launch/return, queue turnover, active drain and the remaining peripheral/UI
work still need qualification.
