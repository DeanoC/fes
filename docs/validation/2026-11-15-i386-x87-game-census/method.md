# Method: what the games ask of an FPU, 15 November 2026

Committed **before** the runs this record reports. Tracker: the census issue
this record closes. The question: would a reduced-precision "game" x87 —
narrower than the 387's 64-bit significand — be worth building for the
titles pc386 actually runs?

This is an oracle-only measurement. No RTL, no kit, no fits.

## What I had already seen before writing this

Full disclosure, because this lane's rule is that a method is committed
before measurement and amendments are dated:

- The [12 November plan record](../2026-11-12-i386-x87-plan/summary.md) and
  its `census.jsonl`, measured on tree `2d895034c` with an uncommitted
  patch. I read its per-title table in full, including Quake at "24-bit
  97%, 64-bit 3%", Screamer at "53-bit 100%", Chris's benchmark at 64-bit,
  the exception-mask figures (3Fh almost everywhere, 3Ah for Chris's
  benchmark, 30h for Norton SI 8) and **DOOM's 11 retired ESCs**.
- Slice 2's [area record](../2026-11-14-i386-x87-s2/summary.md), including
  the ~967-ALM floor and its ablation table.

So the precision-control result is **not** a blind measurement. What is new
here is: a re-measurement on today's tree with the committed
`census.h`, the operand-width and significand-width columns the earlier
census did not have, the three first-acceptance titles it never ran
(Wolfenstein 3D, Ultima VI, Ultima VII: The Black Gate), and the call-site
attribution of Quake's non-PC=24 arithmetic.

## The negative control, restated

The brief asks for DOOM as a negative control that "should show no x87 at
all". **That is not the committed rule**, because the 12 November record
already shows DOOM retiring 11 ESCs with a 387 fitted — the
FNINIT/FNSTCW/FNSTSW detection probe, which a fixed-point engine still
runs. The committed control is:

> **DOOM must retire zero load, store, FXCH, arithmetic and transcendental
> forms.** Control forms (FNINIT, FNCLEX, FNSTCW, FLDCW, FNSTSW) and the
> 287/387 infinity test are expected and do not fail the control. Any
> arithmetic or any memory load or store of a real operand in DOOM's window
> means the instrumentation is wrong and nothing else in this record is
> reported.

This departs from the brief's wording and the departure is deliberate.

## What is counted

Every count comes from `machine_tb --census --x87 1` through
`pc386_census_summary.py`; no new oracle hooks beyond the keys listed
below. The census framework (`cores/fes-pc386/sim/census.h`) is already
committed and already counts ESC forms by mode, the control word by
arithmetic class, exception flags raised and unmasked, ES, #NM/#MF, WAIT,
the emulator fixup interrupts and the AT coprocessor ports. This record
adds, as committed instrumentation in the same header:

1. `x87cwform:<form>:<cw>` — the control word every retired ESC form ran
   under, not only the arithmetic classes. This is what gives precision
   control **and** rounding control per form, including rounding on the
   integer stores where it matters.
2. `x87cwsec:<second>:<cw>` — the control word by whole simulated second,
   which is "over time" as the brief asks.
3. `x87cwset:<form>:<old>><new>` — control-word writes, from
   `after87.control`, for FLDCW, FNINIT, FLDENV and FRSTOR, **including
   writes that leave the value unchanged**, so "does the title ever write
   the control word" is answerable directly rather than inferred from the
   value.
4. `x87sig:<form>:<bucket>` — the significand width in use. For each
   retired ESC, `64 - ctz(significand)` of ST(0), bucketed `le24`, `le53`,
   `le64`, `zero` for a zero significand and `empty` for a slot the tag word
   marks empty (whose contents are stale and are not a width). ST(0) is read
   from the state **after** the step, except for the store forms —
   `FST`/`FSTP m32`, `m64`, `FSTP m80`, `FIST`/`FISTP`, `FBSTP` and
   `FST`/`FSTP ST(i)` — where the value stored is only in the state
   **before** it, since a popping store leaves the register underneath in
   ST(0). This is the measurement that distinguishes "runs at PC=24" from
   "holds only values a 24-bit significand can represent".

   *This definition was written as "ST(0) after the step", with no `empty`
   bucket and no store exception, and is corrected here to match `census.h`.
   Amendments 2 and 5 below record the two fixes, what they cost and when
   they were made; this is the canonical statement brought into line with
   them, not a further change. Anyone reproducing the census should read the
   amendments as part of the method.*
