# DE25 bounded system-clock control proof

FPGA producer `5756143cbeaf09623a54e33c3b9f592a971dc97f`, based on the
main merge and explicit SMMU profile in `codex/de25-linux-platform`.
Post-generation verifier and interconnect bench/helper:
`43f04021d7d5e676a238dd0bb7e886149fac3f4e`. No board, SD or QSPI changes occur.

## Implemented control path

The CPU now reaches a 256-byte system-clock control slave at peripheral offset
0x10200. Its lower 128 bytes forward to the unchanged scanout endpoint ABI 1.0;
the upper half provides guard identity, faults, status, a write-once epoch marker
and the selected 100,000-system-cycle timeout. The guard uses the inherited
peripheral reset rather than waiting for initial pixel-clock lock. Generated
route, width, completion, clock, reset and nonoverlap checks pass.

A missing acceptance or response releases the CPU with dummy zero data and a
permanent local fault. Software must validate local identity/epoch/faults before
and after ordered endpoint access. A held downstream request is never withdrawn
at timeout, and a late response causes no duplicate CPU completion. Poison
prevents further forwarding and has no software clear. The guard proves neither
DMA drain nor endpoint/global reset ownership. Its bound assumes a running system
clock, peripheral bus and released peripheral reset. The epoch marker alone is
not a reset or DMA lifetime proof.

The initial generation job exits 1 because the new checker accidentally placed
a two-element route in its three-element top-reset route list. Its failed
[job record](generation-job.json) and [output](generation.stdout.log) are retained.
The generated reset connections are correct. The corrected verifier validates
those same generated IP, HDL, constraints and inputs without modifying them;
[verification-state.json](verification-state.json) binds original/final receipts.
[generation.json](generation.json) names both the producer and verifier. This is
not a claim that the initial producer's checker passed.

## Actual generated transport and compilation

The generated-interconnect simulation passes all eight cases, including
`bounded_control_during_pixel_clock_loss`. That case stops the pixel clock while
an actual generated AXI read remains outstanding, observes a bounded CPU read,
reads the guard's fault/epoch/status locally, and confirms DMA remains undrained.
After clock resumption, the late control reply retires without clearing poison;
further endpoint reads/CLEAR cannot erase outstanding DMA ownership.
[The simulation receipt](interconnect-receipt.json) binds the actual QIP-selected
sources, generated interfaces, wrapper, executable, bench/helper, generation and
inputs. [Its case log](interconnect-simulation.log) contains all eight results.
The storage model is explicitly **MODELED M20K STORAGE**. It does not qualify
vendor primitives, metastability, physical memory, SMMU translation or DDR.

Quartus Pro 26.3 Build 114 compiles that same generated project and converts
its HPS-first phase artifacts. The compilation/conversion job exits 0 after
434.8 seconds, with a 30-second heartbeat and 7,200-second cap. Compilation
reports 0 errors and 310 warnings. The unchanged vendor SPL remains selected.
All 19 original report hashes and three phase artifact hashes/sizes are checked.
[build.json](build.json), [linux-programming.json](linux-programming.json) and
[artifact-verification.json](artifact-verification.json) bind these results.
The input receipt retains explicit `--hps-first --linux-dma`, nonsecure stream 13,
and whole inherited F2SDRAM context; this is not scanout-only isolation.

All 74 reported timing rows have nonnegative slack and zero reported TNS. Hold
includes rounded 0.000 ns. The original report says setup and hold constraints
are incomplete; six of 85 signoff rules fail and no violations are waived.
These are compilation results, not timing qualification or hardware acceptance.
The raw timing summary and signoff report are retained alongside this record.
[Post-compile verification](post-compile-verification.json) confirms all 181
simulated source files and 18 QIP files remain byte-identical, and rechecks
the complete generated route/clock/reset/guard/SMMU contract.

Sixteen focused producer cases and generated consistency pass (37 consumers,
40 fixture copies). The standalone actual guard RTL bench passes acceptance
stalls, absent/late/error/wrong-kind responses, exact-deadline completion,
retained command stability, permanent poison and reset of the epoch marker.
Separate endpoint, last-frame-ACK and board-clock simulations pass; the guard
was rerun after the Verilog language correction. The broader affected regression
run passes all eight commands, with 1,167 parent cases (2 skipped), 1,677 FPGA
cases (75 skipped), schema/fixture and whitespace checks. Its original
[runner result](affected-result.json) records planner head `b368f5e7b` and a
working-tree guard change. The Verilog correction and verifier/case-order fixes
were committed while this run progressed. This is working-tree regression
evidence, not a frozen-head full-suite claim for 43f04021d. The focused producer
checks on the corrected source and the actual generated-project verification
and eight-case transport run explicitly cover those fixes. The raw affected
log is retained as [affected-regressions.log.gz](affected-regressions.log.gz).

The full phase files, all original reports and receipts are retained independently
and read-only at:

```
/home/deano/fes/out/artifacts/de25-bounded-control/5756143cbeaf09623a54e33c3b9f592a971dc97f/
```

The remote generated project remains at
`fes-build-3090:/data/fes-de25-guard-b368f5e7b/output/`.
The directory name records the first preparation attempt; receipts identify the
actual selected producer/verifier. No artifact is programmed by these commands.

## Remaining normal-kit requirements

The production kernel probe remains closed. A real device/domain binding must
retain mapping, callback, allocation and domain resources through uncertain DMA,
control every master sharing the context, and reject unsafe detach/reconfiguration.
The vendor failed-load cleanup, global reset fencing and physically qualified
translation/drain/clock behavior remain open. A revocable userspace mapping through
the pinned IOMMU DMA implementation also needs an appropriate allocation path;
page-backed mappings cannot bypass the existing revocation checks.

This work does not supply the managed AArch64 runtime/agent/bundle, MENU package,
normal ZX81 catalog/session/persistence/update workflow or exact-artifact board
acceptance. The DE10-equivalent development-kit objective remains incomplete.
