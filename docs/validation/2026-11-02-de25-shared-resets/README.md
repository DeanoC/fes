# Canonical DE25 MENU FIFO reset sharing

Canonical MENU now uses one FIFO reset-release pipeline per receiving clock,
shared by both command/response bridges. This reduces eight FIFO pipelines to
two and the fitted reset-chain heads from twelve to six. It preserves the
original reset OR, polarity, four synchronizer stages plus output register,
clock frequencies, pointer/data logic and all 21 SDC files byte for byte. Standalone diagnostics
continue to use the vendor topology.

The producer selects `shared-fifo-reset-release-v1` in its functional record.
Its captured helper checks all eleven original generated files before writing,
retains them, reproduces every derived byte, and verifies the result during
generation and before/after compilation. Unknown inputs and source/receipt
changes stop the build. README and architecture changes accompany the producer.

## Identity and actual build

- Producer: `7b2601cd3cb31447a0dc7ac9dec07d3f683e2142`.
- BUILD_ID: `a6a023adf49a4c74198c391d47f7f315`.
- Linux core RBF SHA256: `b30faa9be76f4fc14b77d38857f80f195d0f0a8d0fecbb4847a39a7670a1682c`.
- Public Quartus Pro 26.3 generation/compilation/conversion: PASS, 567.4 s.
- All 69 reported timing checks pass. The 85 signoff rules retain raw
  TMC-20012:30, TMC-20011:2 and RDC-50003:2, with zero waivers. Only the two
  previously approved HDMI inputs pass their fresh fitted review.
- Package timing admission, hardware acceptance and normal-kit acceptance
  remain false. No eligible independent package rebuild is claimed.

## Fitted and modeled results

The actual fitted FIFO reset graph contains 82 consumers and 410 pipeline
rows. All consumers share exactly one five-register chain in each receiving
clock. The system and pixel domains each retain three reset heads; the two
RDC findings remain open. Fitter replication changes the consumer count from
the private prototype's 78; the current proof queries the actual circuit.

| Check | Cases | Result | Seconds |
| --- | ---: | --- | ---: |
| Actual generated startup/control transport | 72 | PASS | 14.540 |
| Actual generated transport | 10 | PASS | 64.979 |
| Actual board reset/lock/visibility plus transport | 32 | PASS | 141.820 |
| Six independent chains, finite 0/1-cycle release uncertainty | 2,048 | PASS | 368.623 |

The final matrix covers eight relative phases, four events and all 64 delay
masks. Its 1,024 global-reset cases deliberately retain accepted external DDR
reads while the guard epoch resets to zero: they demonstrate the ownership
boundary, not recovery or drain. Stop/drain and pixel-lock loss retain their
separate predicates. Complete-state reuse preserves the real raster and all
36 host members: 65 classes, 64 independent comparisons and 1,919 restores.
Wrong-delay, corrupted-state and live-bridge-reset controls are rejected.
PLL lock, HDMI completion, M20K storage and AXI memory are modeled. Physical
metastability, native ownership, security and recovery remain unqualified.

The original attribute-only prototype compiled but retained four independent
FIFO pipelines per clock; it is rejected as a sharing repair. Explicit sharing
compiled and passed 72+10 model cases before canonical integration. All private
results and original setup/query failures remain archived.

The first canonical fitted query ran on the original project and rewrote four
compiler report-cache files. The existing input review correctly refused its
changed database binding. All source, netlist, RBF and raw signoff/timing-report
bytes remained identical. That original attempt remains preserved. A separate
immutable snapshot receives a fresh real input review; the corrected fitted
query runs on its private copy and verifies the snapshot remains unchanged.
The finite setup's missing-header elaboration failure is also retained; its
correction copies the complete checked startup source manifest and reuses the
unchanged passing 72+10+32 model results.

## Software validation

The current seven-command affected plan starts on clean producer `7b2601cd3`.
The 62 planner tests, both whitespace checks, parent 1,187 cases (seven skips)
and generated-consumer consistency pass. Its aggregate reaches the 1,800 s
cap during the final FPGA command; that timeout remains preserved.

The retained log matches all 723 completed FPGA case IDs against the exact
current discovery order, with eight skips. The interrupted case plus the
remaining 1,047 run as an ordered suffix: 1,048 cases PASS, 68 skips, 687.911 s.
All 1,771 selected FPGA cases are accounted for (76 skips); no individual
test limit is changed and no completed prefix is rerun. The resume job passes
in 699.8 s, including `make check` in 10.980 s (40 consumers, 42 copies).
This is completed case coverage, not a retroactive aggregate PASS.


## Retention and remaining work

Retained artifacts live under
`/home/deano/fes/out/artifacts/de25-shared-reset/7b2601cd3`.
The canonical archive has 9,665 files (1,447,482,215 bytes), SHA256
`c277671cbca385a897d6a5e08e1a2b73af8762537dc426e6c676d42357b304ab`.
An independent extraction rechecks every file, all 375 canonical inputs,
the public timing/input-review readers, eleven derived sources, the actual
82-consumer graph and every finite case in 13.041 s. The 4,635-file private
archive is 254,557,070 bytes, SHA256
`adb90667ea785518d62fb27879f9331f884d75617d5277235eefd2eee62bd896`;
its exact prototype derivation/fitted/model checks restore separately.
The source bundle is 88,712 bytes, SHA256
`749cfbf5c381c232437785066a80a7145af3c367b879d47d7789c5d5432f60f2`;
it restores from the retained `45037e109` seed and verifies all 375 inputs.
Full inventories, scripts, receipts, original failures and the precise cache
refresh remain in the archives; this record grants no new exception.

Whole-reset and HDMI electrical/load/path signoff remain open. No additional
PCB/trace files were supplied and no physical delay is invented. The normal
registry currently supplies only DE25 MENU; canonical ZX81 production still
needs the managed policy, rather than aliasing its preserved diagnostic.
An admitted factory/card and native MENU/catalog ZX81 launch/stop/relaunch,
input/persistence/update/reboot/recovery acceptance remain required for parity
with the DE10 development kit. This work performs no board operation and
installs or promotes no FPGA, kernel, software or baseline. The two PRs stay
ready in `lane:deano`; no merge or Lander handoff is requested.

The 30-file local check archive has SHA256 `040d33c7ec26dee3d6ec6eab427a1e4c483577b4e2a04d70da4e42b1137bce98`;
its restored logs verify the exact completed prefix, suffix, original timeout
and successful committed-source checks.
