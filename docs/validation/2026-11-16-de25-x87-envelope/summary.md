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
differs from PR #1892 by one declaration and nothing else** — the packed array
described below.

The CPU's `flush` does **not** reach the backend. Arithmetic is entered after
the instruction commits, and the envelope deliberately lets a retired
instruction run on through a later squash, as a 387 does with BUSY# still
active; a straight-through mapping would cancel a committed operation and
leave the unit waiting for a result that was thrown away.

## The pipelined backend is slower here, and why

Measured on the x87 lockstep suite, both configurations, same clock:
the generated results below give the aggregate cycle counts and the per-form
table. On the x87 lockstep suite the DE25 configuration takes about **5.4%
more cycles**; the counts themselves are in the table rather than typed here,
because a typed figure drifts from the run that produced it.

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
from 2 to 80 clocks: with the wrong mapping **the first hang is at 8 clocks**,
and with the right one all thirteen pass. **The case is only known to work
because it was made to fail on purpose.**

*(The thirteen offsets are not independent trials: the loop reuses one `Unit`,
so once an offset hangs the unit everything after it fails too. An earlier
version of this record said "11 of 13 offsets hang", which read as a count of
independent failures and was not one. The first failing offset is the claim
the bench supports.)*

## The pattern: nine ways a check can report without checking

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

