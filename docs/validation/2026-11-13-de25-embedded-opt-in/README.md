# Linux-free DE25 experiment publication

This change preserves the exploratory base behind
`FES_EXPERIMENTAL_DE25_EMBEDDED=1`. It does not select it for ordinary builds,
images or releases. The historical development branch is
`codex/de25-embedded-os` at `d580a60d93ad5dad87f0c311b762d0df2bd2beb5`.
The publication integrates that history with main at
`f2c2e82541a5ad448deb1acf66a31623fb786768` and promotes the latest retained
firmware source into `platform/embedded/firmware`, without generated boot
binaries or machine-local execution approvals.

The common code includes the injected Tenfoot loop/model backend, retained
package/ROM admission, native CPU/firmware/FPGA ownership hooks, FAT32/SD PIO,
USB keyboard/hub and Ethernet prototypes, matched boot preparation and
failure-contained launch/return models. Standalone C and RTOS experiments
remain for comparison. The normal Linux build retains its current programming
profiles and video-status contract; the native experiment uses separate
`de25_embedded` and `fes.fabric.embedded-computer` definitions.

## Evidence boundaries

The dated earlier records retain their exact source and artifact identities.
They include bounded physical ZX81/ZX80 launch/return, keyboard interaction,
visible launcher return, SD installation/reboot discovery and Ethernet-to-SD
readback. They are evidence for those artifacts, not physical acceptance of
this main-merged tree or general Linux feature parity.

The earlier source-retirement diagnostic used chain
`b8f89ef622f07cce3b5091f22d516f3f8e9a1c3de71b0ff7851e10373b74d647`
and FIT SHA-256
`2ba50ae7fd42f25c07990ab5b56943f8610e343522069ef6bae590e212faf5d0`.
On TRWU8CBL it completed 1,708,032 bytes in two posts/two acknowledgements,
with the complete SHA-384 source binding. Retirement then failed in phase 4
(command invalidation), first error -3, after three control writes and before
queue publication. The sampled phase lasted 109,542 us, beyond the existing
100 ms command limit; that limit is preserved. Source memory stayed retained
and activation/reuse were not authorized.

After restoring the exact original FIT, warm baseline stop command 47 failed
and the native runtime panicked with resources retained. The initial failure
summary attributed this to a missing fabric drain acknowledgement. Inspection
of the complete saved trace instead finds a successful independent firmware
reader drain and bridge closure immediately before that panic. The zero fabric
handshake flag is expected for the selected absent-handshake profile; it is not
evidence of a missing reader acknowledgement. The underlying native failure
is not preserved by the final generic panic and remains unproven.
A separate physical reset and baseline-only
restoration completed a fresh 51-command ZX81/ZX80 launch/return/Park cycle,
generation 5. This does not qualify warm recovery from the diagnostic.

Retained failure archive SHA-256:
`3909d7e25b4b523ee2f0e558c79e8a63aff4a8c627def1720ae7bccd314e4434`.
Separate baseline restoration archive SHA-256:
`89a2625a40dc045e4ba80349dd282ae107db5e9201e397d9b5e395dd1c35cbfe`.
These archives remain in the retained evidence store; generated images are
not shipped as source fixtures.

## Integrated source validation

At source `89c621377b73a4ed56da8ae36d9753dfceda7d31`, the Nano suite passes
810 tests with 13 toolchain/hardware-dependent skips. The board ROM/CPU/return
RTL model passes with synthetic externally streamed ROM data. Generated-file,
source and inventory checks pass. The private catalog host uses the current
HTTP API and passes its race test; installer catalog navigation follows the
current Settings sections.

The [Tenfoot cross-build](tenfoot-cross-build.json) uses the pinned TamaGo
compiler for ARM64 and binds all 438 selected source files. It uses the basic
probe profile and does not produce a matched selected-program core-loader boot
chain. Compilation exercises the merged launcher dependencies without running
them on a device.