5. `x87pcsite:<pc>:<cs>:<page>` — for each retired arithmetic ESC, its
   precision control and the code page (CS and EIP>>12) it ran on, so a
   title's non-PC=24 arithmetic can be attributed to the engine or to a
   library or DPMI host.
6. `x87store:<form>:<bucket>:<double|outside>` — for each retired store,
   its significand bucket and whether the value's exponent is inside
   binary64's normal range, **jointly**, since an m64 destination can
   overflow, underflow or round a value whose significand fits. The two
   marginals cannot answer "how many `le53` values did this form store in
   range" once a form stores a mixture, which is why the key is joint and
   why version 3's separate `x87exp` was replaced. Added at key version 4,
   **after** these nine runs, which therefore do not carry it (amendments 10
   and 14).
7. `x87irq13:75:<mode>[:es]` and `x87errintr:<vector>:<mode>` — the AT's
   coprocessor interrupt at the BIOS's own vector, and the
   vector-independent superset of any external interrupt delivered with a
   387 error pending, since the slave 8259's base is programmable
   (amendment 12). Reliance is read from the superset.
8. `x87census:version` — the key set's version, so a reader can tell what a
   census file was able to record rather than inferring it from which keys
   are present (amendment 9). **These nine runs are version 1.**

Operand precision (m32 / m64 / m80) is **not** new instrumentation: it is
determined by the opcode and the ModRM reg field, which the existing
`x87op:<form>` keys already carry. The classification lives in
`pc386_census_summary.py` so that it is reviewable and testable:

| Form | Operand |
| --- | --- |
| D9.m0, D9.m2, D9.m3 | real m32 (FLD, FST, FSTP) |
| DD.m0, DD.m2, DD.m3 | real m64 |
| DB.m5, DB.m7 | **real m80** (FLD, FSTP) — the only explicit extended forms |
| DB.m0/m2/m3, DF.m0/m2/m3 | integer m32 / m16 |
| DF.m5, DF.m7 | integer m64 |
| DF.m4, DF.m6 | BCD m80 |
| DD.m4, DD.m6, D9.m4, D9.m6 | FRSTOR / FNSAVE / FLDENV / FNSTENV images |

FNSAVE and FRSTOR images are reported **separately** from explicit m80
operands, because a DPMI host's lazy x87 context switch produces them and
they are not the title relying on extended precision.

## The window

Each title runs its drive record's own launch and keys, on the same drive
the earlier census and the game-drive census used, for the drive record's
limit (20 s default; the earlier census's longer windows are kept where it
used them: Quake 25 s, the `fpu` bucket 30 s). Every claim in this record
is scoped **to that window**, and each row says what the window reached
(prompt, menu, or play), because a title that uses the 387 only in play is
undercounted by a launch window. That limitation is the earlier census's
too and is not fixed here.

## Calibration

Σ`x87op:*` must equal the D8–DF stem total in the same census file. I do
**not** commit equality with the 12 November run's 45,787,636 for Quake:
the tree has moved since `2d895034c`, and the committed `census.h` classes
DA's register forms and FPREM differently from that run's scratch patch, so
the totals are not required to agree. A difference is reported, not
treated as a failure.

## How a title is classified

For each title, from its own window's counts:

- **PC class**: `24`, `53`, `64`, or `mixed`, from `x87cwform` over the
  arithmetic forms (add, subtract, multiply, divide, square root — the
  forms precision control acts on). `mixed` whenever more than one PC value
  appears over those forms, with each value's count and its code pages.
- **Sufficiency of a 24-bit significand**: a title is **sufficient** only
  if *all* of these hold in its window: every arithmetic ESC ran at PC=24;
  no `x87sig` bucket above `le24` is non-zero; and no m80 load or store
  (DB.m5, DB.m7) executed. **Any arithmetic at PC above 24 with a count
  above zero, or any significand wider than 24 bits, makes the title not
  sufficient** — a small percentage is not sufficiency, and it is reported
  with its count and its sites rather than rounded away.
- **Relies on exceptions**: ES set at least once (`x87es`), or IRQ 13
  delivered, in the window. Unmasked-but-never-fired is reported as
  "unmasked, none fired", which is not reliance.
- **Probe only**: zero load, store, FXCH, arithmetic, `FRNDINT` and
  transcendental forms. *(`FRNDINT` is named explicitly as of 16 November
  2026; when this rule was written the classification counted it as
  arithmetic, and amendment 2 later split it into its own `round` class
  because it ignores precision control. Naming it here keeps the rule's
  **behaviour** as committed rather than letting a later reclassification
  quietly narrow it. All nine titles classify identically either way.)*

