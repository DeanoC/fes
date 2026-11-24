# pc386: the board's 387 as a `pc386_system` parameter

Slice 2 of the DE25 pc386 lane (#2029), issue #2031. The lane plan is
`docs/validation/2026-11-20-de25-pc386-shell-plan/summary.md`, on PR #2030
until it merges.

## The change

`pc386_system` takes one new parameter, `X87_PRESENT` (the i386dx+387
identity), defaulting to 0. `x87_present` is now `X87_PRESENT != 0`, where
it was tied to `1'b0`. Neither the DE10 top nor the seal sets it, so the
DE10 package keeps its identity off. Turning the identity on is x87 slice 7
(#1768). The CPU has one x87 backend since #2037, so there is no backend
parameter.

The fit harnesses take the setting in place of editing the RTL:

- `pc386_quartus.machine_program(..., x87=...)` adds the parameter to the
  Yosys program. It refuses any key other than `X87_PRESENT` and any value
  other than 0 or 1. The seal passes none.
- `fit_i386_device.py --x87-present` sets it for `--design machine`. It
  refuses it for the CPU design, the SystemVerilog front end and a reused
  `--netlist`, because none of those goes through this elaboration. The
  netlist record carries the setting used.
- `quartus_x87_spike.py --identity-on` passes `X87_PRESENT = 1`, and its
  text overlay of `pc386_system.sv` is deleted. The DE25 fits'
  `scratch-edits*.patch` files in earlier records stay, as the history of
  how those fits were made.

## What the change does to the DE10 package

It edits RTL in the sealed source closure, so the DE10 package took its
full gates at the head `e79fc6866` (main `f3140cb33` merged, after #2059).
The machine netlist moves with any declared parameter: Yosys's
auto-generated names shift (DeanoC/yosys#80). Logical equivalence was not
checked formally. The gates below are the evidence.

## Gates

| Gate | Where | Result |
| --- | --- | --- |
| Repo tests (`test_fit_i386_device`, `test_quartus_x87_spike`, `test_pc386_quartus`, `test_build_pc386_product`, `test_pc386_seal_process`) | Powerboat | 165 pass, 1 skipped |
| `make sim-fes-pc386-system` (`pc386_system` with `fes_i386_cpu`, FreeDOS, at the parameter's default) | Powerboat | PASS: prompt at 191,533,231 cycles; the disk save, CMOS, record, port-2 hold and status-ring checks (18) pass |
| Netlist lockstep through the producer's `--with-lockstep` | chip-30x0-ai-dev, CUDA toolchain slot | pass: corpus 1,758,699 tests of 941 files, random 32,000, pmode 8,000, tasks 4,000, debug 4,000, undefined 4,000, directed 84, pmode cases 726, paging 170, no-387 1,933, x87 125, BCD 28, transcendental 181, x87 built forms 1,620 with 0 mismatches, memory 27. CPU source closure `a814e143`, netlist `95b46e65`, report `ee214a51` |
| pc386 seal, registered producer `build_pc386_product.py`, Quartus 17.0.2 Lite, **seed 1** | chip-30x0-ai-dev | BUILD_ID `42ea89c2b19bda6e92fe4684a76a04e7`; package `b30c23424d9fb29d51deaa237183d5ed2abbe385ff05a4ff78b0a89717035c67`; RBF `1fbba035ed69f59c4d1c90039260a7e7908274e7cb621e2915895e1ccd5766dd`; 36,390 of 41,910 ALMs; system clock 61.44 MHz. Worst slack across corners, from [`seal-build-summary.json`](seal-build-summary.json): setup +0.580 ns, hold +0.084 ns, recovery +6.602 ns, removal +0.272 ns, minimum pulse width +1.122 ns. Every analysis passes |
| Kit B, run `k2033d` | Powerboat | pass: the sealed package `b30c2342` launched from the library on the user-supplied drive `a6977555`; FreeDOS prompt detected in 2.0 s, Wolfenstein 3D drawn and walked. Core-only: nothing installed. The kit was restored to its baseline (image `179affbb`, bundle `c9ae79e2`), idle and lease-free, and logged in `kitB-STATUS.md` ([result](kit-b-k2033d-result.json)). An earlier attempt, `k2033c`, stopped at the driver's import before touching the kit; it was verified on baseline and logged FREE |

**The seed.** The agreed sweep was seeds 1 to 4, one at a time under the
Quartus lease, taking the first seed that passes every check. Seed 1
passed, so 2 to 4 did not run. Before #2059, seed 1 at `b258e166a` did not
route (Quartus error 11802, congestion, 36,322 of 41,910 ALMs); that run
produced no package. The seal checks are unchanged.

## A later head

`head_matches_seal.py` (copied from #1984 S4's record) runs the registered
producer's own build record, selection only, on a tree. It checks that the
design identity equals the one in `seal-build-inputs.json`, this record's
seal. A head that adds only records, docs or tests passes:

```bash
python3 -I docs/validation/2026-11-20-pc386-x87-board-params/head_matches_seal.py .
```

## Reproducing

From `sources/misteross`:

```bash
python3 cores/fes-pc386/tools/build_pc386_product.py --with-lockstep --gpu-router CUDA --cache-root /abs/fes-toolchain-cache --seed 1
```

```bash
python3 scripts/fit_i386_device.py --design machine --device de25 --x87-present 1 --cache-root /abs/fes-toolchain-cache --output /abs/new-dir
```
