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
   masked underflow fires only where the denormalising lost bits: 2^-8200
   squared is exactly a denormal and masked raises *nothing at all*.
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
  the CPU's issue interlock. It also checks the oracle agrees.
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
| ... on the seal's Yosys netlist | **537 / 0** |
| lockstep, RTL: every bucket | **0 fail** (1,933 no-387 cases with the identity off, 14 x87 cases with it on, 84 directed) |
| ... on the netlist, and governed at preset 3 | **(pending)** |
| test386 | **POST … E0 EE FF**, halted |
| Quartus 17.0.2 seal | **(pending)** |
| kit run on that seal | **(pending)** |

Every gate runs from a clean worktree at the frozen head, so no later edit of
the bench or the harness can reach the tree a counted run used.

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
- **The unmasked-exception rules are read off one real x87, not a 387.** The
  bias adjust, the pre- and post-operation split and the tininess rule were
  measured on an x86-64 host, which is a much later part. A 387 is believed
  to behave the same -- these are architectural rules rather than
  implementation choices, and the oracle's SoftFloat agrees with them -- but
  no 387 was available to confirm it, which is the same open question the
  plan record already carries for real-387 measurements.
- **The identity's record word** is still unbuilt, as slice 2 left it, so a
  kit can only run the sealed package with the identity **off**. This
  slice's kit run is therefore a **regression run: it exercises none of this
  arithmetic**. Nothing real will exercise it until slice 5 lands either,
  because every title the census found computing except SpeedSys's FPU test
  also runs a transcendental. The bench and the lockstep are the gate here;
  the kit run is not.

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
