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

## Amendments

Each is dated and says what had been seen when it was made.

### 17 Nov 2026, amendment 1: the route is decided, and the references are not equal

**What had been seen.** Deano's answer to the plan record's question 3,
relayed by the coordinating session: "Follow the 387, we can use the real x87
unit in the 2 build pcs if thats useful?". The reference probe
([evidence/reference-probe.json](evidence/reference-probe.json), at
`4c5ef203e`, 20,000 operands a class) and the two-host comparison
([evidence/host-comparison.json](evidence/host-comparison.json)), both of
which run **no RTL**: no RTL of this work existed. The merge-base baseline's
unit bench (763 of 763 on both backends); its lockstep was still running. The
387 manual had still not been read for this work.

What those two reports show, stated because the changes below follow from it:

- The Bochs oracle and the host x87 agree on **every** exception flag and on
  C0, C2 and C3 over every operand class probed, and disagree on C1 often.
- Against QEMU's bracket tables (4,374 rows over F2XM1, FYL2X, FYL2XP1 and
  FPATAN) the host returned one of the two neighbours of the mathematical
  result on **every** row. The oracle was outside the bracket on 57 to 498
  rows of each table, by up to about 247,000 representable values (FPATAN).
- The two build hosts, an Intel Core Ultra 7 270K Plus and an AMD Ryzen 7
  7800X3D, never differ by more than **one** representable value over 704,374
  result values, large arguments and arguments beside multiples of pi/2
  included, and never in a flag or in C0, C2 or C3.
- For operands beside a multiple of pi/2 the oracle and the host differ by up
  to about 8.6e9 representable values while the two hosts agree bit for bit.

**The changes.**

1. **The stop line moves.** The route is the 387's: its argument reduction,
   its ranges and rules, its stated error bound. Algorithm RTL may be written.
   **The numeric bound is still not fixed**, and no transcendental result
   gate is declared passed until it is agreed. Nothing of Bochs's
   transcendental code is ported, copied or read.
2. **Two bounds, not one.** Rule 4's single `--transcendental-ulp` cannot
   serve both references: a bound wide enough for the oracle's own error
   (hundreds of thousands of representable values on FPATAN) checks nothing
   when applied to the host, and one tight enough for the host fails the
   oracle on correct results. It is replaced by
   `--transcendental-ulp-host` on the unit bench and
   `--transcendental-ulp-oracle` on the lockstep, each with no default and
   each refusing a tolerance compare when unset. **This is weaker for the
   lockstep than what was written**: against the oracle the value compare can
   only be a coarse sanity check, and the accuracy gate is the unit bench
   against the host and the bracket tables. Rule 1 (flags, C0, C2, C3, TOP,
   tags and everything else exact) is unchanged in both, and is the part of
   the lockstep that keeps its force.
3. **What the hosts establish.** They are modern x87 units, not 387s, and
   this is not a real-387 capture. They are used as a reference where the two
   agree with each other **and** the 387's documentation gives the same rule;
   where they differ from each other or from the documented rule,
   `rules.md` wins and the case is listed. The unit bench runs on one host;
   the second host's agreement is carried by the comparison report, which is
   re-run on the final directed cases.
4. **The proposed bound is committed before the RTL is run against it.**
   `bound.md` beside this file proposes the two bounds from the 387's
   documented error and from the measured distributions, with the whole
   histograms. It is a proposal for Deano; until he agrees it, results are
   reported as distributions and no pass is claimed.
5. **A model stands between the RTL and the references.** The kernels are
   sequences of the backend's own add, subtract, multiply and divide over
   constants in memory, as the brief's Phase B asks, **not** the 387's CORDIC:
   its microcode is unpublished and unobservable. What follows the 387 is what
   its manual states and software can see. Because every step is an exactly
   defined rounded operation, the sequences are written once, in a generator,
   and run two ways: by an integer model in Python and by the RTL. **The RTL
   must equal the model bit for bit** on every operand both run, which is an
   exact check with no tolerance; the model is what is compared with the
   mathematical result, to a stated error, over far more operands than a
   simulation can reach. The tolerance compares against the host and the
   oracle remain, as the check that the model computes the right function.
