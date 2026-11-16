# How large is the 386 on Agilex 5? 16 November 2026

**The CPU, the pc386 machine and `main`'s 387 together take 79.6% of the
DE25's ALMs, leaving 9,545 of its 46,800. And `fes_i386_cpu` closes at
130 MHz on Agilex 5 and misses at 150** — 61.44, 75, 90, 110 and 130 MHz each
passed every **applicable** analysis at every published corner, the top one
with +0.426 ns to spare. For the CPU that is setup, hold and minimum pulse
width: recovery and removal find zero paths there, because its resets are
synchronous, and are recorded as not applicable rather than passed. 150 MHz fails setup by −0.485 ns at two of the four and 170 MHz by
−1.039 ns at three. On the Cyclone V the same CPU closes at **61.44 MHz**, the
only rung run there. Seed 1 throughout. The 124 MHz figure that appears below
is the 61.44 MHz rung's own restricted-Fmax **estimate**, not a closure.

**And the design the DE10 refused fits too**: `pc386_system` from #1889's head,
slices 3 and 4, closes on Agilex 5 at **41,387 ALMs, 88.43%** — adding 4,132
ALMs over M+ and **zero LABs**. The question of whether the DE25 holds a
*complete* 387 was answered first, and by a different design: the pipelined
backend, reported at four seeds, 87.58–87.87% — **a figure relayed from the
sibling session and not verified here** (see below).

