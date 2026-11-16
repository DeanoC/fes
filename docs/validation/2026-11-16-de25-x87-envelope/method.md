# DE25 x87 envelope integration: method and pass rules

Committed **16 Nov 2026**, before the first lockstep run and before any
Quartus fit. What had already been measured when this was written is listed at
the end, so that nothing below can be read as a rule written to fit a result.

## What is being built

The x87 **envelope** — the eight-register stack, the control, status and tag
words, the instruction and data pointers, the BUSY# latch and the interlocks,
the load and store conversions including m80, architectural retirement, and
the masked and unmasked exception results — is shared between the DE10 and the
DE25. Only the arithmetic is swapped, by the `X87_BACKEND` parameter on
`fes_i386_x87` and `fes_i386_cpu`:

| `X87_BACKEND` | Arithmetic | Device |
| ---: | --- | --- |
| 0 (default) | the unit's own sequenced datapath (slices 3 and 4) | DE10 |
| 1 | `fes_x87_core`, the pipelined backend of PR #1892 | DE25 |

Base: PR #1889's head (`deano/i386-x87-s4-eac381`), with `origin/main` and
PR #1892's branch merged in. A slice-2 base was rejected because every
arithmetic form decodes to `X87_UNIMPL` there, so lockstep would pass while
issuing **zero** backend operations — a gate that cannot fail.

## What is in scope, and what stops

- **Built:** add, subtract, multiply, divide and square root on the DE25
  backend, through the envelope's own rounding and exception rules.
- **Kept on the sequencer in both configurations:** FRNDINT, FSCALE, FXTRACT,
  FPREM and FPREM1. Each needs the shifter the format conversions need anyway
  and none needs the adder or the divider, so keeping them costs no arithmetic
  hardware that the envelope does not already carry. They are *not* built on
  the backend; the backend remainder remains the next step.
