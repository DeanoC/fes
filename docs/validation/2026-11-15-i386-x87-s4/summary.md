# i386 x87 slice 4: divide, square root and the remainders, 16 November 2026

Slice 4 of the [x87 plan](../2026-11-12-i386-x87-plan/summary.md#slices)
([#1768](https://github.com/DeanoC/fes/issues/1768), this slice
[#1888](https://github.com/DeanoC/fes/issues/1888)) gives the unit slices 2
and 3 built its divide, its square root and its two remainders: FDIV and
FDIVR with their integer, ST(i) and popping forms, FSQRT with the exponent
halving, and FPREM and FPREM1 with the partial-remainder behaviour — each
partial step's C2, and the final C0, C3 and C1 quotient bits — under precision
as well as rounding control, with every exception they raise driving the ES,
ERROR# and IRQ 13 path slice 2 built.

The transcendentals, FBLD and FBSTP are slice 5's and stop as not built.
[What is left](#what-is-left) states what this slice's scope names and this
PR does not have.

**The scope was checked against the tracker before any RTL was written**, as
slice 3's record recommends, and it matched. [The scope reading](scope.md) has
that check; two of its own points needed correcting afterwards and
[the design](design.md) records them as withdrawn.

## The finding to read first

**Goldschmidt with 64-bit truncated intermediates floors at about 2^-61
relative error, however many iterations it runs** — measured 2^-15.8 after
one, 2^-31.6 after two, then 2^-61.4 and no better at three, four or five.
Every intermediate is the top 64 bits of the multiplier's product, so each
step truncates and the quadratic convergence stops at the register width.

That is short of the 67 bits the extended format needs, and short in a way no
iteration count fixes. More demandingly, the format needs those bits as an
**exact truncation with a true sticky bit**, because that is the interface
slice 3's `round_pc()` presents, and a rounded reciprocal product supplies
neither.

**So the iterations are followed by an exact tail, and it is cheap.** A
measured error bound `E` gives a candidate below the true truncation by
construction; `k` restoring compare-subtract steps on slice 3's adder find
the increment exactly and leave the exact remainder — which *is* the sticky
bit, free, from a step that was needed anyway. No second multiplier, no
dedicated divider, and the 131-bit back-multiply a naive correction would
need is not required, because the remainder is bounded and only its low bits
matter.

**The remainders then need no divider of their own.** A step forms its
candidate at its own scale and the divide's tail produces both the exact
quotient and the exact remainder. That was checked by running the datapath,
not argued.

## What the host settled

Three rules came out of the bench that a manual reading would have got wrong,
and they continue slice 3's eight rather than standing apart. Each is in the
RTL with the case that settles it.

9. **A denormal operand does not raise DE beside the operation's own
   pre-operation exception.** A pseudo-denormal divided by zero reports ZE
   alone, not ZE and DE — and **so does a denormal the memory conversion
   flagged**: FDIVR with a denormal m32 over zero reports ZE alone. So slice
   3's rule 5 ("an unmasked pre-operation exception reports nothing else")
   holds for **masked** exceptions too, which slice 3's own record could not
   say, because its identity-on lockstep ran everything masked. A later
   slice's bench closed a gap in a merged slice's evidence.
10. **A pseudo-denormal handed back unreduced comes back canonical.** Where
    the divisor is larger, FPREM returns the dividend unreduced — and the host
    returns `0001 8000…` where the operand was `0000 8000…`, because a
    pseudo-denormal's effective exponent is 1, so its canonical form is the
    smallest normal's. **A genuine denormal is left exactly as it is.** An
    implementation written from the PRM would plausibly get this wrong in
    either direction: normalising both, or neither.
11. **The remainders' specials are not the divide's.** `inf mod 2` and
    `2 mod 0` set IE and give the indefinite QNaN; `0 mod 2` gives +0 with
    **no exception at all**; and `2 mod inf` returns the dividend with C2
    clear and no exception — **a divisor of infinity is not an invalid case
    and does not reduce**, which is the one a PRM reading gets wrong.

And two facts about the condition codes, measured rather than read:

- **The quotient bits are `q = C0·4 + C3·2 + C1`**, confirmed over q = 0 to 9
  for both instructions. The PRM's q2/q1/q0 map to C0/C3/C1 in that order,
  which is easy to get backwards from the manual alone.
- **C1 has two different meanings in this unit, and they take different
  paths.** For the arithmetic it is the rounding's direction and comes from
  `pc1`; for a remainder it is the **quotient's bit 0**. `S_APPLY` writes the
  codes as `{nc[3], nc[2], c1, nc[0]}`, so C1 comes from `pc1` and *not* from
  the condition-code vector — a remainder that sets it in the vector leaves it
  always clear. Any matrix row written for rounding-C1 is wrong for the
  remainders, and the rows say which meaning each carries.

**These rest on a modern x87, not a 387**, the same open question slice 3's
record carries: no 387 was available, and the lockstep's programs are masked
throughout, so the oracle corroborates none of the exception rules.

## What Bochs settled, which removes a divergence rather than recording one

**A partial FPREM step reduces `n = (expDiff & 31) | 32` quotient bits — 32
to 63, not 64 — and Bochs's `cpu/fpu/fprem.cc` at the pinned revision
computes exactly that quantity.** So the host and the oracle agree bit for
bit and **lockstep can be exact on partial steps with no divergence to
record**.

The exponent *drop* is `n` plus whatever the subtraction cancels, which is
what made a fixed 64 look plausible: at a difference of 999 the drops are 40
then 64, 64, 64, and those are `n` = 39 then 63, 63, 63 with one bit of
cancellation each. **This record claimed a fixed 64-bit step before the
per-step probe was written, and the claim reached the coordinating session as
measured.** It is withdrawn in [the design](design.md), along with the 65-bit
partial quotient it implied.

## Method

Four instruments, answering different questions.

- **The unit's bench** (`sim/i386/x87_unit_tb.cpp`, driven by
  `scripts/test_fes_i386_x87_unit.py`) is the one that *settles* behaviour: it
  drives the unit's CPU interface and compares the whole architectural state
  against the host's own instruction, generated from its two opcode bytes. The
  remainder rows run under the **full** status mask and not `ARITH_SW`, which
  masks out C0, C2 and C3 — exactly the bits a remainder reports. **It runs
  operations back to back by construction**, reusing one unit across every
  operand pair in a row, which is what makes it able to catch a stale
  registered value rather than lucky to have caught one.
- **The exactness models** (`model/`) sized the datapath before any RTL, and
  `model/fixed_constants.py` is the gate on the constants: `E` and `k` pinned
  as the RTL carries them, validated over 3,000,000 cases on a seed no fitting
  run used.
- **The locked-Yosys netlist** is not a formality. The netlist bench **failed
  at the first attempt** — the locked Yosys rejects `real` in a module body —
  so the seed tables are literals, emitted by `model/seed_tables.py` whose
  `--check` re-derives them and verifies the RTL still holds exactly those
  values. Run only on Verilator, that would have surfaced at the seal.
- **The lockstep** proves the unit is wired up; `x87_built_form` in
  `lockstep_tb.cpp` is a C++ mirror of `x87_decode` and moves with it.

## Results

| gate | result |
|---|---|
| unit bench, RTL | **701 passed, 0 failed** |
| unit bench, locked-Yosys netlist | **701 passed, 0 failed** |
| lockstep, identity off and on | not run yet |
| test386 to POST FFh | not run yet |
| TestFloat div, sqrt, rem | not attempted yet |
| QEMU's fprem test | not run yet |
| directed partial-step chains | not written yet |
| Quartus 17.0.2 seal | not run yet |
| kit run | not run yet |
| area, and the nine-seed sweep | not measured yet |

## What is left

- **Every gate but the unit bench.** The table above is the honest state: the
  arithmetic is built and checked against real hardware on the RTL and on the
  netlist, and nothing else in the slice's gate list has run.
- **The area answer to #1768's peak-or-trend question**, which is the
  decision-relevant number and is not measured. The stop rule was set in
  [the scope reading](scope.md) before any fit existed and has not fired
  either way, because no fit has run.
- **The decode landed per operation with the unit that runs it**, not in one
  commit. Adding `X87_SQRT` or `X87_PREM` to `x87_decode` un-stops the
  instruction — `fes_i386_decode` gates on `!= X87_UNIMPL` — so a branch with
  the decode and without the unit's handling would accept FSQRT with nothing
  to execute it and break lockstep rather than leave it cleanly not-built.
  This is worth stating because it is the kind of thing a later reader tidies
  into a single decode commit.
- **The transcendentals, FBLD and FBSTP** are slice 5's and stop as not built.
- **The cycle-count assertions that would make early termination observable**,
  the sim-only assertion that the post-tail remainder is in range, and the
  divide, square-root and remainder rows in the bench's exception-mask and
  `arith-latch` matrices. Unmasked ZE on the commit edge is the hazard this
  slice's brief names and nothing exercises it yet.
- **Whether a real 387 also slows on denormal operands.** The latency is fixed
  by precision control for *normal* operands; a denormal adds
  pre-normalisation passes. Whether that matches the part is a 387 datasheet
  reading question and it has not been done.

## The registered-read hazard, for the fifth time

Every slice of this unit has hit one hazard, and each time it looked like a
different defect: slice 2's BUSY# latch hole, FSTP ST(0)'s masked underflow
and FLDCW's late summary; slice 3's unmasked OE/UE/PE not latching and the
port-F0h edge; and here **the seed table's registered read**. Starting the
first multiply in `A_OP` used the *previous* operation's seed, because the
entry for this divisor is not out of the table until the cycle after
`A_SETUP` assigns `ob_m`. It cost **585 of 684 cases**.

The general form: anything derived from a registered value is a cycle late
relative to a sequencer that transitions in one edge, so work begun in the
assigning state sees the previous operation's value — and because that value
is usually plausible, the symptom is a wrong result rather than a crash. The
practical defence is the one this bench has by construction: **run operations
back to back**, because a single operation hides the class entirely.

## Reproducing

From `sources/misteross`, with the i386 lock's toolchain slot in
`--cache-root`:

```bash
python3 scripts/test_fes_i386_x87_unit.py --cache-root /abs/fes-toolchain-cache
```

```bash
python3 scripts/test_fes_i386_x87_unit.py --cache-root /abs/fes-toolchain-cache --netlist
```

The models, from `docs/validation/2026-11-15-i386-x87-s4/model`:

```bash
python3 fixed_constants.py 500000
```

```bash
python3 seed_tables.py --check ../../../../sources/misteross/cores/fes-common/rtl/i386/fes_i386_x87.sv
```