**Background on that question, which this record spent the day calling
unsettled:** `main`'s 387 is slice 2 — state,
interface, conversions, no arithmetic at all — and the DE10 **holds it with
the identity on**: [#1821](https://github.com/DeanoC/fes/pull/1821)'s
identity-on machine fit ran **nine seeds at 38,075–38,257 ALMs, 90.9–91.3% of
the Cyclone V, every one meeting setup and hold, worst +0.454 ns**. (That PR's
sealed package, 36,298 ALMs, has the identity **off**, so it is the DE10
equivalent of fit M rather than of M+.)
[#1889](https://github.com/DeanoC/fes/pull/1889)'s refusal was its own head,
slice 4 stacked on slice 3, whose 387 has divide, square root and the
remainders: 8,073 ALMs for the standalone unit against the merge base's 4,076,
requiring 4,292 LABs of 4,191 and never placing. **That design has since been
fitted here** — see [the section on it](#the-design-the-de10-refused-does-fit-on-agilex-5--and-it-adds-no-labs):
it closes on Agilex 5 at 41,387 ALMs, 88.43%.

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

Sixteen internal virtual-pin characterisation fits: the three designs below
at the **61.44 MHz** system clock the pc386 seal uses, the CPU's clock ladder
on Agilex 5 from 75 to 170 MHz, the direct-SystemVerilog cross-check, and one
superseded envelope kept for comparison. No board shell, no pins, no HPS, no
PLL, no reset-release IP, no programming artifact, no seal and no hardware
acceptance.

**Fourteen of the sixteen passed** at **every published operating condition**
of their device — four on Agilex 5, eight on Cyclone V. What "passed" covers,
exactly, and it is not the same for the two designs:

- **Setup, hold and minimum pulse width were analysed at every corner of every
  fit**, and they are the analyses the closure rests on. Among the fourteen
  that passed, the worst setup slack is **+0.426 ns** (the 130 MHz rung) and
  the worst hold slack is **0.000 ns exactly**, on the superseded zero-delay
  envelope fit — which met the gate, since the gate requires non-negative, and
  is one more reason that envelope was replaced. The two missing rungs have
  **negative** setup slack by definition; that is what makes them misses.
- **Recovery and removal were analysed on all four machine fits** — M and M+
  on Agilex 5, M on the Cyclone V, and the slice-4 fit — at every corner,
  worst slack **+0.302 ns** (a removal path on M+), with the slice-4 fit's own
  worst at +0.375 ns. `pc386_system` has asynchronous resets to check.
- **On every CPU fit they found zero paths** and are recorded as
  `not_applicable_synchronous_reset`: `fes_i386_cpu`'s resets are synchronous,
  so for the CPU those two analyses were **not applied rather than passed**,
  and the CPU's 130 MHz closure does not rest on them.

Each corner's coverage word is in [timing.tsv](timing.tsv) beside its slack, so
a reader can see which analyses ran on which fit rather than take the word
"passed" for all of them. The other two are the ladder's **150 and
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

**And the pair's common baseline is demonstrated, not asserted.** M's fit
predates this runner recording a revision, so its own record carries none —
only its netlist digest — while M+'s records a deliberately modified checkout.
Digests alone therefore cannot show the two shared a source baseline. So both
were **re-elaborated from one clean checkout at this record's head**, minutes
apart, with only the one-character flip between them and at the same
output-path length so [yosys#80](https://github.com/DeanoC/yosys/issues/80)'s
path dependence is held constant. The M+ − M line delta came out at **4,117
lines, exactly the fitted pair's delta**, with the x87 multiplier absent from M
and present in M+ with the same count in both pairs. The figures are in
[m-plus-baseline.json](m-plus-baseline.json). So the pair differ by the x87
unit and nothing else — which is what the 1,763 ALM figure attributes the
difference to. What this does *not* do is make either fitted netlist
reproducible; that is the yosys#80 limitation, stated above.

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
a LAB counts as used when one ALM in it is used. M+ occupies 4,590 LABs, which hold 45,900
ALM slots between them, and **the two resource rows give two different
densities — each is stated here with the row it comes from**:

- **ALMs used in final placement: 40,550** — **8.83 of every 10 slots**.
- **ALMs needed, the capacity row: 37,255** — **8.12 of every 10**.

The gap between them is the fitter's own estimate that **4,210 ALMs are
recoverable by dense packing**, with 915 unavailable. On either row, 98% of
LABs are touched while at least a tenth of their slots stand free. The same CPU netlist
used 3,512 LABs under one envelope and 3,626 under the other, a 114-LAB swing
on identical logic — larger than the 90 LABs that remain untouched.

**And it is not what #1889 refused on.** That record reports **LABs
*required*, 4,292 of 4,191**, a pre-placement requirement, and says plainly
that neither fit placed so there is no ALM figure at all. The fit reports here
have no "LABs required" row; the row they have is a post-placement fact. The
two are different quantities and this record does not put them on one scale.

**Nor is M+'s *outcome* comparable with #1889, because M+ is not the same
design.** #1889's identity-on machine carries slice 3 and slice 4's
arithmetic; M+ carries `main`'s slice 2, which the DE10 seals without
difficulty. "Agilex 5 placed what the DE10 could not" cannot rest on M+.

**It rests on the slice-4 fit instead, which has since been run**: #1889's own
head, [fitted here](#the-design-the-de10-refused-does-fit-on-agilex-5--and-it-adds-no-labs)
at 41,387 ALMs, 88.43%, placing, routing and closing. So the outcome
comparison is now available and it is a like-for-like one — the same design on
both devices, refused on the Cyclone V and closing on Agilex 5 — rather than
the mismatched one this paragraph originally warned against.

**And the DE10 settles it directly, on a fit whose total independently
validates this method.** `pc386_system` there comes out at **88.54% of ALMs**,
and the pc386 seal lane's own known figure for this design is about 88% — so
this envelope and flow reproduced a number the lane already had by another
route. That is the main reason a reader should credit the Agilex figures, which
have no such independent check. The same fit, on the device that refused
#1889: **3,946 of 4,191 LABs, 94.15%** — and it placed, routed
and passed every analysis at all eight corners, at 88.54% of ALMs. So 94% of
LABs used is a successful fit on this very device, which is what the LAB row
being a capacity wall would have ruled out. Note too that the two M fits sit
at almost the same LAB percentage (94.15% and 94.53%) while their ALM
percentages are 12 points apart.

LAB occupancy and packing density of every fit:

Two densities per fit, each from its own row, because they are not the same
quantity: **needed/LAB** divides the capacity row by the LABs, **placed/LAB**
divides the final-placement row. A dash means that fit's report did not carry
the final-placement row.

| Fit | ALMs needed | % | ALMs in final placement | LABs used | % | needed/LAB | placed/LAB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| C, Agilex 5 | 29,300 | 62.6% | — | 3,626 | 77.5% | 8.08 | — |
| M, Agilex 5 | 35,492 | 75.8% | 38,750 | 4,424 | 94.5% | 8.02 | 8.76 |
| M+, Agilex 5 | 37,255 | 79.6% | 40,550 | 4,590 | 98.1% | 8.12 | 8.83 |
| C, Cyclone V | 31,244 | 74.6% | 30,672 | 3,494 | 83.4% | 8.94 | 8.78 |
| M, Cyclone V | 37,108 | 88.5% | — | 3,946 | 94.2% | 9.40 | — |

## The clock ladder, and what is not measured

The method precommitted a ladder — 61.44 MHz headline, then 75, 90, 110 and
130 MHz, seeds 1–4 at the headline — because reported Fmax at one constraint
is the fitter's estimate and not a closure. **"Closes at" means the highest
rung passing every applicable analysis at every published corner** — and for
the CPU, recovery and removal are not among them. Those rungs and the
extra seeds were queued and then stopped unrun; any that have not produced
a result when this record is read are **not measured**, and are listed as that
rather than trimmed quietly. The CPU's closure on Agilex 5 is in the ladder
table below; the CPU on the Cyclone V has only its 61.44 MHz rung, and the
machine has no ladder on either device, so **no closure is claimed for those
three**. A third CPU seed at the headline rung was run last, to verify the corrected
timing gate end to end through the runner rather than only against a retained
project. The method precommitted **seeds 1–4 at the headline rung on both
devices**; of those, Agilex seeds 1, 2 and 3 ran and **Agilex seed 4 and
Cyclone V seeds 2, 3 and 4 did not**. Those four, together with extra seeds of
M and M+ and the DE10's rungs above 61.44 MHz, were queued and then **stopped
unrun** to free the shared builder lease for the front-end
cross-check, which had been waiting on it for three hours. They are therefore
not measured, and the one seed that did complete — the CPU's second seed at the
headline rung, 73 ALMs from the first — is in the ladder table.

## The design the DE10 refused does fit on Agilex 5 — and it adds no LABs

`pc386_system` from [#1889](https://github.com/DeanoC/fes/pull/1889)'s head
`5f901426e`, **pre-merge** (that PR targets `deano/i386-x87-s3`, whose base is
conflicting; the tree fitted is the head's own, so this describes the
slice-3-plus-slice-4 design on #1879's branch and no state of main), with
`x87_present` tied high by the same one-character scratch flip, in the
**identical envelope** to M and M+:

| | ALMs needed | % | LABs | % | DSP | M20K | Registers | Worst-corner Fmax |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| M+ (slice 2) | 37,255 | 79.60% | 4,590 | 98.08% | 33 | 138 | 35,233 | 120.47 MHz |
| **#1889 head (slices 3+4)** | **41,387** | **88.43%** | **4,590** | **98.08%** | 33 | 140 | 37,166 | 106.16 MHz |

**Band 2 of the precommitted bands: it fits and it closes.** Timing passes
every analysis at all four published corners, 2 real pins against 2 real
clocks, zero illegal and zero unconstrained clocks. At 88.43% it is below the
**≥ 92%** band that would have made it "not established pending more seeds" —
though it is one seed, and 88.43% is the highest ALM utilisation anyone has
fitted on this device.

So **the design the DE10 could not place does place, route and close on
Agilex 5.** The DE10 refused it needing 4,292 LABs of 4,191; Agilex 5 has
4,680 and uses 4,590 of them.

**And this fit is the cleanest demonstration in the record that LAB occupancy
is not capacity.** Slices 3 and 4 add **4,132 ALMs** over M+ — an eleventh of
the device — and **exactly zero LABs**: 4,590 before, 4,590 after, 98.08%
both times. The arithmetic packed entirely into LABs that were already
occupied. Any reading of 98.08% as "nearly full" would have predicted this fit
could not happen; the capacity row, which went 79.60% → 88.43%, predicted it
comfortably. That is the whole argument of the section above, occurring.

### What this fit is and is not for

It was queued as "the only measurement that would settle whether the DE25
holds a complete 387". **It is not, and that question was answered first by a
different design.** The sibling DE25 envelope session reports that its integration fit —
`pc386_system` with `x87_present` high **and** `X87_BACKEND = 1`, the
pipelined backend, on this runner and this envelope — fits and closes across
four seeds at **40,986 / 41,055 / 41,122 / 41,054 ALMs, 87.58–87.87%**, every
corner, setup never below +7.58 ns. That is the configuration the 16 November
decision ships.

**The evidential status of those five numbers, stated because this record
leans on them:** they were **relayed to this session in a message** and are
**not verified here**. At the time of writing they appear in no committed
artifact that could be found — not on
[#1907](https://github.com/DeanoC/fes/pull/1907), that session's PR, and
nowhere under `docs/validation/` on its branch — and no fit output of theirs
was read. So they are **cited as that session's report, not as measured
evidence in this record.** What would substantiate them is their own
validation record carrying the per-seed figures and timing, which is theirs to
publish; until it exists, a reader should treat the complete-387 conclusion as
resting on their report rather than on anything here. Nothing in this record's
own figures depends on them: the 41,387 below is measured here, and the A/B
comparison is explicitly unresolved either way.

What this fit is, therefore, is the **A/B of the two arithmetic
implementations on one device**, same envelope, same front end, same rung:

- **Sequencer, slices 3+4: 41,387 ALMs, 33 DSP, 140 M20K** (one seed, here).
- **Pipelined backend: 40,986–41,122 ALMs** (four seeds, theirs; its DSP and
  M20K figures are theirs to state).

In ALMs the two land within a few hundred of each other — about 330 between
this fit and the backend's four-seed mean, against a 136-ALM spread across
those four seeds and this lane's ±200 fit-to-fit noise. **That is not a
resolved difference**, and one seed on this side cannot make it one: what the
A/B establishes is that **the two implementations cost about the same ALMs on
this fabric**, not that either is cheaper. Slice 6 should take the comparison
as "roughly equal in ALMs, so decide on the other axes" — DSP, memory, latency
and the duplicated 64×64 multiplier — rather than as a ranking. A seed sweep
on this side would be needed to rank them, and has not been run.

Worth one line on its own: this design's worst-corner Fmax estimate is
**106.16 MHz** against M+'s 120.47, so the arithmetic costs about 14 MHz of
estimate at the same rung. Both are estimates at 61.44 MHz, not closures; no
machine ladder has been run on either.

**`pc386_system` from #1889's head on Agilex 5 — run, and reported
[above](#the-design-the-de10-refused-does-fit-on-agilex-5--and-it-adds-no-labs):
it fits and closes at 41,387 ALMs, 88.43%.** Every other fit in this record is
`main`, whose 387 has no arithmetic. What is still unmeasured here is a **seed
sweep** of that design, which is what would turn its A/B against the pipelined
backend from "about the same" into a ranking.

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
  314 pins. Every fit now **gates on the report's own pin count**, and on the
  **exact** count rather than a ceiling: `real_pins == len(real_clocks)`, in
  the runner and again in the collector. Too few matters as much as too many —
  one real pin on the machine's two-clock envelope would mean a clock input
  had gone virtual or been removed, and the timing gate would then have
  analysed the survivor alone and reported it as covered. The gate takes the
  intended clock list and errors unless TimeQuest finds exactly that many.
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
