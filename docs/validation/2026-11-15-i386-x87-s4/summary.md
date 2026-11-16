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

**The remainders then need no Goldschmidt at all** — which is more than the
"no divider of their own" this record first claimed, and is what the landed
RTL does. FPREM's quotient is never used beyond its low three bits, because
they *are* C0, C3 and C1, so there is nothing to estimate: `A_OP` reduces the
dividend once and `P_SH` takes `n` doublings with a conditional subtract, the
quotient's bits falling out of the same steps. No seed, no iterations, no
candidate, no error bound, and no use of the divide's tail.

The first form did share that tail at a variable scale, and it worked — 740
cases — but it needed a general shift by `65 + s - n` on a path option A says
has no barrel shifter, and because the shifter was **shared**, the divide's
and the square root's own three-constant drops could not be muxed while it
existed. One unnecessary generalisation blocking two cheap specialisations
elsewhere. [The restructure](restructure.md) has the measurements.

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

All of these ran on the frozen head `9c6a7cf2d` — **after** the last RTL
change (`7358e369d`), with everything committed since being documentation,
models and the testbench. That revision is the seal's own recorded one and
the one the kit log names.

| gate | result |
|---|---|
| unit bench, RTL, tail assertions armed | **740 passed, 0 failed** |
| unit bench, locked-Yosys netlist | **740 passed, 0 failed** |
| **QEMU's fprem test**, ported as its operand grid | **23,328 pairs an instruction, 0 failed** |
| directed partial-step chains | **32 chains pass** — both instructions, exponent differences −5 to 999, odd divisors at the tie, full status mask, each step's result fed back |
| exception-mask matrix, divide/sqrt/remainder rows | **passes under all seven mask settings** |
| BUSY# latch trials for ZE, IE and PE from real divides and roots | **pass** |
| lockstep, identity **off**, RTL and netlist | **1,933 no-387 cases pass, 0 failed**, and every other bucket passes |
| lockstep, identity **on**, with slice 4's own programs, RTL and netlist | **21 pass, 0 failed** |
| test386 | **POST FFh reached**, halt at d0:ffb9 after 79,641,629 steps, no EE differences |
| **Quartus 17.0.2 seal** | **passes every analysis** — package `fb0def74`, HIP slot, 36,463 ALMs (87.0%), setup +0.753 ns, total negative slack 0.0 throughout |
| **kit run on that seal** | **Kit B `s4a` passed**, verified from `result.json` |
| standalone area, arithmetic alone, against the merge base in the same flow | **+2,847 ALMs, +0 DSP, +2 M10K** ([the readings](restructure.md)) |
| in-machine fit, identity **off** — what ships and seals | **36,416 ALMs, +0.459 ns: fits and closes** (HIP slot) |
| in-machine fit, identity **on** | **does not fit** — 4,407 of 4,191 LABs as first written, **4,292 in option A's form** ([escalation](STOP-RULE-FIRED.md)) |
| identity-on nine-seed sweep | **nothing to sweep**: the fitter stopped before placement, so there is no ALM figure, no timing and no seeds |
| TestFloat div, sqrt, rem | **attempted, unmet**: a scope of its own, see below |
| per-entity (`--hierarchy`) fit | **run** — both seed tables at **0 ALMs**, verified from the fitter; it cannot apportion within the unit ([why](restructure.md)) |

**This table was itself an instance of the failure it records.** An earlier
version kept the original "not run" rows for the seal, the kit run and QEMU's
grid beside the corrected rows that said they had passed, and the superseded
+3,997 ALM figure beside the +2,847 that replaced it — so the record
contradicted itself on whether its own named gates had completed. Codex found
it. It is the #1855 pattern in a sixth place, and the fix is the same one this
record keeps prescribing: **one table, derived once, with no row that another
row answers.**


### The identity-on lockstep failed first, and the failure was real

Slice 4's seven identity-on programs made the lockstep **fail on their first
run**: 19 of 21, with `div-mem` and `div-prec` failing. **They pass now** —
21 of 21 — and the cause was not the divider.