A rule this method does not have, and will not acquire after the fact: there
is no threshold below which non-PC=24 arithmetic is ignored.

## A limit of this census, stated in advance

**Precision control is not fp32, and PC=24 sufficiency is about the
significand only.** Precision control narrows the significand to 24 or 53
bits; the x87's exponent field stays 15-bit extended at every PC setting,
and the register file stays 80-bit. So a title measured "sufficient" above
would be reproduced by a **24-bit-significand, 15-bit-exponent** unit, not
by an fp32 unit: an fp32 datapath would still diverge on any intermediate
outside fp32's exponent range, on overflow, on underflow and on denormals.
This census does not measure the exponent range of the values the titles
compute, and so **cannot** say whether an fp32 unit would reproduce any
title. Settling that would need the exponents of every intermediate
recorded, which this instrumentation does not do.

Likewise, PC=24 bounds the results of add, subtract, multiply, divide and
square root only. FILD, FLD m64, the constant loads, FRNDINT, FPREM,
FSCALE, FXTRACT and the transcendentals put values in registers under no
precision control at all. The `x87sig` buckets are what catch that, and
they are a measurement of the values observed in the window, not a proof
about values the title could produce.

## Amendments

### 15 November 2026, before the first run

What I had seen when I made it: only the 12 November record and slice 2's
area record, as disclosed above. **No run of this census had been made** —
the simulation build was still compiling. Six changes, all of them because
writing the rules down showed them to be wrong or unmeasurable as stated:

1. **The probe-only rule is now mechanical, and it admits the infinity
   test.** *(Reverted 16 November 2026 — see amendment 13.)* The method's prose allowed the 287/387 infinity test, but that
   test's FLD1, FLDZ, FDIVP, FCHS and FCOMPP class as `load` and `arith`, so
   the prose and the script disagreed. The committed rule is now the
   script's: **probe only means no real or integer memory operand moved, no
   FXCH and no transcendental.** The infinity test is register-only, so it
   passes; one FLD m32 does not. A probe-only title gets **no** sufficiency
   verdict (`null`), because sufficiency is a question about computation.
   The DOOM control is unchanged in substance: zero memory FP operands,
   zero FXCH, zero transcendentals.
2. **`x87sig` reads the state before a popping store.** After FSTP, FISTP
   or FBSTP, ST(0) is the register underneath, or an empty slot holding
   stale bits, so the value the title stored is only in the state before the
   step. As first written the per-form store rows would have described the
   wrong register. An empty slot (tag 3) is now its own bucket, `empty`,
   and not a width.
3. **`x87cwset` catches every writer**, not a list of forms: FNSAVE
   reinitialises the control word on a 387, and a DPMI host's lazy context
   switch will do that inside Quake. Any step whose control word changes is
   counted.
4. **The calibration is reported, not required.** Σ`x87op` counts retired
   ESCs only, while the stem total and the fault and held counts sit on
   different sides of that line, so an exact identity is not a rule I can
   state correctly in advance. The script emits all four numbers
   (`calibration`) and the record reports the difference.
5. **Drives are pinned by SHA-256** to the ones the 12 November census used,
   because several title ids appear on two drives in the store and the
   resolver would otherwise choose. Quake and Chris's benchmark on `bench`
   `e69a4cb6`, Screamer and Terminal Velocity on `dpmi` `9e6ae3a3`,
   Wolfenstein 3D and Ultima VI on `real` `a6977555`, Ultima VII: The Black
   Gate on `xms` `5053c36b`, Serpent Isle on `fpu` `ff322ddc`, DOOM on
   `dpmi4g` `b4be6e29`. The earlier census ran Quake from a `bench-q` copy
   with its own `Q.BAT`; this run uses the store's `quakes` title on
   `e69a4cb6` instead, which is a difference in the launch and is reported
   as one.
6. **Form names are lowercase** in the census keys (`d9.m0`, not `D9.m0`);
   the table above is the mapping, not the spelling.

### 16 November 2026, after the runs — classification fixes

What I had seen: all nine runs. These are **classification and
instrumentation** corrections, not rule changes; each is a case where the
code did not implement the rule the method states. Every verdict is
unchanged by all of them. Four came from the Codex review of this PR and are
credited there.

1. **`FRNDINT` was counted as precision-controlled arithmetic.** It rounds
   under *rounding* control and ignores precision control, so it entered the
   PC distribution wrongly and inflated the first published PC=64 figures by
   exactly its executions (Quake 25,400, Screamer 39,902). It now has its own
   `round` class.
