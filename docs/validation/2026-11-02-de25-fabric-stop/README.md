# Source-bound Nano FPGA master stop callbacks

The cold DDR configuration provider now installs real stop/drain callbacks for
its fixed FPGA master inventory. Before closing control it checks the main
reader, the inherited DDR probe and all three held maintenance masters. Its
completed receipt remains guarded by cold ownership after control closes,
without further reads through the reset aperture. Generic data release and
runtime admission remain denied.

Base: `6996d4ece1db842dbe2a16ae07b27183c3ca89b2`.
Implementation and tested source: `45f32d5d3127b76d81c1947a7cd26495b33e6290`.
This record contains software/model and real source-build evidence. No board,
card, flash, FPGA image or device state was changed in this milestone.

## Contract and binding

The checked control map now requires a separate `fabric_stop` descriptor for
the DDR diagnostic. The producer-side decoder reuses the generated inventory
checker, verifies the maintenance reset wiring and generated inherited probe
against the same sealed source, and separately decodes its lightweight CPU
aperture at `0x20010100` / 128 bytes. Missing, duplicate, overlapping, changed
or foreign maps reject before native calls or header generation.

`nano_fabric_stop.c` checks two observations of the exact reader ABI/build,
fixed `0x95000000` / 8192-byte bus window and live maintenance-held bit. A
never-enabled or already-drained reader needs no command. An enabled reader
receives one DISABLE with its generation and a nonzero, increasing cookie;
held addresses and incomplete beats cannot supply its drain acknowledgement.
The inherited probe independently receives at most one CANCEL and must report
stable IDLE, equal accepted/completed bytes and no faults. A fresh readback of
both receipts follows. Any failure retires the work without command replay,
reset, memory release or bridge authority.

The strong cold provider binds only fixed register apertures and write
allowlists. EL3, cache/MMU state, primary CPU, secondary-release lock, owner,
reset history and bridge mask guard every access. Before the live match its
quiescent callback reports only the independently contained HPS producers;
all FPGA data paths stay held. After the completed stop it can report the full
inventory. The closed receipt requires the same cold owner, with no FPGA MMIO.
Failed identity/stop recovery may assert control reset while data stays held;
it does not manufacture an active DDR drain or permit a new attempt.

## Validation

| Check | Result and scope |
| --- | --- |
| Exact affected software plan | All ten commands pass on the committed source: [plan](software-plan.json), [results](software-result.json) |
| Actual C and both actual reader RTL models | 311 cases, including every one of 264 reader-read failure positions; idle/already-drained, held/accepted addresses, partial/missing responses, wrong acknowledgement, changed identity/maintenance, late restart and uncertain writes: [result](master-rtl.json) |
| Actual strong provider/owned dispatch | Fixed apertures, initial HPS-only bitmap, sixteen additional idle/failure cases, no replay and no read after control closure: [parent log](software-04.log) |
| Actual generated inventory | Positive generated DDR project and ten altered copies checked through the production inventory decoder: [results](generated-inventory.json). This generated project is not a compiled/sealed hardware acceptance artifact. |
| Native runtime/producer/firmware integration | Existing launch/return/failure and actual firmware/RTL regressions pass: [native](software-06.log), [producer](software-07.log), [firmware](software-08.log) |
| Pinned AArch64 DDR callback compile/link | Actual transformed Terasic BL31 builds and defines both new callback functions and both stop helpers. The enabling profile is a deliberately synthetic compiler fixture, not a sealed boot image: [result](ddr-enabled-compile.json) |
| Matched serial-only build regression | Native Go, entry, BL31 and U-Boot build together: [chain](boot-cross.json), [component receipts](serial-builds.json). This profile keeps the DDR callbacks disabled. |
| Shared consumers | `make check`: 35 consumers / 37 fixture copies match: [log](make-check.log). No shared wire definition changed. |

All register/memory responses in the active stop cases are models. Neither a
software drain receipt nor an AArch64 build establishes physical DDR mapping,
firewall policy, traffic attributes or hardware drain. The earlier
[SMMU serial observation](../2026-11-02-de25-smmu-observation/README.md) remains
limited to its sampled cold boot and grants no access here.

The next integration step is a matched DDR serial observer and sealed board
build, followed by addressing/firewall qualification at the point of use and
a bounded read/drain/reclose trial. The diagnostic's production admission gate
and raw on-chip-reader ABI guard remain closed; runtime core activation,
repeated launch/return and peripheral/UI hardware acceptance remain open.

Regular-file source/build outputs, successful models and development failures
are retained at `/home/deano/fes-retained/de25-fabric-stop-20261102/` with a
verified source bundle and fully read-back archive. No private claim or
credential file is included. File hashes are in [manifest.json](manifest.json).