What the run reports:

```
x87 cases (the identity on): 19 pass, 2 fail
FAIL x87:div-mem  @104a.write.7360 fc 0 ... div-mem.halt 1 0
                  div-mem.x87.status 0 1  div-mem.x87.r7 0 1
FAIL x87:div-prec @1044.eax 3801 3822 ... div-prec.halt 1 0
                  div-prec.x87.status 0 1 div-prec.x87.tag 0 1
```

Read as *expected, got*: `div-prec` ends with the oracle's status word 3801h
(**IE**) against the RTL's 3822h (**PE and C1**), and neither case reaches its
HLT on the RTL.

**It was the memory access shape, and the failure pattern said so**: both
failing programs used memory divides and the five others passed.
`fes_i386_decode`'s case on `{esc, reg field}` listed fields 0 to 5 for each
of D8, DA, DC and DE, so the divides' fields 6 and 7 fell through to the
default and the instruction read the wrong bytes.

**So a memory form has to be in three tables and only two of them have a
guard.** `x87_decode` says whether the form is built; `x87_format` gives its
operand format and turns it back into `X87_UNIMPL` when it does not; and the
decode's shape case gives its memory access — where a missing field means the
form **runs and reads the wrong operand** rather than stopping as not built.
Codex found the second table from the diff; the lockstep found the third.
Neither is visible from the unit's own bench, which supplies both the format
and the operand itself.

Both runs are kept beside this record — `lockstep-identity-on-failure-before-the-fix.txt`
and `lockstep-identity-on-pass.txt` — because the pair is what says the
programs were worth writing. — `div-reg`, `div-mem`,
`div-zero`, `sqrt`, `prem`, `prem1` and `div-prec` — so the forms run through
the CPU rather than through the unit's own interface, and the two remainder
programs loop on C2 the way software does, which compares every partial step
against Bochs. **That is what the unit bench cannot reach, and it is not a
formality: `div-mem` is the case for the P1 below.**

### Two tables, not one

`x87_decode` is not the only table a form has to be in. **`fes_i386_decode`
takes the operand format from `x87_format` first and turns the operation back
into `X87_UNIMPL` where it is `X87F_NONE`** — so the memory divides were
accepted by one table, missing from the other, and would have raised
`FAULT_UNIMPLEMENTED` on the real CPU. The unit's bench cannot see it,
because it supplies the format itself rather than going through the decoder.
Codex found it on PR #1889; `div-mem` covers it now.

### What QEMU's test found

QEMU's own header says to run its fprem test on real hardware and diff
against the implementation, which is what this bench already does, so it is
ported as its **operand grid** rather than run as a program: QEMU's own
fields, 432 values an operand, compared on the whole architectural state
rather than on printed fields.

It found a real bug on its fourth case family. **A divisor of infinity gives
the dividend back and never rounds, and that cannot be left to the exponent
difference being "very negative": an infinity's exponent field is 7FFFh, the
largest, so a dividend at 7FFEh is exactly one below it** and took FPREM1's
round-up path at a difference of −1. Four cases of 23,328, and only a
dividend within one binade of the top separates the two readings. An earlier
commit had removed the explicit infinite-divisor case on exactly that
reasoning.

## TestFloat: attempted, and it is a scope of its own

Slice 3 reported this gate unmet because its session's permission classifier
refused the build. **Nothing refused it here, and it still does not pass.**

Berkeley SoftFloat (`07ebd29db0cfb55723a79fbbffe99103ef38e305`) and TestFloat
(`a9c849f1b0eb0264b626d9686ffae167d996e3be`) clone cleanly, and the `8086`
specialisation is the right one for an x87 rather than the `8086-SSE`
default. But **that specialisation has no bf16 support at the pinned
revision** — not two missing files, the header's declarations too — so
SoftFloat builds only with the whole bf16 family excluded from the archive.
`testfloat_ver` then links; **`testfloat_gen` does not**, because it links
bf16 case generators.

