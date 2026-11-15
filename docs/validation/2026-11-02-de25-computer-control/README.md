# Native computer control against the captured DE25 ZX81 design

The native Go computer driver now checks the selected build identity before
reset/run, HID input or tape commands. Source is
`12cba59dbfd692c3c6bf0d9978cf3554aa294d5d`, based on
`ae599d8d85b418980eeaebae32388444409e491f`. This record changes documentation
only. The [driver contract](../../../platform/embedded/tamago/computer/README.md)
defines its injected platform boundary and bounded failure behavior.

The [captured RTL check](captured-rtl-result.json) compiles the actual Avalon
adapter, mailbox, Z80, machine, keyboard, RAM wrappers and ROM from the sealed
project at `out/nano-ddr-matched-20261102/sealed-project`. Its package is
`07b2d01003dc92e4993ba7f72761444ae801a1a005f87e0a1b65f9aad9dfeb7d`, build
`d2435ab5d8221892162a1044d81dfb76`. The selected board producer is revision
`79d616e85e19d65f0d645b67a85ffe7e142dcdfc`. The fabric definition is imported
unchanged from that revision and checked against its captured C++ constants.
Shared definitions generate both native Go consumers; no new emitter is added.

All eight Go test groups and their nested cases pass in the
[model log](native-control.log), including the explicit RTL tests. The driver
releases execution, observes real Z80 address activity through a host-only
probe, delivers HID rows and verifies every tape RAM byte for 1025-, 1- and
16375-byte uploads. Replacement and eject preserve execution. Hold clears input
and stops address activity; release starts it again. A wrong build never gets
an execution command. A later hardware reset poisons the transport and stops
the model CPU. Injected cases cover each identity word, lost submission/reads,
ACK timeout/rejection, reserved/faulted state, partial keyboard failure,
cancellation, backwards time, deadlines and serialization. Uncertain commands
are issued once and retire the handle without a neutralization retry.

The [native library check](native-library-result.json) selects a real
`tamago arm64` object with the pinned TamaGo compiler. It is a library compile,
not a linked native boot image. The [address-route observation](address-route-observation.json)
derives the distinct computer port at `0x20010300`, span 32, from the same
checked generated lightweight bridge hierarchy. This is source evidence;
physical access to that aperture is not qualified.

## Wider software checks

The [verification summary](verification-summary.json) retains the distinction
between completed leaf checks and a failed aggregate command. All 1276 selected
parent tests complete in three intact-module partitions bounded at 300 seconds
each, with two skips. The host checks, native driver race checks, contract
schema/fixture suite and all 1577 FPGA software tests pass. `make check` matches
37 generated consumers and 37 fixture copies.

The [original runtime aggregate](runtime-aggregate-result.json) reaches its
unchanged 300-second bound after the functional, incremental-build and version
checks pass, while preparing `active-tree-test`. It remains failed in the
[selected software report](software-result.json). The remaining
`active-tree-test` and `hardware-support-truth-test` targets then pass in a
[separate bounded continuation](runtime-tail-result.json), taking 125.657 seconds.
The aggregate is not relabelled passed. The two later independent plan scopes
run separately and pass. Logs, exact selection/partition coverage, child
receipts and adapters remain in `out/nano-computer-software-20261102`.

The first model-check attempt stops because its isolated compiler environment
has no Go module cache. That failure remains in
`out/nano-computer-control-20261102`. The corrected v2 model run passes. The v3
run adds distinct snapshot-change versus bus-error handling and partial-input
and active-reset checks; its source hashes match every corresponding Git blob
at `12cba59dbfd692c3c6bf0d9978cf3554aa294d5d`. Its complete local model,
selected RTL, object and receipts remain in `out/nano-computer-control-v3-20261102`.

## Hardware boundary and next integration

This work performs no hardware access, FPGA rebuild or board-image change.
The earlier [fixed DDR read/drain result](../2026-11-02-de25-contained-console/README.md)
remains the physical baseline. The new control driver has host simulation and
native library evidence only. The captured memory wrappers use their simulation
branches; HDMI timing, display and physical execution are not accepted here.

Next, bind the computer aperture and its lifetime to the native boot/platform
ownership path, prepare the matched boot chain and supply an independent
observation of core execution on TRWU8CBL. A GP ACK alone is insufficient.
Ordinary runtime package admission remains denied by the diagnostic profile;
Tenfoot launch/return and DE10-equivalent operation remain open.
