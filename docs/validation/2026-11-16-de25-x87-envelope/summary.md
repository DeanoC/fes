# The x87 envelope on the DE25 arithmetic backend

The verified x87 **envelope** — the register stack, the control, status and
tag words, the pointers, the BUSY# latch and the interlocks, the conversions,
architectural retirement and the masked and unmasked exception results — now
drives either arithmetic implementation, selected by one parameter. The DE25
387 inherits the envelope's oracle lockstep instead of rebuilding it, which is
the whole point of the shared-envelope decision.

Closes #1903. Base: PR #1889's head, with `origin/main` and PR #1892's branch
merged in; the diff therefore carries #1892 and eleven main commits as well as
this work.

**This is the first of three steps — envelope, then transcendentals, then
remainder.** The order comes from the measured game census (PR #1887).

- **Built:** add, subtract, multiply, divide and square root answered by
  `fes_x87_core`, through the envelope's own rounding, its masked and unmasked
  rules, the ±24576 bias adjust and tininess-after-rounding.
- **Kept on the sequencer in both configurations:** FRNDINT, FSCALE, FXTRACT,
  FPREM and FPREM1 — see "What is not settled" below, because for FPREM the
  reason given for keeping them is **not yet measured**.
- **Not built, on either path, and stopping in decode: the transcendentals.**
  F2XM1, FYL2X, FPTAN, FPATAN and FSINCOS are executed by titles in the
  measured corpus, so this is real missing demand, said here rather than
  omitted. It is the next step.
- **No kit run, no seal, no package, no game acceptance.** Host simulation and
  an internal virtual-pin fit only.

[method.md](method.md) carries the method and the pass rules, committed before
the first lockstep run, with every later amendment dated and labelled with
what had already been seen. [interface-gap.md](interface-gap.md) is the
reading of the two interfaces made before any RTL was written, with its two
wrong conclusions corrected in place rather than quietly deleted.

## The seam, and why it is where it is

The DE10 envelope drives a sequencer that multi-passes a shared multiplier.
The DE25 backend accepts operations and returns tagged results in issue order.
The adaptation is not at the backend's **response**, which is where it looks
like it should be, but one stage earlier.

`fes_x87_round` delivers a **masked** result: it substitutes infinity or the
largest finite value on overflow, and it denormalises *before* rounding on
underflow. Both are correct for a masked result and both spend exactly what an
**unmasked** overflow or underflow needs, which is the value rounded at the
unbounded exponent with its exponent biased by −24576 or +24576. The envelope's
`A_RND` already does precisely that, reading OM and UM out of the control
word — and it is the side the oracle verifies. So the CPU consumer taps the
backend **before** its rounder and lets `A_RND` round.

The two representations meet without repacking, which is what makes the seam
cheap rather than merely possible. `raw_t` carries
`{sign, signed [17:0] exponent biased by 16383, significand[66:0]}` with bit 66
the integer bit and bits 2, 1 and 0 the guard, round and sticky; `A_RND`
consumes `{wsign, we, wm[63:0], wg, wrd, ws}` with the same bias and bit order.
**The bit positions were checked against `round_pc` at all three precisions,
and against the tiny and overflow thresholds, rather than assumed** — "the
formats happen to line up" is the kind of claim a later reader should want
evidence for.

`fes_x87_core` is therefore the body of #1892's `fes_x87_pipe` lifted out: the
shared normalising front end, the two arithmetic lanes and their arbitration,
delivering a result before rounding. `fes_x87_pipe` keeps its interface and
its behaviour exactly, as that core plus `fes_x87_round` and the 16-entry
completion RAM. One core, two wrappers; no duplicated RTL, and **#1892's RTL
is unchanged apart from the lift**.

The CPU's `flush` does **not** reach the backend. Arithmetic is entered after
the instruction commits, and the envelope deliberately lets a retired
instruction run on through a later squash, as a 387 does with BUSY# still
active; a straight-through mapping would cancel a committed operation and
leave the unit waiting for a result that was thrown away.

## The pipelined backend is slower here, and why

Measured on the x87 lockstep suite, both configurations, same clock:
**DE10 10,993 cycles, DE25 11,584** — about **5.4% more** on that suite.

Where the cycles go, measured per form **at each precision** in the unit
bench, as clocks from the instruction's commit to `busy_o` falling — which is
what the next ESC waits for. The table itself is in the generated results
below; what follows is what it says.

**Precision reverses the divide's direction, and a PC64-only table would have
hidden that.** The backend's digit recurrence stops at 25, 54 or 65 clocks by
precision, while the sequencer's Goldschmidt barely improves as precision
falls (49, 59, 64 clocks for FDIV). So:

- **At PC24 the backend is faster** — FDIV by 10 clocks, FSQRT by 14.
- At PC53 they are close: FDIV +9, FSQRT level.
- At PC64 the backend loses: FDIV +15, FSQRT +7.

The census (PR #1887) measured that only Quake uses PC24 at all and four
titles use PC64 or PC53, so the corpus sits mostly where the backend loses —
but the loss is a **precision-dependent** property, not a flat algorithmic
defeat.

**The fast forms are flat across precision and lose by a fixed amount:** FADD
+9, FSUB +2, FMUL +4 at every precision. That is the pipeline's fixed ~15-clock
response against a sequencer that finishes a dependent add in 10. #1892's
backend is built for throughput — one operation accepted per clock — and the
envelope holds one ESC instruction in flight, so there is never a second
operation in the pipe. #1892's headline rate of 200 million independent fast
operations a second assumes a concurrency this consumer does not have.

**FSCALE, FRNDINT and FPREM are a positive control, not a footnote.** They
stay on the sequencer in *both* configurations, so their rows must read zero —
and a difference there would have meant the harness was measuring something
other than the backend. They are what licenses reading the other rows at all.

### What the remedy is, after two corrections

This section has been wrong twice, and both versions were asserted before
being measured. They are recorded rather than deleted, because the pattern is
the point:

1. First claim: *"the remedy is envelope work, not backend work — let more than
   one ESC be in flight."* Half right. It would help the fast forms, and only
   where the code has independent x87 operations.
2. Second claim: *"the divide gap is algorithmic and closing it is backend
   work."* **Also wrong as stated**, because it was measured at PC64 only: at
   PC24 the backend's divide is the faster of the two.

What the measurement actually supports:

- The **fast forms'** gap is a fixed pipeline latency. More ESCs in flight
  would help it. **Whether the game workloads contain independent x87 pairs at
  all is unmeasured** — x87 code chains through ST(0), and if the census
  titles' streams are mostly dependent then that remedy is not available to
  them.
- The **divide and square root's** gap exists at PC53 and PC64 and is reversed
  at PC24. It is a property of two different algorithms scaling differently
  with precision, so there is no single remedy: a backend that chose its
  algorithm by precision would win at every precision, and nobody has costed
  that.

The shape of this is worth more than the numbers. **The architecture argument
predicted a uniform pipeline penalty. The first measurement found two causes.
The second measurement found that one of those two reverses sign with
precision.** Each round of measuring contradicted the previous round's
explanation, and none of the explanations was wrong in a way that reading the
code would have exposed.

The **5.4% aggregate** on the x87 lockstep suite is one instance of all this,
at one precision mix and one clock. **The cycle counts are the primary result
and the percentage is derived at 61.44 MHz**, which is the DE10's rung and not
necessarily the DE25's: the sibling measurement found the CPU closes 61.44 MHz
on Agilex 5 with worst-corner Fmax 124.35 MHz, so the same cycle gap costs
less wall time if an integrated DE25 CPU is eventually targeted higher. **The
clock the DE25 configuration would run at is not settled**, and a percentage
quoted without its clock will be read as portable when it is not. The
game-level consequence is **not measured**; what would measure it is the pc386
RTL game suite's preset benchmarks with the DE25 configuration selected,
against the same titles on the DE10 configuration — frames, not cycles.

This does not reopen the DE25 decision. That decision was about **area**: the
DE10 cannot hold the 387 at any speed, so a few per cent on x87-bound work
does not compete with a core that does not fit.

## Three defects this found, and one it nearly missed

**The exact cancellation.** The unit bench failed 240 of 740 cases with the
DE25 backend — every add and subtract whose operands cancel, and no multiply,
divide or square root. A raw result whose whole 67-bit significand is zero is
an exact cancellation; `fes_x87_round` packs it as a true zero by clearing the
exponent field, and `A_RND`, which this path uses instead, would have stored
`{sign, e, 0}` — a pseudo-denormal. This is **seam-only**: on the DE10 path
`A_SUM` packs the cancellation itself and jumps to `S_APPLY`, so `A_RND` is
only ever entered with a non-zero magnitude. PR #1889 has no latent defect
here, and its passing is structural rather than a case its suite happens to
miss.

A detail worth keeping rather than tidying away: `A_SUM` decides the cancelled
zero's sign with `rc == 2'd1` and `fes_x87_fast` with `meta[6].rc == 2'b01` —
the same 387 rule, written twice by different authors with neither consulting
the other, agreeing exactly. That is **independent confirmation** of a rule
that is easy to get backwards, and worth more than one implementation checked
against a manual that both could have misread. The obvious tidy-up is one rule
in one place, and it would delete the cross-check; the duplication is
deliberate, and the seam carries the core's sign through rather than
recomputing it.

**The square root was not excluded.** Exclusion was claimed after guarding the
sequenced adder, the Goldschmidt divide and the reciprocal seed table. The
guard list came from grepping case arms by prefix, and the sequenced square
root's eight `Q_*` states were never in it. Quartus Pro refused the fit, because
the surviving square-root seed table carries the DE10's `ramstyle = "M10K"` and
Agilex 5 has no such block — a fit that refuses turned out to be a better
exclusion check than reading a report.

**The count that was not a measurement.** Lockstep passed with the DE25 backend
and reported a backend operation count of zero. The counters were tallied in
`Rtl`'s destructor, and the one `Rtl` is a local of `main` that outlives the
summary. Had the gate been reported as "lockstep passes", nothing would have
been learned. The rule that the **count** is reported rather than the verdict
is what exposed that the count itself was broken.

**And the one it nearly missed: a test that could not fail.** The
flush-during-divide case was added precisely to cover the flush mapping. Wiring
the backend's flush deliberately wrong — straight from the CPU's `flush`, the
mapping that hangs a committed operation — still passed all 763 cases, because
the case pulsed flush four clocks after commit, inside the envelope's dispatch
and before the backend is ever offered the operation. The flush is now swept
from 2 to 80 clocks: with the wrong mapping 11 of 13 offsets hang the unit,
with the right one all pass. **The case is only known to work because it was
made to fail on purpose.**

## The pattern: five checks that reported without checking

These are listed together because the pattern is the finding, not any one of
them. In each case a check existed, ran, and reported success while being
structurally incapable of failing:

1. **The flush-during-divide case** pulsed flush before the backend was ever
   offered the operation, so a deliberately wrong flush mapping passed all 763
   cases.
2. **The square-root exclusion** was claimed from a guard list built by
   grepping case-arm prefixes; the eight `Q_*` states were not in it, so every
   fit started up to that point was measuring a design other than the one
   being reported on.
3. **The Quartus timing gate** took the wrong argument count and opened the
   wrong project name, so it errored before analysing anything — and the first
   fit produced a complete resource count with **no timing behind it**.
4. **The exclusion query**, written to prove the unselected arithmetic was
   gone, was wrong three ways at once and would have failed *every* DE25 fit:
   its wildcard needed a hierarchy separator the unit top does not have, `pa*`
   also matched `pa_e0`, and it counted `dv_rem` — which FPREM writes, and
   FPREM is kept, so it is present by design rather than by leakage.

All three were found the same way: by making the check fail on purpose, or by
a tool refusing loudly. None would have been found by reading the code, and
two of them had already produced numbers that looked like results. The
Quartus refusal is worth its own line — **a check that fails loudly beat a
check that reports** — and it is why the exclusion evidence is now a query
that fails the build rather than a table for a reader to inspect.

**And a fifth, which is the same mistake made by the person fixing the
fourth.** The `f1-during-divide` case was written to cover the port F1h reset
arriving mid-operation. It passed — and it **still passed** with `f1`
deliberately removed from both the backend's reset and the cancel condition.
Two breaks, neither detected, which was written up here as "the RTL is safe by
construction and the `f1` cancel is belt-and-braces".

**That was wrong, and the reason is instructive.** The case chained a second
*divide*, and a divide is held in `B_ISSUE` by the slow lane's own admission
interlock until the abandoned one drains — so it can never see a stale result.
A *fast* form is accepted at once, and `raw = fast_valid ? fast_raw : slow_raw`
hands `B_WAIT` the old quotient if the abandoned divide completes first.
Chaining an **FADD** instead, swept across every clock of the divide, the same
two breaks fail at **nine offsets, first at 36 clocks**, returning the
abandoned divide's quotient 2.0 as the sum of 4.0 and 4.0.

So the `f1` cancel is **load-bearing**, and the case now chains a fast form.
The first version of the case was vacuous in exactly the way the flush case
had been one section earlier — written by someone who had just been caught by
that, and caught again by the same thing.

A sixth, of the same family, is in the next section: a lockstep that passed
while the count it was gated on was never measured.

## Two gates that had stopped covering what they named

Neither was introduced by this work's design; both were found by it.

**A `--backend de25` lockstep report would have satisfied the pc386 sealer.**
`default_suite` is computed over `SUITE_ARGUMENTS`, and `backend` was not in
it, so a report from a differently parameterised CPU could have been accepted
by `--lockstep-report` as covering the DE10 seal — a seal backed by evidence
about a different design, which is the one thing the sealer exists to prevent.
This hole existed before this branch: **anyone adding a CPU parameter would
have reopened it.** `backend` is now a suite argument; its default is `de10`,
so the default suite is unchanged.

**Moving the backend's RTL dropped its CI family.** `affected.py` mapped
`boards/de25nano/x87/**` to the `de25-x87` family; after the move a change to
`fes_x87_core.sv` would have run the pc386 family and never the standalone
pipeline bench. It is mapped to both now. This is the same shape one layer
out: a gate that stops covering something because a file moved, with nothing
failing.

## The locked Yosys netlist bench is a DE10-only gate

Said in terms, so that nobody later cites a green netlist bench as covering
the DE25 path. The DE10 configuration passes it. The DE25 configuration does
not: the locked Yosys reads and writes a packed-struct member of an
unpacked-array element as a fresh undriven identifier, with **warnings rather
than errors**, so the netlist computes nonsense while the RTL simulation is
correct. [yosys-repro/](yosys-repro/) is the locked-binary repro and
[DeanoC/yosys#79](https://github.com/DeanoC/yosys/issues/79) the fork issue.

This does not block the DE25 387, whose flow is Quartus Pro reading
SystemVerilog with no Yosys in it. The dangerous part is not the miscompile
but the **false coverage**: a silent miscompile is the worst kind of gate
failure, because the gate reports success.

A whole-element-read workaround was tried on `fes_x87_fast.sv` and reverted
once the repro showed that member *writes* are implicitly declared too, so the
read-side fix never rescued it. The file is byte-identical to #1892 again. A
workaround that does not work is worse than none, because it leaves the code
looking defended.

## Results

The tables below are generated from the run reports by
[generate.py](generate.py) rather than typed, because a typed figure drifts
from the run that produced it. What the generator cannot do is say what the
numbers mean; that is the prose around them.

<!-- GENERATED -->

## What is not settled

- **FPREM's area cost is unmeasured.** The reason given for keeping FRNDINT,
  FSCALE, FXTRACT, FPREM and FPREM1 on the sequencer is that they cost no
  arithmetic hardware the envelope does not already carry. For FPREM that may
  be false — `P_SH` and `P_FIN` use `dv_rem` and a wide subtractor the slice 2
  envelope did not have — and the entity table cannot separate it, because it
  is inline. The threshold for deciding it is committed in `method.md`, dated,
  ahead of the fit. No title in the measured corpus executes FPREM, FPREM1 or
  FXTRACT at all.
- **The `X87_BACKEND = 0` control cannot be fitted on Agilex 5**, because its
  seed tables carry a Cyclone V `ramstyle`. Changing that attribute would
  change what the DE10 seal infers, which is not this scope's to move. So
  there is no same-flow, same-device comparison between the two arithmetics.
- **This figure is a unit fit and does not simply add to the sibling CPU
  measurement.** A virtual-pin fit of `fes_i386_x87` keeps `regs_o` (640
  register bits) and the trace outputs that the machine trims away. The
  additive number is an in-CPU delta, not this one.
- **No cross-device ratio against the DE10 figures.** Cyclone V and Agilex 5
  ALMs are different units.
- **The host x87 is a modern one, not a 387.** Where the two differ the case is
  read against the 387 Programmer's Reference Manual. The eleven
  hardware-settled rules slices 3 and 4 measured rest on that same modern part;
  no 387 was available, which the plan record carries as an open question and
  this work does not close.
- **pc386's CPU source digest moves**, because the shared i386 RTL list gained
  the backend's files. No reseal was done.
- **Two lockstep runs appear in the logs** only because the first died on a DNS
  failure fetching the MOO vectors, not on anything in the design.
- **The flush sweep's thirteen offsets are not independent trials.** The loop
  reuses one `Unit`, so once an offset hangs the unit everything after it fails
  too. The claim the bench supports is "the first hang is at 8 clocks; the
  wrong mapping fails and the right one passes all thirteen", not a count of
  independent failures.
- **The pair cases are masked-only.** They do not sweep the exception masks.