Two more of the same class were found by **review** rather than by use, both
in the fit harness, and both would have fired only on measurements that had
not run yet (Codex on PR #1907):

5. **The exclusion query parsed the fitter's RAM names and never gated on
   them.** `rcp_q` and `rsq_q` are synchronous-read registers that Quartus may
   absorb into the RAM block, so they can read as absent while the seed table
   itself survives — a DE25 fit could have passed while holding arithmetic the
   exclusion result claimed was gone.
6. **The SDC and its own timing gate disagreed about the clock.** The SDC was
   written at three decimals, the gate was passed six and requires equality
   within 1e-6 ns. At 16.276 ns both spell the same double, so nothing fired;
   at a natural ladder value — 13.333333 ns for 75 MHz — the SDC would have
   constrained 13.333 and the gate would have rejected a valid fit. **It would
   have fired on the clock ladder, which has not run.**

A seventh, and the one that says most about the pattern: the integration fit
**waited about two hours for the shared Quartus lock, took it, and then
refused on disk space** — "16.6 GiB free; at least 30 GiB required" — because
its output was on `/tmp`, a shared 32 GB tmpfs that other fits were filling.
That guard behaved exactly as it should: it refused *before* starting rather
than half-running and producing a partial report, and it named both the number
and the threshold. What makes it belong here is that **this exact failure had
already been pointed out and already been fixed for the unit fits**, and the
integration fit was then queued with the old path anyway. Knowing about a
failure mode did not prevent reproducing it; the check did.

**A ninth, which a tool would have committed on instruction.** PR #1907's
`plan` check went red on the S8 reader census's
`citation-fingerprints.json`, and the message it prints is
`re-record with --record-fingerprints` — so the fix looked like running one
command. Measuring first changed the answer:

| Tree | Problems |
| --- | ---: |
| `origin/main` | 2 |
| main + PR #1892 | 2 |
| main + #1892 + **this work's two script edits only** | 7 |
| this branch, 7 commits behind main | **328** |

**Five of the 328 are this work's**; about 321 are drift from the branch being
behind main, and resolve themselves when main is merged.
`--record-fingerprints` rebuilds digests for citations **as they currently
resolve** and never touches the record's prose — so it would have asserted
*"the text at `Makefile:936` is what this record cited"* for 321 lines the
census never saw, **and the check would have gone green over it**. The failure
the fingerprint file exists to prevent, performed by the tool meant to
maintain it, in another record's evidence where its author would never look.

The fix was not to run the command but to fix the branch order, and the
diagnosis is on the PR so nobody re-derives it. It is listed here because the
instruction to re-record was reasonable, the error message recommended it, and
only a measurement distinguished "the tool's normal workflow" from "silently
falsify 321 citations".

**An eighth, and it is not mine.** Band 4 was issued with **two different
thresholds on two different quantities** — LABs ≥ 99.5% in the dated
`method.md`, and ALMs ≥ 92% in a later message from the DE10 ceiling prior.
The result clears both, so nothing turned on it; but had they disagreed, a
"precommitted band" would have been two bands, and which one governed would
have been decided after the result. The coordinating session logged it as its
own error of the same family rather than tidying it away, and it is recorded
here for the same reason: **a precommitment issued twice is not a
precommitment**, and the failure is invisible exactly when the result clears
both, which is when nobody looks.

All of the first three were found the same way: by making the check fail on
purpose, or by a tool refusing loudly. The last two were found by someone
else reading the harness, which is the cheap way to catch this class and the
only one that works on a check whose conditions have not arrived yet. None would have been found by reading the code, and
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
two breaks fail, **the first at 36 clocks**, returning the abandoned divide's
quotient 2.0 as the sum of 4.0 and 4.0. (Same caveat as above: the loop shares
one `Unit`, so the first failing offset is the figure, not a count.)

So the `f1` cancel is **load-bearing**, and the case now chains a fast form.
The first version of the case was vacuous in exactly the way the flush case
had been one section earlier — written by someone who had just been caught by
that, and caught again by the same thing.

**The back-to-back pair cases, by contrast, were shown able to fail before
being relied on.** Removing the `wtiny`/`wsat` clear in `S_PRE` — the stale
working state a cross-path pair would inherit — fails **twelve** `b2b-*` cases
(`b2b-denorm`, `b2b-over`, `b2b-tiny`, across rounding modes) on **both**
backends, alongside the general arithmetic cases. That they fail on both is
the right result: the hazard is the envelope's, not the backend's, so a pair
suite that only caught it in one configuration would be testing the wrong
thing.

A seventh, of the same family, is in the next section: a lockstep that passed
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

## The locked Yosys miscompiled the backend, and the workaround is one line

The locked Yosys reads and writes a packed-struct member of an
**unpacked-array element** as a fresh undriven identifier — with **warnings
rather than errors**, so the netlist computes nonsense while the RTL
simulation is correct. The x87 unit bench's `--netlist` mode found it on the
first run with the DE25 backend selected: 763 of 763 on the RTL, **240
failures on the netlist of the same source**.

[yosys-repro/](yosys-repro/) is the locked-binary repro and
[DeanoC/yosys#79](https://github.com/DeanoC/yosys/issues/79) the fork issue.
The dangerous part is not the miscompile but the **false coverage**: a silent
miscompile is the worst kind of gate failure, because the gate reports
success.

**The workaround is a packed array.** `meta_t [6:1] meta` instead of
`meta_t meta [1:6]` in `fes_x87_fast.sv` — the locked Yosys handles member
reads and writes against a *packed* array of packed structs correctly. With
it the DE25 configuration passes the netlist bench **763 of 763**.

Two things about how that was arrived at, because they differ:

- An earlier attempt read the whole element into a wire first. It was
  **believed before being tested**, changed #1892's bytes, did not fix the
  netlist, and was reverted — a workaround that does not work is worse than
  none, because it leaves the code looking defended.
- The packed array was **built as a minimal case and confirmed driven** before
  anything was changed. The repro holds both forms in one module so the
  difference is visible rather than argued.

So `fes_x87_fast.sv` is **not** byte-identical to PR #1892: it differs by that
one declaration, and by nothing else. Everything else moved from
`boards/de25nano/x87/rtl/` unchanged.

**The netlist gate is therefore no longer DE10-only**, which an earlier version
of this record said it was. That also opens the Yosys-netlist flow — the
sibling Agilex 5 measurement's *primary* — to this configuration.

That claim rested on the **unit** bench for a while, and should not have: the
earlier broken netlist produced **no warnings either** and still failed 240
cases, so a clean Yosys log is not evidence that a netlist is right. It is now
backed by **`lockstep --netlist --backend de25`**, which passes: 1,758,699
corpus tests, 32,000 random programs, and the x87 suite 24 of 24 with 38
retirements compared against the oracle's 387.

One reading of that run needs care. Its **backend operation count is zero**,
and that zero is **by construction**, not a measurement: a flattened netlist
has no hierarchy, so the sim-only counters have no accessor and the build
defines `FES_I386_NETLIST` to say so. It is a different zero from the one that
caught a real defect earlier — there the counters existed and the tally ran
too late. The netlist run therefore shows the netlist computes the right
answers; it is the **RTL** run, with 86 operations, that shows the backend was
reached at all. Both run the same source.

## Results

The tables below are generated from the run reports by
[generate.py](generate.py) rather than typed, because a typed figure drifts
from the run that produced it. What the generator cannot do is say what the
numbers mean; that is the prose around them.

The reports themselves are in [evidence/](evidence/) and [fits/](fits/), so
the tables can be regenerated:

    python3 docs/validation/2026-11-16-de25-x87-envelope/generate.py \
        --evidence docs/validation/2026-11-16-de25-x87-envelope/evidence \
        --fits docs/validation/2026-11-16-de25-x87-envelope/fits

The generator **refuses** to table fits taken from different RTL — faults were
deliberately injected into this tree while fits were queued, to show that
several checks could fail, and a timeline argument is not a check.

**What is not here.** Four measurements are **not run**, because the shared
Quartus builder was released to the sibling Agilex 5 measurement whose fits
were queued behind these: the **DE10-arithmetic control** fit, the **in-CPU
M/M+ pair** that would give a figure addable to a machine measurement, the
**FPREM A/B** whose rule and arm B are precommitted in `method.md`, and the
**clock ladder** above 61.44 MHz that rule 6 names. So **rule 6 is met at the
headline constraint only**, and **no additive in-CPU figure exists**.

<!-- generated by generate.py; do not edit between the markers -->

### The unit bench, against the host's own x87

| Configuration | Cases passed | Failed |
| --- | ---: | ---: |
| DE10 sequenced arithmetic, RTL | 763 | 0 |
| DE25 `fes_x87_core`, RTL | 763 | 0 |
| DE10, the seal's Yosys netlist | 763 | 0 |
| DE25, the seal's Yosys netlist — 240 failures before the packed array | 763 | 0 |

### What the backend was actually asked to do

| `req_op` | Operation | Offered | Taken | Reading |
| ---: | --- | ---: | ---: | --- |
| 0 | ADD | 16 | 16 | the gate reached this operation |
| 1 | SUB | 26 | 26 | the gate reached this operation |
| 2 | MUL | 20 | 20 | the gate reached this operation |
| 3 | DIV | 16 | 16 | the gate reached this operation |
| 4 | SQRT | 8 | 8 | the gate reached this operation |
| 5 | CMP | 0 | 0 | zero **by construction** (see the text) |
| 6 | UCMP | 0 | 0 | zero **by construction** (see the text) |
| 7 | (reserved) | 0 | 0 | zero **by construction** (see the text) |

Totals: **86 offered, 86 taken** across 8 suites.

### Lockstep against the Bochs oracle

| Run | Verdict | `default_suite` | Corpus tests | passed | failed | Random programs | failed |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| baseline at the merge commit | **pass** | true | 1758699 | 1758699 | - | 32000 | - |
| DE10 sequenced arithmetic | **pass** | true | 1758699 | 1758699 | - | 32000 | - |
| DE25 `fes_x87_core` | **pass** | false | 1758699 | 1758699 | - | 32000 | - |
| DE10, the seal's netlist | **pass** | true | 1758699 | 1758699 | - | 32000 | - |
| DE25, the seal's netlist | **pass** | false | 1758699 | 1758699 | - | 32000 | - |

`default_suite` is **false** for a DE25 run by design: `backend` is a suite
argument, so a report from a differently parameterised CPU cannot be
accepted by the pc386 sealer's --lockstep-report as covering the DE10 seal.

The directed suites, which are what reach the x87 at all:

| Backend | Suite | Passed | Failed | x87 states compared | Trace hash |
| --- | --- | ---: | ---: | ---: | --- |
| DE25 | reset_store | 1 | 0 | 0 | `14650fb0739d0383` |
| DE25 | directed_cases | 84 | 0 | 0 | `b314a2470d824fd7` |
| DE25 | unreal_cases | 7 | 0 | 0 | `286cb5646eac4124` |
| DE25 | pmode_cases | 726 | 0 | 0 | `71891e83b5b2bec1` |
| DE25 | paging_cases | 148 | 0 | 0 | `a25ec314ad12fe5f` |
| DE25 | no387_cases | 1933 | 0 | 0 | `ce05ef6f406cea6b` |
| DE25 | x87_cases | 24 | 0 | 38 | `15576f711d0b8e87` |
| DE25 | memory_cases | 27 | 0 | 0 | `14650fb0739d0383` |
| DE10 | reset_store | 1 | 0 | 0 | `14650fb0739d0383` |
| DE10 | directed_cases | 84 | 0 | 0 | `b314a2470d824fd7` |
| DE10 | unreal_cases | 7 | 0 | 0 | `286cb5646eac4124` |
| DE10 | pmode_cases | 726 | 0 | 0 | `71891e83b5b2bec1` |
| DE10 | paging_cases | 148 | 0 | 0 | `a25ec314ad12fe5f` |
| DE10 | no387_cases | 1933 | 0 | 0 | `ce05ef6f406cea6b` |
| DE10 | x87_cases | 24 | 0 | 38 | `15576f711d0b8e87` |
| DE10 | memory_cases | 27 | 0 | 0 | `14650fb0739d0383` |

### Trace hashes, DE10 against DE25

`trace_hash` is folded from EIP, the eight GPRs and EFLAGS at every
retirement (`hash_state`). It covers the **integer architectural**
trace: not cycle timing, and **not the x87 register stack**. So
identical hashes say the two configurations are indistinguishable to
the rest of the CPU -- which is what rule 5 asks -- and say nothing
about whether the arithmetic agrees. That evidence is the x87 state
comparison in the table above, and the unit bench.

| Suite | DE10 | DE25 | |
| --- | --- | --- | --- |
| directed_cases | `b314a2470d824fd7` | `b314a2470d824fd7` | same |
| memory_cases | `14650fb0739d0383` | `14650fb0739d0383` | same |
| no387_cases | `ce05ef6f406cea6b` | `ce05ef6f406cea6b` | same |
| paging_cases | `a25ec314ad12fe5f` | `a25ec314ad12fe5f` | same |
| pmode_cases | `71891e83b5b2bec1` | `71891e83b5b2bec1` | same |
| reset_store | `14650fb0739d0383` | `14650fb0739d0383` | same |
| unreal_cases | `286cb5646eac4124` | `286cb5646eac4124` | same |
| x87_cases | `15576f711d0b8e87` | `15576f711d0b8e87` | same |

### Per-form latency, in CPU clocks

| Form | PC | DE10 sequencer | DE25 backend | Difference | |
| --- | ---: | ---: | ---: | ---: | --- |
| FADD | 24 | 10 | 19 | +9 |  |
| FADD | 53 | 10 | 19 | +9 |  |
| FADD | 64 | 10 | 19 | +9 |  |
| FSUB | 24 | 17 | 19 | +2 |  |
| FSUB | 53 | 17 | 19 | +2 |  |
| FSUB | 64 | 17 | 19 | +2 |  |
| FMUL | 24 | 15 | 19 | +4 |  |
| FMUL | 53 | 15 | 19 | +4 |  |
| FMUL | 64 | 15 | 19 | +4 |  |
| FDIV | 24 | 49 | 39 | -10 | **the backend is faster** |
| FDIV | 53 | 59 | 68 | +9 |  |
| FDIV | 64 | 64 | 79 | +15 |  |
| FSQRT | 24 | 53 | 39 | -14 | **the backend is faster** |
| FSQRT | 53 | 68 | 68 | +0 | equal |
| FSQRT | 64 | 72 | 79 | +7 |  |
| FSCALE | 24 | 15 | 15 | +0 | positive control: must be 0 |
| FSCALE | 53 | 15 | 15 | +0 | positive control: must be 0 |
| FSCALE | 64 | 15 | 15 | +0 | positive control: must be 0 |
| FRNDINT | 24 | 22 | 22 | +0 | positive control: must be 0 |
| FRNDINT | 53 | 22 | 22 | +0 | positive control: must be 0 |
| FRNDINT | 64 | 22 | 22 | +0 | positive control: must be 0 |
| FPREM | 24 | 17 | 17 | +0 | positive control: must be 0 |
| FPREM | 53 | 17 | 17 | +0 | positive control: must be 0 |
| FPREM | 64 | 17 | 17 | +0 | positive control: must be 0 |

### The Agilex 5 fit

All 4 fits were taken from the same RTL (8 design files, hashes equal across every fit; the fit script and the timing gate are exempt as harness).

Worst setup and worst hold are the minimum across all four published
operating conditions. **Hold is non-negative but not strictly positive**:
two seeds reach exactly 0.000 ns at their worst corner, which is a pass
and is also as close to the edge as a pass can be. Every ALM figure is
the fit's own total and INCLUDES its virtual-pin envelope's cells; they
are not subtracted, following the sibling Agilex 5 record so that the
two read the same way. The **LAB columns are LABs TOUCHED, not
capacity**: a LAB counts as used when one of its ten ALM slots is used,
so the percentage runs ahead of occupancy. Quartus's own capacity row is
ALMs needed.

| Run | Top and parameters | Seed | Period (ns) | Status | ALMs | of device | LABs | of device | Registers | DSP | M20K | Worst setup | Worst hold |
| --- | --- | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| unit-de25-s1 | `fes_i386_x87`, X87_BACKEND=1, 32 virtual ports | 1 | 16.276 | pass | 6,510 | 13.9% | 780 | 16.7% | 5,613 | 18 | 4 | +6.810 ns | +0.006 ns |
| unit-de25-s2 | `fes_i386_x87`, X87_BACKEND=1, 32 virtual ports | 2 | 16.276 | pass | 6,518 | 13.9% | 790 | 16.9% | 5,629 | 18 | 4 | +5.323 ns | +0.000 ns |
| unit-de25-s3 | `fes_i386_x87`, X87_BACKEND=1, 32 virtual ports | 3 | 16.276 | pass | 6,516 | 13.9% | 797 | 17.0% | 5,582 | 18 | 4 | +4.634 ns | +0.004 ns |
| unit-de25-s4 | `fes_i386_x87`, X87_BACKEND=1, 32 virtual ports | 4 | 16.276 | pass | 6,512 | 13.9% | 791 | 16.9% | 5,634 | 18 | 4 | +5.600 ns | +0.000 ns |

### What the selection left out

| Run | Sequenced-arithmetic registers still present | Positive controls found | Memories the fit kept | Registers in fit |
| --- | --- | --- | --- | ---: |
| unit-de25-s1 | none — as required | cw, pa, wm | rf_a, rf_b | 6801 |
| unit-de25-s2 | none — as required | cw, pa, wm | rf_a, rf_b | 6816 |
| unit-de25-s3 | none — as required | cw, pa, wm | rf_a, rf_b | 6770 |
| unit-de25-s4 | none — as required | cw, pa, wm | rf_a, rf_b | 6821 |

### The integration fit, by seed

| Seed | Fit | Timing, all 4 corners | ALMs | of device | LABs touched | of device | DSP | Worst setup | Worst hold |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | **pass** | **pass** | 40,986 | 87.58% | 4,558 | 97.39% | 42 | +7.596 ns | +0.033 ns |
| 2 | **pass** | **pass** | 41,055 | 87.72% | 4,602 | 98.33% | 42 | +7.582 ns | +0.037 ns |
| 3 | **pass** | **pass** | 41,122 | 87.87% | 4,597 | 98.23% | 42 | +7.754 ns | +0.024 ns |
| 4 | **pass** | **pass** | 41,054 | 87.72% | 4,555 | 97.33% | 42 | +7.895 ns | +0.018 ns |

Spread across 4 seeds: **136 ALMs** (40,986–41,122, 0.29% of the device) and **47 LABs** (4,555–4,602).

The worst seed is **87.87% of ALMs** and **98.33% of LABs touched**, so band 4 does not apply under either of its thresholds (ALMs ≥ 92%, LABs ≥ 99.5%) on **any** seed.

### The controlled arithmetic A/B

| | `X87_BACKEND = 0` (the sequencer) | `X87_BACKEND = 1` (the DE25 backend) | Difference of medians |
| --- | ---: | ---: | ---: |
| Seeds | 4 | 4 | |
| ALMs | 41,476–41,569 | 40,986–41,122 | -462 |
| LABs touched | 4,578–4,606 | 4,555–4,602 | +4 |
| Registers | 37,085–37,221 | 38,828–38,971 | +1,746 |
| DSP blocks | 33 | 42 | +9 |
| M20K blocks | 140 | 138 | -2 |
| ALMs, % of device | 88.62–88.82% | 87.58–87.87% | |
| LABs, % of device | 97.82–98.42% | 97.33–98.33% | |
| Fit / timing | pass / pass | pass / pass | |
| Worst setup, all corners | +7.584 to +7.776 ns | +7.582 to +7.895 ns | |

Difference of medians: **+462 ALMs**, the sequencer being the larger. Tested against the **precommitted** floor — the 1 side's own spread, **136 ALMs** — so **MEASURABLE**.

The 0 side's own spread is **93 ALMs**, reported beside the floor and **not** substituted for it: the floor was fixed before either spread was known and is not redefined now that both exist. It happens to be the wider of the two, so it is also the conservative test — the rule's correctness here did not depend on luck, and the test was not selected to fit.

The two ALM ranges **do not overlap at all** — 40,986–41,122 against 41,476–41,569, a gap of 354 ALMs between the closest seeds. Every seed of the backend is smaller than every seed of the sequencer, which is a stronger statement than a difference of medians.

<!-- end generated -->

## PR #1892's own fit, re-run after its files changed

Changing #1892's RTL came with a condition: re-run its **200 MHz four-corner
fit**, because its record is hash-bound to bytes that no longer exist. Two
changes touched it — the `fes_x87_core` split and the packed array — and the
refit **passes**, marginally better than the original on both axes that moved:

| | PR #1892 | after both changes |
| --- | ---: | ---: |
| ALMs | 2,660 | **2,643** |
| Dedicated logic registers | 4,439 | 4,439 |
| DSP blocks | 9 | 9 |
| M20K blocks | 4 | 4 |
| Logical block-memory bits | 1,568 | 1,568 |
| Worst setup, all four corners | +0.228 ns | **+0.511 ns** |
| Worst hold, all four corners | +0.001 ns | **+0.004 ns** |

Registers, DSPs, M20Ks and memory bits are **identical**, which is what a
behaviour-preserving refactor should look like; 17 ALMs and the setup slack
moved in the design's favour. **No causation is claimed**: two changes, one
measurement, and 17 ALMs is well inside what a different netlist shape can
move. The point is that the condition is met with room, not that the changes
improved anything.

Together with the bench — **368,090 checks, zero failures**, the same figure
#1892's record gives — the split and the packed array are behaviour-preserving
and fit-preserving.

## It fits, and it closes: band 2

**The integration fit passed, on all four seeds.** `pc386_system` with
`x87_present = 1'b1` and `X87_BACKEND = 1` — the machine with this work's
complete DE25 387 in it — placed, routed and closed timing at **every
published corner** at 61.44 MHz, on **every seed 1 to 4**, over an ALM spread
of 136 (0.29% of the device). The per-seed table is in the generated results;
the figures below are seed 1.

| | Used | of device |
| --- | ---: | ---: |
| **ALMs needed** (the capacity figure) | **40,986** / 46,800 | **87.58%** |
| LABs touched (spread, *not* fullness) | 4,558 / 4,680 | 97.39% |
| Dedicated logic registers | 38,828 | — |
| DSP blocks | 42 / 188 | 22.34% |
| M20K blocks | 138 / 358 | 38.55% |
| Block-memory bits | 1,352,230 / 7,331,840 | 18.44% |
| ALMs containing virtual pins (reported, never subtracted) | 651 | 1.39% |

Timing, all four corners, at a 16.276 ns period:

| Corner | Setup | Hold | Min pulse width |
| --- | ---: | ---: | ---: |
| Slow fix4 100C | +8.551 ns | +0.101 ns | +6.205 ns |
| Slow fix4 0C | +7.602 ns | +0.133 ns | +6.164 ns |
| Slow fix4a 0C | +7.596 ns | +0.128 ns | +6.175 ns |
| Fast fix4 0C | +10.513 ns | +0.033 ns | +6.325 ns |

Worst-corner Fmax on the system clock is **115.21 MHz**, against the 61.44 MHz
constrained — so it is not closing marginally.

**Against the precommitted bands:** it fits, LABs are ≤ 100%, and timing
passes at all four corners, which is **band 2**.

Band 4 — "not established, pending more seeds" — was issued with **two
different thresholds on two different quantities**, which the result clears
**both** ways, so the reading does not depend on which is applied:

| Threshold | As issued | Measured | |
| --- | ---: | ---: | --- |
| LABs, in the dated `method.md` | ≥ 99.5% | 97.39% | clear |
| ALMs, in a later message, from the DE10 ceiling prior | ≥ 92% | 87.58% | clear |

Both are recorded because a reader should be able to see that the result did
not turn on the choice. Had they disagreed, the committed one governs — it is
the one in a dated file — and the coordinating session's own position was that
it would have resolved the conflict *against* the headline. **No rule was
changed after the result.**

**No prediction was recorded before this ran and none has been retrofitted
since. Both earlier projections — that it would not fit, and the LAB
arithmetic behind that — were withdrawn *before* the fit, for reasons
unrelated to its outcome.** The bands did exactly the job they existed for.

### What this result does and does not say

- ~~**Single seed, and this is the first caveat for a reason.**~~ **Answered:
  four seeds, all passing.** This was recorded as the first caveat and as the
  highest-value measurement outstanding, so it was measured rather than left
  as a disclaimer. Seeds 1 to 4 all fit and all close at every corner, over a
  spread of **136 ALMs** — 0.29% of the device — and 47 LABs; the table is in
  the generated results above. The worst seed is 87.87% of ALMs and 98.33% of
  LABs touched, so **band 4 does not apply on any seed under either of its
  thresholds**. A four-seed pass at a 0.29% spread is a different claim from a
  single-seed pass, and this is now the former.
- **87.58% of ALMs is the largest thing fitted on Agilex 5 in this project**,
  and nothing above 79.6% had been fitted on the device before. It sits below
  the DE10 lane's practical ceiling of around 92% — **but that ceiling is
  another device's prior**, on different fabric with a different toolchain, and
  it is stated here explicitly because it is the caveat a reader is otherwise
  most likely to treat as a safety margin it has not earned. Agilex 5 has no
  measured ceiling; this fit is the highest point on it so far.
- **LABs at 97.39% are not 97.39% full.** A LAB counts as used when one of its
  ten ALM slots is used, so this is how sparsely the fitter spread the design,
  not how close the device is to capacity. The ALM row is the capacity figure.
- **No DE25 board shell.** The 651 ALMs of virtual-pin cells stand in for one,
  and a real shell — HPS bridge, EMIF, scanout, audio, reset release — is
  measured nowhere. On the DE10 the equivalent is around 770 ALMs.
- **The two 64×64 multipliers are the headline follow-up, and the first
  candidate for slice 6's optimisation pass.** The CPU alone uses **12** DSP
  blocks; this machine uses **42**. The integrated x87 builds *two* 64×64
  multipliers — the envelope's shifter-multiplier (`fes_i386_x87_mul`, nine
  22×22 products) and the backend's FMUL multiplier (nine 22/22/20-bit
  products) — doing the same job. A merged unit would share one.

  **The saving is deliberately not estimated.** That is a measurement, and this
  record is in part a log of what happens to estimates. The DSP counts above
  are the evidence that the duplication exists; what removing it costs or
  saves is for slice 6 to measure. It was a risk while fitting looked doubtful;
  it is now headroom nobody has had to spend.
