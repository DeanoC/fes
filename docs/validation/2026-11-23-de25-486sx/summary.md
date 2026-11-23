# DE25 pc386 as a 486SX (I486 = 1, no 387), 23 November 2026

Deano, 23 Nov 2026: "start the 486sx explore on that PR now, get it working
then add 486 only games to the set". "That PR" is #2036, the DE25 pc386
shell with no 387. This branch is stacked on #2036's head `dfacd28e1`.
Tracker [#1962](https://github.com/DeanoC/fes/issues/1962).

The fit method, [method.md](method.md), was committed (`d1616104c`) before
any fit ran. The fits ran at `d1616104c`; later commits change only this
record and `tests/test_build_de25_machine_package.py`, neither in the
machine's source closure.

**Result.** The DE25 pc386 builds and closes as a 486SX. All four seeds fit
and close every analysis at 61.437908 MHz; seed 1, the registered producer,
admits the package on its own gates. The median is **41,563.4 ALMs**,
**+116.3** over #2036's head fitted the same way (measured: the larger spread
is 63.1), 1,492.6 under the 92% line (43,056) and 5,236.6 under the device
(46,800). The DE10 pc386 netlist and design identity do not move. FreeDOS
boots on the RTL at I486 = 1 on both the DE25 system bench and the machine
sim, and the level-4 lockstep passes on the RTL and the netlist.