2. **The DD register group was all classed `simple`.** DD /2 and /3 are
   `FST` and `FSTP ST(i)` and DD /4 and /5 are `FUCOM` and `FUCOMP ST(i)`.
   This understated Quake's store share by about 1.9 points and dropped
   thousands of compares. The fix is self-checking: with it the mix
   reproduces the 12 November census's load/store/FXCH percentages exactly
   for three titles, which it did not before.
3. **IRQ 13 delivery was not counted at all.** The reliance rule above is
   "ES set **or** IRQ 13 delivered", and the generic event keys are recorded
   only in protected mode, so the rule was not implementable from the output.
   A direct `x87irq13` key now counts it in every mode. **The nine runs in
   this record predate that key**, so their IRQ 13 figures are "not
   recorded", and the conclusion that none fired is an inference from the
   raised-and-unmasked counts, which are `unmasked=00` in every row.
4. **The FRNDINT precision column** in the generated tables was taken from
   the census's `other` class, which holds every load, store, compare and
   control form; it reported 19.6M operations beside an actual FRNDINT count
   of 25,400. It now comes from FRNDINT's own rows.

### 16 November 2026, after the runs — three more, all from review

5. **`x87sig` read the wrong register for `FSTP ST(i)`.** DD /3 is a popping
   store in *register* form and my test for "this is a store" required a
   memory form, so its width came from the register underneath. It runs
   615,685 times in Quake. Fixed in `census.h`; **these runs predate the fix
   and were not rerun**, so the verdict and the width tables use an
   aggregate that excludes `dd.r2` and `dd.r3`
   (`significand_excluding_register_stores`), and the rows carry
   `register_store_widths_reliable: false`.
