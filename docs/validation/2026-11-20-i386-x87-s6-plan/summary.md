# x87 slice 6: the plan, for Deano to decide on

Slice 6 of the x87 phase ([#1768](https://github.com/DeanoC/fes/issues/1768))
was named on 15 November 2026 as "full design optimisation, and acceptance".
Since then its ground has moved twice: the DE25 became the 387's home
(16 November), and the transcendentals landed on the DE25 backend (#1959).
This record restates what slice 6 is for now. **It builds no RTL and runs no
fit.** Every figure is cited from a committed record, and each one names that
record.

The plan has four parts: what is settled, what binds today, the candidates
with their costs, and an order. The decisions Deano must make are collected
at the end.

## What is settled

- **15 Nov.** The switch flip (the identity's record word, its FogCast
  setting, `x87_present`) is slice 7. Slice 6 is a full design optimisation
  and may trade performance on the 387-fitted build.
- **16 Nov.** The DE25 is the 387's home. The DE10 387 is not pursued:
  slices 2 to 4 need 4,292 LABs against 4,191 before placement. The envelope
  is shared and the arithmetic backend is chosen by `X87_BACKEND` (0 is the
  DE10 sequencer, 1 is the DE25 pipeline).
- **17–18 Nov.** The transcendentals and BCD are built once, on the DE25
  backend (#1959). They follow the 387's method, within the agreed bounds.
- **16 Nov, census.** No reduced-precision unit: every computing title leaves
  significands wider than 53 bits in the register file. 71 of 162 x87 forms
  are executed by no title in the census. They must be correct but need not
  be fast.
- **20 Nov.** Defects #1998 and #2003 and the operand probe #2022 have
  merged (`1c7623e2a`).

## 1. Which board

**The DE25 only, for the 387.** The DE10 ships identity-off: `pc386_system`
ties `x87_present` to `1'b0`, and the unit folds away in that build. Slice
4's identity-off seal was 36,463 ALMs (87.0%)
([slice 4](../2026-11-15-i386-x87-s4/summary.md)). No reduction slice 6
could find closes the DE10's 102.4% LAB requirement. The 15 November
option of two DE10 packages (with and without the 387) is overtaken by the
16 November decision.

**The DE10 still pays for every shared-RTL edit.** The envelope,
`fes_i386_x87.sv`, is in the DE10 pc386 record's source closure. An edit to
it moves that package's `BUILD_ID`, even when the logic folds away. So every
RTL slice below still carries #1768's DE10 gates: lockstep on the RTL and on
the locked-Yosys netlist, test386, a Quartus 17.0.2 identity-off seal, and a
kit run on it.

## 2. What binds on the DE25 387-fitted build today

**Source: the latest fit record, not a fit of current main.** The latest
fits are #1959's: four seeds of `pc386_system` with `x87_present = 1` and
`X87_BACKEND = 1`, elaborated from `39e66a3e8`. They were fitted on
A5EB013BB23BE4SCS with Quartus Pro 26.3 on chip-30x0-ai-dev, at 61.44 MHz,
with virtual pins
([fits.md](../2026-11-17-de25-x87-transcendentals/fits.md),
[quartus.json](../2026-11-17-de25-x87-transcendentals/fits/quartus.json)).

| Resource | #1959, four seeds | Of the device | Binding? |
| --- | --- | --- | --- |
| ALMs needed (the capacity row) | 42,943–43,010, median 42,971 | 91.76–91.90% | **Yes** |
| 92% line (43,056) | 46 ALMs of room on the worst seed | — | **Yes** |
| ALMs in final placement, seed 1 | 44,657 | 95.42% | — |
| Packing difficulty, seed 1 | Medium | — | A warning sign |
| LABs touched (spread, not fullness) | 4,657–4,672 | 99.5–99.8% | Not a capacity row (see below) |
| DSP blocks | 42 of 188 | 22.3% | No |
| M20K | 145 of 358 | 40.5% | No |
| Worst setup, `clk`, 61.44 MHz | +7.553 to +7.780 ns | — | No |
| Worst hold | +0.019 to +0.032 ns | — | No, positive on every seed |

**ALMs bind, and timing does not.** On the DE10, slice 3 found that
whole-machine timing bound at about 95%: the L1D, the decoder's
`cpu_decode_by[]`, the front end's aligner. On the DE25 at 61.44 MHz there
is about 7.6 ns of setup slack, and the worst endpoints are still CPU and
presenter paths (`cpu_dfwd_ready`, `cpu_l1d_q_tx`,
`machine_presenter_*`), not the x87. Slice 3's lesson, "don't optimise the
arithmetic for timing", carries over. But the scarce resource here is ALMs,
not slack.

**What this does not establish:**

- **Drift since `39e66a3e8` is unmeasured.** #1998, #2003 and #2022 all
  changed `fes_i386_x87.sv` after it, and other main commits moved the
  machine. With 46 ALMs of room, today's main could sit on either side of
  92%. This record gives no verdict on that.
- **92% is not an Agilex 5 ceiling.** #1959's method says so: it is the DE10
  lane's prior. Nothing above 91.90% has been fitted on this device, and no
  fit there has failed for capacity.
- **The LAB row.** #1907's bands treated LABs ≥ 99.5% as "not established".
  #1959's method dropped that clause ("one threshold, on one quantity"), and
  all four #1959 seeds sit at 99.5–99.8%. The Agilex record explains why
  that row measures spread: slices 3 and 4 added 4,132 ALMs over M+ and
  touched zero more LABs. Under #1907's wording these seeds would read "not
  established". Under #1959's, they read band 2.
- **No DE25 pc386 board shell exists.** There are no pins, PLL, producer or
  package. In the fits, 651 ALMs of virtual pins stand in for the shell (the
  DE10's shell is about 770). The real shell's cost is unmeasured.
- **The clock.** 61.44 MHz is the DE10's rung. The machine has no timing
  ladder on the DE25. The CPU alone closes at 130 MHz and misses at 150
  ([Agilex fit](../2026-11-16-i386-agilex5-fit/summary.md)).

**Where the ALMs are, as far as the records allow.** The chain below joins
fits made on different bases. Only the last step is a controlled
comparison.

| Fit | ALMs | Record |
| --- | ---: | --- |
| CPU alone | 29,300 | Agilex fit, C |
| Machine, 387 off (M) | 35,492 | Agilex fit, M |
| Machine with main's slice-2 387 (M+) | 37,255 | Agilex fit, M+ |
| Plus envelope on the backend | 40,967–41,012 | #1959 baseline (`e2b8130b5`) |
| Plus transcendentals and BCD | 42,943–43,010 | #1959; **+1,994.5 controlled** |

So the 387 on the DE25 is roughly 7,500 ALMs (42,971 − 35,492). That figure
is uncontrolled and good only as an order of magnitude. No per-entity
(`--hierarchy`) fit exists on Agilex 5, so nothing is apportioned within
the x87 or within the CPU on this device.

**Where the DSP blocks are.** 42 = 12 in the CPU (fit C) + 12 in the rest of
the machine (M is 24) + 9 in `fes_i386_x87_mul` (M+ is 33) + 9 in the
backend's FMUL (`fes_x87_fast`). The brief's "42 against the CPU's 12"
includes the machine's own 12.

## 3. The candidates, with costs

The binding resource sets the ranking: **ALMs freed, net**. DSP and M20K are
the slack resources, so a candidate that spends them to save ALMs is the
right direction on this board. A candidate that saves them by spending ALMs
is the wrong one.

| # | Candidate | What it buys | Cost and evidence | Status |
| --- | --- | --- | --- | --- |
| A | **Share one multiplier** (the brief's headline) | ≤ 9 DSP, which aren't binding. ALMs: only `fes_i386_x87_mul`'s own adder tree. | At `X87_BACKEND = 1`, every multiplier use except `S_SHIFT` is guarded out, so it survives **only as the shifter**. It is 176–187 ALMs on Cyclone V (census area.md, slices 2 and 3). Sharing adds a 2×64-bit operand mux and arbitration against a pipe that accepts one operation per clock. | **Unmeasured on Agilex. Net ALMs could be negative.** Ranked low. |
| B | **Remainder onto the backend, then delete the sequencer arithmetic at `X87_BACKEND = 1`** | ALMs held by the FPREM/FPREM1 recurrence, FRNDINT, FSCALE and FXTRACT arms, which still run on the sequencer at backend 1 | This is the third DE25 step ("envelope, transcendentals, remainder"). The census shows FPREM, FPREM1 and FXTRACT executed by no title. The forms already pass lockstep on the sequencer, so the only motive is area. The microengine (`fes_x87_trans`) may take them as microcode for ROM, not logic. | **Unmeasured. Sign unknown.** |
| C | **Fewer pipeline stages in the backend** | Registers and latency. #1892 is built for 200 MHz at one operation per clock, but the envelope keeps one ESC in flight. The controlled A/B put the backend at +1,746 registers and −462 ALMs against the sequencer. The fast forms lose a fixed 2–9 clocks to its ~15-clock response. | Only valid if the DE25 clock target stays well under the backend's 200 MHz (decision D3) | **Unmeasured.** |
| D | **Logic into memory**: the duplicated register file `rf_a`/`rf_b` (2 × 8 × 80 bits in registers), decode tables, other wide constant tables | ALMs and registers moved into M20K or MLAB, of which 59.5% of M20K is free | Each move needs a registered read. A registered read is the x87's recurring defect: a value assigned in one state and read in the next. | **Unmeasured.** Needs the per-entity fit to find the large tables. |
| E | **Whole-machine reduction**: the 14 Nov deferred "system reduction phase" | Whatever the largest entities hold. The CPU is about 68% of the machine's ALMs (29,300 of 42,971, uncontrolled). | The #1584 area audit's identified wins are already banked (#1802, #1795, #1638). New wins need a fresh per-entity reading on Agilex. | **Unmeasured.** Needs 6.0b. |
| F | **Rare forms through shared paths**: the 71 forms no title executes | Dedicated logic for forms that can be slow | It overlaps B and D. Which of the 71 own dedicated logic is not known. | **Unmeasured.** |
| G | Divide algorithm chosen by precision; more than one ESC in flight | Cycles only (the envelope record's two remedies) | Both add ALMs on the board where ALMs bind. Divide is at most 1.8% of x87 work. Independent x87 pairs in the corpus are unmeasured. | **Not proposed** for slice 6 |
| H | Hard FP DSP blocks | — | Ruled out on 16 Nov: FP32 lacks x87 precision and range, and needs exact rounding tails | **Not proposed** |

No candidate here has a measured ALM saving on Agilex 5. Ranking any of them
before 6.0b would repeat the mistake slices 3 and 4 recorded, where each
architecture argument was contradicted by the next measurement.

## 4. What slice 7 needs from slice 6

Slice 7 turns the identity on. It needs:

1. **A DE25 pc386 package**: board shell, pins, PLL, producer, a Quartus Pro
   seal path, and a board run path. None of this exists, and none of it is
   optimisation (decision D4). The 486DX needs it too
   ([i486 plan](../2026-11-17-i486-plan/summary.md)).
2. **Room for that shell.** That is the shell's measured cost, in place of
   the 651 virtual-pin ALMs, plus a margin. This is what sets slice 6's ALM
   target (D2).
3. **`x87_present` and `X87_BACKEND` as real per-board settings.** Today
   they are two scratch edits in every DE25 fit. The DE10 settings are 0/0
   and the DE25 settings are 1/1, whatever D6 decides.
4. **Seed-stable fit and timing of the identity-on DE25 machine at the
   chosen clock:** four seeds in band 2, with bands committed before the
   fits.
5. **The governor's 387 ESC classes.** This is the original slice-6
   performance identity, so that acceptance titles are judged at the 387's
   pace.
6. **Acceptance**: Quake's timedemo, Screamer, the FPU benchmarks, Linux
   2.0.0 with its 387, and WfW 3.11. This needs item 1, so it can't run in
   slice 6 unless the shell lands first (D7).

## 5. Order

Each RTL step is one PR, with #1768's gates plus the DE25 four-seed fit.

- **6.0, measurement. No RTL. Starts as soon as D1–D3 are answered.**
  - **a.** Fit current main (identity on, backend 1), four seeds. Use
    #1959's bands verbatim: band 4 is ALMs ≥ 92% (43,056) on any seed. Label
    the comparison with #1959 as uncontrolled. This settles the drift.
  - **b.** A per-entity (`--hierarchy`, the #1584 method) Agilex 5 fit of
    the same tree. It apportions the CPU's entities, the machine's devices,
    the envelope, `fes_x87_core`, `fes_x87_trans` and `fes_i386_x87_mul`.
    The output is the ranked list that candidates A–F are measured against.
  - **c.** Only if D3 asks for a clock above 61.44 MHz: a machine clock
    ladder on the DE25, rungs committed beforehand.
- **6.1, the shell's cost** (if D4 puts the shell in this lane): fit
  `pc386_system` inside a DE25 board top and replace the virtual-pin
  stand-in with a measured figure.
- **6.2, reductions, largest measured saving first.** Each one is a
  controlled A/B (one tree, one change, four seeds a side), judged against
  the larger of the two arms' spreads. Expected order on present evidence:
  E or D (where the ALMs are), then B and F together (remainder and rare
  forms through the microengine), then C (if D3 allows), then A (last,
  and only if 6.0b shows the shifter's adder tree is worth more than the
  mux).
- **6.3, the governor's 387 classes.**
- **6.4, acceptance**, which moves to slice 7 under D7.

## Decisions for Deano

| | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| **D1** | Which board slice 6 targets | DE25 only / also the DE10 | **DE25 only.** The DE10 stays identity-off and takes only the reseal that shared-RTL edits force. |
| **D2** | The DE25 ALM target slice 6 works to | Keep 92% (43,056) as the line / set an Agilex number / "room for shell + 486 SX delta (350–850 ALMs) + margin" | Keep 92% as the working line until 6.0a and 6.1 measure the drift and the shell, then set the target from them. |
| **D3** | The DE25 clock for the 387-fitted machine | 61.44 MHz, DE10 parity / higher, with a ladder (6.0c) | Decide before C (de-pipelining) is considered. At 61.44, timing is not binding. |
| **D4** | Who builds the DE25 pc386 shell and package | Slice 6 / slice 7 / a separate lane | **A separate lane, started now.** It isn't x87 work, both slice 7 and the 486DX need it, and its cost sets D2. |
| **D5** | The remainder step (FPREM/FPREM1/FRNDINT/FSCALE/FXTRACT on the backend) | Fold into slice 6 as an area candidate (B) / drop it, since the forms already work at backend 1 / do it as planned regardless of area | **Fold into slice 6 and judge it on area.** No title executes FPREM, FPREM1 or FXTRACT. |
| **D6** | The fate of `X87_BACKEND = 0` (the sequencer arithmetic) | Keep it as the DE10's reference / delete it once B lands | Delete it once B lands, per CLAUDE.md's rule on superseded code. It can never be a complete 387, because the transcendentals are backend-1 only. Lockstep's reference is the Bochs oracle, not this RTL. |
| **D7** | Where acceptance (kit titles) sits | Slice 6 / slice 7 | **Slice 7.** It needs the identity-on DE25 package, which D4 builds. |
| **D8** | The multiplier-sharing headline | Pursue first / measure in 6.0b and rank it there | **Rank it in 6.0b.** On this board, DSP is the slack resource and ALMs are binding. |

## Sources

- [Slice 2](../2026-11-14-i386-x87-s2/summary.md),
  [slice 3](../2026-11-15-i386-x87-s3/summary.md),
  [slice 4](../2026-11-15-i386-x87-s4/summary.md),
  [the game census and its area reading](../2026-11-15-i386-x87-game-census/area.md)
- [The Agilex 5 fit, #1899](../2026-11-16-i386-agilex5-fit/summary.md);
  [the envelope, #1907](../2026-11-16-de25-x87-envelope/summary.md) and its
  [merged fits](../2026-11-16-de25-x87-envelope/integration-fit/merged/README.md)
- [The transcendentals, #1959](../2026-11-17-de25-x87-transcendentals/summary.md):
  [fits.md](../2026-11-17-de25-x87-transcendentals/fits.md) and the bands in
  [method.md](../2026-11-17-de25-x87-transcendentals/method.md)
- [The operand probe, #2022](../2026-11-20-i386-x87-exc9/summary.md): it
  changed RTL after #1959's fits and has no fit of its own
- [The i486 plan](../2026-11-17-i486-plan/summary.md): the 92% line on the
  DE25 and the 486 deltas
- RTL read at `1c7623e2a`:
  `sources/misteross/cores/fes-common/rtl/i386/fes_i386_x87.sv` (the
  `mul_start` sites, of which only `S_SHIFT` is unguarded at backend 1) and
  `sources/misteross/cores/fes-pc386/rtl/pc386_system.sv` (`x87_present`
  tied off)
