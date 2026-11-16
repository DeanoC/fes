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
   - the DE25 fit **building at all**, which is possible only if both seed
     tables are gone: they are declared `ramstyle = "M10K"`, a Cyclone V block
     Agilex 5 does not have, so a surviving table makes Quartus refuse;
   - a query of the fitted netlist for the inline arithmetic's named registers
     — `ua_add`, `ub_add`, `qy_s` and the divide's — expecting none, with `wm`
     and `pa` as a positive control so that a zero means absent rather than
     renamed.
   **No cross-device ratio against the DE10 figures**: Cyclone V and Agilex 5
   ALMs are different units.

## What this method does *not* claim

- No kit run, no seal, no package and no game acceptance.
- The host x87 is a modern one, not a 387. Where the two differ the case is
  read against the 387 Programmer's Reference Manual. The eleven
  hardware-settled rules slices 3 and 4 measured rest on that same modern
  part; no 387 was available, which the plan record carries as an open
  question and this work does not close.
- The locked-Yosys netlist bench is a **DE10** gate. The DE25 flow is Quartus
  Pro reading SystemVerilog directly, with no Yosys in it. Whether the DE25
  configuration passes that gate is reported as a finding, not treated as a
  pass rule for this work.

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
  threshold is committed here **before** that fit is run: a delta that falls
  **inside the seed 1–4 ALM spread is not measurable**, and is reported as
  such rather than as cheap; a delta larger than that spread is material, and
  since the census finds no title in the corpus that executes FPREM, FPREM1 or
  FXTRACT at all, a material cost means the form stops in the DE25
  configuration — through decode and the lockstep SCOPE, not by disabling the
  sequencer arms. What had been seen when this was written: no fit had
  completed and no ALM figure existed.
- **The lockstep count's depth**, 16 Nov 2026. Rule 4 says every reachable
  `req_op` must be non-zero. A provisional run met it (ADD 8, SUB 10, MUL 12,
  DIV 16, SQRT 2, 48 in all) and that is a thin gate for the one thing this
  configuration changes. The rule is **not** narrowed and the figure is not
  reported as adequate; directed cases were added to the x87 suite to raise
  the count instead. What had been seen: that provisional run, on a superseded
  commit.
