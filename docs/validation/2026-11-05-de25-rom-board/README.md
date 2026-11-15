# Held ROM endpoint and DE25 producer diagnostic

The ROM-capable ZX81 FPGA producer built successfully from `c60747191` with
four Quartus workers and the frozen board source `79d616e85`. Its sealed
diagnostic build ID is `cb63a0a04c6ca842320a760df7e3ef30`; package ID is
`4bcf0e0a3b79335502afa8c2388284ec8180a6d3894d77bae3e7f97cdec6e31b`.
No image was loaded onto a board during this milestone.

The explicit ROM variant connects a registered 128-byte Avalon slave at
peripheral-relative offset `0x10500` to the real computer's held ROM conduit.
Generated connections, interface widths, shared 100 MHz clock/reset, image
size, source closure and common sealed build identity were checked. It adds
no DMA master. This offset alone does not establish a native CPU aperture.
The variant has a distinct diagnostic profile and required ROM interface;
existing native admission continues to reject it.

The model ran the actual C transport, injected secure-provider/control hooks,
registered Avalon adapter, computer mailbox and Z80 machine for both 4 KiB
and 8 KiB images. A synthetic ROM executed a RAM marker write. Hold/return,
reset retirement, premature Run, corrupted data, lost ownership and malformed
transactions kept the CPU contained or retired the transfer as required.
The model's 68 recorded inputs match the implementation commit. Its digest
oracle and platform are injected; it establishes no EL3 execution, physical
ROM load, display behavior or ordinary package admission.

Selected software checks passed. Parent coverage comprises exactly 1,360
selected cases run in 20 bounded groups: 1,357 passed and 3 skipped. The
aggregate parent command itself was not executed. The FPGA Python suite ran
1,580 cases with 75 skips; default ZX81 simulation, native integration,
firmware/control and generated-consumer checks also passed. The copied sealed
project passed the producer verifier, its 25 DDR reader model cases, three
identity/window rejection cases and inherited probe-stop checks. All 1,862
project files were independently hashed against the build host.

Three preparation/build attempts are retained. The first stopped before
Quartus because the initial source bundle lacked the nonancestor frozen board
commit. An explicit dependency bundle corrected source transfer. The two-worker
build then hit the unchanged 120-second placement-task limit after placement
reported success. The four-worker resource variant completed all ten standard
tasks under the same per-task limits, without changing clocks or timing/seal
checks. Processor count is included in its distinct build identity.

Quartus reports timing requirements met, with minimum reported setup slack
1.965 ns and hold slack 0.000 ns. The retained signoff report still lists six
Design Assistant rule violations, including missing input/output delays and
reset/constraint findings. The producer retains `timing_acceptance` and boot
firmware compatibility as `not-qualified`, and hardware acceptance as
`not-run`. These reports are diagnostic evidence, not whole-board signoff.

Sources, models, software logs, failed preparation/build evidence, complete
candidate project, HPS-first/core pair and sealed package are retained at
`/home/deano/fes-retained/de25-rom-board-20261105-r1/`. Both source bundles were
restored together into a fresh bare repository and passed strict Git fsck.
The separate board dependency bundle is required to reproduce the producer;
the current-HEAD bundle alone does not contain that nonancestor board commit.

Next, derive the native ROM map from this generated candidate, stage the retained
ROM with the version-two program binding, and build a matched firmware/runtime
chain. Ownership, bounds and failed-load containment need model verification
before one bounded physical ROM test. Native display/Ethernet, ordinary package
and media support, and full DE10-equivalent core loading remain open. The earlier
physical controller test remains evidence for its previous matched image only.
