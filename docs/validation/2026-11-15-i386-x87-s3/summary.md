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
all three precisions. Roughly **183,000 comparisons** against real hardware.

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

## Checks

| Gate | Result |
|---|---|
| unit bench, against a real 80-bit FPU | **(pending)** |
| unit bench on the seal's Yosys netlist | **(pending)** |
| lockstep, identity off and on | **(pending)** |
| test386 | **(pending)** |
| Quartus 17.0.2 seal | **(pending)** |
| kit run on that seal | **(pending)** |

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
- **The identity's record word** is still unbuilt, as slice 2 left it, so a
  kit can only run the sealed package with the identity **off**. This
  slice's kit run is therefore a **regression run: it exercises none of this
  arithmetic**. Nothing real will exercise it until slice 5 lands either,
  because every title the census found computing except SpeedSys's FPU test
  also runs a transcendental. The bench and the lockstep are the gate here;
  the kit run is not.