The [retirement snapshot model](../2026-11-13-de25-command-snapshot/README.md)
reduces command-phase supplier reads while preserving all deadlines and
pre-publication checks. Portable AArch64 compilation also passes. Physical
retirement and warm menu-stop acceptance remain open. A subsequent
[complete matched candidate](../2026-11-13-de25-matched-command-snapshot/README.md)
now passes native SDK, entry, BL31, U-Boot/SPL, FPGA composition and fresh
native file admission. Its retained archive readback passes. The cached native
failure cause is preserved by a separate source repair and regression test.
The [subsequent physical command-snapshot trial](../2026-11-13-de25-command-snapshot-physical/README.md)
completes full input transfer but again fails retirement in phase 4. The exact
original FIT is restored and read back. Warm baseline startup then fails with a
DWC2 USB packet error before any App command; its complete cause is truncated
by the collector. Independent firmware reader drain and bridge closure succeed.
There is no final Park or accepted baseline restoration for this attempt.
After the operator's separate physical reset, a
[baseline-only restoration](../2026-11-13-de25-command-snapshot-physical/README.md#separate-post-reset-baseline-restoration)
now independently passes the original 51-command launch/return/Park cycle.
The failed candidate remains unqualified.

## Integration refresh

Main at `daac138c7c9eb5d40009314fd8dbd797dffd2c3f` is merged to repair the
actual merge-build failure caused by main's removal of the obsolete application
firmware mailbox. The experimental application consumer is regenerated from
the current shared definition; the removed mailbox is not reintroduced.
The parent software CI runner now installs mtools and Verilator so the real
FAT/PIO and RTL integration tests run instead of failing on missing tools.
This source refresh does not inherit the preceding candidate's native image
or hardware acceptance. The dated physical report keeps its original source
and artifact identity.

The [refresh checks](integration-refresh.json) pass: full native Go module race
suite (229.1 seconds, valid cached package results reused), focused Tenfoot,
rooms and private-host race suites, nine real FAT/PIO/RTL regression tests,
156 affected-policy/workflow checks and generated-source consistency.
The [current paired C++/Go corpus](compatibility-current-main.json) passes all
691 cases after removal of the mailbox vectors. The
[current AArch64 Tenfoot cross-build](tenfoot-current-main.json) passes with
its complete selected source map. These source maps include the tested patch
on merge head `bbd4a6d0ecc9764c6dbccad8a75ca04cf1c63d63` and are checked
against the final files. This remains a basic-probe cross-build, not a newly
matched boot chain or hardware acceptance.

CI run 43409688178 failed before this refresh: consistency and host build on
the stale generated consumer, parent software on missing mtools/Verilator, and
one producer process-cleanup test while reading a newly created empty PID file.
All nine tests in that producer module pass locally without modifying it.
The previous CI failure remains recorded; a fresh CI result is required.

The fresh CI run 43419138591 passes generated-source consistency, then exposes
five address literals in two native test fixtures defaulting to 32-bit `int`
during ARM dead-code analysis. Those values now explicitly use `uint64`, with
unchanged JSON address bytes. This changes tests only; the production source
validated by the build and compatibility checks above is unchanged.
The [follow-up checks](arm-fixture-checks.json) pass: complete dead-code/U1000
analysis for Linux amd64 and ARM (104.1 seconds), ARM native-fabric test
compilation, and the two exact fixture mutation/JSON regressions under the race
detector. An earlier broad `TestMenu` selection exceeded its host wrapper's
90-second observation cap; its failed log is retained and is not a passing
result. The narrower checks exercise the changed fixture construction directly.

## Remaining work

Resolve source retirement and the post-diagnostic warm USB failure before
attempting ordinary package activation through this experimental path. Capture
the complete native error, rebuild the affected matched chain and qualify its
launch/return and recovery on TRWU8CBL. Broader core/device support, controller
and Bluetooth support, durable updates, multicore startup, signed secure boot
and appliance-level failure recovery remain incomplete. Neither passing host
tests nor source-bound receipts establish those properties.

Publication itself does not program or reboot a board. Following the separately
authorized physical trial and post-reset restoration, the last verified board
state is the original native baseline at final Park. All owned host handles are
closed. The test card remains installed. Factory media, QSPI and the bottom kit
are outside this change.