- **Not built, and stopping in decode:** the transcendentals. They are built
  on neither path. The measured game census (PR #1887) puts F2XM1, FYL2X,
  FPTAN, FPATAN and FSINCOS in the corpus, so this is real missing demand and
  is said plainly rather than omitted.

## Pass rules

Each is a rule, not a target. A rule that fails is reported as a failure; it
is not replaced.

1. **The unit bench, unedited, passes in both configurations.** The envelope's
   behaviour must not depend on which arithmetic answers it, so the pass rule
   is the *same* `x87_unit_tb.cpp` cases passing with `--backend de10` and
   `--backend de25`, against the host's own x87. Any case that passes one way
   and fails the other is a defect in the seam.
2. **Back-to-back pairs are a named requirement.** The registered-read hazard
   has appeared in every slice of this unit and a single operation hides it
   entirely. The bench must run ordered pairs of forms with the second
   instruction presented while the first is still in flight, crossing the two
   arithmetic paths in both directions, over operand patterns that leave each
   kind of working state behind (cancellation, tiny, overflow, denormal,
   exact).
3. **Lockstep against the Bochs oracle passes with the DE25 backend selected**,
   over the same suites slice 4's record uses, with no new divergence entries.
4. **The lockstep run reports a non-zero backend operation count for every
   `req_op` the configuration can reach**, broken down per operation. A zero
   for ADD, SUB, MUL, DIV or SQRT is a hole in the gate, not a pass: a green
   lockstep that issued no arithmetic proves nothing. Issued must equal taken.
5. **The default configuration is unchanged.** With `X87_BACKEND = 0` the
   lockstep trace hashes equal PR #1889's, and the DE10 arithmetic is what
   answers. The selection has to work in both directions: a DE25 build must
   exclude the sequenced arithmetic, and a DE10 build must exclude the
   backend, or #1889's existing figures stop meaning what they say.

   **Amended 16 Nov 2026**, because the rule as written **cannot be checked**
   and was wrongly reported as met. PR #1889's record carries **no trace
   hashes** — `lockstep-identity-on-pass.txt` holds case counts only — so
   there is nothing to compare against. And the three directed cases this work
   added to the x87 suite change `x87_cases`' hash **by construction**, in
   both configurations.

   What had been seen when this was amended: a baseline lockstep run at this
   branch's merge commit `717c3463a`, and runs of both configurations at head.
   Seven of eight suites — directed, memory, no387, paging, pmode,
   reset_store, unreal — are byte-identical across all three. `x87_cases`
   differs from the baseline and is **identical between the two
   configurations**, which is what places the change in the suite rather than
   the RTL.

   The rule is therefore replaced by what is checkable, and the replacement is
   weaker than the original, which is said rather than hidden:
   **(a)** every suite this work did not change has a trace hash identical to
   a baseline generated at the merge commit; **(b)** the changed suite's hash
   is identical between the two configurations; **(c)** the DE10 configuration
   reports a backend operation count of zero, so the backend is genuinely not
   reached. It does **not** establish equality with #1889's own runs, because
   #1889 recorded no hashes to establish it against.
6. **The integrated design fits Agilex 5 at 61.44 MHz**, the headline
   constraint the sibling `i386-agilex5-size` session committed to
   (`docs/validation/2026-11-16-i386-agilex5-fit/method.md`), with seeds 1–4,
   followed by its ladder of 75, 90, 110 and 130 MHz. Fmax at one constraint
   is a fitter estimate, not a closure: the achievable clock comes from
   fitting at successively higher constraints.
7. **The area figure names what it contains.** Device, Quartus Pro version,
   seed, corner set, raw ALMs, DSPs and M20Ks, and percentage of the device.
   It states which arithmetic the fit contains and that the other is excluded
   by the selection.

   **Amended 16 Nov 2026**, after the first fits were attempted and before any
   area figure was read. This rule named "the fit's own entity list" as the
   evidence for exclusion. That is wrong and the rule could not have been met
   as written: the DE10 arithmetic is **inline in the sequencer's `always_ff`**,
   not an entity, so it is absent from the entity table in *both*
   configurations and the table cannot distinguish them. What had been seen
   when this was amended: Quartus Pro refused the first DE25 fit because the
   square root's seed table had survived an incomplete guard, and the
   `X87_BACKEND = 0` control would not fit on Agilex 5 at all. The evidence is
   now three things, none of which is the entity table alone:

   - `fes_x87_core` present in the DE25 entity table and absent at
     `X87_BACKEND = 0`, which is all the entity table can show;
   - ~~the DE25 fit **building at all**, which is possible only if both seed
     tables are gone: they are declared `ramstyle = "M10K"`, a Cyclone V block
     Agilex 5 does not have, so a surviving table makes Quartus refuse;~~
     **Withdrawn 16 Nov 2026.** This held only for the fits taken before
     commit `4e6e1053b`, which retargets `ramstyle = "M10K"` to `"M20K"` in
     the private snapshot so that the CPU's caches elaborate on Agilex 5 at
     all. From that commit a surviving seed table no longer makes Quartus
     refuse, so a fit that builds proves nothing about the tables. What had
     been seen: the four unit seeds passing at `93c5d4362` with the refusal
     property still in force, and the CPU fit failing on `fes_i386_l1i`'s
     M10K request, which is what forced the retarget. The seed tables' absence
     is instead read from the fit report's RAM summary **by name** — a DE25
     fit should list the register-file pair and no seed table, and the
     `X87_BACKEND = 0` control should list both tables. Note that `rcp_q` and
     `rsq_q` are synchronous-read registers that Quartus may absorb into the
     RAM block, so their absence by name is **not** evidence either way and
     the named-register list below does not rely on them alone;
   - a query of the fitted netlist for the inline arithmetic's named registers
     — `ua_add`, `ub_add`, `qy_s` and the divide's — expecting none, with `wm`
     and `pa` as a positive control so that a zero means absent rather than
     renamed.
   **No cross-device ratio against the DE10 figures**: Cyclone V and Agilex 5
   ALMs are different units.

## The integration fit: bands precommitted before the run

**Added 16 Nov 2026, before the fit was queued and before any output of it
existed.** Deano approved it explicitly ("yes do as you recommend, let do a
complete integration fit"); the coordinating session set the bands, which are
recorded here verbatim and are **not** to be changed after seeing output.

**What is fitted:** `pc386_system` with `x87_present` tied high **and**
`X87_BACKEND = 1`, on Agilex 5, Quartus Pro 26.3, at the 61.44 MHz headline,
through the **Yosys SEAL netlist** front end — which the packed-array
workaround for DeanoC/yosys#79 has just opened to this configuration, and
which is the front end the sibling measurement's M and M+ are taken on.
Direct SystemVerilog is not attempted: `fes_i386_l1i`'s `ramstyle = "M10K"`
stops Quartus Pro before elaboration, and a run spent on it would answer
nothing.

**Envelope and reporting conventions are the sibling measurement's, verbatim
and not reconstructed**, because comparability with its M and M+ is the whole
point of this fit and a different envelope destroys it.

### Exactly what is fitted

Labelled by design and sha, never as "the integration fit", because a second
fit was authorised alongside it and in a week nobody would be able to tell the
two numbers apart:

| | This fit | The sibling's second fit |
| --- | --- | --- |
| Design | `pc386_system`, `x87_present = 1'b1`, **`X87_BACKEND = 1`** | `pc386_system`, `x87_present = 1'b1` |
| Base | this branch at `79f34caa8` | PR #1889's head, `5f901426e` |
| Arithmetic | the **DE25 pipelined backend** | the **slice 3 + 4 sequencer** |
| Question | does the DE25 backend fit in the machine | does the design the DE10 actually refused fit on Agilex 5 |

This fit's two changes to `pc386_system.sv` are a **scratch commit in its own
worktree, not merged RTL**, following the 8 November area audit's precedent:
the identity-on wire (exactly as the sibling's M+ does it) and
`.X87_BACKEND(1)` on the `fes_i386_cpu` instantiation.

**It runs the sibling's harness, not this record's** —
`fit_i386_device.py --design machine --device de25 --front-end netlist` with
its `fit_timing.tcl`, from its branch. That removes the envelope difference
rather than managing it: the same virtual-pin set, the same two-clock
asynchronous SDC, the same cut-rather-than-constrained virtual-port paths and
the same report rows. This record's own harness constrains those paths at
zero, which would have made the number incomparable with M and M+ — the exact
confound this fit exists to avoid.

Its pinned-firmware guard fired on the first attempt, as its method says it
would; the firmware build is the sibling's, after checking that
`cores/fes-pc386/firmware.lock` is byte-identical on both branches.

### The bands

1. **Fitter refuses, or LABs > 100%** → the backend **does not fit** in the
   machine. The same class of result as #1889's refusal. Report the LABs
   needed.
2. **Fits, LABs ≤ 100%, timing PASS at all four corners** → it **fits and
   closes** at 61.44 MHz. Report LAB% and ALM%.
3. **Fits, LABs ≤ 100%, timing FAILS at any corner** → **"fits but does not
   close"**. This is **not a pass** and must not be reported as one. Give the
   worst slack and the corner.
4. **LABs ≥ 99.5% in band 2 or 3** → **not established**, pending more seeds.
   One seed at that occupancy tells you about one placement, and M+ at 98.08%
   already shows how little room a seed-to-seed spread needs to swallow.

A further reading the coordinating session attached, which band 4 exists for:
**nothing above 79.6% has been fitted on Agilex 5 at all**, and on the DE10
lane the practical ceiling is near 92% with timing misses at 94.7%. If this
fit lands between 92% and 100% the honest reading is band 4, not a pass.

### The expected outcome: **none**

**No outcome is predicted, and that is deliberate.** An earlier version of
this section was to record band 1, refusal, as expected, on the inference that
this work needed roughly 614 LABs against 256 available. **That inference is
withdrawn outright**, not merely loosened: it converted ALMs to LABs at the
M+ − M delta's density of 10.6 ALMs per LAB, and that is not a rate at which
new LABs get consumed. The LAB budget under it was wrong too — a LAB holds
exactly ten ALMs on both devices and counts as used the moment **one** of them
is, so LAB occupancy measures how sparsely the fitter spread a design rather
than how full the device is. M+ touches 98% of LABs with about a fifth of
their slots free, and the same CPU netlist measured 3,512 LABs under one
envelope and 3,626 under another — a 114-LAB swing on identical logic, larger
than the "90 LABs remaining" the whole inference turned on. #1889's 102.4% is
LABs *required* pre-placement, a row the Agilex fits report nowhere, so the two
numbers were never on one scale.

**The corrected budget is in ALMs:** the machine with main's 387 on takes
37,255 of 46,800, leaving **9,545 ALMs, about 20%**. Against that there is one
measured line — PR #1892's backend at 2,660 ALMs — and two unmeasured ones:
the rest of a complete 387 (the remainder on the backend, scale,
round-to-integer, the transcendentals, BCD) and a **DE25 board shell**, which
nobody has fitted and which the machine fits stand in for with 651 ALMs of
virtual-pin cells.

So the question is **genuinely open**: the measured budget admits the backend
with room to spare, and the two unmeasured lines are precisely the ones that
could consume it. **A prediction retrofitted to the new budget would be worth
less than no prediction**, so none is recorded. What would decide it either
way is this fit.

**Reporting convention, in this record and the sibling's alike:** the **ALM
figure is the capacity number**, and the LAB row sits beside it **labelled as
spread rather than fullness**, with the reason.

## What this method does *not* claim

- No kit run, no seal, no package and no game acceptance.
- The host x87 is a modern one, not a 387. Where the two differ the case is
  read against the 387 Programmer's Reference Manual. The eleven
  hardware-settled rules slices 3 and 4 measured rest on that same modern
  part; no 387 was available, which the plan record carries as an open
  question and this work does not close.
- The locked-Yosys netlist bench's applicability to the DE25 configuration is
  reported as a finding, not treated as a pass rule for this work. *(When this
  was written the DE25 configuration failed it; the cause was a Yosys
  miscompile of an unpacked struct array, and a packed array fixes it, so both
  configurations now pass. Amended 16 Nov 2026, after the unit netlist run
  that found the 240 failures and the one that passed 763 of 763.)*

## Already measured when this was written

Stated so that the rules above cannot be read as fitted to results:

- The split of `fes_x87_core` out of `fes_x87_pipe` re-ran PR #1892's own
  bench: **368,090 checks, zero failures**, the same figure its record gives.
- The unit bench with the DE25 backend first failed **240 of 740** cases, all
  one defect: an exact cancellation to zero, which `fes_x87_round` packs by
  clearing the exponent field and `A_RND` did not. After the fix, **760 of
  760 pass in both configurations**, including the twenty new back-to-back
  pair cases.
- The DE10 configuration passes the locked-Yosys netlist bench, **760 of
  760**. The DE25 configuration does not, and that divergence was being
  characterised when this was written.
- No lockstep run, no Quartus fit and no area figure had been produced.

## Amendments after measuring began

Each is dated and says what had already been seen, because a rule changed
after a result is worthless unless the change is auditable.

- **Rule 7's exclusion evidence**, 16 Nov 2026, above.
- **The FPREM threshold**, 16 Nov 2026. The decision to keep FRNDINT, FSCALE,
  FXTRACT, FPREM and FPREM1 on the sequencer rests on the argument that they
  cost no arithmetic hardware the envelope does not already carry. For FPREM
  that argument is **unverified and may be false**: `P_SH` and `P_FIN` use
  `dv_rem` and a wide subtractor the slice 2 envelope did not have. It is
  settled by an A/B fit with those arms held off, against the same seeds. The
  rule is committed here **before either arm is fit**, verbatim as the
  coordinating session set it:

  > Fit both arms over the *same* seed set (1–4). For each arm take the median
  > ALM count and the within-arm spread (max − min). Let S = the larger of the
  > two arms' spreads. The A/B delta is **measurable only if
  > |median(A) − median(B)| > S**. Otherwise the result is recorded as **not
  > measurable** — not "cheap", not "no cost", not "within noise so effectively
  > free". Those are different claims and only the first is supported.

  This is safe to commit unseen because it fixes the *comparison*, not a
  threshold: S is computed from the same fits that produce the medians, so
  there is no later choice for a result to influence. **Both arms' per-seed
  counts go in the record, not only the medians**, and in **LABs as well as
  ALMs** — if the delta lands inside
  S, the per-seed table is what lets someone else see how far inside, which
  "not measurable" alone throws away.

  If the delta is material, the form **stops** in the DE25 configuration —
  through decode and the lockstep SCOPE, not by disabling the sequencer arms —
  since the census finds no title in the corpus that executes FPREM, FPREM1 or
  FXTRACT at all. If it is not measurable, **that does not retroactively
  justify keeping the forms on cost grounds.** The reason to keep them is that
  FPREM reuses the divide tail's primitive rather than adding a divider, which
  is an argument about structure and stands or falls on its own; a null A/B is
  not support for it.

  **Arm B is defined here, before it is fit**, because "hold the FPREM arms
  off" is not one thing. Arm A is the delivered design. Arm B is a scratch
  commit in its own worktree — not merged RTL, following the precedent of the
  8 November area audit's scratch fits — in which **both** of these are held
  off by the constant `X87_BACKEND == 0`:

  - the `P_SH`, `P_FIN`, `P_ZERO` and `P_PACK` case arms, which are the
    remainder's doubling and conditional-subtract steps; **and**
  - the `cur == X87_PREM` branch inside `A_OP`, which is what *writes*
    `dv_rem`.

  Holding only the `P_*` arms off would leave `dv_rem`'s writers alive and
  measure nothing, which is the trap: `dv_rem` is written from `A_OP` at two
  sites as well as from the `P_*` arms. The check that arm B is really arm B
  is that **`dv_rem` disappears from its `registers.txt`** while arm A's holds
  it — the same exact-leaf-name query rule 7 uses, which also makes this the
  first demonstration that that query can detect something rather than only
  report zero. Seeds 1–4 on both arms, at the 61.44 MHz headline. What had been seen when this was written: no fit had
  completed and no ALM figure existed.
- **The lockstep count's depth**, 16 Nov 2026. Rule 4 says every reachable
  `req_op` must be non-zero. A provisional run met it (ADD 8, SUB 10, MUL 12,
  DIV 16, SQRT 2, 48 in all) and that is a thin gate for the one thing this
  configuration changes. The rule is **not** narrowed and the figure is not
  reported as adequate; directed cases were added to the x87 suite to raise
  the count instead. What had been seen: that provisional run, on a superseded
  commit.
