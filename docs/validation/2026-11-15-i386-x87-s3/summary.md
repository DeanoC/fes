# i386 x87 slice 3: the 387's arithmetic, 15 November 2026

Slice 3 of the [x87 plan](../2026-11-12-i386-x87-plan/summary.md#slices)
([#1768](https://github.com/DeanoC/fes/issues/1768), this slice
[#1871](https://github.com/DeanoC/fes/issues/1871)) gives the unit slice 2
built ([#1821](https://github.com/DeanoC/fes/pull/1821)) its arithmetic: add,
subtract and reverse subtract with their integer, ST(i) and popping forms,
multiply with the same, the ordered and unordered compares, FRNDINT, FSCALE
and FXTRACT, under precision as well as rounding control, with every
exception they raise driving the ES, ERROR# and IRQ 13 path slice 2 built.

Divide, square root and the remainders are slice 4's, on the Goldschmidt
datapath with its M10K seed tables; the transcendentals, FBLD and FBSTP are
slice 5's. [What is left](#what-is-left) states what this slice's scope names
and this PR does not have.

**The scope is the tracker's, and the brief this session was given was not.**
The brief asked for divide and square root here and left out the compares,
FRNDINT, FSCALE, FXTRACT and precision control; it also put FPREM in
microcode. #1768 and the plan record put divide, square root, FPREM and
FPREM1 together in slice 4, and the compares and the rounding forms here.
The tracker's split was confirmed before any RTL was written, by Deano and by
the coordinating session, for the reason that decides it independently of the
tracker: divide and square root share the multiplier and the seed tables with
the remainders, so folding them in would have made this slice's area
unattributable, and attributable area is what these slices were asked to
report.

## The finding to read first

The arithmetic is built, checked and sealable: 537 bench cases against a real
x87 with no failures on the RTL and on the netlist, lockstep with nothing
failing in any bucket three ways over, test386 to POST FFh, and the package
as it ships fitting at **87.0% of the device and closing at +0.896 ns** on
the sealer's own timing report.

**But with the `i386dx+387` identity on, this machine no longer closes
reliably.** At about 94.8% of the device, **five of nine Quartus seeds miss
the CPU clock**, by as much as −0.348 ns, where slice 2 closed on nine seeds
of nine at 91.2% (through the other slot). Closure is the minority outcome,
not an unlucky seed. The failing paths are not in the x87 -- its own paths
keep +0.456 and +1.458 ns on those seeds -- so this is whole-machine
congestion and no change to the arithmetic would fix it.

**No change to slice 3's arithmetic would fix it, and that was looked for
rather than assumed.** The failing paths were read on every failing seed and
in the non-flattened fit before anything was concluded. There is no x87 path
to shorten: the arithmetic's paths run 1.4 to 3.5 ns behind the binding one
on every fit measured, so a pipeline stage in the sum or the rounding would
cost a re-freeze and every gate and buy nothing. The first instinct on
reading "the margin is thin" is to optimise the arithmetic, and that
instinct is wrong here. **The decoder is a separate matter and this record
does not close it**: slice 3 also adds x87 memory operand shapes to
`fes_i386_decode.sv`, and the decode path that misses on four seeds is two
to three logic levels deeper than the merge-base's. An earlier draft said
those paths pre-dated this slice; that claim is withdrawn in
[where the margin went](#where-the-margin-went-which-is-not-where-it-looks),
which says what would settle it. What recovers the margin is area recovered
**elsewhere** -- the area audit's wins, which Deano deferred to a whole-system
reduction phase, and which now buy slack as well as ALMs.

The identity ships **off** in every package until the switch flip, so nothing
sealed or run on a kit today depends on that. **Deano moved that flip out of
slice 5 and into a new slice 7 in response to this finding, and made slice 6
a full design optimisation that may trade identity-on performance to get it
closing** — so the sweep is the evidence that set slice 6's scope rather than
a blocker for slice 5. [What the sweep changed](#what-the-sweep-changed-which-is-the-plan-rather-than-this-slice)
has the detail.
[The sweep](#the-identity-on-seed-sweep-closure-is-seed-dependent) has the
numbers and [where the margin went](#where-the-margin-went-which-is-not-where-it-looks)
has the paths.

## What the host settled

The slice's rules are not read off a manual; they are measured. An x86-64
host's FPU *is* an 80-bit x87, so the bench drives the unit's CPU interface
and compares the whole architectural state -- the control, status and tag
words and all eight registers -- with the host's own instruction, over the
operand classes the 387 PRM names as pairs, in all four rounding modes and
all three precisions. The bench counts them: **242,776 comparisons** against real
hardware, over **537 cases**, and the same bench runs on the seal's Yosys
netlist.

Eight rules came out of it that a manual reading would have got wrong, and
each is in the RTL with the case that settles it:

1. **The exception set splits in two.** Invalid, denormal and divide-by-zero
   are detected before the result exists, so an unmasked one leaves the
   operands and TOP alone. Overflow, underflow and precision are detected
   after it: unmasked, the result is still written. `0 * infinity` with IM
   clear leaves both operands on the stack; an inexact add with PM clear
   writes its result and pops.
2. **An unmasked overflow or underflow stores a result with its exponent
   moved by 24576**, the 387's bias adjust. 2^9000 squared with OM clear
   stores exponent field 264Fh, which is 34383 - 24576.
3. **Unmasked underflow fires on any tiny result, exact or not**, while
   masked underflow fires only where **rounding the denormalised value is
   inexact**: 2^-8200 squared is exactly a denormal and masked raises
   *nothing at all*. Inexact after the denormalising shift rather than lost
   *by* it, which are not the same thing -- the smallest denormal at PC 24
   survives the shift exactly and is then rounded away to zero, and a real
   x87 reports underflow there.
4. **Tininess is decided after rounding.** The value is rounded once at PC as
   though the exponent range were unbounded, and that exponent decides. A
   product whose exact value is below the smallest normal but whose rounding
   carries it back up is not tiny and reports precision alone. Precision
   control moves that line, because it moves where the rounding carries: one
   operand pair is tiny at PC 64 and not at PC 24, and another is the other
   way round. Those two pairs are what tell this rule from the alternatives,
   and an earlier rule fitted to ten cases failed both.
5. **An unmasked pre-operation exception reports nothing else.** With DM
   clear, the largest denormal plus just over one reports the denormal
   exception alone, not the precision exception the sum would have raised,
   and C1 is clear: there was no result to round.
6. **A denormal operand raises DE even where the result is exact**, including
   a pseudo-denormal, but *not* beside an operand that is empty, unsupported
   or a NaN, where the 387 reports that operand's exception or none.
7. **A quiet NaN beats a signalling one**, and otherwise the larger
   significand wins; on equal payloads the **positive** one is delivered,
   both ways round. This is why a memory operand's conversion must hand the
   arithmetic a signalling NaN still signalling, where FLD quiets it.
8. **A subtract's operand order is the encoding's, not the assembler's.**
   With ST(0) = 3 and ST(1) = 10, D8 E1h, DC E9h and DE E9h give
   "destination minus the other operand" and D8 E9h, DC E1h and DE E1h the
   reverse -- so GNU as, which assembles `fsub %st,%st(1)` to DC E1h, names
   them the other way from the hardware. The bench therefore generates the
   instruction from its two opcode bytes rather than writing a mnemonic.

Rules 2, 3 and 4 together are also the reason the record's own earlier draft
was wrong twice: the first underflow rule fitted the samples it had, and only
operand pairs chosen to separate the candidate rules broke the tie.

## Method

Three instruments, and they answer different questions.

- **The unit's bench** (`sim/i386/x87_unit_tb.cpp`, driven by
  `scripts/test_fes_i386_x87_unit.py`) is the one that *settles* behaviour,
  because an x86-64 host's FPU is the same 80-bit unit a 387 is. It drives
  the unit's CPU interface directly, so it can reach the operand classes and
  the exception masks a guest program cannot easily be made to produce, and
  it compares the whole architectural state rather than one result. It runs
  on the RTL and on the netlist the seal's Yosys writes.
- **The lockstep** is the one that proves the unit is *wired up*: the
  decode's uops, the x87 pointers and the opcode a memory form leaves, and
  the CPU's issue interlock. It also checks the oracle agrees, within the
  limits the next section sets out.
- **The fits and the seal** answer whether it fits the device and meets the
  clock, which the sections below report.

Where the host and a 387 differ the case follows the 387 Programmer's
Reference Manual and not the host. No case in this slice needed that: every
rule the host settled is one the manual is silent or ambiguous about rather
than one it contradicts.

**What the oracle did and did not confirm.** Bochs's x87 is SoftFloat-3e, an
independent implementation of the same arithmetic, and the identity-on
programs compare the 387's whole state against it, so the agreement is real
as far as it goes. It does not go as far as the eight rules. Those programs
run with **all six exceptions masked throughout** -- the operand table's own
comment requires it, because this bench drives no IRQ 13 and an unmasked
exception would hold a run for ever -- and over about a dozen operands. So
the oracle confirms the ordinary results, the condition codes, the flags a
masked operation raises, the pops, and the pointers and opcode.

It does **not** confirm the unmasked split (rule 1), the bias adjust (rule
2), unmasked underflow on an exact result (rule 3), the two operand pairs
that settle tininess (rule 4), or the NaN payload tie (rule 7). Those rest on
the host alone. No `divergences.toml` entry was needed, but the honest
statement is that **none was found in what was compared**, not that none
exists.

## What the datapath is

Option A has no barrel shifter and one multiplier, so every step that needs a
shift or a product goes through the same nine-DSP block and returns to the
state it names. One operation can take four passes of it: pre-normalise each
operand, align them, and renormalise the result.

- **The working value** is a 67-bit field -- a 65-bit integer part whose bits
  63:0 are the significand, with a guard and a round bit below -- and a
  sticky bit of its own.
- **Both operands are normalised first**, so an add's alignment is the only
  shift that drops bits and a subtract's cancellation the only one that needs
  a leading-zero count. A denormal's exponent goes below zero and comes back,
  which the signed 18-bit exponent carries.
- **The sticky is borrowed, not positional.** A subtract takes one unit off
  the round position and sets the sticky: the true difference lies in the
  unit below the computed one, which is what a round bit and a sticky say.
- **The alignment's clamp is at 66, not 65.** At a shift of 65 the round bit
  is the operand's own top bit; only from 66 is nothing left but the sticky.
- **Cancellation keeps the bits below the window.** A shift of more than one
  place needs the exponents to have been within one of each other, so there
  the alignment dropped nothing; where it is one place or none, bit 0 and the
  borrowed sticky are the result's own guard and sticky. Dropping them turned
  -1 plus 2^-16382 into -(1 - 2^-64).
- **The minuend is the larger magnitude**, which the exponents alone do not
  decide: equal exponents leave it to the significands, and -2^63 plus
  (2^64 - 1) came out as -3*2^63 before that was fixed.
- **The adder and the multiplier have their own operand registers**, which is
  Deano's D5 decision on #1768 and the fit spike's remedy for the one shared
  register its standalone fits could not route.
- **Rounding is one mux, not three rounders.** PC names bit 0, bit 11 or bit
  40 of the significand, the guard and round bits sit below bit 0, and the
  sticky holds what the shifts dropped. Precision control applies to add,
  subtract and multiply and to nothing else, so FRNDINT, FSCALE and FXTRACT
  pass the extended precision whatever the control word says.

## Three faults of the shape slice 2 was bitten by

Slice 2's three Codex findings were one shape: a state machine that writes
flags and goes idle on one edge, with a summary derived from a *registered*
word, leaves a cycle where the summary has not risen and a latch has not been
set. Slice 3's bench therefore checks the cycle *of* the commit rather than
the one after, and the first two below are that shape and were found that
way. The third is a different kind of fault and was found by reading the
alignment's arithmetic rather than by a case; it has a case now.

- **An unmasked overflow, underflow or precision exception did not latch
  BUSY# at all.** The gate that holds the write back and the gate that raises
  ERROR# are not the same set -- by rule 1 a post-operation exception writes
  its result *and* reports -- and the first draft used one for both. The
  `arith-latch-*` cases check, for each exception the arithmetic raises, that
  ERROR# is up in the cycle the unit goes idle and that an ESC presented in
  that cycle is held.
- **A write to port F0h in the same cycle as the error summary's edge left
  the latch set.** This one is a defect in **merged slice-2 logic**, found by
  slice 3's bench and not introduced here: the apply sets the latch itself,
  so the edge detector at the foot of the unit fires the cycle *after* it,
  and an F0h write landing in that one cycle was undone. Reverting the `!f0`
  gate takes the bench from a clean run to a failed `latch-f0-edge` **and 47
  comparisons in `edge-im` run against a unit still in its FNINIT state**,
  because the stuck latch holds every later FLDCW. That count was measured on
  the 499-case bench, before the last cases were added. Under the pc386 reseal
  exemption it needs no reseal of slice 2.
- **The alignment clamp was off by one**, which is neither of that shape and
  was not found by a case: the first draft treated a shift of 65 or more as
  "nothing but a sticky bit", when at 65 the round bit is still the operand's
  own top bit and only from 66 is nothing left. The counterexample is a
  subtract of two normals 65 exponents apart, where the correct result is one
  exponent down with every significand bit set and the clamp gives the
  minuend back. `align64` to `align67` are the cases that pin it now.

A fourth, in merged slice-2 code, is **not** fixed here: a memory form the
microcode moves in several chunks leaves the x87 data pointer at its last
chunk rather than at the operand (FSTP m80 to 30Ch leaves FDP 314h).
[#1877](https://github.com/DeanoC/fes/issues/1877) carries it with a
reproduction that uses nothing of slice 3 -- two instructions appended to
slice 2's own `stores` program fail the same way. Slice 3's lockstep programs
store their results with FSTP m32, a single-chunk form, so they do not depend
on the defect.

## Checks

All of these are of `8751b81b438ea08e4fe4ba2f47a823c4db2b2e46`, the frozen
head. The only commit after it is this record, which no gate and no seal
reads.

| Gate | Result |
|---|---|
| unit bench, against a real 80-bit FPU | **537 / 0**, 242,776 comparisons |
| ... on the netlist the seal's Yosys program writes | **537 / 0** |
| lockstep, RTL: every bucket | **0 fail** (1,933 no-387 cases with the identity off, 14 x87 cases with it on, 84 directed, and the corpus, random, protected-mode, task, debug, undefined and memory buckets) |
| ... on the netlist the seal's Yosys program writes | **0 fail**, same buckets |
| ... governed at preset 3, `--same-as` the ungoverned report | **0 fail**, and it matched: 14 x87 cases, 1,933 no-387, 31 retirements with the 387's state compared |
| ... on the chip host with the CUDA router, which the sealer requires and verifies | **0 fail**: 14 x87, 1,933 no-387, 84 directed |
| test386 | **POST … E0 EE FF**, halted |
| Quartus 17.0.2 seal | **pass**, package `48717256`, `BUILD_ID` e6261c37, seed 1, all five analyses positive (setup **+0.896**, hold +0.099, pulse width +1.122, recovery +6.19, removal +1.695) |
| kit run on that seal | **pass**, Kit B run `x3a`: FreeDOS to `C:\>` and Wolfenstein 3D in play on the sealed package, `fes.pc386.machine 1.0` active, baseline restored and the kit logged FREE |

Every gate runs from a clean worktree at the frozen head, so no later edit of
the bench or the harness can reach the tree a counted run used.

One qualification on the netlist row: the unit bench's netlist is written by
the **same Yosys program and the same pinned commit** the seal uses, built on
Powerboat, not by the chip host's build of it. That distinction is not
pedantry here -- the two builds' netlists differ in net numbering, which is
why the fits in this record all come from one slot -- but it does not weaken
the gate: what the netlist run checks is that the design survives Yosys's
flatten and renaming, and the seal's own prerequisite, the `--netlist`
lockstep, *did* run on the chip's binary with the CUDA router and passed.

### What the lockstep adds that the unit's bench cannot

The unit's bench drives the unit's own interface. Only the lockstep reaches
the decode's new uops, the x87 pointers and the opcode a memory form leaves,
and the CPU's issue interlock. Six programs were added to `run_x87_cases`:
the register forms in all three destination flavours, the memory forms in all
four operand formats (the 64-bit real through the `UC_FRD8` microcode loop),
the compares each followed by FNSTSW AX, the rounding forms, precision
control across all four control words, and the flag cases.

Two things make that comparison bite. **FNSTSW AX puts the status word in a
GPR**, which the lockstep compares at every retirement whether or not the
unit is still busy -- the 387's own state is compared only where the unit is
idle, and an ESC after an ESC is held until it is, so a run of ESCs gives few
such points. **A result stored to memory** is compared the same way. The
387's whole state is then compared at each program's end, after the unit has
settled.

One harness change was needed and is worth naming for the next slice:
`x87_built_form` in `lockstep_tb.cpp` is a C++ mirror of `x87_decode`, and a
form missing from it stops the run with *every* outcome flag clear and no
mismatch of its own -- which reads as a program that simply did not halt. All
six programs failed that way until it was extended, and the diagnostic the
record's reproduction prints now names `unbuilt` for that reason.

## Area and timing

Every figure here is Quartus 17.0.2, seed 1, through the measurement flow
(`scripts/quartus_x87_spike.py`). **That flow is the sealer's project with
one macro changed**, and this record got it wrong once before it was
checked: an earlier draft said the flow fits `pc386_system` where the sealer
fits `top`, read off the Yosys `hierarchy -top pc386_system` line. Both
elaborate the machine with Yosys and then compile **`top`**, the DE10 level,
from the same generated `top.qsf`. Diffing the two project files leaves, once
tree paths are set aside, exactly one assignment:

    seal   FES_PC386_BUILD_ID=128'he6261c37237b7de8d7ddeb7f9006157c
    spike  FES_PC386_BUILD_ID=128'h00000000000000000000000000000000

The measurement flow zeroes the build id so that fits of different builds are
comparable at all; the sealer embeds the real one. Same device, same SDC,
same 61.44 MHz clock and 16.276 ns period. That constant is the only **project-setting**
difference between the sealer's identity-off report at **36,480 ALMs and
+0.896 ns** and the table below at **36,392 and +0.576**; the two netlists
themselves were not compared, so "only difference" is a statement about the
Quartus projects and not a proof that nothing else differs. The gap is 88
ALMs and 0.32 ns either way, which is the size of this flow's seed-to-seed
spread rather than a difference of designs. **The rows
here difference against each other; the sealer's report is what the shipped
artefact closes at.** **The machine fits** -- which are the ones the deltas rest on -- all ran through
**the chip host's CUDA toolchain slot**; the standalone fit of the unit, in
"The unit on its own" below, ran through Powerboat's HIP slot, which is why
that subsection calls its comparison with slice 2 indicative rather than
exact. The machine
fits were run on
`3dcc08cc037c91d666e2b3283ecf64c4ca389ae0` — whose RTL is byte-identical to
the frozen head's, the only differences between them being two testbench
files that no fit reads. The merge-base tree's pc386 firmware was copied from
the branch tree's after its own build failed for want of `gperf` on that
host; the two trees change no firmware source, and the copy validates with
the same ROM and VGA BIOS digests, so it is the same artefact and not a
substitute.

Why one slot for all the machine fits: the HIP and CUDA slots' Yosys binaries differ,
so their netlists are the same logic with different net numbering and Quartus
fits them differently — 36,055 against 36,069 ALMs and setup +0.594 against
+0.410 on identical RTL, which is the size of the deltas this slice reports.
That is also why none of these are differenced against slice 2's published
figures, which were HIP.

### The unit on its own

| Fit | ALMs | DSP | M10K | CPU setup (ns) | CPU hold (ns) |
| --- | ---: | ---: | ---: | ---: | ---: |
| slice 2's `unit`, seed 1 (its record) | 1,888 | 9 | 4 | +0.656 | +0.085 |
| this slice's `unit`, seed 1 | **4,076** | 9 | 4 | **+0.570** | +0.053 |

So the arithmetic is about **+2,188 ALMs standalone**, and it **meets the
clock**: the datapath closes in isolation with +0.57 ns. The **DSP and M10K
counts have not moved** — all of it came out of logic, and none out of the
device's multipliers or memories, which is where slice 4's seed tables will
go. These two are through different slots (slice 2's CUDA, this one's HIP on
Powerboat), so read +2,188 as indicative; the machine fits below are the
within-slot measurement.

For scale, slice 1's fit spike measured the *whole* of option A — slow path
and transcendentals included — at 2,134 ALMs standalone. The arithmetic alone
is as large as the spike's figure for all of it. The spike fitted a bare
datapath with no CPU interface, no register file, no conversions and no
exception plumbing, which is why it also under-counted slice 2 by about the
same margin; the cost is in what a sketch of an algorithm leaves out — the
alignment and normalisation paths, the PC-aware rounder, the operand
classification and NaN rules, the exception sequencing, and the sequencer
that drives four passes of one multiplier.

### The machine

| Fit | ALMs | % of device | DSP | M10K | worst setup (ns) | worst hold (ns) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| this branch, identity off (the RTL the package ships; the sealer's own fit of it is 36,480 and +0.896) | **36,392** | 86.83% | 40 | 282 | **+0.576** | +0.055 |
| this branch, identity on | **39,682** | **94.68%** | 49 | 286 | **+0.176** | +0.082 |
| the merge-base, identity on (slice 2's unit alone) | **38,073** | 90.84% | 49 | 286 | **+0.385** | +0.068 |
| the merge-base, identity off | **36,254** | 86.50% | 40 | 282 | **+0.382** | +0.090 |

**So slice 3's own cost in the machine is +1,609 ALMs and +3.84 points of
the device.** That is a difference of two fits through one slot that differ
only by this slice's RTL, which is what the brief asks for. The same
subtraction on the setup column gives −0.209 ns (+0.385 to +0.176), **and
that one does not carry**: it is one seed against one seed, and the sweep
below spreads identical RTL across 0.746 ns of slack. The area delta is a
measurement; the slack delta is a coincidence of two placements.
**With the identity off -- which is what ships -- the slice costs about
+138 ALMs**, 36,254 to 36,392. Read that as "small", not as 138: the
identity-on sweep moves ALMs by 114 across seeds on identical RTL, so a
single seed pair cannot resolve a difference this size. What the row does
establish is the mechanism, and that part is not a seed artefact: with the
identity off the fit drops from 49 DSPs and 286 M10Ks to **40 and 282**, the
nine multiplier DSPs and four memories of the arithmetic, so the identity
really is letting Quartus remove the unit rather than merely tying its
output off. That row also makes the point about the
slack column on its own: the merge-base closes at **+0.382** and this
branch, with 138 ALMs *more* logic, closes at **+0.576**. A slice cannot
add logic and gain slack; what moved is the placement. Any slack figure in
this table differenced against another is reading the seed, not the design.

Slice 2's own published in-machine cost was +1,608, but that was measured
through the other slot, so the right sentence is the weaker one: **two slices
of this unit have each cost about 1,600 ALMs in the machine**, against a
spike that predicted 1,687 for all of them. The one-ALM coincidence is not a
result.

**That is 95% of the fit spike's in-machine prediction of +1,687 for the
whole of option A**, slow path and transcendentals included. The standalone
comparison gives 103% of its 2,134. The two framings agree to a few per cent
and say the same thing: **the budget the spike set for all of option A was
spent by slice 3 alone.** The in-machine figure is the one that matters for
fitting a device, and it is the one with no cross-slot objection to make.

The slack is the worst of every clock over every corner, and on seed 1 every
one is positive. **But closure with the identity on is seed-dependent, and
two of the first four seeds miss the clock.** The seed sweep below is the
finding to read before anything else in this section.

**What gets sealed is the identity-off design, and that is not a
one-percent margin.** The identity is tied to 0 in the package, so the
sealed core and the kit run are the identity-off design. Its margin is the
**sealer's** figure, not the row above: the seal fits at **36,480 ALMs,
87.05% of the device, and closes at +0.896 ns -- 5.5% of the 16.276 ns
period** (system clock 61.44 MHz), with every one of the five analyses
positive. The rows in this table come from the measurement flow
(`quartus_x87_spike.py`), the same project with the build id zeroed, which
reads 36,392 and +0.576 for the same RTL; the rows are for comparing fits
with each other, and the sealer's report is what the artefact closes at. The identity-on row is
forward-looking: it is what the switch flip in **slice 7** inherits, what
slices 4 and 5 add to, and what slice 6's reduction has to pull back.
Nothing is being sealed at 1% margin.

**The forward-looking number is the one to read carefully, and it is the
margin and not the area.** Within this slot, turning the identity on takes
setup slack from +0.576 to **+0.176 ns — 1.08% of the 16.276 ns period** and
leaves the device with **2,228 ALMs**. Same slot, same seed -- and the slack
half of it is still one seed pair, so the figure that carries is the sweep's:
**five of nine seeds do not close at all** with the identity on. The ALM
figure is the one to plan against.

What this record does **not** claim is a margin drop against slice 2.
Comparing +0.176 with slice 2's published +0.603 would be a cross-slot
comparison, and the slots differ by 0.184 ns of setup on identical RTL --
about the size of the shift being attributed. The same-slot figure for that
is the merge-base identity-on fit, and it has landed: **+0.385 ns** against
this slice's +0.176, a difference of **−0.209 ns**. **That number is seed 1
against seed 1, and it should not be read as this slice's cost in slack.**
The sweep below puts identical RTL between −0.348 and +0.398 across nine
seeds, a spread of 0.746 ns, so a 0.209 ns difference between two single
fits is inside the noise of the flow that produced it. Establishing a slack
cost would need a merge-base sweep to difference against this one, and that
has not been run. The area delta does not have this problem: **+1,609 ALMs**
is a count, not a slack, and it moves by tens of ALMs across seeds rather
than by its own size. So the honest pair of statements is that the area cost
is measured and the slack cost is not. No Fmax or headroom delta against
slice 2's *published* numbers is quoted here either, because those were
measured through the other slot. The standalone delta
over-predicted the machine delta, which is also a reminder that a standalone
fit is not a machine prediction.

### The identity-on seed sweep: closure is seed-dependent

Slice 2 established nine-of-nine closure at 91.2% of the device, and set the
question for this slice: does that hold as utilisation rises? It does not.
One caveat on that comparison before the numbers: slice 2's nine seeds were
run through the **HIP** slot and these through CUDA, so it is a cross-slot
comparison of *closure*, not of slack. The within-slot control -- a
merge-base sweep on this slot -- has not been run, and running one is a call
for Deano rather than this session. What follows stands on its own either
way, because every row in it is the same slot.
Same flow, same settings, same CUDA slot, identity on, all nine seeds:

| Seed | ALMs | % device | DSP | M10K | memory bits | worst setup | worst hold |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 39,682 | 94.68% | 49 | 286 | 1,356,111 | **+0.176** | +0.082 |
| 2 | 39,689 | 94.70% | 49 | 286 | 1,356,111 | **+0.398** | +0.116 |
| 3 | 39,728 | 94.79% | 49 | 286 | 1,356,111 | **−0.283** | +0.057 |
| 4 | 39,771 | 94.90% | 49 | 286 | 1,356,111 | **−0.348** | +0.063 |
| 5 | 39,789 | 94.94% | 49 | 286 | 1,356,111 | **−0.052** | +0.090 |
| 6 | 39,796 | 94.96% | 49 | 286 | 1,356,111 | **−0.147** | +0.080 |
| 7 | 39,753 | 94.85% | 49 | 286 | 1,356,111 | **+0.179** | +0.074 |
| 8 | 39,759 | 94.87% | 49 | 286 | 1,356,111 | **+0.355** | +0.089 |
| 9 | 39,809 | 94.99% | 49 | 286 | 1,356,111 | **−0.086** | +0.115 |

**Five of nine miss: 3, 4, 5, 6 and 9.** Closing is the minority outcome,
and the four that close do so by between +0.176 and +0.398 ns, so even the
good seeds hold 1 to 2.4% of the period. The spread across the nine is
**0.746 ns of slack and 127 ALMs** on byte-identical RTL, and that spread is
the reason this record refuses to read any single-seed slack difference as a
cost. The DSP, M10K and memory-bit counts are identical on
every seed, which is the check that these are real results and not
interference between concurrent runs: a seed should move ALMs, registers and
slack and nothing else, and that is all it moved. No seed here is a dead run.

**The x87 is not on the failing paths, on any of them.** The worst path
launches from the decoder's byte registers on seeds 3, 4, 5 and 6
(`cpu_decode_by[13]`, `[5]`, `[3]`, `[4]`) and from the front end's aligner
on seed 9; seed 1, which closes, has the L1D. In every case the x87's own
paths sit well behind -- +0.456 on seed 3, +1.458 on seed 4, +1.424 on seed
9 against a machine worst of −0.054, +1.023 on seed 1.
[Where the margin went](#where-the-margin-went-which-is-not-where-it-looks)
has the non-flattened fit, where the machine's worst is the queue aligner at
+0.553 and the nearest x87 path is 1.75 ns behind it. **So nothing in slice
3's arithmetic would fix these misses** -- not a pipeline stage, not a
narrower datapath. That section also withdraws a claim that the decode paths
pre-date this slice: their logic depth suggests slice 3's own decode
additions deepened them, which is a live question rather than a settled one
and belongs to slice 6.

**The sweep triggered this slice's own stop rule, and the decision was
escalated rather than taken here.** Before any seed had run, this session
settled that any negative slack on any seed, clock or corner would mean
report and ask rather than seal -- precommitted so that the rule could not be
reinterpreted once results were in. It fired on seed 3, and four more seeds
missed after the decision was taken; both options
were put to the coordinating session with the evidence and a stated lean, and
the decision to seal and run the kit was **the coordinating session's, on 15
November**, on the grounds that the sealed artefact is the identity-off
design, that the verification gates are unaffected, and that a seal and a kit
run are real evidence about a real artefact which withholding would not
improve. The rule was followed by escalation, not narrowed: what it forbade
was this session deciding alone that the misses did not count.

**The completed sweep is worse than the evidence that decision was taken
on, and that is said here rather than left for someone to notice.** When the
seal and the kit run were authorised, four seeds had run and two had missed.
Nine have now run and five have missed, so closure is the minority outcome
rather than an even split. Nothing about the sealed artefact changes --
it is the identity-off design, which is not affected by any of this -- but
anyone weighing slice 6's scope should weigh the nine-seed picture, not the
four-seed one that was in front of the decision.

**What this does and does not put in question.** The identity is tied to 0 in
every package until the switch flip, now slice 7, so the sealed core and the
kit run are the identity-off design, which the sealer fits at 87.0% and
closes at **+0.896 ns**; and the verification gates are green and have
nothing to do with placement. What does not close reliably is the design the identity
switches on, which is now **slice 7's** rather than slice 5's. That is this
slice's most important finding, it is a finding about the phase rather than
about the arithmetic, and it changed the plan rather than stopping a slice.

### Where the ALMs go

The per-entity fit is the #1584 method: the sealer's own Yosys program with
`flatten` removed, so the fit report lists ALMs by entity. Identity on, same
CUDA slot, seed 1. Dropping `flatten` is a different flow -- it fits at
39,717 ALMs and +0.455 ns against the flattened 39,682 and +0.176 -- so these
numbers attribute area and are not the timing result.

| Entity | ALMs (own) |
| --- | ---: |
| `fes_i386_x87` | **3,851.4** (3,664.0) |
| `fes_i386_x87_mul` | **187.4** (187.4) |
| `fes_i386_cpu`, whole | 28,491.5 (9,081.9) |
| `pc386_system`, whole | 39,053.4 |

Slice 2 measured `fes_i386_x87` at 1,414 and `fes_i386_x87_mul` at 178
through its own slot, so the arithmetic is roughly **+2,437 ALMs inside the
unit** -- read across slots, so indicative. Two things in that are worth
more than the total.

**The multiplier did not grow**: 187 against 178, inside the flow's noise.
That is the shared-multiplier design holding — every shift and every product
in this slice goes through the one nine-DSP block and returns to the state
`sh_ret` names, so the arithmetic bought its multiplies without a second
multiplier, and the DSP count is unchanged from slice 2's at 9.

**Almost all of it is the unit's own logic**, 3,664 of 3,851: the alignment
and normalisation paths, the PC-aware rounder, the operand classification and
the NaN rules, the exception sequencing and the sequencer itself. That is the
same conclusion the spike comparison reaches from the other direction, and it
is where a future reduction phase would have to look.

### Where the margin went, which is not where it looks

The arithmetic is **not** the critical path. The fit reports the worst paths
and, separately, the worst into and out of the x87:

| Fit | worst path | ends at | worst into the x87 |
| --- | ---: | --- | ---: |
| identity off | +1.074 ns | `cpu_r_dst[30]`, the register file's write | +5.563 ns |
| identity on | **+0.317 ns** | **`cpu_l1d_q_tx`, the L1 data cache** | **+1.023 ns** |

These are the path list the fit keeps, which is one corner; the headline
slack above is the worst of the system clock over **all** corners, so the two
differ: by 0.275 ns on seed 3, 0.189 on seed 4, 0.032 on seed 9 and not at
all on seed 6. The list says *where* a path is in the corner it dumps, not
what the worst slack is, and on the seeds where the two differ the path that
binds at the worst corner was not dumped and has not been read here. Both questions are answered below, and the second is answered
in the fit where names survive.

**How the fit selects these, because it changes what they mean.** The flow
dumps three groups of three per fit: the three worst paths to the system
clock, the three worst **into** a register matching the glob `*x87_*`, and
the three worst **from** one. The selector is a register-name glob, not the
`fes_i386_x87` entity, so it catches the CPU's own x87 state (`arch_x87_ds`)
as well as the unit's internals -- which is the right scope for "is the x87
near the critical path", but it is not "inside the entity", and an earlier
draft of this section described it as though it were.

| Non-flattened fit, identity on, seed 1 | slack | path |
| --- | ---: | --- |
| worst to the system clock | **+0.553** | `frontend\|qalign\|acc[7]` → `qalign\|start[2]` |
| worst **into** an `*x87_*` register | +2.300 | `wb_fault[1]` → `arch_x87_ds[5]` |
| worst **from** one | +4.082 | `x87\|mul\|p[1]` → `x87\|rnd_q[14]` |

So on that fit the nearest any x87 path comes to the critical path is
**1.75 ns behind it**. The margin is seed-dependent like everything else
here -- on seed 9 the machine's worst is −0.054 and the x87's paths hold
**+1.424**, so the gap is 1.48 ns -- but no fit in this sweep puts an x87
path anywhere near the binding one. The binding logic is the front end's
queue aligner.

**Which paths bind moves with the seed, and it is not one region.** The
worst path is the L1D (`cpu_l1d_q_tx`) on seed 1, the decoder's byte
registers (`cpu_decode_by[n]`) on seeds 3, 4, 5 and 6, and the front end's
aligner (`cpu_frontend_align_acc[8]`) on seed 9 -- the same aligner the
non-flattened fit names. Three regions, none of them the arithmetic.

**One claim this record made here has been withdrawn, and it is the one that
would have mattered most.** An earlier version said the decode paths were
already tied for critical in the merge-base and so did not become critical
because of slice 3, resting on `cpu_decode_by[5]` sitting at +0.727 there
against that fit's own worst of +0.720. The slack comparison is the
single-seed kind this record refuses elsewhere, and the **logic depth points
the other way**: the merge-base's decode path is **10 levels**, and the ones
that miss on this branch are **12 and 13**. Depth is a property of the
synthesised logic rather than of placement, and slice 3 does add x87 memory
operand shapes to `fes_i386_decode.sv`, so the likely reading is that this
slice's *decode* work deepened that path by two or three levels.

That is a different statement from the one this section is really making,
and both should be read exactly:

- **Pipelining or narrowing the arithmetic would still recover nothing.**
  The x87's own paths are 1.4 to 3.5 ns behind the binding one on every fit
  measured. That conclusion is unaffected.
- **"Nothing in slice 3 could help" is too strong.** The decoder is slice
  3's work too, and a decode path that gained two levels is a candidate for
  restructuring. This record does not claim it would close the clock -- the
  aligner and the L1D bind on other seeds -- only that the question is open
  and was wrongly closed here.
- **Settling it needs a targeted `report_timing` on both fits**, from
  `cpu_decode_by*` with the same query, which needs the fit databases and so
  a re-fit of each. That was not run: the gates for this slice were complete
  and the measurement belongs with slice 6, which is now the slice that
  looks exactly here.

The margin did not go into the adder, the rounder or the sequencer. It went
into the machine, through placement at 94.8% lengthening paths that have
nothing to do with the arithmetic -- and, on the evidence above, possibly
through two extra levels in a decoder this slice also touched.

**This record's own first draft of this section got it wrong, and the way it
went wrong is worth more than the conclusion.** It said the slack was going
into the shared datapath, and that slice 4's reuse of the same adder and
rounder would crowd the critical logic further. That is a plausible story
about logic this slice had just written, told without opening a path report
-- which is exactly when a plausible story is least safe. Had anyone acted on
it, the fix would have been a register stage in the sum or the rounding: a
re-freeze, every gate again, and **no slack recovered at all**, because those
paths already have a nanosecond. The paths were there to read the whole time.

Two consequences follow. **Pipelining the x87 would recover nothing**, for
the reason above. And **slice 4's cost in slack will track its cost in area**
rather than its reuse of the datapath: what the data supports is duller and
more general than the crowding story, which is that adding logic anywhere in
this machine at this utilisation costs timing everywhere, through placement.
If slice 4's ALM cost really is a few hundred, that is the hopeful reading
rather than the alarming one.

It is also an argument for the whole-system reduction phase beyond the area
it recovers. Deano deferred the audit's wins to a phase after the unit works,
on the reasoning that piecemeal trimming costs design quality for a few
hundred ALMs; that reasoning is untouched, but the payoff is larger than it
looked, because at 1% of a period slack is the binding constraint and those
wins buy slack as well as ALMs.

### The kit run

**Kit B, run `x3a`, 15 November, 20:48:50 to 20:51:47 EET: pass.** The
sealed package `48717256` -- digest-checked at both ends of the copy, and
its `core.rbf` matching the seal summary's `rbf.sha256` `926271cf` -- loaded
on the kit's baseline image as a fresh library game on the user-supplied
drive `a6977555`, FreeDOS booted to `C:\>`, Wolfenstein 3D started from the
DOS prompt and ran, and the run stopped and restored cleanly. HIL `rc=0`,
and the wrapper logged Kit B **FREE** at 20:51:47 with the baseline image
and bundle unchanged, no reboot, the agent idle, the lease free, and the
three FogCast data trees the run created removed.

`rc=0` is not the verdict, and was not taken as one. What the run shows is
in the frames:

| Frame | What it shows |
| --- | --- |
| [`x3a-boot`](kitB-x3a-boot.png) | FreeDOS to `C:\>`: `C: HD1 … 127 MB`, HimemX 3.38, `Interface : XMS 3.00 80386 4G`, Fast A20, CuteMouse at the PS/2 port, boot menu at entry 1 |
| [`x3a-s4`](kitB-x3a-wolf3d.png) | Wolfenstein 3D in play: Floor 1, HEALTH 100%, AMMO 8, LIVES 3, walls and a downed guard drawn correctly, full status bar |
| `x3a-moved`, `x3a-turned` | the view moves and turns under the agent's input, so the renderer and the input path are live rather than one frozen frame |

The load's eleven active interfaces are exactly the manifest's, **`fes.pc386.machine 1.0`
among them**, which is the check that the kit ran this package and not a
cached core.

**The check that the kit got the sealed bits was specified wrongly first,
and the right one is in the repository.** The plan was to compare the
`.fcore`'s sha256 with its filename before loading it. That failed, because
a package id is not the tar's hash: `scripts/core_package.py`'s
`package_identity` hashes `FES-CORE-PACKAGE-2\n`, then the manifest and the
payload each with a length prefix. Recomputing *that* over the transferred
file reproduces `48717256…` exactly, and the payload inside it hashes to
`926271cf…`, the `rbf.sha256` in the seal's own build summary. So the copy
is provably the sealed artefact. The failed check was not a gate the
artefact failed -- it was a gate written against a guessed format, and the
fix was to read the packager rather than to invent a second check.

**What this run is evidence of, and what it is not.** The identity is off in
this package, so the run exercises **none** of this slice's arithmetic: it
is a regression check that the machine around the unit still boots and plays
with slice 3's RTL in it, and that is all it is. The arithmetic's gates are
the unit bench and the lockstep. A green kit run here would not have
rescued a red bench, and the frames are worth reading for what they are:
evidence that 1,609 ALMs of new logic did not disturb the machine.

**Four ways the kit wrapper would have stranded the kit, all found before it
touched one.** Two kits were wedged on 14 November by this class of thing, so
every check in the wrapper was run against either the live kit or a real
`result.json` before the run. Slice 2's own x87 runner is the right base,
because it takes the baseline image and software as environment variables
rather than parsing the status file. What was wrong:

- **A release gate built on a guessed `result.json` schema.** The first
  version looked for an event with `op == "load"`. There is none: a command
  event is `{"seq", "cmd", "result"}` and a load's state is at
  `result.load.state`. A clean run would have been read as "records no load",
  the wrapper would have halted, and the kit would have stayed IN USE.
  Caught by running the gate against slice 2's own passing `x7a` run, where
  it must print nothing.
- **A `"phase":"idle"` check that can no longer pass.** The kit's software
  has moved and its local-control status path now answers 404, so the
  snapshot line reads `LOCAL 404 page not found` and the grep never matches.
  It would have halted every run. Dropped, and the guarantee kept: the lease
  probe is live and authoritative and must read `idle free` both before the
  claim and after the run.
- **Two helper scripts importing the repository's `kit` module from deleted
  worktrees.** They carried absolute paths into other sessions' trees.
  Repointed at this slice's frozen worktree, which is also what makes the
  module the one the gates ran against.
- **A stale baseline regex** in the first wrapper this session adapted: the
  form it matched (`baseline <40> overlay <64>; OS <64>`) still appears in
  entries from early November, so reading the whole status file for it picks
  up a baseline from a week ago and the post-run comparison is against the
  wrong software.

The command template ends in `{"op": "quit"}`, without which the driver sits
in a thirty-minute idle wait still holding the lease.

## What is left

- **TestFloat's extF80 vectors, which the plan names as this slice's gate,
  are not built.** The plan asks for TestFloat add, sub, mul, compare and
  roundToInt in every precision and rounding mode. Berkeley SoftFloat
  (`07ebd29db0cfb55723a79fbbffe99103ef38e305`) and TestFloat
  (`a9c849f1b0eb0264b626d9686ffae167d996e3be`) clone cleanly and the 8086
  specialisation is the right one for an x87 rather than the 8086-SSE
  default, but building them in this session was refused by its own
  permission settings, so the gate is **unmet** and the host bench is
  reported as what it is: the same 80-bit oracle, driving the real CPU
  interface, over the PRM's operand classes rather than TestFloat's
  systematic grid. The difference worth naming is coverage, not reference.
- **Divide, square root, FPREM and FPREM1** are slice 4's, and the
  transcendentals, FBLD and FBSTP slice 5's. They stop as not built.
- **A store with an unmasked precision exception** should still make its
  access, by rule 1 above, and slice 2's gate does not let it. Slice 3
  confines its change to its own forms rather than alter RTL slice 2 sealed
  and lockstepped; [#1872](https://github.com/DeanoC/fes/issues/1872) carries
  it with the host probe that settles it.
- **The area figures are not differenced against slice 2's.** Slice 2's were
  measured through the HIP toolchain slot and these through the CUDA one, and
  the two slots' Yosys binaries differ (Powerboat gcc 14.2, the chip host gcc
  13.3), so their netlists are the same logic with different net numbering
  and Quartus fits them differently -- 36,055 against 36,069 ALMs and setup
  +0.594 against +0.410 on identical RTL, which is the size of the deltas
  this slice reports. Quartus itself is host-independent. So every figure
  here says which slot it came from, and slice 3's own cost is measured as
  identity-on on this branch minus identity-on at its merge-base **through
  the same slot**, rather than against slice 2's published numbers. The next
  slice should do the same rather than read this as an omission.
- **The unmasked exception masks are exercised on the register forms only.**
  The seven mask settings in the bench's edge matrix run over register-form
  add, subtract, multiply, compare, FSCALE, FRNDINT and FXTRACT; the memory
  forms run every operand format and reg field but with all six exceptions
  masked. The exception path does not depend on where the second operand came
  from -- by the time the apply runs, a memory operand has already been
  converted into the same registers -- but that is an argument and not a
  measurement, and adding the cases now would change a gate input after the
  freeze.
- **The unmasked-exception rules are read off one real x87, not a 387.** The
  bias adjust, the pre- and post-operation split and the tininess rule were
  measured on an x86-64 host, which is a much later part. A 387 is believed
  to behave the same, because they read as architectural rules rather than
  implementation choices, but **nothing here corroborates that**: the
  lockstep's programs are masked throughout, so the oracle never exercised
  any of them, and no 387 was available. It is the same open question the
  plan record already carries for real-387 measurements, and these five
  rules now depend on it.
- **The identity's record word** is still unbuilt, as slice 2 left it, so a
  kit can only run the sealed package with the identity **off**. This
  slice's kit run is therefore a **regression run: it exercises none of this
  arithmetic**. Two separate things have to land before a kit can: the
  identity flip, now **slice 7**, and slice 5's transcendentals -- because
  every title the census found computing except SpeedSys's FPU test also
  runs a transcendental. The bench and the lockstep are the gate here;
  the kit run is not.

## What the sweep changed, which is the plan rather than this slice

Deano changed the x87 plan in response to the sweep, on 15 November
([#1768](https://github.com/DeanoC/fes/issues/1768#issuecomment-6899342397)):

1. **Turning the identity on leaves slice 5 and becomes a new slice 7.**
   Slice 5 is now the transcendentals and FBLD/FBSTP alone; the identity's
   record word, its FogCast setting and switching `x87_present` on are slice
   7's.
2. **Slice 6 becomes a full design optimisation** rather than the governor's
   ESC classes and acceptance, and **may trade performance on the
   identity-on configuration** to get it closing. It carries the whole-system
   area reduction deferred on 14 November.
3. **Two core versions on the DE10 are acceptable** — one without the 387 and
   one with it — rather than forcing one package to close both ways.
4. **A DE25 version may need only one**, since that board's hard
   floating-point DSP blocks could make a 387 much cheaper in fabric.

**So this sweep is no longer a warning; it is the evidence that set slice 6's
scope.** The identity ships off through slices 5 and 6 as well, so no slice
before 7 needs the identity-on machine to close, and the identity-on figures
from slices 4 and 5 now serve the same purpose: telling slice 6 what to fix.

**What slice 6 should fix, and what it should not.** The only evidence of what
binds is in [the margin section](#where-the-margin-went-which-is-not-where-it-looks):
the **L1 data cache** (`cpu_l1d_q_tx`, `cpu_l1d_stall`) and the register
file's write path, with the x87 holding **1.0 to 1.5 ns on every seed
examined**, including both that missed. The negative result matters as much as
the positive one and is stated plainly because the first instinct on reading
"the margin is thin" will be to optimise the arithmetic: **an x87 fix was
looked for and there is not one.** A pipeline stage in the sum or the
rounding would cost a re-freeze and every gate and buy no slack at all.

**One part of the spike's estimate is still intact, and it is the memory.**
The spike predicted **+1,687 ALMs, 9 DSP and 17 M10K** in-machine for the
whole of option A. The ALM budget was spent by slice 3 alone, and the DSP
and M10K counts have not moved at all: the merge-base's identity-on fit
already reads 49 DSPs and 286 M10Ks, exactly what this branch reads, so
**slice 3 spent none of the memory budget and none of the DSPs** -- the four
M10Ks between 282 and 286 are the identity-off to identity-on difference,
which is slice 2's register file, not this slice's. **17 M10K of the 17
remain** for the slices the budget was meant for. (An earlier draft credited
those four to slice 3, by differencing identity-off against identity-on
instead of the merge-base against the branch.) Slice 4's Goldschmidt
seed tables and slice 5's microcode ROM are exactly what that budget was for,
and the spike's own finding was that microcode transcendentals cost ROM
rather than logic. Worth stating with the caveat that it is the same
estimating method that produced the 1,687 — so read it as the one part not
yet falsified rather than as a reliable forecast.

## A method note, from four review findings of one kind

Codex found four P2s on this record, and all four were the same failure, so
it is worth one paragraph rather than four fixes. None was a wrong
measurement. Each was a **claim that drifted from the evidence somewhere else
in the document**: stale comparison totals after the bench grew; a SoftFloat
corroboration narrowed in one section and left standing in another; a
provenance sentence saying every figure came through one toolchain slot while
a subsection reported a fit from the other; and a margin drop computed across
two slots in a record that quantifies those slots as differing by 0.184 ns on
identical RTL -- about half the drop being attributed to the slice.

That last one is the sharpest, because this record exists partly to prevent
exactly that comparison, and it made it anyway. The common cause is a long
record written in pieces over hours: each paragraph was right when written,
and later paragraphs reasoned from prose rather than from the measurements.
It is the #1855 pattern in a new form -- not one claim restated in several
places and going stale, but one claim stated correctly and then reasoned past.

What would have prevented all four: **compute every comparison from the
fits' own table rather than from a sentence earlier in the document, and
quote no delta whose two sides are not in that table.** Three of the four
reached the coordinating session before they were caught, so the cost is not
only a review round. A slice that reports numbers should keep them in one
place and derive, not restate.

## Reproducing

From `sources/misteross`, with the i386 lock's toolchain slot in
`--cache-root`:

```bash
python3 scripts/test_fes_i386_x87_unit.py --cache-root /abs/fes-toolchain-cache
```

```bash
python3 scripts/test_fes_i386_x87_unit.py --cache-root /abs/fes-toolchain-cache --netlist
```

```bash
python3 scripts/test_fes_i386_lockstep.py --download --cache-root /abs/fes-toolchain-cache
```

```bash
python3 scripts/test_fes_i386_test386.py --download --cache-root /abs/fes-toolchain-cache
```

The standalone fit of the unit, and the machine fits the area section
reports, with the pc386 lock's slot:

```bash
QUARTUS_ROOTDIR=/abs/intelFPGA_lite/17.0/quartus python3 scripts/quartus_x87_spike.py --cache-root /abs/fes-toolchain-cache --mode standalone --variant unit --seed 1 --output /abs/scratch/unit-s1
```

```bash
QUARTUS_ROOTDIR=/abs/intelFPGA_lite/17.0/quartus python3 scripts/quartus_x87_spike.py --cache-root /abs/fes-toolchain-cache --gpu-router CUDA --mode machine --variant base --identity-on --seed 1 --output /abs/scratch/identity-on
```

To see a finding rather than take it on trust, the bench names a case for
each: `latch-f0-edge` for the port-F0h race, `arith-latch-pe` for the
precision exception that writes its result and still latches BUSY#,
`align65` for the clamp, `tiny-edge4` and `tiny-edge5` for the two operand
pairs that settle tininess after rounding, `qnan-tie` for the NaN sign rule,
and `xtract-overflow-masked` for the one push this slice makes.
