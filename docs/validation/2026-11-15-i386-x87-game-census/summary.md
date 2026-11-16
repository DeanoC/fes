# What the games ask of an FPU, and whether a reduced x87 is defensible

15–16 November 2026. Oracle-only: no RTL, no kit, no fits.

**All nine runs are complete and the negative control passed.** DOOM
retires 11 ESCs, every one of them a control form — `FNINIT` (DB E3) ×5,
`FSETPM` (DB E4) ×4, `FNSTCW m16` ×1, `FNSTSW m16` ×1, which is the classic
287/387 detection sequence — and **zero** real or integer memory
operands, zero FXCH and zero transcendentals, which is the committed
control exactly. It also reproduces the 12 November census's 11. Tracker:
[#1886](https://github.com/DeanoC/fes/issues/1886). Phase 6's plan is
[#1768](https://github.com/DeanoC/fes/issues/1768).

The question Deano asked: DOS games are almost always fp32 at most, so would
a reduced-precision "game" x87 — cheaper and faster than a correct one,
aimed at the actual use case — be worth building?

**The answer is no, and the deciding fact is not the one anyone expected.**

## Result

**No title in this census could be reproduced by a 24-bit-significand
datapath, and the premise behind the question is contradicted by the corpus
itself: of the five titles that compute, only one uses 24-bit precision at
all.**

| Title | arithmetic by precision control | x87 retired |
| --- | --- | ---: |
| Quake timedemo | **24-bit** 11,926,211 / 64-bit 462,063 | 31,997,318 |
| Chris's 3D Benchmark | 64-bit 3,086,420, **none at 24** | 15,236,311 |
| Screamer | 53-bit 424,703 / 64-bit 1, **none at 24** | 1,266,208 |
| Wolfenstein 3D | 64-bit 3,190, **none at 24** | 13,605 |
| Terminal Velocity | 53-bit 1,580 / 64-bit 1, **none at 24** | 6,850 |
| DOOM | none — probe only (the negative control) | 11 |
| Ultima VI, Ultima VII, Serpent Isle | **no x87 at all** | 0 each |

**Four of the five computing titles never execute a single arithmetic
operation at 24-bit precision.** The clearest case needs no interpretation:
**Screamer** runs 424,703 add, subtract, multiply, divide and square-root
operations at PC=53 and one at PC=64, with **none at PC=24** — and Screamer
is a title that does not boot without a 387. A reduced unit would be wrong
for it from its first frame, and no amount of special-casing another title
touches that.

Quake is the only title with PC=24 arithmetic, and it is also the only title
where the "games are fp32" intuition has any support — 97% of its arithmetic
is at PC=24, which it sets deliberately and brackets (below). It still
fails: 462,063 operations at PC=64 remain, and it leaves significands wider
than 24 bits in the register file on 165,520 steps.

**Every computing title leaves wide significands in the registers**, because
precision control bounds add, subtract, multiply, divide and square root and
does **not** bound `FILD`, `FLD m64`, the constant loads, `FRNDINT`,
`FPREM`, `FSCALE` or the transcendentals: Quake **163,792** steps, Screamer
**121,717**, Chris's benchmark **4,648,017**, Wolfenstein 3D
9,942, Terminal Velocity 3,323. These exclude `FST`/`FSTP ST(i)`, whose buckets in these
runs are unreliable — see the note below.

Beyond coverage, two arguments stand independently of any count:

- **Precision control is not fp32.** PC narrows the **significand** and
  leaves the exponent 15-bit extended at every setting. So a
  *24-bit-significand* unit would be exact for Quake's PC=24 arithmetic,
  while an *fp32* unit would not: the exponent range, the overflow and
  underflow thresholds and the denormal handling stay at extended width.
  The premise "games are fp32, so build an fp32 FPU" fails even for the one
  title whose precision matches.
- **There is no fallback to degrade into.** The `i386dx+387` identity tells
  the guest a 387 is present, so there is nothing to trap to. The PS2 had
  fp32-only floating-point hardware *with* software emulation behind it, so
  default-precision code there ran slowly and could be profiled and fixed
  per title. A reduced unit here would not run wide code slowly — it would
  run it **wrong**, silently, wherever it is.

A third argument, that the area saving is smaller than the width suggests,
is **weaker than this record first claimed**; [area.md](area.md) now gives
only the component that is isolated by a measurement.

**Two claims, and the record needs both.** Coverage — which precision each
title selects — shows a reduced unit would be incorrect **by
specification**: told PC=53, a 24-bit-significand unit rounds to 24. The
observed value widths show it would be incorrect **in fact**: Screamer
leaves a significand wider than 24 bits in the register file on **121,717
steps**, and Quake and Chris's benchmark write **3,887** and **4,221** such
values into real `m80`, where the format preserves them bit for bit. A
24-bit-significand unit rounds every arithmetic result to 24 bits and so
would not have produced any of them. Coverage alone would not establish
divergence, because a workload whose every exact result fitted in 24 bits
would be reproduced whatever precision it selected; the value evidence is
what rules that out. See "Which evidence establishes what" below, a note
this record had to rewrite after over-correcting in the other direction.

**And the 53-bit case fails too**, which is worth stating because it is the
obvious retreat from 24. A 53-bit-significand unit would cover Screamer and
Terminal Velocity exactly, but **Chris's benchmark and Wolfenstein 3D run
every arithmetic operation at PC=64**, and Quake has 462,063 there. So the
only width that covers the corpus is the 387's own 64, which is the unit
slices 2 and 3 are already building.

**So there is no defensible reduced design** — not a narrower one at either
width, not a hybrid with a wide fallback, and not a per-title variant. The idea is
closed, and closing it cost a census rather than a slice.

### What the titles actually store, which is the brief's question

`x87sig` is read from the state **before** a popping store, so the value
stored is the one reported. Wide values going to memory, by store width:

| Title | **real m80 (exact)** | real m64, significand fits | real m64, rounded in the store | real m32 (rounds) | integer conversions |
| --- | ---: | ---: | ---: | ---: | ---: |
| Screamer (1995) | 0 | 9,303 | 0 | 10,854 | 0 |
| Quake timedemo (DOSBench) | 3,887 | 1,535 | 5,192 | 11,549 | 3,167 |
| Chris's 3D Benchmark | 4,221 | 0 | 4,304 | 556,971 | 386,900 |
| Wolfenstein 3D (1992) | 0 | 151 | 1,051 | 240 | 1,141 |
| Terminal Velocity (1995) | 0 | 0 | 0 | 3 | 0 |

**What each column establishes**, corrected twice by review and now stated
at the strength the measurement supports:

- **Real `m80` stores are exact unconditionally.** The m80 destination *is*
  the register format — the same 64-bit significand and 15-bit exponent — so
  every wide value stored there is preserved bit for bit whatever its
  exponent. **Quake 3,887, Chris's benchmark 4,221.** These are bytes in
  memory that a 24-bit-significand unit could not have produced, and they
  need no further argument. An earlier revision counted only the `le53`
  share of them (2,543 and 3,861) as exact, which **understated** its own
  evidence; the `le64` share is equally exact here.
- **Real `m64` stores are significand-exact, exponent unverified.** An
  `le53` source's significand fits binary64's 53 bits, but the conversion can
  still overflow, underflow or round if the exponent is outside binary64's
  normal range — and **these runs did not record the exponent**, so
  "preserved exactly" was an overclaim. `census.h` now records it
  (`x87exp`), and a rerun would settle it; for these nine runs the honest
  reading of Screamer's 9,303 and Quake's 1,535 is *the significand fits,
  the exponent is unchecked*.
- **Real `m64` from an `le64` source** is rounded to 53 bits in the store.
  The stored value still exceeds 24 bits except where the rounding leaves
  trailing zeros, so it is a sound inference, not a measurement.
- **Real `m32` settles nothing**: it rounds to 24 bits under rounding
  control whatever the register held.
- **Integer conversions are not a datapath width**: `FIST`, `FISTP` and
  `FBSTP` convert to an integer or BCD, so a wide *source* says nothing
  about the width a floating-point datapath needs. An earlier revision
  reported Chris's benchmark at 68,890 wide m64 stores, of which 64,586
  were `FISTP m64`.

**So the store-side evidence that stands without qualification is the m80
traffic — Quake 3,887 and Chris's 4,221 — and Screamer's contribution to it
is zero.** Screamer's divergence rests instead on its **register-file
widths**, which is the stronger ground anyway and needs no store conversion
at all: a 24-bit-significand unit rounds every arithmetic result to 24 bits,
so a register observed holding a 53- or 64-bit significand is a value such a
unit would not have. Screamer leaves one on **121,717 steps**.

### Exceptions: three titles run with them unmasked

The brief asked whether exceptions are unmasked and whether any title relies
on them, because slice 2's floor is largely the exception path.

| Title | control-word low byte | instructions under it | which are unmasked |
| --- | --- | ---: | --- |
| Chris's 3D Benchmark | `3a` | 15,236,298 | invalid, divide-by-zero |
| Wolfenstein 3D | `32` | 13,598 | invalid, divide-by-zero, overflow |
| Quake | `3a` | 139 | invalid, divide-by-zero |

**No run raised an unmasked exception and no run set ES.** Every exception
row in all nine runs reports `unmasked=00`: flags were raised and every one
of them was masked. IRQ 13 therefore cannot have fired, since the AT's latch
is driven by an unmasked exception — that is an **inference** from the
raised-and-unmasked counts, sound but not a direct observation. A direct
`x87irq13` key now exists and counts the delivery in every mode, because the
committed rule is "ES set **or** IRQ 13 delivered" and a title that restores
state with ES already set would take the interrupt without a fresh ES
transition; these nine runs predate that key, so their IRQ 13 columns read 0
as "not recorded" rather than as "observed not to happen". So none of these
titles *relies* on the exception path in the sense of taking IRQ 13. But the path
is live architectural state for real titles, not only for Linux's boot
check: an invalid operand or a zero divisor in Chris's benchmark or
Wolfenstein 3D would take IRQ 13 and must. A reduced unit would carry the
whole of that path — six flags, their masks, the summary bit, ES, the
latch, F0h and F1h — unchanged, which is where slice 2's floor largely
lives.

### Wolfenstein 3D uses the FPU, which the earlier census never tested

Wolfenstein 3D is a first-acceptance game and a fixed-point engine, and the
12 November census never ran it. With a 387 fitted it retires **13,605 ESCs,
3,190 of them arithmetic, all at PC=64**, moves 7,662 real and integer
memory operands, and leaves significands above 24 bits on 9,942 steps — with
invalid, divide-by-zero and overflow unmasked throughout.

**And the census explains how it also runs without one.** Wolfenstein 3D
executes **201 emulator fixup interrupts** in its window — INT 3Eh ×152, and
INT 34h, 35h, 37h–3Bh and 3Dh for the rest — which is **Borland's
floating-point emulator fixup protocol**. So its floating point goes through
an emulator library: with no 387 the library emulates in software, and with
one fitted each fixup runs once and is patched to its ESC. That is why
[#1533](https://github.com/DeanoC/fes/pull/1533) plays it on an empty socket,
and it needed no second run to establish.

**And the fixups sharpen the no-fallback argument rather than weakening
it**, which is worth saying because the finding reads the other way at first
glance. An emulator fixup *is* a fallback — but only for a 387 that is
**absent**. The game has exactly two paths: "no 387 present", which runs the
emulator in software, and "387 present", which it then trusts. There is no
path for "387 present but imprecise". A reduced unit would be selected onto
the second path by the identity bit and would be unable to honour it, and
the fixups it had already patched away are no longer there to catch it. So
Wolfenstein 3D is not the one title that would tolerate a narrow unit; it is
a title with a working fallback that a narrow unit would route *around*.

It also corrects the 12 November record, which concluded that "no run
executed an INT 34h–3Eh emulator fixup, so none of these titles was built
with Borland's or Microsoft's fixup emulation". That conclusion was **sound
for the titles that run had, and silent about the one it never ran** — which
is the distinction worth keeping, rather than calling the earlier census
wrong. The same shape as the top-N forms above: a census answers for its
own corpus and its own cut, and both have to be read before a design
decision leans on it.

**What this still does not establish:** that Wolfenstein 3D *needs*
extended precision. Its arithmetic is at PC=64 because that is Borland's
emulator library's default, not necessarily because the game's own
computation requires it. What the measurement does say is what a reduced
unit would have to reproduce when the identity bit is set — which is the
question for this census, since the same bit selects the unit.

### A measurement error the review caught, and what it costs

`x87sig` reads the value a popping store wrote from the state **before** the
step, because after the step ST(0) is the register underneath. I applied
that only to the **memory** store forms. **`DD /3` is `FSTP ST(i)` — a
popping store in register form** — so its width bucket was read after the
pop, from the wrong register. It executes 615,685 times in Quake and is
large in Chris's benchmark, so those forms' per-form buckets in these nine
runs are **wrong and are not reported**.

What it costs, exactly: the wide-significand totals above are given with
`dd.r2` and `dd.r3` excluded, which removes 1,728 of Quake's 165,520,
9,774 of Screamer's 131,491 and 84,440 of Chris's 4,732,457. The remaining
counts are unaffected and the conclusion does not move. The fix is in
`census.h` and a rerun would recover those forms; that rerun has **not**
been made, so this record simply does not use them.

The same finding showed that those two forms never belonged in the
stored-to-memory table at all: `FST`/`FSTP ST(i)` move no memory operand.
Removing them is correct on its own terms as well as a consequence of the
bug, and the table's m64 column is now memory stores only.

### A classification error the review caught

The first generated mix classed the whole DD register group as `simple`.
**DD /2 and /3 are `FST` and `FSTP ST(i)` and DD /4 and /5 are `FUCOM` and
`FUCOMP ST(i)`**, so Quake's store share was understated by about 1.9
percentage points and thousands of compares were dropped. Corrected, with a
test, and the correction is self-checking: the mix now reproduces the 12
November census's load/store/FXCH percentages **exactly** for Chris's
benchmark (27.90% / 22.66% / 0.61%), Screamer (26.14% / 12.94% / 7.66%) and
Terminal Velocity (15.74% / 15.58% / 7.52%), which it did not before.

Two further review findings were real and are fixed: the FRNDINT precision
column was taken from the census's `other` class, which holds every load,
store, compare and control form, and so reported 19.6M operations beside an
actual FRNDINT count of 25,400; and [area.md](area.md) added 1,414 and 2,250
to 3,850 where they come to 3,664.

### What four exact reproductions buy

Four of the nine runs reproduce the 12 November census's retired-instruction
totals **exactly**, on a moved tree with reimplemented census code:
Chris's benchmark 15,236,311, Screamer 1,266,208, Terminal Velocity 6,850
and DOOM 11. Chris's benchmark also reproduces its 15,236,298 unmasked
instructions exactly.

That is a strong check on both censuses and on this instrumentation, and it
is why the one title that does **not** reproduce is worth stating plainly
rather than explaining away: see "Calibration and reproduction" below.

### A headline this record withdraws

**Retracted, 16 November 2026, in place.** An earlier revision of this
record (commit `ac52d574d`) led with Quake's **47,161 `FLD m80` and 46,847
`FSTP m80`** and said that Quake "moves 80-bit extended values through
memory", so that a reduced significand could not round-trip data already
extended in memory. **That claim was wrong**, and the measurement that
refutes it is one this census had already collected and I had not looked at:
the significand widths of those very operations.

| Quake's m80 traffic | `FLD m80` | `FSTP m80` |
| --- | ---: | ---: |
| significand `le24` | 38,047 (80.7%) | 37,795 (80.7%) |
| significand `le53` | 2,516 | 2,543 |
| significand `le64` | 1,436 | 1,344 |
| zero significand | 5,162 | 5,165 |

**Four fifths of them carry values a 24-bit significand holds exactly.**
That is the measurement. **That they are compiler register spills is an
inference** — it came to me from the coordinating session and I did not check
it against Quake's build or a disassembly — resting on the near-equal load
and store counts (47,161 against 46,847) and that width distribution.
Whatever the mechanism, the measured fact is enough: four fifths of this
traffic holds values of 24 bits or fewer, so the counts do not show a title
relying on extended precision. A narrow unit would round-trip its *own*
spills exactly, because it would spill its own narrower values. So the m80
traffic is **not** an independent reason, and where it does carry wide values
(3,952 loads) it is the wide-significand reason restated.

The committed rule is unchanged: the method says any `FLD m80` or `FSTP m80`
makes a title not sufficient, and the script still reports it among the
verdict's reasons. **A rule is not rewritten because a result made it look
overstated.** What changes is the prose: this reason is weak and is no
longer led with, and the conclusion rests on coverage, the exponent range
and the missing fallback — each of which was already sufficient.

### The titles write the control word themselves

The brief asked whether a title ever writes the control word, and to report
the PC in force when arithmetic executes rather than the value at `FNINIT`.
Measured directly, from control-word writes rather than inferred from
values:

- **Quake writes it 353,466 times in 25 seconds**, 353,462 of them by
  `FLDCW`, and the dominant pair is `033f>0c3f` **25,735 times** with
  `0c3f>033f` **25,734 times** — the title switching **to** PC=24 and back
  **to** PC=64, about 1,030 round trips a simulated second. So Quake's PC=24 arithmetic is deliberate and bracketed,
  and its PC=64 arithmetic runs outside those brackets. It also makes
  rounding-mode changes (`033f>073f` and `033f>0b3f`, 10,644 each).
- **Screamer** writes it heavily too, consistent with the earlier census's
  61,006 `FLDCW`s, and switches rounding around its float-to-integer
  conversions.

This is what "the PC in force when arithmetic executes" means in practice:
neither title simply runs at whatever `FNINIT` left.

### Which evidence establishes what — a note this record had to rewrite

**Rewritten 16 November 2026 after review.** An earlier revision of this
note said the conclusion was "carried by coverage alone" and that the
stored- and register-value evidence was "corroboration that can be removed
without touching the conclusion". **That was wrong, and wrong in the
direction that matters**: it discarded the only evidence that establishes
divergence. The two kinds of evidence prove different things and the record
needs both.

**Coverage — which precision the title selects — establishes
non-conformance, not divergence.** That Screamer runs 424,703 arithmetic
operations at PC=53 shows a 24-bit-significand unit could not *implement*
what the title asks for: told PC=53, it would round to 24. That is a
specification violation on its face. But it does **not**, by itself, show
any result would differ, because a workload whose every exact result happens
to fit in 24 bits would be reproduced bit-for-bit by a 24-bit unit whatever
precision it selected. And Screamer's needing a 387 to boot proves an FPU is
required, not that its selected width is — the same distinction this record
draws for Wolfenstein 3D above.

**The value evidence — significand widths actually observed — is what
establishes divergence**, and it is therefore load-bearing, not decoration:

- **Every computing title leaves values wider than 24 bits in the register
  file**, where later arithmetic reads them: Screamer on 121,717 steps,
  Quake 163,792, Chris's benchmark 4,648,017, Wolfenstein 3D 9,942,
  Terminal Velocity 3,323. A 24-bit unit rounds every arithmetic result to
  24 bits, so these are values it would not have. This needs no reasoning
  about store conversions and is the firmest datum in the census.
- **Quake and Chris's benchmark store 3,887 and 4,221 wide values as real
  `m80`**, which preserves the register format bit for bit — so those bytes
  in memory could not have been written by a narrower unit.
- The real `m64` stores (Screamer 9,303, Quake 1,535) are
  **significand-exact with the exponent unverified**: see the store table's
  note. They are consistent with divergence and do not establish it on
  their own.

So the honest structure is: **coverage shows a reduced unit would be
incorrect by specification; the stored-value widths show it would be
incorrect in fact.** Neither alone closes the question and this record
offers both.

**What review did remove, and it was right to.** Three columns were
withdrawn because they did not measure what I claimed — the m80 traffic
(80.7% register spills), the register-form stores (`FST`/`FSTP ST(i)` move no
memory operand), and the integer conversions (`FIST`/`FISTP` convert to an
integer, so a wide source says nothing about a floating-point datapath's
width, and they were 64,586 of the 68,890 I reported for Chris's benchmark).
Removing those was correct. **Then declaring the remaining value evidence
removable was an over-correction**, and this note is the retraction of it.

The transferable lesson survives in a sharper form: say what each piece of
evidence establishes, rather than ranking evidence as carrying or
accompanying. Three of my columns measured nothing I claimed; the rest
establishes something *different* from coverage, which is not the same as
establishing less.

### A correction to this census's own classification

**Dated 16 November 2026**, after the first two runs and before the rest.
`FRNDINT` was classed as arithmetic and so entered the precision-control
distribution. It should not: `FRNDINT` rounds under **rounding** control and
**ignores** precision control (387 PRM; the plan record's "Accuracy"
section). Counting it inflated the PC=64 arithmetic figures this record
first published — Quake's to 483,513 from 462,063, and Screamer's to 30,503
from 1, the differences being exactly its 25,400 and 39,902 executions.

`FRNDINT` now has its own class, `round`: counted in the mix, absent from the
precision distribution, with a test. **Neither verdict changes** — both
titles were, and are, not sufficient. The figures in this record are the
corrected ones.

### What a full form table bought, and what a top-N census costs

The 12 November census published its **top twelve forms by frequency**.
Quake's `FLD m80` and `FSTP m80`, at about 47,000 each out of 32 million x87
instructions, are 0.15% each and fell below that cut.

The lesson is not the one the withdrawn headline suggested. What the full
histogram and the per-form significand buckets actually bought was the
ability to **test** the m80 claim and retract it. A top-N census would have
hidden the forms; a census with the forms but without their value widths
would have let a wrong and confident conclusion stand. **Reporting every
form that executed, and the width of the values each one moved, is what made
the error findable** — by me, before it reached a design decision, rather
than by a reviewer afterwards.

### A prediction that failed, and why the conclusion does not rest on it

Before the runs, the coordinating session and Deano registered a prediction
from the PS2's precedent ([method](method.md), "Predictions registered
before the runs"): that Quake's non-PC=24 arithmetic would be **diffuse** —
many library, startup and incidental code pages each with small counts,
because that is the shape the PS2 experience had.

**It failed.** Quake's PC=64 arithmetic is **concentrated**: 462,063
operations across 28 code pages, with **78.2% of them on one page**.

The irony is instructive, and is the reason the record states the failure
rather than softening it. Concentrated is normally the *better* news for a
hybrid — 28 sites, with one carrying four fifths of the traffic, is a set one
could imagine special-casing, where a diffuse tail would be hopeless. **So
the prediction failing made a hybrid look more feasible than predicted, and
the conclusion still does not change**, because what rules a hybrid out is
Screamer: a title with no PC=24 arithmetic at all, which no amount of
special-casing one hot page in a different title addresses. The conclusion
does not depend on the prediction in either direction — and it does not
depend on the m80 reading this record withdrew above.

And it should be said plainly that the PS2 precedent is still a sound
argument about *fallbacks*; it was only the distribution it predicted that
did not hold here. One failed prediction about one title's code layout is
not a reason to doubt the precedent that a reduced unit with nothing to trap
to is worse off than one with software emulation behind it.

### What this census does not establish

- **Whether Quake's PC=64 code is its engine or its C runtime.** Every
  arithmetic site in the run, at both precisions, sits in the one flat
  selector `00a7`, with the PC=24 pages (`10010`–`10036`) and the PC=64 page
  (`1001a`) interleaved in a single code region. DJGPP links the engine and
  its libraries into one flat segment, so CS cannot separate them and the
  page numbers do not either. **Not established**, and a plausible
  attribution is not offered. The brackets themselves **are**
  measured — the 25,735 matched `033f>0c3f` / `0c3f>033f` pairs above — so
  what is open is only the attribution of the code *between and outside*
  them to the engine or to a library, which needs Quake's own symbols.
- **What a 24-bit-significand unit would actually save.** That needs a fit
  of such a unit, and no fits are run here. [area.md](area.md) gives the
  measured floor and an upper bound instead.
- **What any title does beyond its launch window.** Each run is 20–30
  simulated seconds of launch, menus and, where it got there, play. A title
  that uses the 387 only later is undercounted, as it was in the earlier
  census.

## Secondary finding: what the mix tells slice 6

### Square-root early termination buys almost nothing; divide's is free

Both figures were asked for by the slice 4 session while this census ran and
are in its record. The full corpus makes the square-root case much stronger
than the two-title reading did:

| Title | FSQRT | by precision control |
| --- | ---: | --- |
| Chris's 3D Benchmark | 67,292 | **all 67,292 at PC=64** |
| Quake timedemo | 758 | 594 at PC=64, 164 at PC=24 |
| Screamer | 575 | all at PC=53 |
| Terminal Velocity | 1 | at PC=53 |
| every other title | 0 | — |

**Of 68,626 square roots in the whole census, 164 run at PC=24 — 0.24%.**
Early termination there would save 12 clocks on a quarter of one percent of
square roots, and cost 37 bits of remainder width on every one. The census
confirms slice 4's decline, and it is no longer a close call.

**Divide is the opposite.** Quake's divides are 584,405 at PC=24 against
26,049 at PC=64, and Screamer's 5,839 are at PC=53, which also terminates
early. The free 8-clock saving lands on the overwhelming majority of the
hottest non-trivial FP operation in the corpus.

### Which 387 operations no title executed

**Corrected 16 November 2026: this list was typed and was wrong.** An
earlier revision said "every transcendental except FSIN and FCOS" never ran,
and that FSCALE never ran. Checked against all nine runs, **F2XM1, FYL2X,
FPTAN, FPATAN, FSINCOS and FSCALE all executed** — the claim was false, and
it was false because I wrote the list by hand instead of generating it. The
earlier "71 of 162 forms" fraction was withdrawn for a different reason (a
meaningless denominator) and this replaced it with something worse: a
confident list that contradicted the census's own rows.

The list is now **generated over all nine runs, with mnemonics**, in
[tables.md](tables.md), which is the authoritative copy. A second review
round then caught that the generator was filtering on which forms happened
to have a mnemonic in its table, so the **eight m16 integer arithmetic
instructions `FIADD`, `FIMUL`, `FICOM`, `FICOMP`, `FISUB`, `FISUBR`,
`FIDIV` and `FIDIVR` were silently missing** from it, and the surrounding
text described all the omitted entries as register spellings when they were
not. The inventory is complete now.

A third round then found `FNOP` (`d9d0`) missing from the inventory the same
way. **The inventory is now checked mechanically rather than maintained by
inspection**: a test enumerates every form `census.h` can emit, from the
header's own naming rule, and requires each to be either a named instruction
or an explicitly listed reserved encoding. That check found 67 further gaps
at once — all of them reserved ModRM values inside the ESC groups, now
classified as such. Adding a real instruction means taking it *out* of the
reserved list, which is what gives the check teeth. It is the check that
should have existed before the first version of this list.

The 387 instructions no title executed: `FXTRACT`, `FPREM`, `FPREM1`,
`FYL2XP1`, `FDECSTP`, `FINCSTP`, `FNOP`, `FNCLEX`, `FRSTOR`, `FNSAVE`,
`FFREE`, `FST ST(i)`, the eight m16 integer arithmetic forms above,
`FIST m16`, `FISTP m16`, `FBLD`, `FBSTP` and three constant loads
(`FLDL2T`, `FLDPI`, `FLDLG2`). [tables.md](tables.md) is the generated and
authoritative copy. **No title executed a reserved encoding**, which the
same classification now reports rather than leaving as an unknown form.

For slice 6: those are what the census titles never reach, so optimising
them buys nothing measurable here. They must still be *correct* — a 387
missing a form is not a 387 — but they need not be fast. The caveat is the
window's: a title that reaches one only in later play is not represented.

### What is hot

FXCH is **20.09% of Quake's x87 instructions**, which supports the plan's
instinct to make it a swap in a table of physical register numbers rather
than a data move. Loads and stores together are 31–50% of every computing
title. Transcendentals are 0.04% of Quake's and 1.11% of Screamer's, so the
microcoded slow path is the right home for them.

## The tables

Per-title: [tables.md](tables.md) — the opcode mix, precision and rounding
control with the verdict, the operand precisions, exceptions, FSQRT by
precision, and the calibration. The rows are
[census.jsonl](census.jsonl).

The verdict column is the summary script's `pc24_sufficient`, with each
failing reason and its count, so the claim is the script's output and not a
reading of the other columns. A title that only probes gets no verdict,
because sufficiency is a question about computation.

## Calibration and reproduction

**All nine runs calibrate exactly**: Σ`x87op` equals the run's D8–DF stem
total in every one, with no faulted or held ESC steps outside that identity
except Quake's 443 faults. **No run has an unknown form**: every form that
executed is named by the classification, so no count is silently dropped.
All nine ended on `limit`. [tables.md](tables.md) carries both tables.

**Screamer reproduces the 12 November run exactly** at 1,266,208 retired
x87 instructions, which is a useful check on both runs given the tree has
moved and the census code is a reimplementation.

**Quake does not, and the reason is _not_ established.** This run retires
31,997,318 against the earlier 45,787,636. The two runs used different
launches — the earlier census ran Quake from a `bench-q` scratch copy with
its own `Q.BAT`, this one the store's `quakes` title on the pinned `bench`
drive — and that is the obvious candidate, but it is an **inference**: I did
not compare the two runs' drives or step totals to confirm it, and a
different launch is not the only thing that could move the figure. The
method says such a difference is reported rather than reconciled away, so it
is reported as unexplained. Nothing in the conclusion depends on the total:
the PC distribution, the stored-value widths and the verdict are properties
of the run that was made.

## A tool fix the census needed

`machine_tb` refused Quake's launch outright — `quake +timedemo demo1
+exec mode0.cfg …` — with "cannot type that", because its typist had no
`+`. That is why the 12 November census ran Quake from a scratch copy with a
`Q.BAT` wrapper: the workaround was hiding a missing feature rather than
working around a drive problem. The typist now has the shifted characters a
DOS command line needs. `^` stays Ctrl and `~` stays Esc, so shift-6 and
shift-grave are still unreachable.

## Method

[method.md](method.md), committed in `5b03a4c41` **before** any run, amended
in `a9227d9be` still before the first run, and amended again after the runs
with the classification fixes and one omission. Every amendment is dated and
states what had been seen when it was made. The runs themselves were made on
`b76aab67f`. It discloses
that the 12 November record's per-title precision table had been read before
any rule was written, so the precision result is **not** a blind
measurement; what is new here is this tree's re-measurement, the operand-
and significand-width columns, the three first-acceptance titles the earlier
census never ran, and the call-site attribution.