| Gate | Result |
| --- | --- |
| DE10 pc386 netlist | Byte-identical. `de10_netlist.py` at `dfacd28e1` and at `5544f9bc7` (the RTL-side change) from one checkout path, `~/fes-scratch/arm-486sx`: both `65cbd41232883f713ef6c120757a1fa95aced1d8047af86d891491a960274daf`, `cmp` equal. Yosys `d273c1cb` (binary `0594dfdb…`), `YOSYS_MAX_THREADS=1`. The digest is the one the A-dma2 branch recorded for `dfacd28e1` |
| DE10 pc386 design identity | Unchanged. `de10_design_id.py` (the registered producer's `create_build_record`, selection-only) gives `BUILD_ID 426e518dcd8092e1f87dd72386f428a7` at `dfacd28e1` and at `d1616104c`, which includes the shared sim edits |
| Level-4 directed cases, RTL | 486SX 148 of 148 agree; 386 56 agree and 92 stop as expected; two runs each. Equal to #2087's `directed/both-rtl.json` in all 1,786 lockstep and 971 self-check values ([`lockstep/level4-rtl.json`](lockstep/level4-rtl.json)) |
| Level-4 random programs, RTL | All 56,000 keep step. Equal to #2087's `level4-486sx-rtl.json` in all 943 random values (every trace hash, retirement and form count) |
| Level-4, netlist (Yosys `d273c1cb`) | 486SX 148 of 148 cases and all 56,000 random programs; equal to #2087's netlist report in all 3,740 values except 14 netlist, Yosys-lock and oracle-build digests ([`lockstep/level4-netlist.json`](lockstep/level4-netlist.json)). #2087 ran on Yosys `e240a465`; the CPU sources are unchanged since |
| DE25 system bench, I486 = 1 | PASS. `boards/de25nano/pc386/sim.py`: the derived `pc386_system` behind the board's window path boots FreeDOS to the prompt at 3.063 simulated seconds; every check of the system suite passes ([`sim/de25-system-486sx.json`](sim/de25-system-486sx.json)). The Verilated CPU's EFLAGS is 22 bits (`/*21:0*/ …cpu__DOT__eflags`), the I486 = 1 width |
| Machine sim, RTL at I486 = 1 | FreeDOS boot: PASS. `sim_fes_pc386.py --suite boot --cpu rtl --cpu-level 4`: the prompt and `dir` at 3.5 simulated seconds, 145,543,067 retirements, 0 oracle steps, no stops, the VGA trace replays against Bochs's VGA with 0 mismatches ([`sim/machine-boot-rtl-486sx.json`](sim/machine-boot-rtl-486sx.json)). Game rows: <!-- GAMES --> |
| DE25 fit, four seeds | Pass ([the fits](#the-fits)): every seed closes every analysis; median 41,563.4 ALMs; +116.3 over `dfacd28e1`, measured |

## The fits

[`fit486sx.sh`](fit486sx.sh) at `d1616104c` on Powerboat, Quartus Pro 26.3,
23 Nov 2026, each seed under a Pro slot of the Quartus lease. Seed 1 is the
registered producer, `build_pc386_package.py`: BUILD_ID
`4da642f9cab7f730359b51e251b495b0`, RBF `56c75370…`, `package_timing_admission`
true ([`fits/build-summary.json`](fits/build-summary.json),
[`fits/signoff-timing.json`](fits/signoff-timing.json)). Seeds 2–4 are copies of its
project with only `SEED` changed. All four fitted the same machine netlist,
`7816b5397e01015a608fac40d9655ed92baa35e4da9c216b92e29f796c60a024`.
[`fit_summary.py`](fit_summary.py) reads every figure below from the reports
([`fits/486sx-d1616104c.json`](fits/486sx-d1616104c.json)).

| Seed | ALMs needed | RAM | DSP | Setup | Hold | Recovery | Removal | Min pulse width | Max skew | Net delay |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 41,571.4 | 168 | 24 | +1.510 | 0.000 | +7.230 | +0.151 | +0.220 | +7.106 | +3.671 |
| 2 | 41,532.7 | 168 | 24 | +1.617 | 0.000 | +6.609 | +0.045 | +0.220 | +7.076 | +3.659 |
| 3 | 41,595.8 | 168 | 24 | +1.247 | 0.000 | +6.324 | +0.056 | +0.220 | +7.078 | +3.622 |
| 4 | 41,555.4 | 168 | 24 | +1.571 | 0.000 | +6.961 | +0.054 | +0.220 | +6.992 | +3.556 |
| **Median** | **41,563.4** | | | | | | | | | |
| Spread | 63.1 | | | | | | | | | |

Worst over all four seeds and every clock and corner: setup +1.247 ns,
hold 0.000 ns, recovery +6.324, removal +0.045, minimum pulse width +0.220.
Each seed's worst setup is on the pixel clock
(`fes_video_inst|video_clock|iopll_0_outclk0`); its worst hold is 0.000 with no
negative slack or TNS anywhere, on the machine clock (seed 3: the 100 MHz
`pll_inst` clock).
The JSON names the clock and corner of every figure.

**Against the method's rules.**
- **Closure: pass.** All four seeds, every slack ≥ 0 at every corner.
- **Producer gates: pass.** Seed 1's producer admitted the package.
- **Capacity: pass.** Every seed is at most 46,800.
- **The 92% line: under.** The median is 1,492.6 under 43,056; the largest
  seed, 41,595.8, is 1,460.2 under.
- **Against #2036's head, controlled: +116.3, measured.** The shell-cuts
  lane fitted `dfacd28e1` four seeds the same way (board build plus SEED
  copies, the same Yosys lock, 23 Nov 2026; `~/fes-scratch/ps2fifo-area/board-base23b*`):
  41,404.4, 41,463.2, 41,446.4 and 41,447.8, median 41,447.1, spread 58.8,
  every seed closing ([`fits/base-dfacd28e1.json`](fits/base-dfacd28e1.json)).
  41,563.4 − 41,447.1 = 116.3, larger than the larger spread (63.1). That
  baseline ran without `YOSYS_MAX_THREADS=1` (netlist `f7bff7c7…`), so its
  netlist bytes are not a threads-1 elaboration; the comparison is of the
  same source at the same Yosys commit.
- **Against #2036's pair (`bc0a124ba`, 42,574.0): −1,010.6, uncontrolled.**
  That pair predates the fixed Yosys lock and #2036's later shell changes
  (its RAM was 295 blocks; this build's is 168).
- **The expected +290** was #2146's CPU-only envelope figure. The board
  measures less: +116.3. Why is not established here.

## What changed

- **`boards/de25nano/pc386/build.py`.** The machine elaborates with the Yosys
  top parameter `I486 = 1` beside the guest rates (`machine_parameters()`).
  The model is `486sx`; the record carries `i486`, so a 386 and a 486SX DE25
  build cannot share a design identity. No shared RTL changes. The parameter
  is a top parameter of `pc386_system`, which already passes it to
  `fes_i386_cpu` and sets the 486SX reset state (#2045).
- **`boards/de25nano/producers/build_pc386_package.py`.** The model's
  description names the 486SX.
- **The DE25 system bench** (`pc386/sim.py`, `sim/pc386_de25_sim.sv`) takes
  `I486` and sets it from `build.py`, so it tests the identity the producer
  builds.
- **The machine sim** (`scripts/sim_fes_pc386.py`, `machine_tb.cpp`):
  `--cpu rtl --cpu-level 4` now runs the RTL at I486 = 1 beside the level-4
  oracle. `machine_tb` reads EFLAGS AC and ID back, and starts the RTL with
  the 486SX's reset signature (042Bh) in EDX, not the DX4 oracle's 0480h.
  At level 3 nothing changes.

## What this does not establish

- **No D12 proof for this netlist.** #2036's pending D12 proof is for the 386
  netlist; it does not carry over.
- **No kit run.** DE25 pc386 needs slice 4's runtime first (#2115, 4e–4g).
- **The governor bench at I486 = 1** is #2087's, on the same CPU sources; it
  was not rerun here.