6. **The stored-value table conflated three different things.** It is now
   four labelled columns — real m64 preserved exactly, real m64 rounded in
   the store, real m80, real m32, integer conversions — because `FIST`/
   `FISTP`/`FBSTP` convert to an integer (a wide source says nothing about a
   floating-point datapath's width), `FST`/`FSTP ST(i)` move no memory
   operand, and an m32 destination rounds to 24 bits whatever the register
   held. Only the first three are offered as evidence, and the second as an
   inference rather than a measurement.
7. **The never-executed list is generated, with mnemonics, over all nine
   runs.** Written by hand it was wrong: it claimed every transcendental but
   FSIN and FCOS went unexecuted when F2XM1, FYL2X, FPTAN, FPATAN, FSINCOS
   and FSCALE all ran. The "N of 162" fraction is gone for a separate reason
   — the denominator counted spellings the census cannot emit.

### 16 November 2026, after the runs — two more, also from review

8. **The never-executed list was filtered by which forms had a mnemonic**,
   so the eight m16 integer arithmetic instructions (`FIADD`, `FIMUL`,
   `FICOM`, `FICOMP`, `FISUB`, `FISUBR`, `FIDIV`, `FIDIVR`) were silently
   missing, and the surrounding text described every omitted entry as a
   register spelling when they were not. The mnemonic inventory is complete
   and the unnamed remainder is now listed rather than characterised.
9. **The provenance flags were sniffed from the keys present**, so a run
   made with the fixed `census.h` in which IRQ 13 never fires was
   indistinguishable from a file written before the key existed — which
   defeats the point of having the flags. `census.h` now emits
   `x87census:version`, and `irq13_recorded` and
   `register_store_widths_reliable` are derived from it. **The nine runs in
   this record are version 1**, so both flags are false for them, correctly
   and for a stated reason rather than by inference.

### 16 November 2026, after the runs — three more, also from review

10. **"Preserved exactly" was an overclaim for real m64 stores.** An `le53`
    significand fits binary64's 53 bits, but the conversion can still
    overflow, underflow or round if the exponent is outside binary64's
    normal range, and **these runs did not record the exponent**. `census.h`
    now records it (`x87exp`); for these nine runs the honest reading is
    *significand fits, exponent unchecked*, and the record says so. This cost
    the census its headline store datum: Screamer's 9,303 m64 stores are no
    longer offered as exact.
11. **Real m80 stores were understated.** The m80 destination *is* the
    register format, so a wide value stored there is exact whatever its
    exponent — yet the prose counted only the `le53` share (Quake 2,543,
    Chris's 3,861) rather than all of it (3,887 and 4,221). Corrected
    upwards; this is the store evidence that stands unconditionally.
12. **IRQ 13 was keyed to vector 75h**, which is only the BIOS's own
    programming: the slave 8259's base is programmable (ICW2) and IRQ 13
    arrives at base + 5. There are now two keys — `x87irq13` for vector 75h
    exactly (sound, complete only under the BIOS's base) and `x87errintr`
    for any external interrupt delivered with a 387 error pending
    (vector-independent, and a **superset**, since an unrelated timer tick
    also lands there once ES is set). Reliance is read from the superset,
    which cannot miss a delivery.

### 16 November 2026, after the runs — amendment 1 reverted

13. **Amendment 1's widening is withdrawn and the original rule restored.**
    Amendment 1 relaxed probe-only from the committed "zero load, store,
    FXCH, arithmetic and transcendental forms" to "no memory operand, no
    FXCH, no transcendental", so that the register-only 287/387 infinity
    test would still count as a probe. Review pointed out that this also
    admitted **unbounded register-form arithmetic**: a title computing
    entirely from constants and registers moves no memory operand, so it
    would have been called probe-only and had its sufficiency verdict
    **suppressed** — a silent miss, and a contradiction of the rule this
    method states.

    The original rule is restored, not patched, and restored in
    **behaviour**: `FRNDINT` was classed as arithmetic when the rule was
    written (`9601ee0da`), so the restored predicate names its later `round`
    class explicitly rather than letting amendment 2's reclassification
    narrow the rule by a side effect. It is the stricter rule and it was
    pre-registered, so this is a return to the committed method rather than a
    new one: **all nine titles classify identically under
    both**, checked before the change. The widening was never needed —
    **no title in the census runs the infinity test at all**, and DOOM, the
    case it was written for, retires 11 ESCs of which none is arithmetic.

    Amendment 1 was itself made before the runs and for a real
    inconsistency between the prose and the script; what it got wrong was
    the direction of the fix. The lesson is that a rule relaxed to admit one
    known case should be checked for what *else* it admits.

### 16 November 2026, after the runs — the exponent key made joint

14. **`x87exp` was a marginal and is replaced by `x87store`.** Amendment 10
    added a per-form exponent-range key so that m64 store exactness could be
    settled by a rerun. Review then showed that it could not: with the
    significand bucket and the exponent range counted *separately*, a form
    that stores a mixture leaves the joint question unanswerable. A
    constructed case makes it plain — one form, 10 stores, 6 `le53` and 4
    `le64`, 6 in range and 4 out, is consistent with anywhere from 2 to 6
    exact stores. The key is now joint (key version 4) and the generated
    table's exactness column reads **not recorded** for these version-1 runs
    rather than a number.

### 16 November 2026 — one promise this record does not keep

The method says each row will say "what the window reached (prompt, menu, or
play)". **It does not.** All nine runs ended on `limit` — they ran out of
simulated time rather than returning to the prompt — and that is reported,
but I did not classify each run's frames into menu or play. The frames exist
in the run directories and were not examined. Recorded as an omission rather
than quietly dropped; it weakens the per-title window description and
nothing else, since no claim here rests on how far a title got beyond the
fact that it launched.

### Predictions registered before the runs

Written down now so that the record states whether they held, rather than
presenting the outcome unprefaced. These are the coordinating session's and
Deano's, not mine, and they are predictions, not rules — nothing in the
classification depends on them:

- **Quake's non-PC=24 arithmetic is library and runtime code, not engine
  code.** Quake brackets its renderer with its own precision-control
  changes, and its PC=64 operations should therefore be in code the engine
  never configured — the C runtime, the startup path, incidental maths. In
  particular the 1,018 PC=64 square roots the earlier census counted should
  be libm `sqrt()` calls, not engine ones.
- **That tail should be diffuse, not localised:** many code pages each with
  small counts, rather than a few pages carrying most of the 553k. This
  comes from the industry having run the experiment: the PS2's
  floating-point hardware was fp32-only, and PC games ported to it hit
  software emulation not in their hot loops — which developers had already
  tuned — but in the **non-hot** code that used defaults, scattered through
  library, startup and incidental paths nobody had thought about. On the PC
  nobody had noticed, because wider-than-fp32 cost nothing visible outside
  the hot areas.
- **If the opposite shows** — a few sites carrying most of the PC=64 tail,
  or PC=64 arithmetic inside the engine — the record says so plainly. A few
  hot sites is the one shape in which a targeted design is conceivable; and
  PC=64 inside the engine would be a *stronger* argument against a reduced
  unit than the coverage one, because it would mean the engine itself needs
  extended precision.

These predictions change no count and no verdict. They are here to be
checked.