So the plan's gate needs a newer SoftFloat revision, or bf16 stubs, **and** a
harness to drive generated `extF80_div`, `extF80_sqrt` and `extF80_rem`
vectors through the unit's CPU interface and compare. That is a piece of work
of its own, not a build step.

**The host bench is reported as what it is: the same 80-bit oracle, driving
the real CPU interface, over the PRM's operand classes rather than
TestFloat's systematic grid. The difference worth naming is coverage, not
reference.** For FPREM the host is arguably the *better* reference, because
TestFloat's `rem` covers only FPREM1's IEEE remainder while the host executes
FPREM itself, partial semantics and all.

## What is left

**The seal and the kit run are done** — they were listed here as blocked
while the identity-on escalation was open, and the coordinating session's
judgement settled it: the identity-off package is what ships and seals, so
holding the whole slice on a configuration that does not ship would have been
the wrong call. Both are in the results table.

- **[The stop rule's escalation is resolved](STOP-RULE-FIRED.md#how-this-escalation-was-resolved-16-november).**
  Deano decided on 16 November that the **DE25 is the 387's home**, the DE10
  387 is not pursued further and **DE10 slice 5 is not being started** — so
  this slice is the end of the DE10 x87 line and its **verified reference
  implementation**, not a blocked step. The identity-on machine does not fit
  the device in either form (4,407 LABs as first written, 4,292 in option A's,
  against 4,191), and that measurement is what made the DE25 the live question
  rather than an experiment.
- **The identity-on seed sweep**, which has nothing to sweep: the fitter
  stopped before placement, so there is no ALM figure, no timing and no seeds.
- **A TestFloat harness**, which the section above explains is a scope of its
  own. QEMU's fprem test is **not** outstanding: it is ported as its operand
  grid and passes, which the results table records.
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

## The seal

| | figure |
|---|---|
| package | `fb0def74e5bf9b5ac1e87e29a8ce14ffcc267edc32cf752dd65c894cd82b152c` |
| build_id | `1ae2d16ccde57815edc1664646b1497b` |
| toolchain slot | **HIP**, seed 1, Quartus 17.0.2 |
| resources | **36,463 ALMs of 41,910 — 87.0%**, 40 DSP, 282 M10K, 31,866 registers |
| timing | **pass**, total negative slack **0.0 on every analysis** |
| setup / hold | **+0.753 ns** / +0.084 ns |
| minimum pulse width / recovery / removal | +1.122 / +7.323 / +0.267 ns |
| clocks | system 61.44 MHz, pixel 74.27, audio 12.29 |
| source revision | `9c6a7cf2d85178eebd925f32846771e230741b44` |

**The seal covers `9c6a7cf2d`, and the kit log names the same revision** —
checked against the sealer's own `build-inputs.json` rather than assumed, so
the permanent kit record and the package agree on what was built. Commits
after that revision on this branch are documentation only; none of them is
RTL, so none of them is outside the seal's coverage in substance, but the
seal's claim is about `9c6a7cf2d` and that is the revision to cite.

Nothing was relaxed and no analysis was waived: the sealer's own report is the
source of every figure above.

### The slot is on both numbers, because the comparison does not exist

Slice 3's identity-off seal was **87.0% and +0.896 ns setup through the CUDA
slot**; this one is **87.0% and +0.753 ns through the HIP slot**. Those two
figures sit next to each other looking comparable and **they are not**: a
within-slot comparison of the two identity-off seals **does not exist**, and
making one would need one of them re-fitting in the other slot.

The citation for why is in this slice's own measurements: the same RTL fits at
**36,055 ALMs and +0.594 ns through one slot and 36,069 and +0.410 through
the other**, because the two slots' Yosys binaries differ and Quartus fits
their netlists differently. That spread is larger than the 0.143 ns someone
would otherwise read off the two seals as slice 4's cost.

**The slot choice itself needs no defence** — the pc386 sealer takes
`--gpu-router` only to select which authenticated toolchain slot it uses and
routes nothing on a GPU, and the project treats HIP and CUDA route and timing
evidence as interchangeable. Only the labelling does.

## The kit run, and what it does not show

Kit B, run **`s4a`**, 16 November 02:24–02:27 EET, on the sealed package
`fb0def74`. **Core-only on the running baseline**: no install, no image
change, no reboot, no host change. Claimed in `kitB-STATUS.md` with owner, PR
and start time before anything was touched, and released there afterwards.

**Verified from `runs/s4a/result.json`, not from the driver's exit code** —
a driver reports `rc=0` while the kit refuses a package at admission, which is
how a non-canonical `.fcore` once read as a pass:

| check | result |
|---|---|
| events with an `error` | **0 of 35** |
| the load's state | **`active`** |
| package staged | `fb0def74…fcore` — the seal, by digest |
| disk staged | requested `a6977555…`, reply `a6977555…` — matching |
| active interfaces | display plane, HID keyboard, ATA disk, 48 kHz audio |
| HDMI frames captured | 8 |

And the restore, from the kit log rather than from the wrapper's say-so:
software `c9ae79e2` active with nothing pending; the post snapshot has the
same image, previous, pending, idle, launcher, images, **quirk** and `boot_id`
keys as the pre-run probe, so no reboot and Kit B's
`usb-storage.quirks=152d:0583:i` line is intact; agent idle and lease free;
and the FogCast data trees identical to before, with the run's 3 new trees
removed and none of the earlier ones gone.

### What it does not show

**The `i386dx+387` identity is off in this package**, as it is in every
package until slice 7, so **this run exercises none of slice 4's arithmetic.**
It is a no-regression check of the machine around the unit — that the slice's
decode, format and shape table changes and its extra logic did not break a
machine that boots DOS and runs a game. The arithmetic's evidence is the unit
bench, the netlist bench and the lockstep; the kit run is not it, and a reader
should not take a green kit row as coverage of the divide.

That is the same thing slice 3's record said of its own kit run, and it stays
true for the same reason — with one addition this slice measured: the
identity-on package **cannot be built at all**, so there is no version of this
run that could exercise the arithmetic on hardware today.

## The seal's order, and why it is the order

The RTL changed substantially **after** the lockstep and test386 last passed
— FPREM's doubling loop and both shift restructures — so a seal reported
against those runs would not cover its own head. That is the trap slice 2
fell into three times, and it cost it three full re-freezes.

**The tool enforces it rather than the discipline having to**:
`build_fes_pc386_quartus.py` takes `--lockstep-report` as a required input, so
a seal cannot be produced without pointing at a lockstep run. It is worth
naming because a required input is a far better guard than a rule someone has
to remember at the end of a long day.

**It refused this slice three times and was right every time**: the wrong
*kind* of lockstep evidence, a package store it will not relocate, and a
source closure that moved mid-build. **The last one is the least obvious: a
seal needs the worktree quiet for its whole duration, including commits that
touch no RTL**, because the closure is hashed rather than the RTL subset — a
`docs/validation/` commit invalidated the first attempt after its compilation
had already succeeded with 0 errors. Committing findings as they are found is
the right habit everywhere except across a seal.

**And it caught this slice doing exactly that.** Re-running the gates because
the RTL had moved was discipline; bringing the *wrong kind* of evidence was
not caught by discipline at all. The first seal attempt was refused with
"lockstep report is not a `--netlist` run" — the sealer requires the lockstep
against the Yosys netlist, not the RTL one, and produced nothing rather than a
package backed by weaker evidence than it claims. Two things worth knowing for
the next slice: that prerequisite is not optional, and `--package-output`
cannot be pointed anywhere but `build/packages`.

## A note on the kit status files, which cost two readings

The two status files use **different timezone conventions**: Kit A's entries
carry `+00:00` and Kit B's carry `+0200`. A Kit B timestamp therefore needs
converting before it is compared with a Kit A one or with wall clock, and
reading one as the other puts the claim **two hours further in the past than
it is** — which is the error direction that would make someone take a live
kit.

Separately, and the reason this note exists at all: reading a *truncated tail*
of either file can show an `IN USE` line that a `FREE` line two entries later
has already answered. Both kits read as claimed on a tail window and both were
in fact free. **Read the last `## ` line, not the last screenful.**

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

## One lesson in five places

Five findings in this slice are the same failure, and the last two are its
most expensive form.

1. **Circular validation.** `E` and `k` were measured as maxima over the
   cases that then verified them, which made "zero mismatches" partly true by
   construction when they are RTL constants. Pinned and revalidated on an
   unused seed.
2. **A record reasoning from a number instead of recomputing it.** Three
   claims reached the coordinating session as measured and were withdrawn:
   the 64-bit FPREM step, the FSQRT early-termination decline, and remainder
   widths from fitted bounds.
3. **A gate never run until the end.** The locked-Yosys netlist bench failed
   at its first attempt — `real` in a module body — which would have
   surfaced at the seal.
4. **A model that did not terminate rather than generating poor cases.** The
   exact-square construction needed a random 66-bit value to have 34 trailing
   zeros: 2^-34 a trial, about 26 trillion trials for 1,500 cases. **Its
   timeout was read as slowness**, and the background runner's later SIGTERM
   as a stale job. Constructing the trailing zeros instead, the same script
   runs in **2.3 seconds** — which is the measure of how far "slow" was from
   the truth, and the reason a timeout deserves a cause rather than a retry.
5. **A test that could pass having tested nothing.** The QEMU grid's
   stub-page failure only broke the inner loop, leaving the difference count
   at zero, so the row reported success having compared no cases.
6. **The same hole in five more places, and the fix had been applied once
   rather than to the invariant.** Codex found it again at the precision
   grid, which is what made it worth measuring properly: **with every host
   stub denied, 704 rows reported PASS and the bench printed `COMPARED 0`.**
   Five of the nine `host_form` call sites broke out of their loop without
   recording anything. The evidence was already on screen and nothing gated
   on it.

   **The fix is at the helper, not the call sites.** `host_form` is the only
   place that can tell a stub was unavailable, so it counts the failures and
   one check at the end fails the run — which covers all nine callers and any
   grid added later. Guarding each site would have been nine copies of one
   rule, and the tenth grid would have been missed the same way. The
   counterexample is reproducible: `FES_X87_NO_STUB=1` makes every stub
   unavailable, and the run now fails with `protocol.stub` and
   `protocol.compared` where it previously reported 704 passes.

**The last three are the expensive class, because they remove a gate while
appearing to add one.** A test that cannot fail is worse than no test: the
record counts it as coverage, and the thing it was supposed to catch goes
unmeasured with a passing line beside it. All three were found by review, not by
the tests themselves — which is the argument for reviewing a gate's
*construction* and not only its verdict. And the sixth adds a second lesson:
**fixing an instance is not fixing the invariant.** The QEMU grid's guard was
correct and local, and five other call sites kept the hole for another day
because the fix went where the finding was rather than where the rule lives.

## The thing worth measuring was the thing nobody runs

The census says **FPREM, FPREM1 and FXTRACT are executed by no title in the
corpus at all.** So the operation this slice spent longest getting exactly
right is the one no game reaches — the partial-step semantics, the
`q = C0·4 + C3·2 + C1` encoding, the tie on an **odd** divisor, the infinite
divisor sitting one binade below the top, the pseudo-denormal handed back
canonical.

Every one of those came from **measuring the host** rather than reading the
manual, and every one of them would have been wrong otherwise. Nothing in the
corpus would ever have caught them: no title runs the instruction, the
identity-on lockstep's programs are this slice's own, and three titles that
run with exceptions unmasked never raise one.

**That is the argument for measuring what nobody will exercise.** A rule that
no workload reaches has no feedback path at all — it is correct or it is
quietly wrong forever — so it is exactly the kind that must come from a
measurement rather than a reading. The parts of this slice a game does reach
would have failed loudly; these would not have failed at all.