- **No prediction was recorded before this ran**, deliberately, and none is
  retrofitted now. The record's earlier projection that it would *not* fit was
  withdrawn before the fit, for reasons unrelated to the outcome.

**Host:** `chip-30x0-ai-dev`, Quartus Pro **26.3.0 Build 114** read off that
host, not copied from the other. The netlist was elaborated on `powerboat` with
the authenticated locked Yosys and reused here by hash
(`95b72a8d546f8c0a1475…`, 133,035 lines, verified at both ends), because the
chip host has neither the HIP nor the CUDA Yosys slot built. Harness, envelope,
SDC and report rows are the sibling measurement's, unmodified.

## Whether it fits in the machine was **not** established before this fit

*(This section is kept as it stood before the integration fit, because the
reasoning it records — and the two projections it withdrew — are what made the
fit worth running. The answer is in the section above.)*

This record measures the envelope-plus-backend **on its own**. Nothing in
*this* part of it says it fits inside `fes.pc386`; that was settled only by the
fit above.

What *is* measured, by the sibling Agilex 5 measurement
(`docs/validation/2026-11-16-i386-agilex5-fit/`, tracker #1898): `pc386_system`
on Agilex 5 with main's 387 **on** — its fit M+ — closes 61.44 MHz at every
published corner at **37,255 of 46,800 ALMs, 79.6%**, leaving about **9,545
ALMs**. The 387 in that fit is main's — **slice 2 only, no arithmetic at all**,
every form `X87_UNIMPL` — and its whole cost in the machine was **+1,763 ALMs,
+9 DSP, +4 M20K**. Single seed.

**A second claim is withdrawn here**, and it was the sibling's reading that
this record repeated: that "Agilex 5 placed what the DE10 could not". It did
not, because M+ is not the design the DE10 refused. **The DE10 already holds
slice 2's 387** — PR #1821 sealed exactly that at 36,298 of 41,910 ALMs with
every analysis positive over all four corners and a kit run. What #1889
refused is **slice 4 on slice 3**: divide, square root and the remainders,
**6,923** ALMs of standalone unit against its merge base's 4,076. M+ is main
plus a one-character flip, so comparing its success to #1889's refusal
compared two different designs.

*(Corrected: this said **8,073**, which this record repeated from the figure
in circulation. 8,073 is slice 4 **as first written**; 6,923 is the form it
settled on and the one that was fitted, so 8,073 overstates the sequencer by
about 1,150 ALMs.)*

Two further labels on the 9,545 ALMs, which the sibling supplies and this
record repeats rather than softening: it is **arithmetic to 100% of the
device**, and **nothing above 79.6% has been fitted on Agilex 5 at all** — on
the DE10 lane the practical ceiling is near 92%, with timing misses at 94.7%.
So the remainder is not all usable.

**A projection that stood here earlier is withdrawn.** It read the fitter's
"Total LABs: partially or completely used" row as a capacity figure — 98.08%
for M+, 90 LABs spare — and derived from it that this work would need roughly
614 LABs against 256 and so would not fit. That was wrong, and the sibling
measurement withdrew the underlying reading: **a LAB counts as used when one
of its ten ALM slots is used**, so M+ touches 98% of LABs while leaving about
a fifth of their slots free, and the fitter itself estimated 4,210 ALMs
recoverable by denser packing. The capacity row is ALMs needed, not LABs
touched. The 10.6-ALMs-per-LAB density that projection used was a measured
delta's ratio, not a rate at which new LABs get consumed, and using it that way
was the error. Both the projection and the framing it produced — that nothing
pointed at fitting — are retracted.

So: there is apparent ALM headroom, and **nothing here establishes that this
design fits inside it.** What is genuinely open, rather than invented:

- **Whether this work and M+'s slice-2 387 combine arithmetically at all.**
  They cannot simply be added: both carry their own significand multiplier and
  rounding. This work's unit fit uses **18 DSP blocks** where M+ − M shows
  **+9**, which is exactly `fes_i386_x87_mul`'s nine 22×22 products — so the
  integrated design currently builds **two** 64×64 multipliers. **A merged
  unit would share one.** Nobody has costed that, and it is the first place to
  look if an integration fit refuses.
- **What this work's logic costs in the machine rather than standalone.** The
  unit fit's 6,510 ALMs include `regs_o` (640 register bits), the trace
  outputs and its own virtual-pin cells, all of which the machine trims, and a
  standalone unit packs more loosely than the same logic under whole-machine
  pressure. The figure is an upper bound on a quantity nobody has measured.
- **The rest of a complete 387** — the remainder on the backend, scale,
  round-to-integer, the transcendentals, BCD — and **a DE25 board shell**,
  which nobody has fitted and which the machine fits stand in for with 651
  ALMs of virtual-pin cells.
- **The measurement that would settle it** is `pc386_system` **from PR #1889's
  head** — the design that actually hit the wall — on Agilex 5 with
  `x87_present` high, or better, with `X87_BACKEND = 1` and this work in it.
  The sibling session has offered to run it through the same runner and
  envelope as its other fits, which would put the numbers on one scale. It is
  about twenty minutes of Quartus and it has **not been run**: it is a
  different base from the one its task named, and queueing it is Deano's
  call.

## The two front ends agree to about 1%

This record's fits read SystemVerilog directly; the sibling measurement's read
the Yosys SEAL netlist. That was an **unbounded** difference between the two
records' figures until the sibling measured it, on `fes_i386_cpu`, same rung,
same seed, same envelope:

| | Yosys netlist | direct SystemVerilog | |
| --- | ---: | ---: | --- |
| Dedicated logic registers | 20,842 | 20,893 | +0.24% |
| ALMs needed | 29,300 | 29,035 | −0.90% |
| ALUTs | 42,126 | 41,609 | −1.23% |
| LABs used | 3,626 | 3,609 | −0.47% |
| DSP blocks | 12 | 12 | identical |
| M20K blocks | 53 | 53 | identical |
| Block-memory bits | 408,320 | 408,320 | identical |
| ALMs containing virtual pins | 2,351 | 2,351 | identical |

Both closed 61.44 MHz at every corner. The register count is the quantity its
method gated on, fixed before the fit ran, and +0.24% is well inside its ±5%
threshold.

So a reader may set figures from the two records beside each other carrying
**about a 1% front-end uncertainty rather than an unknown one**. It does
**not** make the scopes comparable, and it says nothing about whether two
units' areas add.

**A consequence for this work's own flow:** Quartus Pro's SystemVerilog front
end *accepts* the i386 RTL, where the locked Quartus 17.0.2 rejects it
outright. The reason the DE10 lane feeds Quartus a netlist does not apply on
the Pro side — so direct SystemVerilog is a legitimate front end here, not a
fallback, once the M10K requests are retargeted.

## The CPU's clock, which bears on the latency finding

The sibling also measured `fes_i386_cpu` closing **130 MHz** on Agilex 5 —
every rung of its precommitted ladder passed, the top with +0.426 ns — with
area flat across all five rungs to within 155 ALMs, and seed 2 at the headline
rung within 73 ALMs of seed 1.

That matters for the per-form latency above. **The 5.4% aggregate is derived
at 61.44 MHz, which is the DE10's rung**, and the CPU is not what pins the
clock on Agilex 5. A cycle gap costs proportionally less wall time at a higher
clock, while the backend's fixed pipeline latency stays the same number of
clocks — so the figure is not portable, which is why the cycle counts are the
primary result and the percentage is labelled with its clock. If a DE25 CPU
were targeted near 100 MHz for a two-backend-clocks ratio, that is inside what
the CPU closes at and costs no measurable area.

## The unit fits' harness constrains virtual pins at zero, and that costs

**The four unit fits in this record constrain every virtual-port path at zero
delay** (`set_input_delay`/`set_output_delay` at 0.000). The sibling
measurement **cuts** those paths instead (`set_false_path`), and has since
measured what the difference is worth, on two fits of identical CPU logic
under the two envelopes:

| | zero-delay | cut paths | |
| --- | ---: | ---: | --- |
| ALMs | 29,114 | 29,300 | +186 |
| LABs | 3,512 | 3,626 | +114 |
| Dedicated logic registers | 23,024 | 20,842 | **−2,182 (−10.5%)** |
| Worst-corner Fmax | 97.77 MHz | 124.35 MHz | **+26.6 MHz** |

So **the zero-delay form is the worse envelope**, and it is this record's unit
fits that use it. What that means for the figures above:

- **ALMs are barely affected** — under 200 on a 29,000-ALM design, inside this
  lane's noise. So 6,510–6,518 stands, and it also answers a question this
  record asked: how much of it is harness? Not much, on ALMs.
- **The registers may be inflated by around 10%.** The zero-delay form makes
  the fitter duplicate registers to meet a hold requirement that the
  non-existent pins invented. 5,582–5,634 should be read with that in mind.
- **The setup slack is understated**, and the Fmax implied by it is too low —
  by something like 27 MHz on a CPU-sized design. The +4.6 to +6.8 ns reported
  is a floor, not an estimate.

**The integration fit is not affected**: it ran on the sibling's harness, which
cuts the paths. So the headline — 87.58–87.87% of ALMs over four seeds, timing
closing at every corner — needs no adjustment.

Two consequences recorded rather than acted on, since they are a re-fit each:
the unit fits would be worth re-running with cut paths, and **a zero-delay
figure should not be compared against a cut-path one at all**, which is why
this record's unit fits and the sibling's CPU and machine fits are kept in
separate tables.

## A cross-record dependency, and an uncontrolled A/B

The sibling measurement has fitted **PR #1889's head on Agilex 5** and it
closes: **41,387 ALMs, 88.43%**, all four corners, 33 DSP, 140 M20K. So the
sequencer design the DE10 refused fits on this device too.

Set against this work's **40,986–41,122**, that looks like an A/B of the two
arithmetic implementations — and in ALMs it finds no difference worth the
name: about 330 ALMs, against this work's own 136-ALM seed spread and the
lane's ±200. **But it is not a controlled comparison**, which the sibling's
record says rather than leaving implied: its fit is #1889's head on #1879's
branch, and this work's is this branch carrying #1889 *plus* #1892 *plus*
eleven main commits. **The base varies as well as the arithmetic.**

What the uncontrolled pair does show clearly is **nine DSP blocks**: 33 for the
sequencer against this work's **42**. The sequencer's 33 is unchanged from
M+'s, so slices 3 and 4 added no DSP beyond slice 2's multiplier, while this
work's backend adds nine for its FMUL. **That is the duplicated 64×64
multiplier appearing in the device's own counts** — slice 6's first
optimisation candidate on evidence rather than on inspection.

**The controlled version has now been run**, and it is in the generated
results above. One tree, one rung, one seed, one host, one harness, with the
two source trees differing in **exactly one line** — the `.X87_BACKEND(1)` on
the `fes_i386_cpu` instantiation. It settles the comparison the uncontrolled
one could not:

**The DE25 pipelined backend is about 460 ALMs *smaller* than the sequencer it
replaces**, over **four seeds each side**, against the precommitted floor of
136 — so the difference is **measurable** by the rule fixed before the 0 side
ran. It costs **9 more DSP blocks** and **about 1,750 more registers**, and
uses 2 fewer M20Ks.

**The two ALM ranges do not overlap**: 40,986–41,122 against 41,476–41,569, a
gap of 354 ALMs between the closest seeds. Every seed of the backend is
smaller than every seed of the sequencer, which is stronger than a difference
of medians and is what the extra seeds bought.

*(The first 0-side seed alone gave −514 against the 1 side's median; four
seeds put it at −462. The single-seed figure was not wrong, but it was a point
against a distribution, and this record said so at the time rather than after
the extra seeds narrowed it.)*

Three things worth drawing out, the first being the one that generalises
past this measurement:

- **The uncontrolled comparison could not have told us either way, and it
  happened to point the right way.** It put the sequencer about 330 ALMs
  larger: right sign, but *inside* this 136-ALM floor plus the lane's ±200, so
  it established nothing. Controlling the base moved it to 514 and over the
  floor. The reading is **not** "the uncontrolled version was roughly right" —
  it is that it was **not evidence**, and a reader who met 330 would have had
  no way to know. This is the sharpest instance of this record's pattern,
  because every other one arrived as a wrong number and **this one arrived as
  a nearly-right number that was still not evidence**. A nearly-right number
  is the harder case: nobody goes back to it.
- **The direction is the opposite of the intuition, and it is a trade rather
  than a win.** A deeply pipelined arithmetic unit reads as the bigger thing
  and is the ALM-cheaper one — but **it saves 514 ALMs by spending 9 DSP
  blocks and about 1,750 registers**, the nine 22/22/20-bit FMUL products and
  the pipeline's own stages. The −514 should not be quoted without the +9 and
  the +1,750.
- **The nine DSP blocks are confirmed as the duplicated multiplier.** The
  sequencer's 33 is unchanged from M+'s 33 — so slices 3 and 4 added no DSP
  beyond slice 2's shifter-multiplier — while the backend's FMUL adds exactly
  nine. That is now a controlled measurement rather than an inference from two
  differently based fits, and it is what makes sharing one multiplier slice 6's
  first candidate. **Still uncosted**, deliberately.

**All eight fits close, per clock.** Every one of them reports a finite Fmax
for **both** `clk_clock` and `pixel_clk_clock` at all four corners, so "passes
at every corner" is a claim about both clock domains rather than about
whichever had paths — confirmed from the retained reports, not assumed, since
the harness these ran on could otherwise have passed a machine fit having
analysed one domain (table in `integration-fit/README.md`).

The 0 side is band 2 on its own terms: 88.62–88.82% of ALMs, 97.82–98.42% of
LABs touched, worst setup +7.584 to +7.776 ns. The worst 0-side seed is 88.82%
of ALMs and 98.42% of LABs, so band 4 does not apply to it either, under
either threshold, on any seed.

**Why the non-overlap is the figure to quote.** A difference of medians leans
on a summary statistic, and with four seeds a reader may reasonably distrust
it. Two **disjoint ranges** make no such demand: eight fits, 354 ALMs between
the closest pair, and anyone can check the ranges against the committed fit
records themselves.

~~Caveat: the 0 side is one seed.~~ **Answered: four seeds each side.** The
0 side's own spread is **93 ALMs**, narrower than the 1 side's 136, and the
136 remains the floor because it was fixed before either spread was known —
it is not redefined now that both exist, and it is the more conservative of
the two in any case. Both spreads are reported so a reader can see which test
was applied.

**This record is now cited by another.** The sibling reads all four seed rows
out of this directory rather than from a message, so the A/B in its record
depends on what is published here. The four rows as published are
40,986 / 41,055 / 41,122 / 41,054 ALMs at 87.58 / 87.72 / 87.87 / 87.72%, LABs
4,558 / 4,602 / 4,597 / 4,555, and 42 DSP on every seed — checked against the
committed fit records, which is where the generated table above comes from.

## What is not settled

- **FPREM's area cost is unmeasured.** The reason given for keeping FRNDINT,
  FSCALE, FXTRACT, FPREM and FPREM1 on the sequencer is that they cost no
  arithmetic hardware the envelope does not already carry. For FPREM that may
  be false — `P_SH` and `P_FIN` use `dv_rem` and a wide subtractor the slice 2
  envelope did not have — and the entity table cannot separate it, because it
  is inline. The threshold for deciding it is committed in `method.md`, dated,
  ahead of the fit. No title in the measured corpus executes FPREM, FPREM1 or
  FXTRACT at all.
- **Whether the integrated design fits the machine is open**, as the section
  above says, and the density inference points at it not fitting. No fit of
  `pc386_system` with `X87_BACKEND = 1` exists.
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
- **The pair cases are masked-only.** They do not sweep the exception masks,
  so the unmasked paths' interaction across a back-to-back pair is untested.
