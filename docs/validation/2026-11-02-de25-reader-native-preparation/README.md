# Native SD-to-FPGA reader candidate, 2026-11-02

The native loader now selects the owned secure reader operation after its
contained SD-package configuration. Its strict observer validates the complete
reader recipe, and all eight affected software groups plus six matched build
stages pass. The first hardware attempt **stopped before native launch**:
programming the second known recovery image failed with a JTAG chain/rescan
error. The new native SOF was never programmed and no package upload occurred.
This record supplies no new physical reader or core-load acceptance.

Source: `b08fc1143ddd3e6909ad31a3aced3d89bba31429`. Candidate boot: `2ad9e8e05267cc2c`.
See the [outcome](evidence.json), [matched composition](composition.json),
[experiment](experiment.json), [programmer error](programmer-failure.log) and
[independent cleanup audit](cleanup.json). The earlier
[passing SD-package configuration](../2026-11-02-de25-stored-configuration/README.md)
remains separate evidence for its source/artifacts and identity-only operation.

## Native path and software checks

`fpgaload` retains the real canonical SD archive, contains SD at generation 4,
programs the same sealed 2,027,520-byte core, verifies unchanged closed source
ownership and invokes `ProbeReader`. After the secure reader-specific result it
reports both reclosed identity and `FES_NATIVE_FPGA_READER PASS`. No identity-only
fallback remains in that loader. The SDK still emits the package separately;
the [ELF check](package-artifact.json) finds the reader marker and no built-in RBF.
The [linked firmware audit](linked-firmware.json) checks twelve strong symbols,
the reader write/progress markers and the unchanged 2,912-word pinned digest
transform. The FPGA payload and transfer mapping are unchanged.

`decode_native_reader_configuration` combines the existing complete
SPL/CPU/SD/transfer/readiness/identity checks with paired reader accesses and
writes. It requires the held address across disable, a still-pending partial
burst, exact final generation/cookie ACK, two final identity/state snapshots,
control reclose and the final native receipt. State-read sets may poll within
the firmware's 128-set bound. Missing, duplicated, reordered, changed, malformed,
partial or excessive observations never complete. The observer also decodes
the actual strong-provider/SD/configuration/Avalon-RTL fixture, including its
seven failure cases. This responder is on chip; it proves no DDR/scanout or
other-master drain.

All eight [affected software groups](software.json) pass on the exact source,
including parent regressions, generated consistency, the existing Tenfoot
launch/return/failure integration and producer/native control checks. The
unchanged preceding firmware/RTL contract checks are retained in the
[reader preparation](../2026-11-02-de25-reader-cycle-preparation/README.md).
The current native and orchestration helpers pass 46 and 35 cases, including
reader omissions, replay, stale closure, early ACK, malformed/late records,
storage timeout and both recovery failure positions. The unchanged retirement
capture checks pass 56 cases. No native firmware or storage deadline was raised.

## Stopped hardware attempt and next action

The first qualified recovery boot passed. The [boot FIT upload](upload-boot.json)
wrote only `FES/UPLOAD.BIN`, independently read it back and compared its hash
and bytes while preserving protected boot files and recovery metadata. Before
the separate package upload, the second volatile recovery programming returned
`JTAG chain broken` and a rescan error. The attempt stopped without package
download/write, new native SOF programming, `fesboot` or reader activity.
Its fuse, programmer output, empty second recovery console and receipts remain
preserved. No failed native proof was retried or bypassed.

The cleanup audit checks the original 15 MHz clock readback, restored USB
permissions and free UART. Two subsequent read-only exact-cable queries
successfully identify `4362C0DD`; their [receipt](cable-readback.json) and
[raw output](cable-readback-1.log) establish current chain response, not recovery
boot or FPGA state. The board's boot state remains unverified after the failed
programming. Its spare card remains inserted, factory card remains safe outside,
and the exclusive top-board claim is retained. No factory restore, QSPI write
or other-kit operation occurred.

Next perform one bounded known recovery boot, recheck the already verified FIT,
and resume package upload/readback and the native reader test using fresh
one-attempt records. Reuse these exact built bytes; another software campaign
or rebuild is unnecessary for transport recovery. A second failure on this
source stops further hardware attempts while the transport is investigated.
Runtime admission, active data release, repeated core loads, full physical
launch/return/failure acceptance and USB/network/display integration remain open.

The [matched-build archive](build-retention.json) retains every compiled input
and output with each member read back. Helpers, all development failures and
checks, the stopped attempt, cable diagnostics and this committed evidence are
retained under `/home/deano/fes-retained/de25-reader-native-20261102` without
claim credentials.
