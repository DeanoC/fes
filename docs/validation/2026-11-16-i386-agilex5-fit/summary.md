# How large is the 386 on Agilex 5? 16 November 2026

**The CPU, the pc386 machine and `main`'s 387 together take 79.6% of the
DE25's ALMs, leaving 9,545 of its 46,800. And `fes_i386_cpu` closes at
130 MHz on Agilex 5 and misses at 150** — 61.44, 75, 90, 110 and 130 MHz each
passed every analysis at every published corner, the top one with +0.426 ns to
spare; 150 MHz fails setup by −0.485 ns at two of the four and 170 MHz by
−1.039 ns at three. On the Cyclone V the same CPU closes at **61.44 MHz**, the
only rung run there. Seed 1 throughout. The 124 MHz figure that appears below
is the 61.44 MHz rung's own restricted-Fmax **estimate**, not a closure.

**What was not measured, and what the DE25 plan actually turns on:** whether
the DE25 holds the 387 the DE10 *refused*. `main`'s 387 is slice 2 — state,
interface, conversions, no arithmetic at all — and the DE10 **holds it with
the identity on**: [#1821](https://github.com/DeanoC/fes/pull/1821)'s
identity-on machine fit ran **nine seeds at 38,075–38,257 ALMs, 90.9–91.3% of
the Cyclone V, every one meeting setup and hold, worst +0.454 ns**. (That PR's
sealed package, 36,298 ALMs, has the identity **off**, so it is the DE10
equivalent of fit M rather than of M+.)
[#1889](https://github.com/DeanoC/fes/pull/1889)'s refusal was its own head,
slice 4 stacked on slice 3, whose 387 has divide, square root and the
remainders: 8,073 ALMs for the standalone unit against the merge base's 4,076.
Nothing in this record fits that design. Settling it needs #1889's head through
this runner, and that is listed under [what is not
measured](#the-clock-ladder-and-what-is-not-measured).

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

Fifteen internal virtual-pin characterisation fits: the three designs below
at the **61.44 MHz** system clock the pc386 seal uses, the CPU's clock ladder
on Agilex 5 from 75 to 170 MHz, the direct-SystemVerilog cross-check, and one
superseded envelope kept for comparison. No board shell, no pins, no HPS, no
PLL, no reset-release IP, no programming artifact, no seal and no hardware
acceptance.

**Thirteen of the fifteen passed** setup, hold, recovery, removal and minimum
pulse width at **every published operating condition** of their device — four
on Agilex 5, eight on Cyclone V. The other two are the ladder's **150 and
170 MHz rungs, which miss**, and they are in the record precisely because they
do: they are what bounds the closure from above. Nothing was relaxed for any
of them, and no rung's verdict was revisited.

| Fit | Top | 387 | Device |
| --- | --- | --- | --- |
| **C** | `fes_i386_cpu` | `x87_present` a live virtual input, so main's slice-2 387 is in it | both |
| **M** | `pc386_system` | as main ties it, `wire x87_present = 1'b0` | both |
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
**2,351** ALMs containing virtual pins on each device (its 149 ports become
4,700 virtual pin bits), and the machine fits carry **651** (43 ports, 1,301
pin bits). A real core has neither — it has
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
| M, Cyclone V | 37,108 | 88.54% | 3,946 | 94.15% | 56,271 | 30,756 | 40 / 112 | 274 M10K / 553 | 1,354,284 |
| M+, Agilex 5 | 37,255 | 79.60% | 4,590 | 98.08% | 57,906 | 35,233 | 33 / 188 | 138 M20K / 358 | 1,352,230 |

**Achievable clock.** The constraint is 61.44 MHz and these are that rung's
areas; a fit at a higher constraint is a different fit with a different area.
Restricted Fmax at each device's worst slow corner, the fitter's own estimate:

| Fit | Worst slow corner | Best slow | Fast corner |
| --- | ---: | ---: | ---: |
| C, Agilex 5 | **124.35 MHz** | 142.09 | 225.73 |
| C, Cyclone V | **63.79 MHz** | 65.01 | 135.08 |
| M, Agilex 5 (system clock) | **117.80 MHz** | 132.22 | 207.08 |
| M, Cyclone V (system clock) | **63.83 MHz** | 65.04 | — |
| M, Agilex 5 (pixel clock) | 187.30 | 222.37 | — |

The figures in that table are the **fitter's estimates at a 61.44 MHz
constraint, not closures**. The closures come from the ladder below: the CPU
closes at **130 MHz** on Agilex 5 and at **61.44 MHz** on the Cyclone V, which
is the only rung that has been run there. The machine has **no ladder at all**,
on either device, so its 117.80 MHz and 63.83 MHz are estimates and nothing
more. These are each device's separate figures and no ratio between them is
stated here.

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

**Nor is the *outcome* comparable with #1889, because it is not the same
design.** #1889's identity-on machine carries slice 3 and slice 4's
arithmetic; M+ carries `main`'s slice 2, which the DE10 seals without
difficulty. "Agilex 5 placed what the DE10 could not" would need #1889's head
fitted here, and it has not been.

**And the DE10 settles it directly.** The same design, fit M, on the device
that refused #1889: **3,946 of 4,191 LABs, 94.15%** — and it placed, routed
and passed every analysis at all eight corners, at 88.54% of ALMs. So 94% of
LABs used is a successful fit on this very device, which is what the LAB row
being a capacity wall would have ruled out. Note too that the two M fits sit
at almost the same LAB percentage (94.15% and 94.53%) while their ALM
percentages are 12 points apart.

LAB occupancy and packing density of every fit:

| Fit | ALMs needed | % | ALMs in final placement | LABs used | % | ALMs per LAB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| C, Agilex 5 | 29,300 | 62.6% | — | 3,626 | 77.5% | 8.1 |
| M, Agilex 5 | 35,492 | 75.8% | 38,750 | 4,424 | 94.5% | 8.0 |
| M+, Agilex 5 | 37,255 | 79.6% | 40,550 | 4,590 | 98.1% | 8.1 |
| C, Cyclone V | 31,244 | 74.6% | 30,672 | 3,494 | 83.4% | 8.9 |
| M, Cyclone V | 37,108 | 88.5% | — | 3,946 | 94.2% | 9.4 |

## The clock ladder, and what is not measured

The method precommitted a ladder — 61.44 MHz headline, then 75, 90, 110 and
130 MHz, seeds 1–4 at the headline — because reported Fmax at one constraint
is the fitter's estimate and not a closure. **"Closes at" means the highest
rung passing every analysis at every published corner.** Those rungs and the
extra seeds were queued and then stopped unrun; any that have not produced
a result when this record is read are **not measured**, and are listed as that
rather than trimmed quietly. The CPU's closure on Agilex 5 is in the ladder
table below; the CPU on the Cyclone V has only its 61.44 MHz rung, and the
machine has no ladder on either device, so **no closure is claimed for those
three**. A third CPU seed at the headline rung was run last, to verify the corrected
timing gate end to end through the runner rather than only against a retained
project. Extra seeds of M and M+, and the DE10's rungs above 61.44 MHz, were
queued and then **stopped unrun** to free the shared builder lease for the front-end
cross-check, which had been waiting on it for three hours. They are therefore
not measured, and the one seed that did complete — the CPU's second seed at the
headline rung, 73 ALMs from the first — is in the ladder table.

**The measurement that would settle the DE25 plan, and has not been made.**
`pc386_system` from #1889's head — slice 4 on slice 3, the 387 with divide,
square root and the remainders — through this runner on Agilex 5, with
`x87_present` tied high. That is the design the DE10 refused. Everything in
this record is `main`, whose 387 has no arithmetic.

### The ladder

| Rung | Device | Closed? | ALMs | % | LABs | % | Worst-corner Fmax estimate |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 61.44 MHz | Agilex 5 | **yes**, all 4 corners | 29,300 | 62.61% | 3,626 | 77.48% | 124.35 MHz |
| 61.44 MHz, seed 2 | Agilex 5 | **yes**, all 4 corners | 29,373 | 62.76% | 3,607 | 77.07% | 128.12 MHz |
| 61.44 MHz, seed 3 | Agilex 5 | **yes**, all 4 corners | 29,400 | 62.82% | 3,593 | 76.77% | 124.55 MHz |
| 75 MHz | Agilex 5 | **yes**, all 4 corners | 29,360 | 62.74% | 3,611 | 77.16% | 132.01 MHz |
| 90 MHz | Agilex 5 | **yes**, all 4 corners | 29,304 | 62.62% | 3,590 | 76.71% | 125.69 MHz |
| 110 MHz | Agilex 5 | **yes**, all 4 corners | 29,348 | 62.71% | 3,592 | 76.75% | 133.23 MHz |
| 130 MHz | Agilex 5 | **yes**, all 4 corners, +0.426 ns | 29,455 | 62.94% | 3,597 | 76.86% | 137.63 MHz |
| 150 MHz | Agilex 5 | **no** — −0.485 ns at Slow fix4 0C, −0.482 at Slow fix4a 0C; passes the other two | 30,000 | 64.10% | 3,688 | 78.80% | 139.84 MHz |
| 170 MHz | Agilex 5 | **no** — −1.039 ns at Slow fix4a 0C, misses three of four corners | 30,169 | 64.46% | 3,711 | 79.29% | 144.49 MHz |
| 61.44 MHz | Cyclone V | **yes**, all 8 corners | 31,244 | 74.55% | 3,494 | 83.37% | 63.79 MHz |
| 75 MHz and above | Cyclone V | **not measured** — queued, then stopped unrun to free the shared builder lease | — | — | — | — | — |

Two things the ladder shows that a single constraint could not.

**Area is flat across every rung that closes**: 29,300, 29,360, 29,304, 29,348 and 29,455
ALMs at 61.44, 75, 90, 110 and 130 MHz — a 155 ALM spread on the same netlist,
inside this lane's ±200 noise. Three seeds at the headline rung land within 100 ALMs of each other — 29,300,
29,373 and 29,400 — so seed scatter is the same small size as rung scatter and
the budget does not turn on the seed either. So on this device, in this range, **the CPU's area and its clock are
effectively independent**. That is a statement about the CPU only: M and M+
have no ladder, so nothing here says the machine's area is independent of its
clock. That is worth having measured rather than assumed; it
was not safe to assume.

**And the Fmax estimate is not even monotonic in the constraint**: 124.35 MHz
at the 61.44 rung, 132.01 at 75, 125.69 at 90, 133.23 at 110, 137.63 at 130.
Those are five estimates of one quantity from five fits of one netlist, spread
over 13 MHz and moving in both directions. Which is exactly why the method made the closure a
ladder: a single constraint's Fmax is the fitter's estimate under that
constraint, and the answer to "what does it close at" is the highest rung that
passes every analysis at every corner.

So **`fes_i386_cpu` closes at 130 MHz on Agilex 5** — every rung of the
precommitted ladder passed, the top one with +0.426 ns to spare. That is the
design's own register-to-register timing: every path through its ports is cut,
so this is not a figure any board I/O could be held to. Against against
**61.44 MHz on the Cyclone V** — which is simply the only rung that has been
run there. Its 75 MHz rung and above were queued and **not run**, so this
record does not establish a Cyclone V ceiling either: its 63.79 MHz estimate
suggests it would miss, but the Agilex ladder closed 130 MHz from a 124 MHz
estimate, which is exactly why an estimate is not a verdict here.

**And the extension found the ceiling: 150 MHz misses.** It fails setup by
−0.485 ns at Slow fix4 0C and −0.482 ns at Slow fix4a 0C, while passing the
slow 100C and fast corners — so it misses on two of four, and the method's rule
is that a rung missing any analysis at any corner does not close. The ceiling
for this netlist, this envelope and seed 1 therefore sits **between 130 and
150 MHz**.

170 MHz misses further, −1.039 ns and three corners of four, which is the
shape you would expect either side of a real ceiling rather than a seed
accident.

The two missing rungs are also the only place where area moves: **30,000 and
30,169 ALMs**, 700 and 869 above the 61.44 MHz rung and outside this lane's
±200 noise, with LABs up to 79.29%. So the area-is-flat finding holds across
every rung that *closes* and stops at the rungs that do not — the fitter spends
area trying to make a target it cannot reach. Both figures are reported with
their stage: these are complete fits whose **timing gate failed**, not partial
fits. And because a missed rung is what bounds the closure from above, both had
their envelope and their clock coverage verified rather than assumed: **1 real
pin against 1 real clock** on each, and TimeQuest reporting **zero illegal and
zero unconstrained clocks** — so the bound does not come from a clock the
analysis never looked at.

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
  braced (Critical Warning 136021) and leaves the port a real pin. Every one of
  the CPU's was ignored, and the fit failed only because the Cyclone V has
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

## The front-end cross-check, and it passes

The method's secondary flow — **Quartus Pro reading the SystemVerilog itself**,
with the same M10K-to-M20K rename applied to the RTL that the primary applies
to the netlist — ran at the same 61.44 MHz rung, same seed, same envelope. Two
things came out of it.

**Quartus Pro's SystemVerilog front end elaborates and fits this RTL**, where
the locked Quartus 17.0.2 rejects it outright. What that does **not** establish
is that Pro elaborates it *correctly*: 17.0.2's documented problem included a
silent misread of a variable index into a packed struct member, matching
resource counts are not functional equivalence, and **no lockstep has been run
on Pro's elaboration**. This fit is an area and timing cross-check, not a
front-end qualification.

**And the two front ends agree to within about 1%.** The comparison quantity
and its threshold were fixed in the method before this fit ran: dedicated
logic registers within ±5% of the primary's 20,842, or the fit is withdrawn as
an area result.

| Figure | Yosys netlist (primary) | Direct SystemVerilog | Difference |
| --- | ---: | ---: | ---: |
| Registers (the gated quantity) | 20,842 | 20,893 | **+0.24%** |
| ALMs needed | 29,300 | 29,035 | −0.90% |
| ALUTs | 42,126 | 41,609 | −1.23% |
| LABs used | 3,626 | 3,609 | −0.47% |
| DSP blocks | 12 | 12 | **0** |
| M20K blocks | 53 | 53 | **0** |
| Block-memory bits | 408,320 | 408,320 | **0** |
| ALMs containing virtual pins | 2,351 | 2,351 | **0** |

The gated quantity lands at **+0.24%**, well inside ±5%, so the secondary is
reported as the cross-check it was meant to be. Both fits closed 61.44 MHz at
every corner with one real pin.

**What that buys.** [#1892](https://github.com/DeanoC/fes/pull/1892) and the
DE25 envelope work fit through the direct-SystemVerilog front end; everything
else here goes through the Yosys netlist. Without this fit, those figures and
these would have differed by front end **and** by scope, two confounds at
once. With it, the front-end part is measured at **under 1% on ALMs and zero
on DSP, M20K and block-memory bits** — so a reader may set #1892's 2,660 ALMs
beside these figures and carry about a 1% front-end uncertainty rather than an
unknown one. It does not make the scopes comparable, and it says nothing about
whether two units' areas add.

## What the fits were built from

One Yosys for every netlist here: the `cores/fes-pc386/toolchain.lock` slot,
commit `e240a4653bb6578e0b267f730128f8a5dac28483`. The machine netlists carry
the pinned firmware as their ROM contents, `pc386-rom.hex`
`815620361ef28a2bbdd97b1964700a4678a26c0ffc9582bb95544473fe099d18`, and the
sealer's own `guest_rates()` at `SYSTEM_CLOCK_HZ` for the N/D parameters and
`ATA_RESET_CLOCKS`.

The CPU netlist **reproduces byte for byte** — a fresh elaboration gave
`3e485e237dbcc50dff66bdce5982238775457b3943fc6f4df28c4db72d74e024` again. The
machine netlists reproduce **within one checkout path**: the fix pins the
netlist, script and log to fixed paths, but those paths are still absolute, so
the same sources elaborated from a different checkout location would be
expected to give a different digest again. That was not tested, and the claim
is not made. See the path-dependence reason in the method
([DeanoC/yosys#80](https://github.com/DeanoC/yosys/issues/80)), which is fixed
for future fits; each machine fit's own `inputs.json` carries the digest of the
exact bytes it fitted, and the equivalence check in the method is why their
figures stand.

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
