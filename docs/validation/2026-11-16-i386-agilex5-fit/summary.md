# How large is the 386 on Agilex 5? 16 November 2026

**The DE25 holds the CPU, the pc386 machine and main's 387 together, where the
DE10 does not — and after doing so it has about 9,500 of its 46,800 ALMs
left.** The 386 is smaller on Agilex 5 in ALM terms and roughly twice as fast.
That is the answer to Deano's question.

The question, in his words: *"we need see the entire size of the 386 on the
agilex 5, it may be bigger or smaller as different fabric and toolchain."* It
gates the DE25 plan, because [#1889](https://github.com/DeanoC/fes/pull/1889)
showed the DE10 cannot hold the machine with the `i386dx+387` identity on —
4,292 LABs needed against the Cyclone V's 4,191, 102.4%, the fitter stopping
before placement — and [#1892](https://github.com/DeanoC/fes/pull/1892) fitted
a standalone x87 arithmetic backend on Agilex 5 at 2,660 ALMs, 9 DSP and
4 M20K at 200 MHz. Nobody had measured the 386 itself on that device.

[method.md](method.md) is the method, committed before any fit ran, with every
amendment dated and stating what had already been seen. Tracker:
[#1898](https://github.com/DeanoC/fes/issues/1898).

## What was measured

Five internal virtual-pin characterisation fits. No board shell, no pins, no
HPS, no PLL, no reset-release IP, no programming artifact, no seal and no
hardware acceptance. Every fit passed setup, hold, recovery, removal and
minimum pulse width at **every published operating condition** of its device —
four on Agilex 5, six on Cyclone V — at a **61.44 MHz** system clock, the
pc386 seal's — four published corners on Agilex 5 and eight on Cyclone V.
Nothing was relaxed.

| Fit | Top | 387 | Device |
| --- | --- | --- | --- |
| **C** | `fes_i386_cpu` | `x87_present` a live virtual input, so main's slice-2 387 is in it | both |
| **M** | `pc386_system` | as main ties it, `wire x87_present = 1'b0` | Agilex 5 (the DE10 fit is queued, see below) |
| **M+** | `pc386_system`, one-character scratch change to `1'b1` | identity on | Agilex 5 |

`pc386_system` **contains** `fes_i386_cpu`, so M is a superset of M's CPU — but
**fit C is not fit M's CPU share**: C holds every `trace_*` and `arch_*` output
alive on a virtual pin, including the 640-bit `arch_x87_regs`, where M leaves
them to be swept. Nothing here adds C to M.

## Cyclone V ALMs and Agilex 5 ALMs are not the same unit

Different fabric, different packing, different toolchain. **This record states
no cross-device ALM ratio and computes none.** It also cannot separate the two
halves of Deano's question: the experiment varies **fabric and toolchain
together** — Cyclone V through Quartus 17.0.2 against Agilex 5 through Quartus
Pro 26.3 — and nothing in it attributes a difference between them. A smaller
Agilex figure does **not** establish a denser fabric. What is comparable is
each device's own utilisation and each device's own achievable clock, and both
are below.

Every figure includes its fit's envelope. Both fitters count it, and the count
sits beside each ALM figure and is **never subtracted**: the CPU fits carry
**2,351** ALMs containing virtual pins on each device (1,306 ports are 4,700
pin bits), and the machine fits carry **651**. A real core has neither — it has
a board shell instead.

## The fits

Quartus Pro **26.3.0 Build 114** on `A5EB013BB23BE4SCS` (Agilex 5, 46,800
ALMs, 4,680 LABs, 188 DSP, 358 M20K); Quartus **17.0.2** on `5CSEBA6U23I7`
(Cyclone V, 41,910 ALMs, 4,191 LABs, 112 DSP, 553 M10K). Seed 1, both on
`powerboat`. Percentages are the fitter's own denominators; ALUTs and
registers are counts, because Quartus states no device total for them.

| Fit | ALMs | % ALMs | LABs | % LABs | ALUTs | Registers | DSP | Block RAM | Block-memory bits |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| C, Agilex 5 | 29,300 | 62.61% | 3,626 | 77.48% | 42,126 | 20,842 | 12 / 188 | 53 M20K / 358 | 408,320 |
| C, Cyclone V | 31,244 | 74.55% | 3,494 | 83.37% | 43,032 | 19,268 | 23 / 112 | 90 M10K / 553 | 409,384 |
| M, Agilex 5 | 35,492 | 75.84% | 4,424 | 94.53% | 55,055 | 33,908 | 24 / 188 | 134 M20K / 358 | 1,350,950 |
| M+, Agilex 5 | 37,255 | 79.60% | 4,590 | **98.08%** | 57,906 | 35,233 | 33 / 188 | 138 M20K / 358 | 1,352,230 |

**Achievable clock.** The constraint is 61.44 MHz and these are that rung's
areas; a fit at a higher constraint is a different fit with a different area.
Restricted Fmax at each device's worst slow corner, the fitter's own estimate:

| Fit | Worst slow corner | Best slow | Fast corner |
| --- | ---: | ---: | ---: |
| C, Agilex 5 | **124.35 MHz** | 142.09 | 225.73 |
| C, Cyclone V | **63.79 MHz** | 65.01 | 135.08 |
| M, Agilex 5 (system clock) | **117.80 MHz** | 132.22 | 207.08 |
| M, Agilex 5 (pixel clock) | 187.30 | 222.37 | — |

So the speed half of Deano's question has a clear answer: on Agilex 5 the CPU
reaches 124 MHz at its worst slow corner and the whole machine 118, where the
Cyclone V reaches 64 — which is why the DE10 is pinned at 61.44 in the first
place. A DE25 would not be. The clock ladder that would turn these estimates
into a closure is in [the ladder section](#the-clock-ladder-and-what-is-not-measured).

## The 387's cost, and what it is not

M+ − M, one seed each: **+1,763 ALMs, +166 LABs, +9 DSP, +4 M20K, +1,325
registers.** The ALM delta is well clear of this lane's established ±200
fit-to-fit noise, so it is real; it is still a single-seed delta. The +9 DSP is
exactly `fes_i386_x87_mul`'s nine 22x22 significand products.

**That is slice 2's cost, not a 387's.** On main every x87 *arithmetic* form
decodes to `X87_UNIMPL`; the arithmetic exists only on #1879 and #1889. So
M+ − M buys the 387's state, its CPU and AT-board interface, its conversions,
its rounding and that shared multiplier — and no arithmetic whatsoever.

It was confirmed rather than assumed that M has the unit swept and M+ has it:
the M netlist contains **zero** x87-multiplier signals, because Yosys removes
the whole unit when `x87_present` is 0, and the M+ netlist contains **66**, the
same as the standalone CPU netlist.

**M+ − M and #1892's backend may not simply be added.** Both carry their own
64x64 significand multiplier and their own rounding; a merged unit would share
one, not build two. So their sum is an **upper bound on those two pieces** and
a **lower bound on a complete 387**, since neither contains remainder, scale,
round-to-integer, transcendentals, BCD or unmasked exception results.

## The budget

At the 61.44 MHz rung, seed 1, on `A5EB013BB23BE4SCS`. The capacity figure is
Quartus's own `ALMs needed [=A-B+C]`: ALMs used in final placement, less what
dense packing would recover, plus what is unavailable. That is the row the
fitter's `Logic utilization` percentage is computed from.

| | ALMs needed | % of 46,800 | Left |
| --- | ---: | ---: | ---: |
| The device has | 46,800 | — | — |
| The machine, 387 off (**measured**, fit M) | 35,492 | 75.84% | 11,308 |
| The machine, main's 387 on (**measured**, fit M+) | 37,255 | 79.60% | **9,545** |

Against those 9,545 ALMs sit three things, and only one is measured:

- **#1892's arithmetic backend: 2,660 ALMs, 9 DSP, 4 M20K at 200 MHz.**
  Measured on this same device and toolchain, so it belongs in this budget
  directly — but as a floor for the arithmetic part alone, and whether it
  combines arithmetically with M+'s 387 is for an integration fit, since both
  carry their own significand multiplier and rounding.
- **The rest of a complete 387: not measured.** Remainder, scale,
  round-to-integer, transcendentals, BCD, unmasked exception results.
- **A DE25 board shell: not measured anywhere.** HPS bridge, EMIF, scanout,
  audio, reset release. On the DE10 the equivalent is about 770 ALMs between
  `pc386_system`'s own logic and the whole sealed core, from the
  [8 November area audit](../2026-11-08-i386-area-audit/summary.md) at a
  different commit — cited, not measured here. The machine fits stand in for
  it with 651 ALMs of virtual-pin cells.

**So: 2,660 measured ALMs of arithmetic backend against 9,545 left, with the
rest of a 387 and a board shell unmeasured.** That is room, and it is the
first time this plan has had any. It is not a clearance to assume the whole
387 fits: the two unmeasured lines are the ones that would consume it, and
only an integration fit on this device settles the question.

### Why this record does not quote a LAB percentage as the budget

M+ uses **4,590 of 4,680 LABs, 98.08%** — and that figure is *not* the
capacity figure, which is why it is here rather than in the table above. A LAB
holds exactly **10 ALMs on both devices** (46,800/4,680 and 41,910/4,191), and
a LAB counts as used when one ALM in it is used. M+ places 40,550 ALMs across
those 4,590 LABs: **8.1 of every 10 slots**, with the fitter estimating
**4,210 ALMs recoverable by dense packing** and 915 unavailable. So 98% of
LABs are touched while a fifth of their slots are free. The same CPU netlist
used 3,512 LABs under one envelope and 3,626 under the other, a 114-LAB swing
on identical logic — larger than the 90 LABs that remain untouched.

**And it is not what #1889 refused on.** That record reports **LABs
*required*, 4,292 of 4,191**, a pre-placement requirement, and says plainly
that neither fit placed so there is no ALM figure at all. The fit reports here
have no "LABs required" row; the row they have is a post-placement fact. The
two are different quantities and this record does not put them on one scale.

**What is comparable with #1889 is the outcome, not a percentage.** On the
DE10 the identity-on machine required more LABs than the device has and never
placed. On Agilex 5 it placed, routed and closed timing at every published
corner. That is the comparison, and it is the one that matters for the plan.

For the record, the LAB occupancy and packing density of every Agilex fit:

| Fit | ALMs needed | % | ALMs in final placement | LABs used | % | ALMs per LAB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| C | 29,300 | 62.6% | — | 3,626 | 77.5% | 8.1 |
| M | 35,492 | 75.8% | 38,750 | 4,424 | 94.5% | 8.0 |
| M+ | 37,255 | 79.6% | 40,550 | 4,590 | 98.1% | 8.1 |

## The clock ladder, and what is not measured

The method precommitted a ladder — 61.44 MHz headline, then 75, 90, 110 and
130 MHz, seeds 1–4 at the headline — because reported Fmax at one constraint
is the fitter's estimate and not a closure. **"Closes at" means the highest
rung passing every analysis at every published corner.** Those rungs, the
extra seeds, and **fit M on the DE10** are queued; any that have not produced
a result when this record is read are **not measured**, and are listed as that
rather than trimmed quietly. So this record contains **no "closes at" figure**
for either device — only the fitter's per-corner estimates above, labelled as
estimates. Extra seeds of M and M+ were added beyond the precommitted set,
because the budget rests on those two fits: an addition at the same rung, not
a relaxation of anything.

Also not measured, and stated as such:

- **Any per-module split.** The `SEAL` recipe flattens, so each fit reports one
  entity. The 8 November audit's no-flatten variant would be needed, and was
  not run here.
- **A machine clock ladder.** M and M+ ran at the headline rung only.
- **Any board I/O timing.** Every virtual port path is cut, including the video
  output. No pin, package or external-device timing is established on either
  device.
- **Anything that would make the LAB percentages comparable to #1889's.**
  Both fitters report LABs partially or completely used, and that is a
  different row from the LABs-required figure #1889 refused on. No fit here
  reproduces #1889's measurement, and none is claimed to.

## The two netlist rewrites

The Agilex side does not read byte-identical input, and this record does not
claim it does. The `SEAL` recipe is structurally device-neutral — no
`synth_intel_alm`, no technology mapping — so each locked Quartus does all of
its own synthesis, mapping, placement and routing. But:

1. The RTL asks for Cyclone V block RAM by name, 30 `ramstyle = "M10K"`
   attributes in the CPU netlist and 57 in the machine's, and Agilex 5 has no
   M10K: `altera_syncram` refuses the name and synthesis stops before
   elaboration. The Agilex side renames exactly that token to `M20K`.
2. Yosys writes both a bare and a valued `parallel_case` on some `casez`
   statements. A bare Verilog attribute means the value 1, so the pair says
   one thing twice; Quartus 17.0.2 warns, Quartus Pro 26.3 stops with
   `Error (22074)`. The Agilex side drops the repeated bare line — 30 of the
   machine netlist's 650, the other 620 untouched. Fork issue:
   [DeanoC/yosys#78](https://github.com/DeanoC/yosys/issues/78).

Both are mechanical, verified by round trip, and counted in each fit's record.
The DE10 side reads the netlist exactly as the pc386 seal lane does. The
block-RAM figures are the weakest part of the comparison for this reason, on
top of M10K being 10 Kb and M20K 20 Kb.

Two tool behaviours were gated rather than noted, because each would have
produced a wrong number quietly:

- Quartus 17.0.2 **ignores** a `VIRTUAL_PIN` assignment whose node name is
  braced (Critical Warning 136021) and leaves the port a real pin. All 1,306
  of the CPU's were ignored, and the fit failed only because the Cyclone V has
  314 pins. Every fit now **gates on the report's own pin count**: more real
  pins than real clocks is a failure.
- The first envelope gave each virtual port a zero input and output delay,
  min and max, as #1892 does. A virtual pin has no package, trace or external
  device, so that invents a **hold** requirement on a path to a pin that does
  not exist; the fitter padded 2e+04 ns of routing delay to meet it and the
  DE10 fit terminated on routing congestion at 74% of ALMs. Every virtual port
  path is now cut instead. The superseded Agilex fit is kept in this record,
  labelled, not replaced: it gave 29,114 ALMs against 29,300 — inside the ±200
  noise band — but 97.77 MHz against 124.35, because the hold padding was
  costing the design's own paths 27 MHz.

## Files

[host.json](host.json) names the revision each fit was prepared from, the
hosts and the comparison rule. [quartus.json](quartus.json) is every fit's
figures with the report row each was read from and that device's totals.
[quartus-inputs.json](quartus-inputs.json) binds each fit's netlist, rewrites,
seed, constraint and Quartus. [timing.tsv](timing.tsv) is every corner's worst
path per check, per fit. Raw fitter and TimeQuest reports stay under the task
worktree's ignored `sources/misteross/build/diagnostic/i386-agilex5/`.

Reproduce a fit with
`scripts/fit_i386_device.py --design {cpu,machine} --device {de10,de25}`, and
collect a record with `scripts/record_i386_device_fits.py`.
