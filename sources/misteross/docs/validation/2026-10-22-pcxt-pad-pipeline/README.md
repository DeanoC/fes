# PC/XT SDRAM output pipeline diagnostic

This candidate improves internal command/address pad-register paths but is
**not qualified for promotion**. CPU setup still fails, failing setup rows
increase, and microcode has only 1 ps of setup margin. No hardware ran;
external SDRAM timing and the unsupported boundaries remain open.

Base: `612f907d` (completed RTL baseline `be5f2253`). Candidate: `9eca8583`.
The sole changed synthesis input is `pcxt_memory.sv`, enabling the existing
controller's `IO_OUTPUT_REGISTERS` parameter. Commands, addresses, write data
and output enable gain one aligned stage; read capture adjusts by the same
cycle. CPU clocks, SDC/QSF, compiler binary, seed 1, placement weight 2000 and
exponent 5 are unchanged. Packing verifies all 20 command/address outputs are
MISTRAL_SDROUT without dynamic CEOUT or ACLR.

| Final routed metric | Completed baseline | Output-pipeline candidate |
| --- | ---: | ---: |
| Worst command/address pad-register setup | -2.555 ns | +5.021 ns |
| Worst command/address pad-register hold | +2.192 ns | +1.136 ns |
| CPU/system setup WNS | -2.555 ns | -1.602 ns |
| CPU/system reported Fmax | 76.406 MHz | 64.004 MHz |
| Memory setup WNS | +3.175 ns | +2.854 ns |
| Microcode setup WNS | +0.285 ns | +0.001 ns |
| Pixel setup WNS | +1.471 ns | +0.675 ns |
| Failing setup rows | 257 | 369 |
| Failing related hold rows | 0 | 0 |
| Unsupported boundaries | 105 | 105 |

The candidate's worst reported setup path is last ALU result through jump logic
into the next microcode address register (-1.602 ns). Last-result writeback
also fails (worst -0.729 ns), so adding a zero reduction there is not an
established solution. Further work needs CPU placement and logic analysis.

The first pipeline simulation exposed a model assumption: FPGA cold reset
immediately marked SDRAM as deinitialized. SDRAM has no FPGA reset input;
an already-issued command can retire before CKE falls. The model now starts
reinitialization checks at the observed CKE-low boundary and retains bank
recovery/RAM state. A regression permits the queued ACTIVATE but rejects a
new ACTIVATE before reinitialization. The reset sweep also requires that the
CKE boundary actually occurred.

Validation passed: the model regression, all 80 warm/40 cold-reset phases,
memory-backed CGA/MDA boots, CGA Direct and MDA Scanlines system boots at each
zero/one/two-cycle model DQM delay, parent consistency, and `make test-changed`
(805 parent tests, 1269 component tests with 30 skips, complete PC/XT simulation
sweep). Yosys-dependent Python tests were skipped in this repeat because Yosys
is not on PATH; diagnostic synthesis explicitly used the same locked Yosys as
the baseline. No CPU or shared controller RTL changed.

The candidate route completed with exit 1 after 224.164795 wall seconds, within
its 600-second active cap, using automatically leased HIP with Router1 cleanup.
No GPU lease remains. Conservative incident charge includes lease waits:
**607.674011 seconds remain**, down from 831.838806 seconds. Compiler source:
`45476abd07a8881ebeee1e5d061be6b5fe123d71`. Binary/input hashes, argv and environment
are in `runs.jsonl`. The baseline is the completed candidate from
`../2026-10-22-pcxt-commit-timing/`, not its timed-out baseline.

Full source is recoverable from the committed candidate. The frozen netlist,
constraints, generated inputs, packing checks, routed RBF/graph, reports and
both comparison reports are in `capture.tar.gz`. Member hashes are in
`manifest.json`; `comparison.json` retains all 20 pad rows and native clock
metrics. The compiler binary is not embedded. `test-changed.log.gz` retains
the test run. No toolchain lock, seal, shared protocol or physical acceptance
changed. This remains part of draft #979 and open timing issue #840.


## CPU locality follow-up

On base `930f2fbe`, two host-only fresh synthesis-to-placement replays tested
existing `--critical-cohort-budget 16` repair with the same frozen inputs,
compiler, seed and placement weights. The first used the complete final routed
report as guidance. The second selected its 49 same-clock CPU paths ending at
microcode read-address registers. This selection only directs proposals; all
full-design clock, setup endpoint, related-hold and legality guards remain on.
Neither replay routes or writes an RBF.

Both exited successfully and rejected every move: 16 STA trials each, zero
retained. The complete-guidance search attempted 16 geometries; address-focused
search attempted 21. Their output modules, including placement and connectivity,
are identical. This bounded negative result does not prove that all placements
fail. It provides no new final-routed or hardware evidence.

The worst routed address path spends 7.978 ns in wiring and 3.045 ns in logic.
Its first two wires alone total 3.780 ns, travelling from last-result at (10,37)
to reduction logic at (12,27), then back to (8,37). The existing hard-IP capture
locality pass is inapplicable to this combinational CPU path. Adding a cached
zero flag is also unjustified while last-result writeback already fails by
0.729 ns. The next compiler investigation should address coordinated placement
of the reduction and address-select cone, with writeback and microcode capture
protected, before another route is spent.

`cpu-locality-host.tar.gz` retains exact commands, scripts, logs, input hashes,
results and original report indices to reproduce the selected guidance. No RTL,
compiler implementation, clock, lock, seal or shared contract changed. No GPU
route time was spent: the incident allowance remains 607.674011 seconds.
