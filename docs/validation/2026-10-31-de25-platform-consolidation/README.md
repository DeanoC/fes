# Consolidated DE25 Linux DMA foundation

[#1312](https://github.com/DeanoC/fes/pull/1312) is the opt-in Linux DMA
foundation above consolidated bring-up
[#1297](https://github.com/DeanoC/fes/pull/1297), both in `lane:deano`.
The profile is selected only with `--hps-first --linux-dma`; the normal driver
probe still returns `EOPNOTSUPP` and no production runtime path is admitted.

The frozen FPGA/software producer for this consolidation is
`70f8fee9c6d90c2bc338eef0be38829883f63cf8`, based on bring-up
`2243c34a250867b324d6b26146fd32acdb778882`. The later inherited KUnit refusal
hook correction is incorporated at
`cb7b307ef4c3baad531adf8399f0fac101a7c2eb`; it changes no FPGA or shared-package
source bytes. Receipts name their actual producer instead of assigning those
results to a later documentation/merge commit.

The corrected AArch64 QEMU KUnit run passes all 18 required cases and the raw
kernel-log gate. This includes contiguous mapping aliases and the new-mm
reference cleanup on a forced mapping refusal. The initial run's assertions
passed but the raw-log gate correctly rejected `BUG: Bad page map`: the old
test hook cleared `VM_PFNMAP` after special PTE installation. The fix injects
the error while preserving the real mapper's VMA/PTE semantics. Failed and
passing results are both retained; neither qualifies physical DMA.

The affected plan's parent, consistency and contract commands pass. Its FPGA
suite selected 1,690 tests: 1,614 passed, 75 skipped and the original
current-source composition-receipt case failed because it used the dated
2026-10-30 producer. The failed run is preserved. The refreshed combined proof
on `cb7b307ef` passes all fourteen native/AArch64 cases in 1762.1 seconds,
including ROM SAVE, two LOAD/RUN cycles, reload removing an edit, interrupted
LOAD/freeze and empty/16K/empty selection. Guest phase bounds and the
1800-second session watchdog are unchanged. `combined-proof/` retains its
receipts, logs and saved images; complete model/client binaries and source
snapshots are independently retained under
`out/artifacts/de25-pr-consolidation/2026-10-31/combined-proof/platform/`.

The branch-local current fixture retains exact live compiler/runtime hash
matching and both client architectures. All sixteen focused recipe/gate tests
pass after this refresh, resolving the failed case without repeating the
unaffected passing tests or weakening the freshness checks. This remains a
selection-mux persona model with behavioral memory, not physical partial
reconfiguration acceptance.

The fresh Quartus Pro 26.3 build passes in 538.7 seconds. All 74 reported
timing rows have nonnegative slack and zero TNS, but **six signoff rules fail**:
missing input/output constraints and CDC/reset findings remain open. No
constraints were relaxed and no waivers were added. The receipt verifies all
19 report hashes, 431 handoff/SDC hashes and the three HPS-first phase artifacts.
The exact generated-interconnect simulation passes all eight cases, including
bounded control during pixel-clock loss. FPGA metadata and selected compressed
reports are under `fpga/`; complete programming binaries/reports are retained
independently at
`out/artifacts/de25-pr-consolidation/2026-10-31/fpga/platform/`. The copied build
manifest describes that complete retention. Timing and hardware acceptance are
still not qualified.

This remains an unfinished foundation. The guard bounds CPU control accesses
and poisons after uncertainty; it is not a global reset or drain proof.
Contiguous allocation does not establish physical DDR/SMMU/firewall operation.
The generated-interconnect simulation uses modeled M20K storage. Actual Linux
device/domain binding, callback and allocation lifetime protection across
uncertain DMA, detach/reconfiguration prevention, serialized loader/epoch
handling, normal AArch64 MENU/runtime integration, timing coverage and
exact-artifact hardware acceptance are still required.
[#1284](https://github.com/DeanoC/fes/issues/1284) remains open.
No board, SD or flash changes were performed for this consolidation.
