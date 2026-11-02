# DE25 managed fabric and retired JTAG constraints

The canonical MENU recipe removes the vendor maintenance Avalon masters and
their ACE5-Lite translator, disables the unused native FPGA-to-HPS bridge with
`f2s_data_width=0` (Unused), and retires the GHRD's inclusion of the constraints
for those removed fabric JTAG interfaces. This is compiler/model evidence for
an **unsealed, unprogrammed** candidate. Normal kit acceptance remains open.

The selected policy is `managed-scanout-initiators-v3`. Standalone maintenance
keeps its debug profile. Physical FPGA configuration over JTAG is independent
of the removed bus masters. The HPS lightweight control route, HPS-to-OCM route
at base zero, and the isolated SID13 scanout-to-F2SDRAM route remain required.
The eight checked initiator endpoints include CPU bridges and relay stages;
they are not eight DMA devices. The kernel/runtime/agent source is unchanged
by this constraint repair.

| Identity | Actual value |
| --- | --- |
| Final producer | `d8549cc79132f6d6a0752021552125c1220e337f` |
| BUILD_ID | `546f89796e415930cc4a87eafaacd67b` |
| RBF SHA-256 | `b6eab6805067050a69cb66b40f5708543e868a9d898250662b78eda77eba3eb2` |
| RBF bytes | 1,425,408 |
| Canonical source inputs verified | 374 |
| Generated HDL/header files and QIPs matched | 125 / 17 |

The [independent checks](checks.json) bind source, tools, execution, generated
graph, actual HPS parameters, fitted database, input review, models and RBF.
The full HPS/EMIF review allows exactly the existing F2SDRAM SMMU exposure and
the F2H disable for this canonical HPS-first profile. It grants no firmware or
native acceptance. The native F2H interface must be absent in the actual HPS
IP, SOPC interface definition and generated parent ports.

## Current results

- Actual Quartus 26.3 compilation, Linux-load conversion and fresh fitted-input
  review pass in 569.9 seconds. All 69 reported timing
  checks are nonnegative; RES-50101 is clear.
- Ten generated-transport cases pass in 66.0 seconds. The 72-case startup
  matrix passes in 14.7 seconds **WITH MODELED M20K STORAGE**. It binds the five
  actual parent controllers and seven reset connections. HPS/OCM operation,
  physical reset/metastability and global boot/allocation epochs remain outside
  this finite model.
- The private read-only fitted query passes in 9.3 seconds: all 24
  recovery/removal paths for the three remaining selected 100 MHz receivers,
  at all four corners. Worst recovery is 7.806 ns;
  worst removal is 0.357 ns. Both ends are 100 MHz.
  The query finds zero compiler-visible fabric JTAG ports and clocks. Original
  fit data, constraints, reports and RBF remain bound and unchanged.
- The GHRD/JTAG SDC input hashes are pinned. Only `source ./jtag.sdc` is
  retired; original GHRD bytes and the untouched vendor JTAG file are retained.
  Original/derived constraints and their receipt are rechecked before and after
  compilation. Other constraints remain present. This adds no timing exception.
- Final focused checks pass 12 managed-fabric, 35 scanout, 24 HPS and 12
  canonical-producer cases (83 total) on the byte-identical committed tree.
  `make check` passes 40 generated consumers/42 fixture copies.
- The original full affected suite on precursor `e7d7d4d64`, against
  `11999b896`, remains **FAILED** after 1973.8 seconds:
  1,114 parent tests pass (three skips); the FPGA suite runs 1,757 cases with
  76 skips and one stale combined-client receipt failure. All seven commands
  start; every other command passes. The failed receipt captured an older
  `hps/build.py`; runtime/RTL inputs did not change.
- A fresh committed-source combined native/AArch64 proof reruns actual ROM SAVE,
  two BASIC LOAD/RUN cycles, reload removing an edit, empty/16K/empty selection
  and the failure controls. All 14 cases pass on the final source in 1572.8 seconds. The fixture
  is refreshed only from this actual report and the recipe/source-matching
  controls pass afterward. The combined proof uses behavioral RAM and a persona
  selection mux; it exercises no physical SDM, MMIO, DDR or HDMI. This is a
  **failed aggregate plus a successful
  focused continuation**, not a rewritten green aggregate. The final constraint
  repair also receives the 83 focused checks and new compiler/model proofs
  above. [Source comparison](software-scope-equivalence.json) binds the unchanged
  runtime, agent, contracts, image, platform and parent-script trees. Exact-head
  CI and review remain separate requirements.
- The existing four-case HDMI protocol/40-cell `async2sync` digital equivalence
  proof is reused only after all ten compiled sources match exactly. It is not
  physical IO or reset qualification.

The raw failed classes are TMC-20012 (30 output findings), TMC-20011 (two input
findings) and RDC-50003 (two groups), with zero waived findings. Only the two
explicitly approved HDMI inputs are accepted after the fresh fitted topology,
timing and default-rate MTBF review. **Output timing and whole reset-domain
coordination still block export.** The raw inventory lists six system and six
pixel chains; these counts are not observed faults or a waiver. No eligible
independent identical-RBF rebuild or described package is produced.

## Independent FIFO reset-release diagnostic

The additional [finite reset test](../../../sources/misteross/tests/de25_reset_skew.py)
uses the existing passing generated startup model. In private copies only, it
adds a selectable one-cycle delay to each FIFO reset synchronizer's first stage.
The original shift stages, final reset register, FIFO pointers, credits,
interconnect and endpoint remain present. Elaboration checks all eight selected
four-stage asynchronous reset chains and the modeled RAM cells. A receiving-edge
monitor requires release on the exact fifth or sixth edge for each selection;
the negative control omits a requested delay and must fail that monitor.

