# DE25 pc386 486SX: how fast it clocks, 24–25 November 2026

Deano, 24 Nov 2026: "lets have a session see what 486sx on de25 can clock to".
Refs [#2135](https://github.com/DeanoC/fes/issues/2135) (the DX plan; its
decision 4 asks for a PLL check of a 2× integer core). This is a measurement
study: no RTL changes, and the registered producer still ships 61.437908 MHz.
Three ladders, each method committed and pushed before any of its fits:
[method.md](method.md) (`c1ca3e31c`, amended once at `e0f871976` before any
rung was fitted), [method2.md](method2.md) (`f4de199f6`) and
[method3.md](method3.md) (`7ef458293`). Ladders 2 and 3 exist because the
ladder before each topped out with no failing step.

## Answers

1. **Timing: the full DE25 486SX machine, real board shell included,
   closes every timing analysis on four seeds at 130.555556 MHz (C0 = 24)**,
   and at 111.904762 MHz (C0 = 28) on four more. Every rung's seed 1 from
   74.60 to 142.42 MHz passed. At 142.42 MHz (C0 = 22) seeds 1, 3 and 4
   passed and seed 2 missed by 0.060 ns, so under the ladder rule the
   four-seed claim stays at 130.56 MHz. **The first failing step is
   156.67 MHz (C0 = 20): all four seeds miss setup** (−0.118, −0.168,
   −0.035, −0.243 ns), and 174.07 and 195.83 MHz miss further (seed 1:
   −0.762, −1.297 ns). The analyser's machine-clock Fmax estimate rises with
   the target up to 142 MHz and then stops: 150.9–156.2 MHz at every fit
   from 156.67 MHz up.
   **None of these is admitted by the producer**, which refuses every clock
   but 61.44 MHz before the fitter ([below](#the-producer-cannot-build-any-clock-but-6144-mhz)).
   **With the shipped constraints and signoff bases as written, 92.16 MHz
   (C0 = 34) is the highest rung** ([below](#what-breaks-above-90-mhz-signoff-not-timing)):
   the design assistant's findings equal the control's up to 97.92 MHz and
   gain ten CDC-50003 "Unconstrained" held bundles from 104.44 MHz; the
   reviewed `LOADED` basis's budget for its flag's own route is 0.7 ns at
   97.92 MHz, and that route measures 0.94 ns (2.45 ns with clock skew) in
   the 97.92 MHz fit. 92.16 MHz has one seed.
2. **What limits it.** **At the first failing step, 156.67 MHz, the
   limit is both the CPU's decode stage and the shell's window, in roughly
   measure, the CPU worse on three seeds of four** (resolved owners of the
   negative setup endpoints per seed, Slow fix4 0C: CPU 29, 47, 37, 87;
   shell 24, 31, 2, 102; nothing in the machine's devices). The CPU paths run through the decode stage:
   `cpu_dc` (Yosys's `_35174_`), `cpu_d_out` (`_35180_`), `cpu_dc_valid &&
   cpu_dc_more`, `cpu_desc_t0`. The shell paths are the HPS window's port
   heads into the arbiter (`window|port0|head` → `arbiter|w_address`,
   `port2|head` → `arbiter|writes_owed`), and at 174.07 MHz also into the
   `fes_win_cdc` command FIFO and the ports' command RAMs. The design
   assistant's failing-setup rules appear on the failing fits only
   (TMC-20205, explicit power-up states that restrict retiming; TMC-20214,
   adder chains), and retimed register names first show in the worst paths
   at 142.42 MHz; how much the retimer contributed below that is not
   measured here. Below the failing step, the tightest paths
   are, at every rung and seed: the **CPU's own paths** (CPU→CPU after
   resolving Yosys-merged registers; the decode stage, `cpu_decode_by` →
   `cpu_dc`, the L1I fill/valid and `cpu_d_eip`), then the **shell's window
   `port2|head` → `arbiter|writes_owed`**, then the machine's devices (the
   ATA pointer → AT decoder read data). So the limit is the CPU, with the
   window arbiter close behind. The fixed-bound crossings (the 10 ns
   `set_max_delay` held bundles, +1.0 to +1.6 ns at every rung, and the
   worst setup in #2171) do not move with the machine clock in STA; what
   moves is the signoff view of them (answer 1).
3. **A 2× CPU clock is not the next step.** The CPU alone closes 120.513 MHz
   on four seeds and 125.333 MHz on two of four; the CPU with the whole
   machine closes 125.333 MHz on four (virtual-pin envelopes). 122.88 MHz,
   2 × 61.44, was not fitted; it lies between those two and is not on this
   PLL's VCO. The single machine clock already closes 130.56 MHz in the
   board, so on timing a CPU clock at 2 × 61.44 MHz would close, but it
   would buy nothing over a single raised clock. The trade is elsewhere:
   everything that breaks in signoff above 90 MHz (answer 1) is a
   machine-clock crossing in the shell or the machine, which a 2× CPU with
   the machine left near 61 MHz would not touch. So the choice is
   constraint and basis rework (one raised clock) against RTL at the CPU
   boundary (a second clock). The paths that would cross (CPU↔machine and CPU↔shell, at
   least 255–400 endpoints in each rung's report) are not the problem: their
   slack projected to a 7.979 ns fast period is positive at every rung from
   74.60 MHz up. What a 2× design needs is RTL, not timing: the CPU's bus
   and I/O handshakes with `pc386_machine` assume one clock, so it needs a
   clock enable on the machine side or a registered 2:1 boundary. Not built.
4. **Legal clocks.** The machine PLL keeps its VCO (50 MHz × 188 / 3 =
   3,133.333 MHz) and the audio master (VCO / 255 = 12.287582 MHz; C1 = 255
   in every fit report), so the machine runs at **VCO / C0 for an integer
   C0**. AUDIO's N/D becomes C0 / 65280 (1/1280 only at C0 = 51), and every
   guest rate stays an exact rational under 2^31. The machine↔audio crossing
   is bounded by absolute constraints: its setup row sits at +8.7 to +8.8 ns
   at every rung, integer ratio or not. Nothing else fixes the ratio. Only
   the fitted C0 values are measured; the others are derived. Near each step
   (MHz): 61.44 (C0 51), 62.67 (50), 63.95 (49); 72.87 (43), **74.60 (42)**,
   76.42 (41); 82.46 (38), **84.68 (37)**, 87.04 (36); 89.52 (35),
   **92.16 (34)**, 94.95 (33); **97.92 (32)**, 101.08 (31); **104.44 (30)**,
   108.05 (29); **111.90 (28)**, **116.05 (27)**, **120.51 (26)**,
   **125.33 (25)**, **130.56 (24)**, 136.23 (23), **142.42 (22)**,
   149.21 (21), **156.67 (20)**, **174.07 (18)**, **195.83 (16)**. A 2:1
   pair needs an even C0: 62.67/125.33 (50/25) or 60.26/120.51 (52/26);
   2 × 61.437908 is not on this VCO.

## Recommendation

**Raise the single machine clock. No separate CPU clock, and no CPU RTL fix
yet.** Two steps, Deano's choice:

- **92.16 MHz (C0 = 34), the shipped constraints as they are.** Timing has
  +2.011 ns on seed 1 (one seed fitted), the design assistant's findings
  equal the control's ([`signoff-findings-diff.txt`](signoff-findings-diff.txt)),
  and `LOADED`'s route budget (3.9 ns) covers the measured route (2.54 ns
  with skew). 97.92 MHz needs only `LOADED` re-argued from a longer minimum
  row-fetch time than five machine clocks. The PR moving the
  clock needs AUDIO's ratio written as C0 / 65280, the shared-FIFO-reset
  parent digest reviewed again at the new clock, four seeds through the
  producer with its own gates, the DE25 system bench and Kit D25 HIL.
- **Above 100 MHz, up to the measured 130.56 MHz**, the same PR also needs
  the machine-clock held-bundle bounds in `pc386-clocks.sdc` tied to the
  machine period rather than a flat 10 ns (and the bases that quote them
  re-argued), and `LOADED` re-argued from the real minimum row-fetch time
  rather than five machine clocks. That is constraint and review work, not
  RTL.

Past 130.56 MHz, the next limit is in RTL: the CPU's decode stage and the window's
port-head → arbiter path both need a pipeline cut (and the registers whose
explicit power-up states stop the retimer, TMC-20205, looked at; which module
they are in is not tabulated here) before anything above about 142 MHz
closes. Neither is worth doing before a raised clock has run on
the kit.

## The producer cannot build any clock but 61.44 MHz

At every rung the registered producer, `build_pc386_package.py`, stopped
before the fitter with the same line ([logs/](logs), `producer-s1-*.log`, one
per rung, each `exit 1`):

```text
de25-pc386: shared FIFO reset: unreviewed generated input: peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v
```

`boards/de25nano/scanout/shared_resets.py` checks each generated Qsys file
against a reviewed digest; for pc386 the parent's is `PARENT_SHA256` in
`boards/de25nano/pc386/build.py`. Qsys names the window's machine-clock
interconnect module with a hash of its parameters, which include
`EXPLICIT_CLOCK_RATE` (`…mm_interconnect_1920_xi5gepi` at 61.44 MHz,
`…_ruphjby` at 74.60, `…_ywxqcpy` at 111.90). Measured at every rung, on
each rung's own generated project before derivation:

- [`pinned_inputs.py`](pinned_inputs.py), output
  [`pinned-inputs.txt`](pinned-inputs.txt): at all 14 rungs every pinned
  generated input but the parent has its reviewed pin; the parent differs.
- [`qsys_equivalence.py`](qsys_equivalence.py), output
  [`qsys-equivalence.txt`](qsys-equivalence.txt): every other rung's
  generated project against C0 = 42's, over every `.v`, `.sv`, `.sdc` and
  `.vh` (593 files) outside the PLL's own directory, with the one
  interconnect name the pair does not share normalised. At all 13 the only
  differences are the two netlist copies and the scanout wrapper's
  `BUILD_ID` line; 0 unexpected.
- The control's generated project was rewritten by the derivation, so it is
  compared through the pins: its pinned inputs are the pins (the producer
  admitted it), and its interconnect module equals C0 = 42's apart from the
  name (checked by `diff` after normalising the name).

So every rung's fit used the same shell as its own generation would have,
apart from the `BUILD_ID` constant and that one module name. A new clock needs
the parent digest reviewed again; nothing was changed here to
get past it. Under amendment 1 every rung was fitted through the control's
generated project with only the rung's PLL IP and netlist swapped in
([`compile_rung.sh`](compile_rung.sh); each seed's `diff -rq` against the
control is in [logs/](logs), `swap-*.diff`: the PLL files, the two netlist
copies, and for seeds 2–4 `golden_top.qsf`). **The producer's post-fit gates
did not run at any rung**: no package timing admission and no
reviewed-signoff-findings verdict is claimed above 61.44 MHz.

## What breaks above 90 MHz: signoff, not timing

**Design assistant rules** ([`drc_table.py`](drc_table.py) over every kept
project's `golden_top.tq.drc.signoff.rpt`; bold = differs from the control):

| Rule | Severity | control | c42 | c37 | c34 | c32 | c30 | c28s1 | c28s2 | c28s3 | c28s4 | c27 | c26 | c25 | c24s1 | c24s2 | c24s3 | c24s4 | c22s1 | c22s2 | c22s3 | c22s4 | c20s1 | c20s2 | c20s3 | c20s4 | c18 | c16 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| CDC-50001 1-Bit Asynchronous Transfer Not Synchronized | High | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 | 67 |
| CDC-50002 1-Bit Asynchronous Transfer with Insufficient Constraints | High | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 | 6 |
| CDC-50003 CE-Type CDC Transfer with Insufficient Constraints | High | 0 | 0 | 0 | 0 | 0 | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** | **10** |
| CDC-50102 Synchronizer after CDC Topology with Control Signal | Low | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 | 1 |
| LNT-30010 Nets Driving both Reset and Clock Enable Signals | Low | 2 | 2 | 2 | 2 | 2 | **3** | **3** | **3** | **3** | 2 | **3** | 2 | **3** | **3** | **3** | 2 | **3** | 2 | 2 | **3** | 2 | **3** | **3** | **3** | 2 | 2 | 2 |
| LNT-30023 Reset Nets with Polarity Conflict | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **1** | **1** | 0 | **1** | 0 | **1** | 0 | **1** | 0 | 0 |
| RDC-50003 Multiple Asynchronous Reset Synchronizers in the Same Clock Domain | Medium | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 |
| TMC-20011 Missing Input Delay Constraint | High | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 | 2 |
| TMC-20012 Missing Output Delay Constraint | High | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | 5 |
| TMC-20200 Paths Failing Setup Analysis with Impossible Requirements | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **2** | **3** |
| TMC-20204 Endpoints of Paths Failing Setup Analysis with Retiming Restrictions | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **3** | 0 | **22** | 0 | **2** | **6** |
| TMC-20205 Endpoints of Paths Failing Setup Analysis with Explicit Power-Up States that Restrict Retiming | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **26** | **23** | **2** | **43** | **60** | **21** |
| TMC-20206 DSP Blocks with Unregistered Outputs that are the Source of Paths Failing Setup Analysis | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **1** | **2** | 0 | **2** | **2** | **2** |
| TMC-20207 DSP Blocks with Unregistered Inputs that are the Destination of Paths Failing Setup Analysis | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **1** | **2** | 0 | **2** | **2** | **2** |
| TMC-20208 RAM Blocks with Unregistered Outputs that are the Source of Paths Failing Setup Analysis | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **3** | 0 | 0 | 0 | **1** | 0 |
| TMC-20209 Paths Failing Setup Analysis with High Routing Delay due to Congestion | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **58** | 0 | 0 | **9** | 0 |
| TMC-20214 Buses with Incoming Paths Failing Setup Analysis with Multiple Sequential Adder Chains | Medium | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | **1** | 0 | 0 | **4** | **14** | **3** | **8** | **6** | **8** |
| TMC-20602 Registers with High Timing Path Endpoint Tension | Low | 23 | **24** | 23 | **24** | **22** | **21** | **21** | **24** | **24** | **24** | **21** | 23 | 23 | **26** | **24** | **40** | 23 | **30** | **58** | **21** | **36** | **33** | **36** | **31** | **35** | **30** | **24** |

- **CDC-50003 (High), 0 → 10 between 97.92 and 104.44 MHz, at every seed
  from there up.** The ten are the held bundles with an end in the machine
  clock: the display crossing's command and reply payloads, the GP request
  and reply words, the pixel counters' copy, the window grant base and the
  window fault record. Each is bounded by `set_max_delay 10.000` (with
  skew, net and data delay) in `pc386-clocks.sdc`, and the design assistant
  now reports their skew, net or data delay as "Unconstrained". The boundary
  is where the machine period (9.574 ns at 104.44 MHz, 10.213 ns at 97.92)
  falls below the 10 ns bound. The SDC's own comment justifies 10 ns as "at
  most one fast system period": above 100 MHz the machine is the fastest
  clock.
- **LNT-30010 (Low), 2 → 3 on most seeds from 104.44 MHz.** The extra net is
  a fitter copy of `cpu_biu_reset` (`~_Duplicate_1`), the kind #2187's basis
  already admits.
- **LNT-30023 (Medium, reset nets with polarity conflict), 0 → 1** on five
  of the eight seeds at 142.42 and 156.67 MHz, none below. Not examined.
- **TMC-20200 to TMC-20214 (Medium)** appear only on the failing fits: they
  describe the failing setup paths (retiming restrictions, explicit power-up
  states, adder chains, congestion).
- **TMC-20602 (Low)** varies with the seed at every rung (21–58).

The findings themselves, not just the counts, equal the control's at every
rung through 97.92 MHz ([`drc_findings.py`](drc_findings.py), output in
[`signoff-findings-diff.txt`](signoff-findings-diff.txt); LNT-30010 rows are
keyed by their driving net and TMC-20602 tension is left out, because the
fitter picks their example endpoints and counts). At 104.44 MHz the only new
rows are the ten CDC-50003 bundles.

**Reviewed bases that name the machine period** (`signoff_bases.py`). The
`LOADED` argument: the loaded-row report is still for at least five machine
clocks, the pixel side needs at most 50.4 ns of that, and the rest is what
the flag's own route into its first synchroniser stage may take (31.0 ns at
61.44 MHz); that route is cut by a false path, so nothing bounds it.
[`loaded_route.tcl`](loaded_route.tcl) measured it (`get_timing_paths
-false_path`, every corner, under the lease) in the kept fits; the worst
corner is Slow fix4 0C:

| Rung | MHz | Budget 5T − 50.4 ns | Route data delay | Clock skew | Delay + skew | Holds |
| ---: | ---: | ---: | ---: | ---: | ---: | --- |
| control | 61.44 | 31.0 | 0.966 | −1.511 | 2.477 | yes |
| c42 | 74.60 | 16.6 | 1.051 | −1.500 | 2.551 | yes |
| c37 | 84.68 | 8.6 | 0.871 | −1.494 | 2.365 | yes |
| c34 | 92.16 | 3.9 | 1.029 | −1.510 | 2.539 | yes |
| c32 | 97.92 | 0.7 | 0.940 | −1.507 | 2.447 | **no** |
| c30 | 104.44 | −2.5 | 1.126 | −1.504 | 2.630 | **no** |

`FETCH` and `WINDOW_BASE` need the 10 ns bundle bound under two machine
clocks: 2T > 10 ns holds below 200 MHz, every rung here.

## The fits

**Critical path at each step** (seed 1, worst machine-to-machine setup
over every corner; fitter-copy suffixes dropped; `_N_` names are Yosys-merged
registers, mostly the CPU's decode stage):

| C0 | MHz | Seed 1 worst machine→machine setup | From | To |
| ---: | ---: | ---: | --- | --- |
| 51 | 61.438 | +4.290 | `system\|cpu_dfwd_ready[19]` | `system\|cpu_d_eip[1]` |
| 42 | 74.603 | +2.956 | `system\|_35174_[51]` | `system\|cpu_r_dst[5]` |
| 37 | 84.685 | +2.231 | `system\|_35174_[149]` | `system\|cpu_x87_iss_part[0]` |
| 34 | 92.157 | +2.011 | `system\|cpu_l1i_blk[3]` | `system\|cpu_l1i_valid[232]` |
| 32 | 97.917 | +1.512 | `system\|_35172_[252]` | `system\|cpu_d_eip[2]` |
| 30 | 104.444 | +1.279 | `system\|_37000_[289]` | `system\|cpu_x87_iss_part[0]` |
| 28 | 111.905 | +0.954 | `system\|cpu_decode_by[0]` | `system\|_35174_[110]` |
| 27 | 116.049 | +0.815 | `system\|_37000_[288]` | `system\|cpu_s_tgt[15]` |
| 26 | 120.513 | +0.636 | `system\|_36999_[0]` | `system\|_35180_[20]` |
| 25 | 125.333 | +0.284 | `system\|_37001_[290]` | `system\|cpu_d_eip[21]` |
| 24 | 130.556 | +0.316 | `system\|_35172_[76]` | `system\|_35178_[24]` |
| 22 | 142.424 | +0.112 | `system\|cpu_dc_load` | `system\|_35180_[84]` |
| 20 | 156.667 | -0.118 | `window\|port0\|head` | `arbiter\|w_address[31]` |
| 18 | 174.074 | -0.762 | `window\|port2\|head` | `window\|port1\|command_burstcount[0][0]` |
| 16 | 195.833 | -1.297 | `system\|cpu_r_dst[1]` | `system\|_35172_[80]` |

Powerboat, Quartus Pro 26.3, each under a Pro slot of the Quartus lease.
Control: the producer at the method commit, admitted (package `e8d37ea9…`,
netlist `01185e2f…`). Rungs: [`fitrung.sh`](fitrung.sh) generated each rung's
project from its pushed scratch branch, and [`compile_rung.sh`](compile_rung.sh)
compiled it. Every seed's figures are in [`fits/`](fits) (`rung_summary.py`
output: every row of the timing summary by analysis, clock and corner; ALMs;
the PLL as fitted; the path classification); [`results.py`](results.py)
prints these tables from them. The run logs are in [logs/](logs).

| C0 | MHz | Commit | `make_rung.py` diff SHA-256 | Netlist SHA-256 |
| ---: | ---: | --- | --- | --- |
| 51 | 61.438 | `c1ca3e31c` (method) | — | `01185e2f…` |
| 42 | 74.603 | `b63615a7a` | `af2a509a…` | `71873859…` |
| 37 | 84.685 | `0e1158447` | `fff81c32…` | `0da90f6e…` |
| 34 | 92.157 | `71b2f18bf` | `bbcd7e1f…` | `3ef0858c…` |
| 32 | 97.917 | `5e5c8f39b` | `49be2d5e…` | `ffb9ffb4…` |
| 30 | 104.444 | `7df48e2ac` | `e02559f6…` | `5090f243…` |
| 28 | 111.905 | `8e9e7268c` | `14596498…` | `0be022e0…` |
| 27 | 116.049 | `287a460e4` | `1b229d23…` | `0e484dfa…` |
| 26 | 120.513 | `041ecaed5` | `8c2a49ce…` | `c7698aad…` |
| 25 | 125.333 | `33e5f3cec` | `852a3d64…` | `42053bb9…` |
| 24 | 130.556 | `eee970148` | `ef7a6fd3…` | `e5652d60…` |
| 22 | 142.424 | `d5307ceba` | `ed055651…` | `1a3554b8…` |
| 20 | 156.667 | `f1457fc76` | `986f1ce0…` | `b39ec7f1…` |
| 18 | 174.074 | `45450fe59` | `366a1ade…` | `a135fe77…` |
| 16 | 195.833 | `8c3361dd7` | `6424f960…` | `7bef9797…` |

The rung branches are `deano/de25-486sx-fmax-cNN`; they are scratch and never
merge.

| Rung | MHz | Seed | ALMs | Machine→machine setup | Machine clock setup row | Machine clock hold row | Setup | Hold | Recovery | Removal | Minimum Pulse Width | Max Skew | Net Delay | Setup Data Delay | Fmax est. | Pass |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| control | 61.438 | 1 | 41,686.9 | +4.290 | +1.604 | +0.000 | +1.604 | +0.000 | +7.018 | +0.063 | +0.220 | +6.892 | +3.426 | +3.426 | 83.43 MHz | pass |
| c42 | 74.603 | 1 | 41,644.5 | +2.956 | +1.688 | +0.000 | +1.452 | +0.000 | +6.608 | +0.001 | +0.220 | +6.953 | +3.501 | +3.501 | 95.71 MHz | pass |
| c37 | 84.685 | 1 | 41,648.2 | +2.231 | +1.745 | +0.001 | +1.272 | +0.000 | +7.196 | +0.011 | +0.220 | +7.017 | +3.564 | +3.564 | 104.42 MHz | pass |
| c34 | 92.157 | 1 | 41,650.7 | +2.011 | +1.585 | +0.000 | +1.560 | +0.000 | +7.347 | +0.086 | +0.220 | +6.923 | +3.405 | +3.405 | 113.12 MHz | pass |
| c32 | 97.917 | 1 | 41,651.6 | +1.512 | +1.512 | +0.002 | +1.512 | +0.000 | +7.357 | +0.069 | +0.220 | +7.189 | +3.704 | +3.704 | 114.94 MHz | pass |
| c30 | 104.444 | 1 | 41,613.4 | +1.279 | +1.279 | +0.000 | +1.279 | +0.000 | +7.022 | +0.066 | +0.220 | +7.048 | +3.604 | +3.604 | 120.55 MHz | pass |
| c28 | 111.905 | 1 | 41,689.1 | +0.954 | +0.954 | +0.000 | +0.954 | +0.000 | +6.538 | +0.034 | +0.220 | +6.721 | +3.337 | +3.337 | 125.28 MHz | pass |
| c28 | 111.905 | 2 | 41,728.7 | +1.120 | +1.120 | +0.002 | +1.120 | +0.000 | +6.576 | +0.013 | +0.220 | +6.540 | +3.561 | +3.561 | 127.94 MHz | pass |
| c28 | 111.905 | 3 | 41,667.6 | +0.893 | +0.893 | +0.003 | +0.893 | +0.000 | +6.324 | +0.028 | +0.220 | +6.719 | +3.523 | +3.523 | 124.33 MHz | pass |
| c28 | 111.905 | 4 | 41,664.0 | +0.846 | +0.846 | +0.000 | +0.846 | +0.000 | +6.505 | +0.001 | +0.220 | +6.702 | +3.620 | +3.620 | 123.61 MHz | pass |
| c27 | 116.049 | 1 | 41,732.2 | +0.815 | +0.815 | +0.001 | +0.815 | +0.000 | +6.218 | +0.003 | +0.220 | +6.325 | +3.369 | +3.369 | 128.17 MHz | pass |
| c26 | 120.513 | 1 | 41,757.2 | +0.636 | +0.636 | +0.000 | +0.636 | +0.000 | +5.597 | +0.020 | +0.220 | +6.230 | +3.496 | +3.496 | 130.53 MHz | pass |
| c25 | 125.333 | 1 | 41,699.1 | +0.284 | +0.284 | +0.000 | +0.284 | +0.000 | +5.433 | +0.054 | +0.220 | +5.794 | +3.379 | +3.379 | 129.97 MHz | pass |
| c24 | 130.556 | 1 | 41,703.8 | +0.316 | +0.316 | +0.000 | +0.316 | +0.000 | +5.068 | +0.096 | +0.220 | +5.651 | +2.843 | +2.843 | 136.18 MHz | pass |
| c24 | 130.556 | 2 | 41,721.3 | +0.222 | +0.222 | +0.000 | +0.222 | +0.000 | +5.252 | +0.035 | +0.220 | +5.673 | +3.520 | +3.520 | 134.46 MHz | pass |
| c24 | 130.556 | 3 | 41,766.6 | +0.014 | +0.014 | +0.000 | +0.014 | +0.000 | +5.191 | +0.018 | +0.220 | +5.543 | +3.668 | +3.668 | 130.8 MHz | pass |
| c24 | 130.556 | 4 | 41,811.9 | +0.355 | +0.355 | +0.001 | +0.355 | +0.000 | +5.106 | +0.079 | +0.220 | +5.635 | +3.410 | +3.410 | 136.91 MHz | pass |
| c22 | 142.424 | 1 | 41,894.4 | +0.112 | +0.112 | +0.000 | +0.112 | +0.000 | +4.525 | +0.050 | +0.220 | +5.246 | +3.544 | +3.544 | 144.74 MHz | pass |
| c22 | 142.424 | 2 | 41,841.7 | -0.060 | -0.060 | +0.000 | -0.060 | +0.000 | +3.986 | +0.100 | +0.220 | +5.049 | +3.388 | +3.388 | 141.22 MHz | **FAIL** |
| c22 | 142.424 | 3 | 41,824.9 | +0.009 | +0.009 | +0.001 | +0.009 | +0.000 | +4.609 | +0.041 | +0.220 | +5.057 | +3.438 | +3.438 | 142.61 MHz | pass |
| c22 | 142.424 | 4 | 41,843.2 | +0.005 | +0.005 | +0.000 | +0.005 | +0.000 | +4.697 | +0.120 | +0.220 | +5.044 | +3.593 | +3.593 | 142.53 MHz | pass |
| c20 | 156.667 | 1 | 42,217.8 | -0.118 | -0.118 | +0.000 | -0.118 | +0.000 | +3.719 | +0.035 | +0.220 | +4.627 | +3.645 | +3.607 | 153.85 MHz | **FAIL** |
| c20 | 156.667 | 2 | 42,239.1 | -0.168 | -0.168 | +0.000 | -0.168 | +0.000 | +3.835 | +0.059 | +0.220 | +4.603 | +3.545 | +3.545 | 152.67 MHz | **FAIL** |
| c20 | 156.667 | 3 | 42,185.0 | -0.035 | -0.035 | +0.000 | -0.035 | +0.000 | +4.053 | +0.028 | +0.220 | +4.651 | +3.451 | +3.451 | 155.84 MHz | **FAIL** |
| c20 | 156.667 | 4 | 42,138.0 | -0.243 | -0.243 | +0.000 | -0.243 | +0.000 | +3.731 | +0.054 | +0.220 | +4.654 | +3.350 | +3.350 | 150.94 MHz | **FAIL** |
| c18 | 174.074 | 1 | 42,602.5 | -0.762 | -0.762 | +0.000 | -0.762 | +0.000 | +3.510 | +0.007 | +0.220 | +4.108 | +3.504 | +3.504 | 153.7 MHz | **FAIL** |
| c16 | 195.833 | 1 | 42,534.9 | -1.297 | -1.297 | +0.003 | -1.297 | +0.001 | +2.659 | +0.032 | +0.220 | +3.527 | +3.610 | +3.643 | 156.18 MHz | **FAIL** |

| Rung | Seed | Owner pair (resolved): worst machine-to-machine setup slack, Slow fix4 0C (endpoints reported) |
| --- | ---: | --- |
| control | 1 | cpu->cpu +4.290 (14235); shell->shell +6.323 (703); machine->machine +6.835 (3544); machine->anonymous +7.483 (365); anonymous->shell +7.888 (143) |
| c42 | 1 | cpu->cpu +2.956 (13967); shell->shell +4.181 (701); machine->machine +4.693 (3851); machine->anonymous +5.542 (371); cpu->machine +5.661 (210) |
| c37 | 1 | cpu->cpu +2.231 (13906); shell->shell +3.017 (742); machine->machine +3.199 (3836); machine->anonymous +4.297 (340); cpu->machine +4.298 (205) |
| c34 | 1 | cpu->cpu +2.014 (13611); shell->shell +2.549 (702); machine->machine +2.812 (3910); machine->anonymous +3.720 (384); cpu->machine +3.746 (160) |
| c32 | 1 | cpu->cpu +1.512 (13724); shell->shell +1.866 (716); machine->machine +2.324 (3891); cpu->machine +2.868 (178); machine->anonymous +2.888 (425) |
| c30 | 1 | cpu->cpu +1.279 (13475); shell->shell +1.639 (710); machine->machine +2.024 (4137); cpu->machine +2.795 (141); anonymous->anonymous +2.863 (284) |
| c28 | 1 | cpu->cpu +0.958 (13721); shell->shell +1.034 (709); machine->machine +1.439 (3842); machine->anonymous +1.693 (398); cpu->machine +2.244 (179) |
| c28 | 2 | cpu->cpu +1.120 (13578); shell->shell +1.273 (800); machine->machine +1.666 (4059); cpu->anonymous +2.132 (13); anonymous->anonymous +2.148 (575) |
| c28 | 3 | cpu->cpu +0.893 (13973); shell->shell +1.104 (855); machine->machine +1.798 (3686); cpu->machine +1.933 (158); cpu->anonymous +2.048 (4) |
| c28 | 4 | cpu->cpu +0.846 (14135); shell->shell +1.326 (713); cpu->machine +1.838 (140); machine->machine +1.865 (3578); anonymous->anonymous +2.226 (198) |
| c27 | 1 | cpu->cpu +0.815 (14748); shell->shell +0.963 (716); machine->machine +1.312 (3465); anonymous->anonymous +2.018 (194); machine->anonymous +2.180 (304) |
| c26 | 1 | cpu->cpu +0.636 (13790); shell->shell +0.719 (734); cpu->machine +1.319 (258); machine->machine +1.369 (3567); anonymous->anonymous +1.663 (303) |
| c25 | 1 | cpu->cpu +0.284 (13922); shell->shell +0.662 (716); machine->machine +0.992 (3772); anonymous->anonymous +1.342 (535); cpu->machine +1.574 (151) |
| c24 | 1 | cpu->cpu +0.316 (13825); shell->shell +0.372 (805); machine->machine +0.695 (3695); anonymous->anonymous +0.992 (200); cpu->machine +1.382 (163) |
| c24 | 2 | cpu->cpu +0.232 (14600); shell->shell +0.563 (709); machine->machine +0.693 (3357); anonymous->anonymous +1.048 (454); anonymous->cpu +1.380 (69) |
| c24 | 3 | anonymous->anonymous +0.020 (481); anonymous->cpu +0.079 (5110); anonymous->machine +0.378 (345); cpu->anonymous +0.443 (868); cpu->cpu +0.493 (9544) |
| c24 | 4 | cpu->cpu +0.355 (14636); shell->shell +0.550 (770); machine->machine +0.587 (3259); anonymous->anonymous +1.023 (241); cpu->machine +1.498 (194) |
| c22 | 1 | cpu->cpu +0.112 (12819); shell->shell +0.201 (715); cpu->anonymous +0.328 (2074); anonymous->anonymous +0.387 (320); machine->anonymous +0.573 (399) |
| c22 | 2 | anonymous->cpu -0.060 (204); cpu->cpu -0.021 (13281); cpu->anonymous +0.167 (1629); shell->shell +0.211 (718); anonymous->anonymous +0.422 (426) |
| c22 | 3 | cpu->cpu +0.009 (12812); machine->anonymous +0.084 (537); shell->shell +0.198 (717); cpu->anonymous +0.278 (1308); anonymous->anonymous +0.388 (457) |
| c22 | 4 | cpu->cpu +0.005 (12416); shell->shell +0.254 (718); anonymous->anonymous +0.410 (688); cpu->anonymous +0.518 (1459); anonymous->cpu +0.659 (381) |
| c20 | 1 | shell->shell -0.118 (811); cpu->cpu -0.099 (13628); anonymous->cpu -0.082 (479); cpu->anonymous -0.082 (971); anonymous->anonymous -0.057 (482) |
| c20 | 2 | cpu->cpu -0.168 (14059); cpu->anonymous -0.135 (943); shell->shell -0.129 (814); anonymous->anonymous -0.059 (301); machine->machine +0.186 (2573) |
| c20 | 3 | cpu->cpu -0.035 (10758); shell->shell -0.022 (794); anonymous->cpu -0.004 (1874); anonymous->anonymous -0.003 (1741); cpu->anonymous +0.176 (1285) |
| c20 | 4 | cpu->cpu -0.243 (10747); shell->shell -0.206 (939); anonymous->anonymous -0.157 (745); cpu->anonymous +0.111 (1741); machine->anonymous +0.124 (501) |
| c18 | 1 | shell->shell -0.762 (829); cpu->cpu -0.754 (13759); anonymous->cpu -0.718 (151); anonymous->anonymous -0.696 (346); cpu->anonymous -0.559 (898) |
| c16 | 1 | cpu->cpu -1.297 (11884); shell->shell -1.280 (775); anonymous->anonymous -1.259 (541); cpu->anonymous -1.161 (968); anonymous->cpu -1.131 (1781) |

"Machine→machine setup" is the worst period-scaled path (launch and latch the
machine clock) from `paths.tcl` over every corner. The machine clock's summary
rows also hold the fixed-bound crossings it latches, so at the lower rungs the
summary's machine setup row is a 10 ns bundle, not a machine path. The worst
hold over every clock is 0.000 or +0.001 ns at every seed; the machine clock's own hold
row is 0.000 to +0.003 ns, never negative. The worst removal is always
`adv7513_config` (the HDMI transmitter's I2C configuration clock, +0.001 to
+0.096 ns), which no rung changes.

**Ownership.** Path ends are owned by the CPU (`system|cpu_*`), the machine
(`system|machine_*`, `pixel_*`, `frame_*`), the shell (outside `system|`) or
are anonymous (`system|_N_`). Many anonymous registers are registers Yosys
merged, followed in the netlist by `assign {cpu_dc[…], …} = _35174_;`
(`_35174_` is the CPU's decode stage). `rung_summary.py`'s `resolver` gives
such a register to the CPU or the machine when every name it drives belongs
to one of them. That resolution was added after method.md and is labelled
"resolved" wherever it is used; the unresolved counts are in each JSON's
`machine_pairs`.

## The first failing step: 156.67 MHz

Four seeds, every one failing setup on the machine clock only (no hold,
recovery or removal failure at any corner, and nothing at the 100C or fast
corners at this rung). Negative setup endpoints, Slow fix4 0C, by resolved
owner pair:

| Seed | Worst setup | Negative endpoints | CPU→CPU | CPU↔anonymous | Shell→shell | Worst path |
| ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | −0.118 | 54 | 11 | 18 | 24 | `window\|port0\|head` → `arbiter\|w_address[31]` |
| 2 | −0.168 | 80 | 45 | 2 | 31 | `cpu_desc_t0` → `cpu_dc[…]` (`_35174_[153]`) |
| 3 | −0.035 | 40 | 15 | 22 | 2 | `cpu_dc[…]` → `cpu_dc[…]` (`_35174_[141]` → `[254]`) |
| 4 | −0.243 | 191 | 87 | 0 | 102 | `cpu_dc[…]` (`_35174_[106]`) → `_35179_` |

(CPU↔anonymous counts the endpoints with one end in the CPU and the other
an anonymous register the resolver could not attribute; anonymous→anonymous
adds 1–2 per seed.) The
ALMs grow with the target from about 41,700 to 42,100–42,600, as the
retimer and duplication add registers; every seed still fits the device
(46,800) with room.


## Q3 in numbers

| Envelope fit (`fit_i386_device.py`, chip-30x0-ai-dev) | Clock | Seeds passing | Worst setup per seed (ns) | ALMs |
| --- | ---: | ---: | --- | ---: |
| `fes_i386_cpu`, I486 = 1 | 125.333 MHz | 2 of 4 | +0.006, −0.189, −0.353, +0.019 | 35,004–35,071 |
| `fes_i386_cpu`, I486 = 1 | 120.513 MHz | 4 of 4 | +0.009, +0.031, +0.020, +0.054 | 35,023–35,090 |
| `pc386_system`, I486 = 1 | 125.333 MHz | 4 of 4 | +0.365, +0.789, +0.495, +0.732 | 35,096–35,157 |

Both netlists were elaborated on Powerboat by the pc386 lock's Yosys (HIP
slot) from the method commit with [`envelope-i486.patch`](envelope-i486.patch)
applied (`fes_i386_cpu.netlist.v` `dc4eb486…`, `pc386_system.v` `cc36f031…`,
both with a 22-bit `eflags`), and fitted on chip with `--netlist`, because
chip has no install of that Yosys. The CPU alone carries 2,352 ALMs of virtual
pins, which is why it is no smaller than the machine, and probably why it
closes lower than the machine and the board do. Each fit's `fit.json`,
`inputs.json`, `netlist.json` and `timing.json` are in
[`fits/envelope/`](fits/envelope).

From the board fits (resolved owners, Slow fix4 0C, seed 1), the machine-clock
paths with exactly one end in the CPU, and their slack if the relationship
were cut from the machine period to 7.979 ns with everything else equal:

| C0 | MHz | CPU-boundary endpoints reported | Worst boundary slack at 7.979 ns | Boundary endpoints negative | Worst CPU-internal slack at 7.979 ns | CPU-internal endpoints negative |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 51 | 61.438 | 388 | -0.260 | 39 | -4.007 | 4181 |
| 42 | 74.603 | 392 | +0.236 | 0 | -2.469 | 2877 |
| 37 | 84.685 | 255 | +0.469 | 0 | -1.598 | 2413 |
| 34 | 92.157 | 360 | +0.874 | 0 | -0.858 | 1869 |
| 32 | 97.917 | 386 | +0.635 | 0 | -0.721 | 943 |
| 30 | 104.444 | 393 | +1.200 | 0 | -0.316 | 55 |
| 28 | 111.905 | 398 | +1.287 | 0 | +0.001 | 0 |
| 27 | 116.049 | 340 | +1.613 | 0 | +0.177 | 0 |
| 26 | 120.513 | 478 | +1.001 | 0 | +0.318 | 0 |

The worst boundary path at every rung is the CPU's bus interface head
(`cpu_biu_head`, `cpu_biu_e_line`) into the EMS board's memory read data
(`machine_ems_mem_rdata`, `machine_ems_p2_write`). The counts are of the
20,000 worst machine-clock endpoints `paths.tcl` reports, so the endpoint
counts and "negative" are lower bounds when the window is full. The
projection keeps each path's placement at its rung: a fitter asked for the
fast clock would place differently.

## Deviations from the methods

- Envelope netlists were elaborated on Powerboat (the pc386 lock's Yosys is
  not installed on chip) and fitted on chip with `--netlist`. Two earlier
  envelope launches (`…-fmax-env-cpu-b1d375`, `…-fmax-env-449cb1`) failed in
  setup before any fit (no `nvcc` on the remote's PATH; then the missing
  Yosys); a third was refused by the lease guard for inline shell code. None
  produced a fit.
- Under method.md's amendment 1, seed 1 at every rung is the compile path,
  not the producer (above).
- `rung_summary.py`'s owner resolution, the negative-path owner groups, and
  `drc_findings.py`, `loaded_route.tcl`, `pinned_inputs.py` and `check.py`
  were written after method.md; each reads results and changes no rule.
  [`check.txt`](check.txt) is `check.py`'s output: every fit's PLL is M 188,
  N 3, C1 255 and its rung's C0; every rung's seeds compiled one netlist;
  every machine↔audio setup path is a fixed bound, slack +8.63 to +9.44 ns.

## Where things are

- Raw projects, reports and path files: Powerboat
  `/data/projects/fes-486sx-fmax/` (`wt-<rev>` worktrees with each rung's
  generated project, `k<rev>-s<seed>/project` for every compiled seed, the
  `paths/` reports in each; `env-wt` is the envelope worktree with
  `envelope-i486.patch` applied). Envelope fits: chip-30x0-ai-dev
  `/data/projects/fes-quartus/runs/c1ca3e31cbee-20261124T210244Z-fmax-env-32a840`
  (125.333 MHz) and `…-20261124T220801Z-fmax-env-cpu8298-8457da`
  (120.513 MHz).
- The commits the record cites live on this branch and on the scratch rung
  branches `deano/de25-486sx-fmax-c{42,37,34,32,30,28,27,26,25,24,22,20,18,16}`.

## What this does not establish

- **Admission above 61.44 MHz.** No package was built at any rung; the
  producer's timing admission and signoff findings review did not run.
- **Four seeds at 92.16 MHz**, the highest rung under the shipped
  constraints: it has seed 1 only.
- **Function at a raised clock.** No simulation or kit run at any rung. The
  guest rates are exact at every rung by construction, but the window's DDR
  bandwidth per machine clock, the governor's headroom and the scanout's
  frame pacing at a faster machine are not examined here.
- **A clock-crossing CPU.** Not built (question 3 asked for timing evidence
  only).
