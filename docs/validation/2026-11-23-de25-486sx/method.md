# DE25 pc386 as a 486SX: fit method (committed before the fits)

Deano, 23 Nov 2026: "start the 486sx explore on that PR now, get it working".
"That PR" is #2036, the DE25 pc386 shell with no 387. This branch is stacked on
#2036's head `dfacd28e1`.

## The change

`boards/de25nano/pc386/build.py` elaborates `pc386_system` with the Yosys top
parameter `I486 = 1` beside the guest rates. No shared source changes, so the
DE10 netlist and `BUILD_ID` do not move (`de10-netlist.md`).

## The fits

- **Seed 1** is the registered DE25 producer,
  `boards/de25nano/producers/build_pc386_package.py`, with its gates: the
  package timing admission and the reviewed signoff findings. Its record names
  the netlist's SHA-256.
- **Seeds 2, 3 and 4** are copies of seed 1's generated project with only
  `SEED` changed in `golden_top.qsf`, compiled with `quartus_sh --flow
  compile`. Each copy's `fes_mailbox/pc386_system.v` must have seed 1's
  SHA-256.
- Quartus Pro 26.3, from a clean checkout of this branch's pushed commit, with
  the pc386 lock's Yosys (`d273c1cb`) and `YOSYS_MAX_THREADS=1`.
- Every Quartus run holds a Pro slot of the Quartus lease
  (`scripts/quartus_jobs.py`), one slot per seed.

## The quantities

- **ALMs:** whole-design `ALMs needed`, each seed and the median of four.
- **RAM and DSP blocks**, from the fit summary.
- **Timing:** for every seed, the worst setup, hold, recovery, removal and
  minimum pulse width over every clock and every corner.

## Rules (fixed now, not changed after a fit)

- **Closure (the pass rule).** All four seeds fit, and every slack is ≥ 0 at
  every corner, the machine clock at 61.437908 MHz. A seed that misses is a
  fail, not a candidate. Three of four is a fail. A miss stops the work and is
  reported. No check is relaxed and no constraint changes.
- **Producer gates.** Seed 1's producer must admit the package on its own
  gates. If it refuses (timing, signoff findings, or a reviewed basis that no
  longer matches the netlist), that is reported as a fail with its reason. Any
  basis change would be a separate, reviewed change, not part of this rule.
- **Capacity.** Every seed's ALMs needed is at most 46,800, the device. No
  lower bound.
- **Against the working line.** The median is compared with 43,056 (92%,
  #2027 D2) and reported as "under" or "over". Over is reported, not tuned in
  this PR: it is an input for Deano.
- **Against #2036.** The median minus #2036's four-seed median, 42,574.0
  (`2026-11-20-de25-pc386-shell-s3/pair.md`), is reported with both spreads
  (#2036's is 97.8). The difference counts as *measured* only when it is
  larger than the larger spread; otherwise it is "within the spread". The
  expected figure is about +290 (#2146, CPU only). This decides measurability
  only. It is not a band.
- **The controlled reference.** #2036's pair (`bc0a124ba`) predates the
  fixed Yosys lock, so against it the difference is uncontrolled. The
  shell-cuts lane is fitting #2036's head `dfacd28e1` itself, four seeds on
  the fixed lock (`~/fes-scratch/ps2fifo-area/board-base23b*` on Powerboat,
  the board build plus SEED copies). If all four of its seeds complete, the
  same measured-or-within-the-spread rule is applied against its median too,
  and that is the controlled figure. Its seed 1 is the board build, not the
  producer; the two differ in the `BUILD_ID` the machine reports.