6. **Trigonometric brackets.** QEMU has no table for FSIN, FCOS, FSINCOS or
   FPTAN (confirmed in its `tests/tcg/i386` at v11.1.3). Their mathematical
   reference is the model's own high-precision evaluation, and their second
   reference the two hosts.
7. **QEMU's tables, now that they have been read** (pinned at v11.1.3,
   `005c61c0e`, with the digests in `probe_fes_i386_x87_trans.py`). Each of
   the four transcendental tables gives, per row, the two extended values
   bracketing the mathematical result under the default control word, and
   passes either; none checks a flag, a rounding mode or the stack. They are
   parsed at run time into operand and bracket; the pass rule for those rows
   is the host bound applied to the distance outside the bracket.
   `test-i386-fbstp.c` is seventeen inline cases, not a table: its operands
   and expected bytes are run as the unit bench's own directed FBSTP cases,
   written from the instruction's rules, and the QEMU file is fetched and its
   seventeen expectations compared with those cases' at run time.

### 17 Nov 2026, amendment 2: C1, the distance function, and the engine's first size

**What had been seen.** The engine RTL against its model: 49,304 vectors,
every one equal bit for bit, and a rounding fault injected on purpose caught.
The unit with the engine in it, on the DE25 backend, against the host at the
proposed bound of 4: first 16,935 of 18,177 cases passing, and after the
changes below all 18,177, on the RTL and on the locked-Yosys netlist, with
7,994 of the model's instruction-level vectors equal bit for bit and the
largest distance from the host 2. The DE10 backend: 774 of 774. No lockstep
run with the transcendentals, and no machine fit.

1. **Rule 5 on C1 could not stand, and is replaced.** It gated C1 "exactly
   when the two results are bit-identical". The first unit run failed 1,042
   cases on that alone, and the reason is not the unit: the two build hosts
   disagree with each other on C1 for results that are identical to the bit
   (the host comparison has it on thousands of such rows). C1 after an
   inexact transcendental is the round-up bit of each implementation's own
   last rounding and nothing else. **The rule is now: C1 is compared exactly
   where a stack fault set it and where the result was exact (PE clear);
   elsewhere it is counted and not gated.** This is weaker than what was
   written. It was changed after a result that failed it, which this file
   forbids for a pass rule on the unit's behaviour; it is recorded as a rule
   that the references themselves cannot meet, with the measurement that
   shows it, rather than as a rule the unit now passes.
2. **The distance function was wrong at a binade boundary.** Rule 3's integer
   was computed as `(exponent - 1) << 63 | significand` in every copy, which
   drops 2^63 where `exponent - 1` is odd. Two results either side of a
   power of two then read as about 2^63 apart, not 1. It showed as 92 unit
   failures at exactly that distance. It is `+`, and the committed
   distance reports are regenerated with the corrected function; a distance
   inside one binade was never affected.
3. **FSINCOS's last result is the sine**, which replaces ST(0), and the
   cosine is the other, pushed: the unit's two-result apply is FXTRACT's,
   and this is its order. The model and the engine were changed together
   and re-checked against each other.
4. **A first, non-gating size.** `fes_x87_trans` alone, every port a virtual
   pin, one seed, an ad hoc project on chip-30x0-ai-dev with Quartus Pro
   26.3: 1,687 ALMs, 706 registers, 7 RAM blocks, no DSP block, and a setup
   slack of 10.7 ns at 16.276 ns. It is not a fit of the machine, its
   virtual pins are counted in it, and no band is judged on it. It is
   recorded because band 4's threshold is about 2,000 ALMs above the
   baseline's median, so the machine fits may land beside it.
