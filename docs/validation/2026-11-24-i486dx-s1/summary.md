# The first i486DX on the DE25 (slice 1), 24 November 2026

Tracker [#2135](https://github.com/DeanoC/fes/issues/2135); plan
`docs/validation/2026-11-22-i486dx-core-plan/summary.md` (#2146); round 2's
cut measurements, #2149 (record head `7ff764c37`). Deano, 24 Nov 2026
(relayed): "agreed, start the first DX slice with cuts".

The method, [method.md](method.md), was committed (`cc136b1a6`) before any
fit or simulation of the DX, with four dated amendments, each written before
the runs it governs.

## Result

The DE25 pc386 builds as an i486DX: the 486 identity (`I486 = 1`), the 387
on (`X87_PRESENT = 1`), both L1s at 8 KiB 2-way, and no load forwarding. With
the two shell cuts (#2177 S-lat, #2178 A-dma2) and #2187's signoff bases
merged, **it fits the device and closes timing on all four seeds at
61.437908 MHz**, median **44,913.3 ALMs** (96.0% of 46,800). It keeps step
with the oracle in every suite the integer core passes. Its CPI costs are
DOOM ×1.058 and 3DBench ×1.016, as round 2 measured. The DE10 pc386
netlist and identities do not move.

**Quake** stalled on round 2's bench because of a real bug in the shared
x87 unit, not the bench edit: a wrong-path ESC form the unit does not build
left the unit's "first uop issued" flag set, which holds every later ESC and
FNINIT for ever. The fix and its lockstep case go in their own PR against
main ([G5](#g5-quake-on-the-rtl)).

| Gate | Result |
| --- | --- |
| G1, DE10 identity | **Pass** at the head reported here: see [G1](#g1-de10-identity) |
| G2, lockstep and the x87 suites | **Pass**: no failure in either arm, at level 3 (with every x87 suite) and level 4 |
| G3, DE25 fit, device + four seeds | **Pass**, second attempt (`a36169588`). The first attempt (`5112461a9`) **failed** at the producer's signoff gate (no basis for the window-base CDC row); fixed by #2187, a separate reviewed PR |
| G4, CPI | **Pass**: DOOM r = 1.058, so 2,062 × r = 2,182 realtics ≤ 4,682 (DX-33). DX2-66 (2,912) is kept by the bound too (reported, not gated) |
| G5, Quake on the RTL | The stall is a real bug, found and fixed with a case that fails before the fix: see [G5](#g5-quake-on-the-rtl) |

## What was built

- **`boards/de25nano/pc386/i486dx/`** (decision 1):
  - `fes_i486dx_l1i.sv`, `fes_i486dx_l1d.sv`: forks of `fes_i386_l1i.sv` and
    `fes_i386_l1d.sv` at `499c3c3ea`, with the way count halved (128 sets of
    32-byte lines, 2 ways). Round 2's ablations held ways 2–3 invalid; these
    remove them. `derive.py` pins each upstream's bytes, so an upstream change
    fails `tests/test_de25_i486dx.py` until the fork is synced.
  - `derive.py`: derives `fes_i486dx_cpu` from `fes_i386_cpu.sv` by anchored
    edits (the module name, the two L1 instances, `d_out.ldfwd = 0`). Its
    benches' form keeps the upstream module name, so the shared benches read
    the DX's hierarchy unchanged.
  - `lockstep.py`: the shared lockstep runners with the DX's sources.
- **The DE25 producer** (`pc386/build.py`, model `486dx`) elaborates
  `pc386_system` with `I486 = 1` and `X87_PRESENT = 1` and swaps in the DX's
  CPU sources; `pc386/derive.py` retargets the CPU instance. The record
  carries `cpu`, `i486` and `x87_present`.
- **Benches:** `sim_fes_pc386.py --cpu-core i486dx`; `machine_tb` now wires
  the RTL CPU's x87 unit as `pc386_system` does (`x87_present`, F0h/F1h in,
  ERROR# on IRQ 13), replacing round 2's scratch line.
- No shared RTL, no `yosys_netlist.py`, no `pc386_quartus.py` change.

## G1, DE10 identity

[`de10/`](de10/), harness `de10/i486dxs1_g1.sh`: the DE10 seal's machine
netlist (`de10_netlist.py`) and `de10_design_id.py`, from one checkout path,
Yosys `d273c1cb`, `YOSYS_MAX_THREADS=1`.

| Base → head | Netlist | Design | Evidence | Record | Toolchain |
| --- | --- | --- | --- | --- | --- |
| `2d4edf547` → `cc136b1a6` (the method's commit) | not run (no firmware build) | equal | **moved** | **moved** | equal |
| `2d4edf547` → `63aa526c8` (amendment 1) | `cmp` equal, `65cbd412…` | equal | equal | equal | equal |
| the four bases merged (`87562da40`, tree `291a6c31…`) → final head | see [g1-final.md](de10/g1-final.md) | | | | |

At `cc136b1a6` the lockstep runner and bench edits were pc386 design-v2
evidence, and the record digest covers every source input, so both moved.
Amendment 1 removed those edits. Since the merges brought in newer main, the
final comparison is against the same four branch heads merged without this
PR's commits (#2171 `2d4edf547`, #2178 `7147b7d05`, #2177 `642e22f50`, #2187
`884b5959c`; the one DE25-only conflict, `pc386/sim.py`, taken from #2178).

## G2, lockstep and the x87 suites

At `3739c0d43`, one checkout, the locked Verilator (`b7d20bdd…`), base =
the shared runner on `fes_i386_cpu`, DX = `i486dx/lockstep.py`
([`lockstep/`](lockstep/)). [`g2_compare.py`](g2_compare.py) applies the rule.

| Run | Base | DX |
| --- | --- | --- |
| Level 3 (`--random-programs 500 --random-seeds 8 --limit-per-file 20`): directed, unreal, PM, paging, no-387, x87 (125 with the identity on), BCD, transcendental, built forms (2,048 opcodes, 1,620 built, 0 mismatches), random/PM/task/debug/undefined programs | 0 failures | 0 failures |
| Level 4 (`--identity 486sx`): 148 directed cases, 56,000 random programs | 148 agree, 56,000 keep step | 148 agree, 56,000 keep step |

The two arms differ only in counters (clocks, replays, the latency-free gaps):
227 values at level 3, 11 at level 4 ([`lockstep/l3-compare.txt`](lockstep/l3-compare.txt),
[`lockstep/l4-compare.txt`](lockstep/l4-compare.txt)). The level-3 reports
name the DX's files; the level-4 report does not list files, so
[`lockstep/l4-built-cpu.txt`](lockstep/l4-built-cpu.txt) shows the DX build's
Verilated CPU has no L1D way 3 and the base's does.

The x87 unit benches Verilate the unit alone, whose files the DX shares byte
for byte ([`lockstep/x87-files.sha256`](lockstep/x87-files.sha256)): unit
bench 18,337 vectors, 0 failed; the transcendental engine 23,795, 0 failed;
TestFloat extF80 (TestFloat `a9c849f1`) 3,356,352 vectors in 84 configurations,
0 failed.

## G3, DE25 fit

[`fitdx.sh`](fitdx.sh) on Powerboat, Quartus Pro 26.3, every compile under a
Pro slot of the Quartus lease. Seed 1 is the registered producer; seeds 2–4
are SEED copies of its project.

**First attempt, `5112461a9`: fail.** The seed-1 producer refused at the
reviewed-signoff gate: "CDC-50001 row matches 0 bases" for
`fes_scanout|window_control|base_lo[13]` → `base_machine[13]`. No basis
covered the window-base held bundle. The run stopped, as the rule says
([`fits/attempt1/`](fits/attempt1/)); seed 1's own fit, for information
only: 44,997.9 ALMs, every analysis closing. Deano chose "Basis PR, then
refit": #2187 added the bases (reviewed on its own) and was merged
(amendment 4).

**Second attempt, `a36169588`: pass** ([`fits/attempt2/`](fits/attempt2/)).
Seed 1's producer admits the package: `BUILD_ID`
`da9f008e03ae73f7ef9cd6cc64551c48`, RBF `750514e2…`,
`package_timing_admission` true. All four seeds fitted netlist
`e0b01e055ffa5474d88df64ec12b84ddd92707d974bdc00435c3ca79a0a31643`.

| Seed | ALMs needed | Machine (`system`) | RAM | DSP | Setup | Hold | Recovery | Removal | Min pulse |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 44,952.7 | 37,678.1 | 159 | 42 | +1.399 | 0.000 | +6.816 | +0.019 | +0.220 |
| 2 | 45,004.9 | 37,761.1 | 159 | 42 | +0.489 | 0.000 | +6.891 | +0.007 | +0.220 |
| 3 | 44,829.0 | 37,580.4 | 159 | 42 | +0.733 | 0.000 | +6.974 | 0.000 | +0.220 |
| 4 | 44,873.9 | 37,594.1 | 159 | 42 | +0.437 | +0.003 | +6.806 | +0.066 | +0.220 |
| **Median** | **44,913.3** | **37,636.1** | | | | | | | |
| Spread | 175.9 | 180.7 | | | | | | | |

The worst setup is on the machine clock (`fes_machine_pll|machine_pll_outclk0`,
Slow 0C) on seeds 2–4 and on the pixel clock on seed 1; the worst removal is
`adv7513_config` at Fast 0C. [`fits/attempt2/fits.json`](fits/attempt2/fits.json)
names the clock and corner of every figure. The max-skew, net-delay and
setup-data-delay checks all close (≥ +3.418).

**Worst slack per corner** ([`corners.tcl`](corners.tcl),
[`fits/attempt2/corners.txt`](fits/attempt2/corners.txt)), seeds 1 / 2 / 3 / 4:

| Corner | Setup | Hold | Recovery | Removal | Min pulse |
| --- | --- | --- | --- | --- | --- |
| Slow fix4 100C | 2.109 / 2.061 / 2.114 / 1.988 | 0.009 / 0.021 / 0.013 / 0.013 | 7.190 / 7.267 / 7.312 / 7.208 | 0.050 / 0.062 / 0.055 / 0.105 | 0.220 all |
| Slow fix4 0C | 1.399 / 0.489 / 0.733 / 0.437 | 0.010 / 0.011 / 0.008 / 0.012 | 6.816 / 6.891 / 6.974 / 6.809 | 0.252 / 0.303 / 0.299 / 0.279 | 0.223 all |
| Slow fix4a 0C | 1.408 / 0.502 / 0.736 / 0.437 | 0.011 / 0.011 / 0.008 / 0.013 | 6.828 / 6.906 / 6.984 / 6.806 | 0.214 / 0.260 / 0.259 / 0.242 | 0.225 all |
| Fast fix4 0C | 4.294 / 4.387 / 4.379 / 4.247 | 0.000 / 0.000 / 0.000 / 0.003 | 8.189 / 8.251 / 8.268 / 8.225 | 0.019 / 0.007 / 0.000 / 0.066 | 0.292 all |

**Against the rules.** Device: every seed is under 46,800 (the largest,
45,004.9, by 1,795.1). Timing: every slack ≥ 0 at every corner. Producer:
admitted. **Pass.** The margins are thin: setup +0.437 ns at the slow corner
and removal 0.000 ns at the fast one.

**Reported, not judged:**
- **The 92% line (43,056):** the median is 1,857.3 over. Decision 7 made the
  device, not the 92% line, the DX's line.
- **Against #2171's 486SX median, 41,563.4:** +3,349.9. That difference is
  not a cost of the DX alone: it nets the 387 (on), the cuts, both shell
  cuts and #2187's base, which carries newer main.
- **The first-order expectation** (amendment 2) was about 46,110; the fit is
  1,197 under it.
- **Round 2's `cuts`** (#2149, `7ff764c37`): 1,657 ALMs in the CPU-only
  envelope, for `l12` + `xshift` + `xdisp` + `lfwd`. This build carries `l12`
  + `lfwd` only (Deano's choice): #2149's `l12` 1,176 + round 1's `lfwd` 360 ≈
  1,536 first-order. This record does not measure the cuts' saving in the
  machine; that would need a 387-on machine fit without them.

## G4, CPI

Round 2's windows on `machine_tb --suite cpu-profile`, base and
`--cpu-core i486dx`, level 3, at `63aa526c8` ([`cpi/`](cpi/)). Each pair
handed over at the same cycle (DOOM 2,493,936,063; 3DBench 750,000,005).

| Window | Base CPI | DX CPI | r | Round 2's `cuts` |
| --- | ---: | ---: | ---: | ---: |
| DOOM h2 | 1.668 | 1.765 | **1.058** | ×1.058 |
| 3DBench 8.0–11.5 s | 2.154 | 2.189 | **1.016** | ×1.016 |

The bench charges port 0's first data 18 clocks against the kit's 11, so both
losses are upper bounds. Most of DOOM's comes from L1D and port 0 (16.1% of
h2's clocks on the base, 19.0% on the DX) and AGI (6.5% → 7.2%).

## G5, Quake on the RTL

**The cause.** Two diagnostic runs of round 2's bring-up on the base CPU
(`2d4edf547`, scratch bench edits that print the x87 unit's state; disclosed
in the method):
- At the stall, `00a7:0005256c` `DB E3` (FNINIT), CR0.TS set: `fired = 1`,
  `started = 0`, `nw = 1`. FNINIT was in the unit's no-wait slot, but
  `hold` = `fired` held it.
- `fired` was set at cycle 460,018,215, 65 cycles after the handover, in the
  BIOS (`F000:535D`): a **wrong-path** ESC (op `14F`, `D9 /1` with a memory
  operand, reserved, a form the unit does not build) issued its first uop.
  The unit never took it (`take = 0`), so the branch flush, which clears
  `fired` only with `started`, left it set.
- So it is not the bench edit, and not FNINIT itself: any wrong-path unbuilt
  form wedges every later ESC and FNINIT, on the integer core too, once the
  387 is on. The lockstep never met it: its x87 cases are directed, and its
  random programs run with no 387.

**The case and the fix** (their own PR against main, branch
`deano/x87-wrongpath-fired`; the fix moves the DE10 netlist, so it is not in
this PR):
- Lockstep x87 case `wrong-path-unbuilt`: FNINIT, `CMP AX, AX`, `JZ` over a
  `D9 4F 00`, FNINIT, FLD. On main it fails: `FAIL x87:wrong-path-unbuilt
  @102d.timeout`.
- `fes_i386_x87.sv`: `fired` is set only for an instruction the unit has
  taken (`started || take`). With it the case passes, 126 of 126.

**Quake on the DX with the fix** (a scratch merge, `494479998` + the fix):
see [quake.md](quake.md).

## What this does not establish

- **The CPUID and reset signature are the 486SX's.** The DX identity is i486
  slice 4.
- **No x87 instruction runs at `I486 = 1` in lockstep:** level 4 has no x87
  cases; they run at level 3.
- **The 387's record word and FogCast setting** (x87 slice 7) are not here.
- **No kit run, no D12 proof for the DE25 netlist, no seal.**
- **The cuts' saving inside the machine** is not measured (G3, above).
- **CPI with the 387 in use** (Quake) is not measured.