The [actual result](reset-skew-summary.json) at `263a19e06` passes 2,304 cases in
72.8 seconds: 2,048 healthy cases cover all 256 independent reset-delay masks
at eight clock phases, including early control, accepted AXI reads and complete
response draining. Another 256 cases hold the first-lock source beyond the
control timeout, then verify that late release preserves poison and refuses
DMA/re-entry. All 42 focused scanout/startup tests pass in 25.345 seconds.
The initial `0ae1c7d40` harness fails elaboration after 0.6 seconds because
Verilator does not support the deep `defparam` syntax. It never simulates and
remains retained. The corrected private model passes parameters through the
existing FIFO wrappers.

The later helper at `ebb12c489` also refuses relocated absolute replay paths
before copying or running commands. Its [derivation check](reset-skew-derivation.json)
recreates all 130 model/source/bench inputs byte-for-byte against the executed
test and exercises that refusal; it does not rerun unchanged simulation.
The [retention record](reset-skew-retention.json) verifies 515 archived regular
files, the original failure, raw results and logs, a restored source bundle and
unchanged 374 MENU/59 combined-compiler/58 runtime-test inputs. The archive is
26,706,305 bytes, SHA-256
`ff5e168f8b1890e03289a5def1ddedf70a45feb8f0ea49c858788565b2f31711`.

This diagnostic uses **MODELED M20K STORAGE** and covers eight of the twelve
reported system/pixel reset chains. It excludes the outer top reset, lock
qualifier and visibility release, analog metastability, live global reset,
HPS/boot/allocation epochs and physical acceptance. **RDC-50003 remains open;
no extra exception is accepted.** Inspection of the actual SDK/generated FIFO
finds that `SYNC_RESET` changes the register reset style while retaining the
unconditional read/write reset synchronizers. Changing that setting would not
remove those chains. Altera's [rule guidance](https://resources.altera.com/quartushelp/current/da_rules/rdc_50003.htm)
still requires common reset distribution or a justified crossing protocol.
No production RTL, compiler input, fitted database, installed artifact or board
state changes. The earlier FPGA and native/AArch64 proofs are reused only after
source equality; their evidence levels and remaining gates stay unchanged.

## Preserved rejected attempts

| Source | Actual outcome |
| --- | --- |
| `ca767cf0d` | Generation guard fails after 158.8 s: enabled unused F2H request-valid inputs are floating. No fit/RBF. Its partial software run is deliberately stopped after 465.4 s, not an aggregate pass. |
| `5399f9b91` | Synthesis fails after 229.5 s: request-valid tie-offs leave the enabled F2H hard response interface illegally disconnected (24265/129015). No fit/RBF. Partial software deliberately stopped after 443.7 s. |
| `01ce2c1d1` | Integration stops after 82.3 s because the SDK refresh already removed the obsolete F2H clock/reset edges. The corrected guard accepts removal or prior absence and rejects unknown edges. No IP generation/fit/RBF. Partial software stopped after 119.0 s. |
| `e7d7d4d64` | Compilation/conversion/fitted-input checks pass in 583.0 s; ten transport and 72 startup cases pass. The actual timing report adds TMC-20025 (9) and TMC-20026 (5) from stale JTAG constraints, so this compiled precursor is rejected for export. It has its own RBF, failed source-bound aggregate (stale receipt) and archive. |

The final fresh report clears TMC-20025 and TMC-20026. Earlier failed API probes,
raw compile outputs, deliberate process-group stops and the compiled rejected
precursor remain retained; no failed result is rewritten as a pass. The
[preceding five-receiver record](../2026-11-01-de25-staggered-startup/README.md)
keeps its original source and hardware boundaries.

## Retention and remaining acceptance

The [retention manifest](retention.json) and independent hash verification bind
the final source increment, compiled archive, models, original failures and
original failed aggregate and actual refreshed combined proof/recipe continuation. The final archive is
586,393,262 bytes, SHA-256
`3ccc58069c3523d36ba77e3ad08be3f0729fd6c44e539ceb91df290a744695eb`.
All 3,851 regular files are hashed; both canonical records, actual RBF, both
reset receipts, both retired-debug receipts, original/derived SDCs and both
models are verified. Six parent inputs are independently reparsed.

Source restoration passes from the untouched precursor full bundle plus the
final increment. All three failed producer versions remain recoverable. The
artifact root is `/home/deano/fes/out/artifacts/de25-managed-fabric/d8549cc79`; the full base bundle is under `precursor-e7/`,
and the final repair is `source-incremental.bundle`.

The user has no additional PCB files. No numerical PCB delay, lower clock or
further exception is introduced. Firmware compatibility of the disabled F2H
profile, HDMI output timing, whole reset coordination and native acceptance
remain required. Managed image/storage/startup assembly, factory/MENU/catalog
launch-stop-relaunch, restart/adoption and update/restore remain open under the
[normal-kit acceptance scope](../../design/de25-linux-platform.md).

This milestone accesses no board and writes no installed kernel, SD, FPGA or
QSPI. The prepared ABI3 kernel and new candidate remain uninstalled. The bottom
board's last authenticated observation remains the earlier ABI2 tracing setup;
this record grants no fresh board attestation, baseline promotion or release.