5. **Manual over host, in the unit bench.** Two classes of case follow the
   manual where the host does otherwise, and say so in the bench: a
   trigonometric form of an infinity is compared with the host's state with
   C2 set (rules.md TRIG4), and FYL2XP1 of an operand at or below -1 is not
   compared with the host at all but asserted against the unit's own rule
   (rules.md LOG5). The report counts both as `manual`.

### 18 Nov 2026, amendment 3: what the lockstep found, and the fits restarted

**What had been seen.** The lockstep's transcendental suite on both backends,
RTL and locked-Yosys netlist, at `71c412786`: 181 of 181 gated cases on the
DE25 backend with nine further instructions kept out because they failed,
and every existing suite's case count and trace hash equal to the baseline.
One baseline fit (seed 1) had finished; no fit of this work had.

1. **A real difference, fixed in the microprogram and not in the rule.** For
   FSIN, FSINCOS and FPTAN of an operand beside the boundary between the
   denormals and the normal values, under a directed rounding mode, the unit
   returned the operand stepped by one in the mode's direction. That is the
   mathematically directed result, and it crossed the boundary, so the tag
   and, for the sine, the underflow flag differed from the oracle **and from
   both hosts**, which return the operand itself. Rule 1 does not allow a
   flag to differ from the references, and the manual says nothing either
   way (rules.md G4). Measured on the host afterwards: below 2^-68 a
   trigonometric form returns its operand (and FCOS 1.0) in every rounding
   mode, and above it rounds in the mode's direction. The microprogram now
   has that threshold. The nine instructions are in the gated suite.
   **This changed the design to fit the references after a failing result;**
   it did not change a pass rule.
2. **Two oracle differences are accepted beyond the three this file's
   amendment 1 anticipated,** each as a `divergences.toml` entry that names
   exactly what is accepted, because on every measured instance the unit
   equals the host x87 and the oracle does not: Bochs raises UE for FPATAN
   from its own quotient where the result is beside pi, and raises DE in
   FYL2X, FPATAN and FYL2XP1 with both operands normal. The second entry's
   predicate is wider than what was measured, and the entry says so.
3. **The lockstep compares a transcendental where the unit is parked,** not
   at its own retirement: the instruction retires while the unit still
   works, as a 387's does with BUSY# active, so the 387 state is compared
   before the next ESC or WAIT, or at HLT. A program that never gives the
   bench that point fails as "compare pending"; it does not pass.
4. **The fits of this work were restarted.** The first arm was elaborated at
   `b8ca4f3ef`; change 1 altered the microprogram ROM, so that arm was
   stopped before any of its fits finished, re-elaborated at the commit that
   holds the change, and requeued. No figure from the abandoned arm exists.
   The baseline arm, at the merge base, is unaffected.

### 18 Nov 2026, amendment 4: the two bounds are agreed

**What had been seen.** Everything: every report under `evidence/final`,
the eight machine fits, and the distributions in results.md and
lockstep.md. This agreement is not blind and is not presented as one.

1. **Deano agreed both proposed bounds** on 18 Nov 2026: 4 representable
   values against the host x87 and QEMU's brackets, and 2^21 against the
   Bochs oracle with the trigonometric window of bound.md. The question and
   his reply, verbatim, are in [agreement.json](agreement.json), as relayed
   by the coordinating session.
2. **Nothing was changed for it.** Neither figure, the window, nor any pass
   rule; no RTL, bench, runner or script. No report was re-run: every run
   had used the values now agreed.
3. **The transcendental result gates are judged in
   [gates.md](gates.md)**, generated from agreement.json and the committed
   reports. A report that used any other bound than the agreed one is
   printed there as not judged.
4. **Still open, and not covered by the agreement:** the C1 rule of
   amendment 2, the cases where the unit follows the manual against the
   references, the two oracle flag divergences of amendment 3, and real-387
   captures.
