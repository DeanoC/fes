# x87 slice 6, step 6.0: what the DE25 387-fitted machine measures

Step 6.0 of the x87 slice 6 plan
([PR #2027](https://github.com/DeanoC/fes/pull/2027), Deano's decisions of
20 November 2026), under the tracker
[#1768](https://github.com/DeanoC/fes/issues/1768). It measures and builds no
RTL.

- **The design:** `pc386_system` from `origin/main` at `8a162c536`, plus
  the two scratch edits (identity on, `X87_BACKEND = 1`).
- **The fits:** 13, on A5EB013BB23BE4SCS with Quartus Pro 26.3, on
  chip-30x0-ai-dev.
- **No seal, no board shell, no kit.**

The method, with its bands and rules, was committed before the first fit:
[method.md](method.md). Every figure below is in
[results.md](results.md), which [generate.py](generate.py) writes from
the committed evidence under [fits/](fits/) and from
[entities.tsv](entities.tsv). This file states no figure those files do
not carry.

## The three answers

### 6.0a: current main sits on the 92% line

All four seeds fit and pass timing at 61.44 MHz, at every corner and on
both clocks. ALMs needed are 43,008 to 43,080 (median 43,048.5, spread 72).
Seeds 1 and 2 are at or above 43,056, by 3 and 24 ALMs. So the
precommitted verdict is **band 4: not established**.

- **The line lies inside this design's own seed spread.** Two seeds fall
  on each side of it, so placement alone decides which side a fit lands.
  That is what "not established" says, and it is the one figure in this
  step that answers D2's question: today's main is at the line, not under
  it.
- **Against #1959 the medians differ by +77.5 ALMs.** The larger of the
  two spreads is 72. The base moved as well as the x87 (#1998, #2003,
  #2022, #2024, and the netlists were elaborated on another host), so no
  PR's cost is read from this.
- **Nothing else is near a limit.** DSP is 42 and M20K 145 on every fit,
  as in #1959. Setup slack is +7.378 ns or more. Hold is positive on every
  seed (+0.014 ns at worst).

### 6.0b: where the ALMs are

The hierarchy-kept fit lands **+0.84%** from the flattened seed 1, inside
the ±5% gate, so its table is the ranking. What it says, at the level the
plan's candidates need:

| Block | ALMs | What it bounds |
| --- | ---: | --- |
| `cpu`, all of it | 31,426 | — |
| `cpu`, its own logic (anonymous, not apportioned) | 9,349 | Candidate E, inside the CPU |
| `cpu/x87`, all of it | **7,557** | The 387 on this board |
| `cpu/x87`, the envelope's own logic | **4,408** | B and F: the arms still on the sequencer at backend 1 live here, beside the stack, the words and the conversions |
| `cpu/x87/g_de25_core` (`fes_x87_core`) | 1,885 | C: de-pipelining (3,267 registers, 9 DSP) |
| `cpu/x87/g_de25_trans` | 1,114 | Not a candidate (needed for the transcendentals) |
| `cpu/x87/mul` (`fes_i386_x87_mul`) | **151** | A: sharing the multiplier saves at most this, plus its 9 DSP |
| `cpu/frontend`, `l1d`, `l1i`, `biu` | 3,835 / 2,312 / 1,609 / 1,506 | E: the CPU entities slice 3 found binding on the DE10 |
| `machine/at`, including `kbc` | 5,060, including 1,422 | E: the AT devices; the keyboard controller is the largest |
| `machine/vga` | 1,849 | E |
| `audio` | 1,293 | E |

**Candidate A is now measured, and it is small.** The multiplier holds
151 ALMs. Sharing it adds an operand mux, so the net saving is that figure
minus the mux's cost. D8 ranked it after 6.0b, and 6.0b ranks it last.

**The envelope's own 4,408 ALMs is the largest x87 figure.** This fit does
not separate how much of it is the sequencer arithmetic that backend 1
still uses (shifts, FPREM, FPREM1, FRNDINT, FSCALE, FXTRACT) from the
state and conversions every 387 needs. Candidate B's A/B is what
measures that.

### 6.0c: the clock

Seed 1 passes at every rung: 61.44, 75, 90, 100, 110 and 120 MHz. Seeds 2
to 4 then pass at 120 MHz as well. Under the method's rule, **the machine
closes at 120 MHz on four seeds.** 120 MHz is the ladder's top rung, so
this is a floor on the machine's ceiling, not the ceiling itself, and the
method adds no rung above it.

- **Area is flat across the clock.** ALMs needed run from 43,008 to
  43,140 over all twelve flattened fits (61.44 to 120 MHz, every seed), so a
  higher clock costs no measurable area here.
- **Slack narrows as expected.** Seed 1's worst setup falls from +7.660 ns
  at 61.44 to +0.906 at 120. Seeds 2 to 4 at 120 MHz hold +0.431 to
  +0.738 ns.
- **The worst paths are the CPU's, with one x87 exception.** At 100 MHz
  the worst `clk` path is inside the x87, from `cpu_x87_wm` to
  `cpu_x87_sh_src`, which feeds the shifter. At every other rung it is a
  CPU path (`cpu_l1d_q_tx`, `cpu_dfwd_*`, `cpu_ex_ag_result`, `cpu_dc0`)
  or an anonymous one. The per-rung table is in results.md.
- **This is data for D3, not a choice.** The DE25 shell lane's plan (PR
  #2030) proposes a 61.44 MHz system clock. These fits say the 387-fitted
  machine would pass timing well above that, in the virtual-pin envelope,
  without a board shell.

## What this changes in the plan

- **D2 has its first measured answer.** Current main sits on the 92%
  line, inside the seed spread, before any board shell is added. The
  shell's cost is still unmeasured; that is the D4 lane's step 6.1.
- **The 6.2 order, read against 6.0b.** The plan's expected order was E or
  D first, then B and F, then C, then A.
  - 6.0b keeps A last, now measured at 151 ALMs.
  - It shows the envelope's 4,408 ALMs as the largest x87 block, which is
    where B and F act.
  - It names the CPU's largest measurable entities for E.
  - Nothing here measures any candidate's saving. Each is still its own
    controlled A/B.
- **C (de-pipelining) waits on D3.** At 120 MHz, timing still passes on
  four seeds. So removing pipeline stages has slack to spend at any clock
  up to 120 MHz, and that slack shrinks as the clock rises.

## Caveats that stand

- **Virtual pins and no shell.** 651 ALMs of virtual pins stand in for a
  DE25 pc386 shell that does not exist yet.
- **Netlists elaborated on chip-30x0-ai-dev with the CUDA-slot Yosys**, not
  on powerboat ([method.md](method.md), "What is fitted").
- **The hierarchy fit's "own" figures cannot be split.** After Yosys keeps
  module boundaries, the CPU's own registers are anonymous, so its 9,349
  ALMs are not apportioned.
- **No lockstep or simulation was run.** The scratch edits are the same two
  that #1907 and #1959 fitted. This step checks only that the backend's
  logic is present in the netlist.
