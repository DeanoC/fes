# x87 transcendentals and BCD on the DE25 backend: method and pass rules

Committed **17 Nov 2026**, before any simulation, lint, netlist or Quartus run
of this work and before any RTL was written. What had been read and measured
when this was written is listed at the end, so that no rule below can be read
as fitted to a result. Every later amendment is dated and says what had been
seen when it was made.

Tracking issue: [#1958](https://github.com/DeanoC/fes/issues/1958), under the
x87 tracker [#1768](https://github.com/DeanoC/fes/issues/1768). This is the
second of the three DE25 steps the envelope record names (envelope, then
transcendentals, then remainder). Base: main at `e2b8130b5`, which contains
the envelope ([#1907](https://github.com/DeanoC/fes/pull/1907), `d13a26a75`)
and the Agilex 5 fit record
([#1899](https://github.com/DeanoC/fes/pull/1899), `84318b52d`).

## Scope

The instruction list is the plan record's slice 5
([plan](../2026-11-12-i386-x87-plan/summary.md), "Slices"), not this file's:

| Instruction | Encoding | Kind | Compared |
| --- | --- | --- | --- |
| F2XM1 | D9 F0 | transcendental | within the bound |
| FYL2X | D9 F1 | transcendental | within the bound |
| FPTAN | D9 F2 | transcendental | within the bound |
| FPATAN | D9 F3 | transcendental | within the bound |
| FYL2XP1 | D9 F9 | transcendental | within the bound |
| FSINCOS | D9 FB | transcendental | within the bound |
| FSIN | D9 FE | transcendental | within the bound |
| FCOS | D9 FF | transcendental | within the bound |
| FBLD m80 | DF /4 | BCD load | exactly |
| FBSTP m80 | DF /6 | BCD store and pop | exactly |

Which of these the census titles execute, and in what order that puts the
work, is **generated** from the census's committed rows
([census.jsonl](../2026-11-15-i386-x87-game-census/census.jsonl)) into this
record by its generator, not typed: the census record's own history is a typed
list of this kind that was wrong
([census summary](../2026-11-15-i386-x87-game-census/summary.md), "Which 387
operations no title executed").

**Built for `X87_BACKEND = 1` only.** With `X87_BACKEND = 0` every form in the
table keeps stopping in decode as not built. The tracker's slice 5 wording
("in microcode on the DE10 datapath, or on the HPS") is superseded by the
16 November decision that the DE25 is the 387's home: one build, on the DE25
backend, all in fabric, no HPS.

### Two phases and a stop line

- **Phase A** needs no decision and is everything in this file except the
  eight transcendentals' algorithm: the 387's architectural rules, decode and
  the interface, the tolerance model in the unit bench and lockstep, the
  audit, and **FBLD and FBSTP built in full**, RTL included.
- **Stop line.** No algorithm RTL for the eight transcendentals is written,
  and **no numeric pass bound is fixed**, until Deano's answers to the plan
  record's open questions 3 and 4 are relayed: Bochs's results bit for bit or
  the 387's method within a bound; the bound in ulps; whether real-387
  captures exist. `options.md` beside this file costs each route. It
  recommends; it does not choose.
- **Phase B** is the algorithm RTL. Its rules that depend on the answer are
  added to this file, dated, **before** the first transcendental result.

## The 387's architectural rules come first, and from the manual

`rules.md` beside this file states, per instruction and with the page it is
read from (387 Programmer's Reference Manual 231917-001, the Intel387 DX
datasheet 240448-005): the operand range and what happens outside it; the
stack effect and both stack faults; which exceptions it raises and whether
each is detected before or after the operation; what precision control and
rounding control do or do not do to it; FPTAN's pushed 1.0; the C2
"out of range, operand unchanged" rule at |x| >= 2^63; FBSTP's invalid cases
and its rounding; FBLD's handling of invalid BCD digits.

- **A rule is read, not recalled.** Where the manual is silent or says
  "undefined", `rules.md` says so and names what the RTL does and why.
- **Where Bochs differs from the 387, the RTL follows the 387** and the case
  is added to `divergences.toml`, as the plan record requires.
- **Every rule in `rules.md` has a directed case**, in the unit bench and, for
  rules visible at retirement, in lockstep. The generator prints, per rule,
  the cases that cover it; a rule with none is printed as uncovered rather
  than omitted.
- `rules.md` is committed before the RTL that implements it. A rule changed
  after a result is amended in place, dated, with what had been seen.

## How each instruction is checked

### FBLD and FBSTP: exactly

Both are exact conversions (FBSTP rounds to an integer under rounding
control), so there is no tolerance anywhere on them.

- **Unit bench**, against the host's own x87, bit for bit: the stored 10
  bytes, the loaded register, the six flags, SF, ES, C1, TOP and the tags.
  A modern x87 is not a 387; where `rules.md` finds the two differ the case
  is read against the manual, as in the earlier slices.
- **Lockstep** against the Bochs oracle, exactly, at every retirement.
- **QEMU's `test-i386-fbstp.c`**, see below.

### The eight transcendentals: flags exactly, results within a bound

The unit bench compares against the host's x87, which is neither a 387 nor
Bochs for these, and lockstep compares against Bochs, whose polynomial
results match no FPU. An exact compare is wrong in both. The tolerance model,
fixed here before any result:

1. **Compared exactly, always:** the six exception flags, SF, ES and the
   board's latch, C0, C2 and C3, TOP, every tag, every register the
   instruction does not write, the control word, the opcode and the pointers.
   That includes the out-of-range case: C2 set and the operand unchanged is
   an exact compare of the whole state.
2. **Compared exactly, when the reference result is not finite and non-zero:**
   a result that is a zero (with its sign), an infinity, a NaN or the
   indefinite is architectural, not numeric, and has no tolerance.
3. **Compared within the bound, otherwise.** The distance between the RTL's
   result and the reference's is counted in **representable extended values**:
   each finite non-zero extended value maps to a signed integer that orders
   the format (sign, then exponent, then the significand below its integer
   bit), and the distance is the absolute difference of the two integers. In
   one binade that is the difference in units of the last place of the 64-bit
   significand; across a binade boundary it stays well defined, which a
   difference of significands would not. A result of a different sign from
   the reference, or one that is not a normal or denormal extended value,
   fails whatever the distance.
4. **The bound is a named parameter with no default**:
   `--transcendental-ulp` on the unit bench runner and the lockstep runner.
   A run that reaches a tolerance compare with the parameter unset **fails**,
   it does not skip. This file fixes no value for it; that is the stop line.
5. **C1.** The brief for this work says "C-bits exact", and C1 cannot be:
   after a transcendental C1 is the round-up bit of that implementation's own
   last rounding, so two results one ulp apart can both be right and differ in
   C1. The rule here is: **C1 is compared exactly when the two results are
   bit-identical, and when a stack fault sets it (SF set); otherwise it is
   reported, counted and not gated.** This is a decision made in this file
   and flagged to the coordinating session at the stop line, not a reading of
   the brief.
6. **PE.** The inexact flag is in rule 1 and so exact. If a route's results
   can be exact where the reference's are not, away from the architectural
   cases of rule 2, that is reported as a failure of this rule, not absorbed
   by widening it.
7. **The tolerance cannot leak into anything else.** A within-bound compare
   leaves the RTL and the reference holding different values, so every later
   compare in the same program would inherit the difference. After each
   within-bound compare the **reference is resynchronised to the RTL's
   value**, in the registers that instruction wrote and nowhere else: the
   unit bench reloads the host's registers, and the lockstep sets the
   oracle's through a new accessor on the oracle interface (it has a getter
   for the x87 state and no setter today). Each run reports the number of
   resynchronisations and the largest distance seen, per instruction. A
   resynchronisation happens only after a compare that passed; a failed
   compare stops the case as a failure.
8. **Every tolerance compare is shown able to fail.** A self-test injects a
   result at bound + 1 and requires a failure, and at exactly the bound and
   requires a pass, per instruction. A tolerance that has never been seen to
   reject anything is not a check.

### QEMU's tests

The plan's gate names QEMU's `tests/tcg/i386` x87 tests. They are
GPL-2.0-or-later test inputs, handled like test386.asm: **pinned by commit and
SHA-256, fetched at run time, run outside any package, never committed and
never shipped**, and nothing derived from their text is committed.

- Phase A runs `test-i386-fbstp.c`'s cases, exactly.
- Phase B runs `test-i386-fpatan.c`, `test-i386-f2xm1.c`, `test-i386-fyl2x.c`
  and `test-i386-fyl2xp1.c`, under the tolerance model above.
- QEMU's expected values are those of the processors QEMU models, not a 387.
  Where a QEMU expectation contradicts `rules.md`, the RTL follows `rules.md`
  and the case is listed with both values; it is not dropped.
- How the cases reach the bench (the pinned commit, the file digests, and how
  inputs and expectations are extracted) is added to this file, dated,
  **before the first QEMU result**. Nothing about those files had been read
  when this was written.

### The 2^63 range cases

For FSIN, FCOS, FSINCOS and FPTAN: operands at 2^63 - 1 ulp, exactly 2^63,
2^63 + 1 ulp, the largest finite value and both infinities, each sign. Below
2^63 the result is under the tolerance model; at and above it the compare is
exact (rule 1). Phase B.

## Decode and the built set

- **One predicate says what is built**, and it takes the backend. Today
  `x87_decode` is blind to `X87_BACKEND` and drives both `known` in
  `fes_i386_decode` and `unbuilt` in the unit; both must read the same
  backend-aware answer, and so must the bench's C++ copy (`x87_built_form`
  in `lockstep_tb.cpp`).
- **The RTL's predicate and the bench's copy are compared mechanically** over
  all 2^11 x87 opcodes at both parameter values, not by inspection. The
  comparison's output states the count of built forms per backend.
- **FBLD and FBSTP are memory forms and need all three decode tables**
  (`x87_decode`, `x87_format`, and the access-shape case in
  `fes_i386_decode`). Only the third fails unsafe, and only lockstep sees it,
  so both have a lockstep case with a memory operand whose bytes a wrong
  shape would misread.

## Pass rules

Each is a rule, not a target. A rule that fails is reported as a failure and
is not replaced. If a rule turns out not to be checkable as written, the
amendment says so, dated, and says what replaced it and whether the
replacement is weaker.

1. **Nothing that exists moves.** A baseline of the unit bench and the
   lockstep suites is taken at the merge base `e2b8130b5` on both backends,
   after this file is committed. On the final head, for both backends:
   every existing lockstep suite has the same case count and the same trace
   hash as that baseline, the no-387 suite among them at 1,933 cases; and
   every existing unit bench case passes, with the same case count.
   So that this is checkable, **new lockstep cases go in a new suite** and the
   existing x87 suite's programs keep generating from the built set they have
   today, on both backends: adding to the existing suite would move its hash
   by construction, which is the amendment #1907's rule 5 needed.
2. **`X87_BACKEND = 0` still stops.** A directed case per instruction in the
   table shows it stopping as not built with the DE10 backend, in the unit
   bench and in lockstep. The check is shown able to fail by leaking one form
   into the DE10 built set on purpose and seeing the case fail.
3. **The DE25 run reaches every form it claims.** The lockstep and unit bench
   reports give an executed count per instruction in the table. A form
   claimed as built with a count of zero is a hole in the gate, not a pass.
   In Phase A the eight transcendentals are reported as not built, by name.
4. **Unit bench passes on both backends**, RTL and locked-Yosys netlist, with
   the new cases. Back-to-back pairs are a named requirement, as in #1907:
   each new form is followed, while still in flight, by a form that reads
   what it wrote and by one that does not, and preceded by forms that leave
   each kind of working state behind.
5. **Lockstep passes on both backends**, RTL and locked-Yosys netlist: the
   x87, no-387 and memory suites and the new suite.
6. **Reports name what they tested**: the FES revision, an empty list of
   tracked files differing from it, and a SHA-256 per tested source, as
   #1907's post-merge reports do. A report without them is not evidence for
   the final head.
7. **Lint is clean at both parameter values.**
8. **The locked-Yosys netlist bench runs from the first working RTL**, not at
   the end. A new toolchain workaround gets a DeanoC-fork issue with a
   locked-binary repro, linked from the PR.
9. **The unguarded-arm audit covers the new arms.** `x87_envelope_fit.py`
   derives, from the RTL, the registers written only inside arms guarded by
   `X87_BACKEND == 0`. The new arms are guarded the other way, so the audit
   gains the mirror direction: registers written only inside arms guarded by
   `X87_BACKEND == 1` must be absent from a DE10 build. A test removes one
   new arm's guard and requires exactly that arm's registers to leave the
   list.
10. **Figures cite a committed artifact.** `summary.md`'s tables are generated
    from committed reports by this record's generator. A figure with no
    artifact is labelled as an estimate where it stands.

## The machine fit: bands committed before the first fit

No fit of this work had been run or queued when this was written. The fits
come after the stop line; the coordinating session may amend these bands
before the first fit is queued, and such an amendment is dated here. They are
not changed after any fit output exists.

**What is fitted:** `pc386_system` with `x87_present = 1'b1` and
`X87_BACKEND = 1`, on Agilex 5 `A5EB013BB23BE4SCS`, Quartus Pro 26.3, at the
61.44 MHz headline, seeds 1 to 4, on `chip-30x0-ai-dev`, with the merged
runner and collector (`fit_i386_device.py`, `record_i386_device_fits.py`) and
their envelope unmodified. The identity-on and backend edits to
`pc386_system.sv` are a scratch change in the fit's own tree, not merged RTL,
as in #1907, and their presence in the fitted snapshot is checked by reading
the snapshot.

**Reporting.** "ALMs needed" is the capacity figure. The LAB row is spread,
not fullness, and is never reported as capacity. The wording is "passes
timing at 61.44 MHz"; "closes" is not used unless a clock ladder is run.

**Bands**, per seed and then over the four:

1. The fitter refuses, or ALMs needed exceed the device's 46,800: **does not
   fit**.
2. Fits, and timing passes at all four corners on both clocks, on all four
   seeds: **fits and passes timing at 61.44 MHz**.
3. Fits, and timing fails at any corner on any seed: **fits but does not pass
   timing**. Not a pass. Work stops and the miss is reported with the worst
   slack and its corner; no timing check is relaxed.
4. In band 2, with ALMs needed at or above **92%** of the device (43,056) on
   any seed: **not established**. One threshold, on one quantity: #1907's
   band 4 was issued twice with two, which its record lists as a defect.
   92% is the DE10 lane's prior and not a measured Agilex 5 ceiling; nothing
   above 87.87% has been fitted on this device.

**The delta.** Two comparisons, labelled apart:

- **Against #1907's four seeds** (40,986 / 41,055 / 41,122 / 41,054 ALMs,
  spread 136;
  [its record](../2026-11-16-de25-x87-envelope/summary.md), "The integration
  fit, by seed"). This is what the brief asks for. It is **uncontrolled**:
  those fits predate #1907's own merge of main and main has moved since, so
  the base varies as well as the 387. It is reported as a difference of
  medians beside both spreads and is labelled uncontrolled wherever it
  appears.
- **Against a controlled baseline**: the same four seeds, the same harness and
  host, at this PR's merge base with the same two scratch edits, so that only
  this work differs. The difference is **measurable only if the difference of
  medians exceeds the larger of the two arms' spreads**; otherwise it is
  recorded as **not measurable**, and not as "small" or "free". DSP, M20K and
  register counts are exact counts and are reported as such. Per-seed rows
  for both arms go in the record.

**DSP.** Phase B is to reuse the backend's adder and multiplier with constants
in memory. Any DSP block above the baseline's 42 is stated with the block
that uses it; none is expected and that expectation is not a pass rule.

## What this method does not claim

- **No DE25 board shell, seal, package or kit run exists for this
  configuration**, and none is produced here unless one has landed on main by
  then. Host simulation and a virtual-pin machine fit only.
- **No real 387 has been consulted.** The host x87 is a modern part and the
  oracle is Bochs. Whether real-387 captures exist is one of the open
  questions; until answered, "follows the 387" means "follows the manual".
- **No game acceptance and no frame rate.** The census says which forms
  titles execute; nothing here runs a title.
- **The DE10 gains nothing.** It stays identity-off; its lockstep and seal are
  untouched except by rule 1's check that they did not move.
- **Not in this step:** the remainder on the backend, the governor's 387
  clocks, the identity's default, and sharing the two 64x64 multipliers.

## Already read or measured when this was written

Stated so the rules above cannot be read as fitted to results.

- **Read:** `CLAUDE.md`, `AGENTS.md`; the plan record; the envelope record's
  `summary.md`, `method.md` and `interface-gap.md`; the census record's
  per-form table and unexecuted list; `fes_i386_pkg.sv`'s `x87_format` and
  `x87_decode`; the ESC case of `fes_i386_decode.sv`; the head of
  `fes_i386_x87.sv`, its state list and its backend seam; the oracle
  interface header; the lockstep runner's suite arguments.
- **Seen in those, and bearing on the rules:** the census table shows FYL2XP1,
  FBLD and FBSTP executed by no title; `x87_format`'s eight values are all in
  use; `fes_i386_decode` has no backend parameter; the oracle interface has no
  x87 state setter; no QEMU test is wired into the repository; and the
  backend RTL under `boards/de25nano/x87/rtl/` is read by nothing (the
  standalone build and bench read `cores/fes-common/rtl/i386/`) and two of its
  five files differ from the copies that are built.
- **Not run:** no lint, no unit bench, no lockstep, no netlist build, no
  Quartus fit, on this branch or on its base in this session. No RTL, bench
  or script had been changed. The 387 manual and the datasheet had not been
  opened for this work; the plan record's summary of them is all that had
  been read.
