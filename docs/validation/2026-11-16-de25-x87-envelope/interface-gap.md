# DE25 x87 envelope integration: the interface gap, read from the source

Written before any RTL. Everything below is read out of `origin/main`,
`origin/deano/i386-x87-s3`, `origin/deano/i386-x87-s4-eac381` and
`origin/codex/de25-x87-pipeline` as they stand on 16 Nov 2026. Where I say
*inferred* I have not run anything.

## Summary: three findings that change the brief's scope

1. **The merged envelope has no arithmetic call site at all.** The brief's
   "the DE10 envelope drives a sequencer that multi-passes a shared multiplier
   and expects a result when its state machine says so" is true of the slice 3
   and slice 4 envelope, which is in **two open PRs I am told not to touch**.
   The envelope on `origin/main` is slice 2 only, and in it every arithmetic
   form decodes to `X87_UNIMPL` and is stopped in `fes_i386_decode`.
2. **The backend's response cannot produce an unmasked overflow or underflow
   result.** The brief assigns unmasked exception results to the envelope, but
   the information the envelope needs has already been destroyed inside
   `fes_x87_round`. This is a backend interface change, not adapter work.
3. **Lockstep cannot currently run against a DE25 configuration**, for two
   independent reasons. This is the assumption the whole approach rests on, so
   it is stated loudly below.

---

## 1. What the envelope is, on main

`sources/misteross/cores/fes-common/rtl/i386/fes_i386_x87.sv`, 1,282 lines,
merged. Its port list is a per-uop CPU interface — `iss*` at D with `hold`,
`sval`/`nx` at EX, `wb`/`wb_last` at commit, `flush` — plus the state the CPU
and bench read (`cw_o`, `sw_o`, `tw_o`, `opcode_o`, `busy_o`, `regs_o`,
`clr_pointers`, `error`, `latched`).

It contains, as the brief says: the eight 80-bit registers in two read-port
copies (`rf_a`/`rf_b`), the control word, the derived status word (`es` is
combinational from `fl & ~cw[5:0]`, not a register), the derived tag word
(one `empty` bit plus two stored class bits per register), the instruction and
data pointers' opcode field, the BUSY# latch and both holds, the load/store
conversions, FXCH, the constants, FCHS, FABS, FTST, FXAM, the stack faults,
and the AT error path driving `error` and `latched`.

**It does not contain any arithmetic.** `fes_i386_pkg::x87_e` on main ends at
`X87_XAM`; its comment says "the arithmetic forms join this table in slice 3",
and `x87_decode`'s default is `X87_UNIMPL`, explicitly listing "FBLD, FBSTP and
the arithmetic forms". There is no adder, no `fadd`/`fmul` case, no
pre/post-operation exception split, no ±24576 bias adjust, no
tininess-after-rounding rule. All of that is slice 3 and slice 4.

Measured sizes of `fes_i386_x87.sv`: **main 1,282 lines; `i386-x87-s3` 2,420;
`i386-x87-s4-eac381` 3,409.** PR #1879 adds 1,206 lines to the x87 and 36 to
`fes_i386_decode.sv`; #1889 stacks on #1879's branch. Both are **OPEN**.

### The one arithmetic block the envelope does own, and what it is for

`fes_i386_x87_mul.sv` (37 lines, nine 22x22 DSP products, four clocks) is used
by slice 2 **only as a shifter**, because option A has no barrel shifter: a
left shift by n is a multiply by 2^n taking the low half, a right shift by n a
multiply by 2^(64-n) taking the high half with the low half as sticky
(`shm`/`shs`, lines 646-647). It is driven from the sequencer's `S_SHIFT` ->
`S_MUL` -> `S_POST` path and is used by the **format conversions** — FLD/FILD
of f32/f64/i16/i32/i64 and FST/FIST back out — not by arithmetic.

## 2. What the backend offers

`sources/misteross/boards/de25nano/x87/rtl/` on `codex/de25-x87-pipeline`:
`fes_x87_pipe.sv` (145 lines) over `fes_x87_fast.sv`, `fes_x87_slow.sv`,
`fes_x87_round.sv`, `fes_x87_pkg.sv`.

Interface: `req_valid`/`req_ready` with `req_op` (3 bits), `req_a`, `req_b`
(both `{sign, exp[14:0], explicit significand[63:0]}` — the same 80-bit memory
format the envelope stores), `req_pc`, `req_rc` (CW bits 9:8 and 11:10
directly), `req_tag` (8 bits, opaque); `resp_valid`/`resp_ready` with
`resp_value`, `resp_flags` (`{PE,UE,OE,ZE,DE,IE}`, this operation only),
`resp_cc` (`{C3,C2,C0}`), `resp_incremented` (C1), `resp_tag`.

`req_op`: **ADD, SUB, MUL, DIV, SQRT, ordered compare, unordered compare.**
Seven operations. Encoding 7 is reserved and refused.

Latency: 15 clocks acceptance to response for a fast op, one accepted per
clock; DIV/SQRT are one-at-a-time, 76 clocks dependent at PC64. Flush cancels
everything accepted, including an already offered response.

### What matches, cleanly

- **The operand format is identical.** No repacking: the envelope's `rf_a`
  value is a legal `req_a`.
- **PC and RC are passed as raw CW fields.** The envelope already has `rc`/`pc`
  as `cw[11:10]`/`cw[9:8]`.
- **`flush` maps straight through.** Both mean "the pipeline squashed it".
- **The ordering machinery is free but unused.** The CPU holds one ESC in
  flight (`x87_busy` interlock in `fes_i386_cpu`), so the envelope will issue
  at most one operation at a time. `req_tag`, the 16-entry completion RAM and
  the in-order guarantee are all dead weight for this consumer — which makes
  the adaptation *easier*, not harder: the backend degenerates to a
  valid/ready engine with a known latency. Area cost of the unused queue is
  measured below as an open item.

### What does not match

**(a) Three operations are in neither the envelope nor the backend.** Slice 3
built FRNDINT, FSCALE and FXTRACT; the backend's own README assigns "remainder,
scale, round-to-integer, transcendentals and BCD" to the adapter. On a main
base none of them exists on either side. They are not arithmetic the backend
can be asked for.

**(b) The conversions have no shifter.** The envelope's shifts go through
`fes_i386_x87_mul`'s raw 128-bit product. The backend's MUL is a
*rounded, masked, 80-bit float* multiply and cannot substitute: it rounds at
PC and cannot return the exact low half as a sticky bit. So either
`fes_i386_x87_mul` comes across as-is (9 more DSPs on top of the backend's 9),
or the DE25 envelope gets a real barrel shifter — which on Agilex 5 is
plausibly the cheaper and much simpler choice, since "shifts are multiplies"
was a DE10 area workaround, not a design preference.

**(c) The unmasked overflow and underflow results are not recoverable from the
response.** This is the one that needs a backend change. Reading
`fes_x87_round.sv`:

- On overflow (`exponent >= 32767`) the rounder **substitutes** infinity or
  the largest finite value and sets OE+PE. The true unbounded exponent is
  gone. An unmasked OE must deliver the rounded significand with the exponent
  biased **down by 24576**; the adapter cannot compute it, because it never
  sees that exponent.
- On underflow the rounder **denormalizes before rounding**
  (`if (raw.exponent < 1) denorm.significand <= shr_sticky(...)`, exponent
  forced to 1), then rounds. An unmasked UE must deliver the result rounded at
  the **unbounded** exponent, biased **up by 24576** — a different rounding of
  a different significand, not a rescaling of the masked answer. Also
  unrecoverable.
- `resp_flags`' UE additionally encodes the **masked** rule (`tiny_s2 &&
  rounded_inexact`), whereas unmasked UE fires on any tiny result. So even the
  flag is the wrong flag for the unmasked case.

The backend is therefore not merely "masked-result only" in the sense of
leaving the sticky status to the caller. It is masked-result in a way that
**erases the unmasked result**. Fixing it means touching `fes_x87_round.sv`
and `fes_x87_pipe.sv` and re-running #1892's bench and its 200 MHz four-corner
fit. My preferred shape, for the coordinator to accept or reject: one extra
request bit ("unbounded"), asserted by the adapter when OM or UM is clear,
which makes the rounder skip denormal packing and the overflow substitution
and apply the ±24576 bias itself — keeping the 18-bit exponent inside the
backend rather than widening the response.

**(d) BUSY#, the commit machinery and the exception latching are written
against the sequencer.** As the brief says. `busy` on main is
`seq != S_IDLE || rw_en`; `hold` is a function of `seq` and `val_form`; every
architectural change happens in `S_APPLY` at `wb_last`. Against the backend
these become "an operation is outstanding" and "the response has arrived",
with the response's own registered-read hazard: `resp_valid` follows a
**registered** RAM read (`output_data <= values[read_ptr]`), which is exactly
the class that has bitten every DE10 slice. ES/ERROR#/BUSY# must be derived
from flags written on the commit edge, and the bench must drive back-to-back
pairs.

## 3. Lockstep: the assumption does not hold yet

Two independent blockers, both measured, not inferred:

**(i) There is nothing for a DE25 envelope to sit inside.** `grep -rl i386
sources/misteross/boards/de25nano/` returns **nothing**. There is no DE25
i386 CPU. The DE25 tree's convention is forked copies under
`boards/de25nano/shared-rtl/`, and the x87 backend already lives under
`boards/de25nano/x87/`. `lockstep_tb.cpp` Verilates `fes_i386_cpu` from
`cores/fes-common`. So a DE25 envelope placed under `de25nano/` has no CPU
wrapper and cannot be reached by lockstep. Reaching it needs either a backend
seam inside fes-common's `fes_i386_x87`/`fes_i386_cpu` — which edits files both
open slices also edit and moves pc386's record closure — or a forked DE25 CPU,
which does not exist.

**(ii) Even with a seam, a slice-2 base exercises no arithmetic.** With
arithmetic stopping at `X87_UNIMPL` in decode, a lockstep run over the DE25
configuration would **pass while issuing zero backend operations**. That is a
green result that proves nothing, which is worse than a red one. The decode and
format tables for the arithmetic forms are in #1879's unmerged
`fes_i386_decode.sv`/`fes_i386_pkg.sv` changes.

Verilator itself is not the problem: the backend is plain SystemVerilog and the
`ramstyle`/`dsp` attributes are inert in simulation, so *mechanically* a
lockstep build can contain it. The problem is reachability, in both senses.

Not yet checked, and I will check before any seal: whether the **locked i386
Yosys** netlist bench accepts the backend's packed structs and `always_comb`
functions. The DE10 lane's netlist gate uses a pinned Yosys; the DE25 flow is
Quartus Pro. Which netlist gate applies here is an open question.

## 4. The three questions I need answered before writing RTL

1. **Which base?** The merged slice-2 envelope has no arithmetic seam and no
   lockstep-reachable arithmetic. The envelope the brief describes is on
   `deano/i386-x87-s4-eac381` (via `deano/i386-x87-s3`), both open. I can
   branch *from* #1889 without touching it, which gives a real seam and real
   lockstep coverage at the cost of stacking on two unmerged PRs; or work from
   main and re-derive slice 3's decode tables, which duplicates #1879; or wait.
   My recommendation: **branch from #1889's head**, and say so in the record.
2. **Fork or seam?** A fork under `boards/de25nano/x87/` follows the DE25 tree's
   convention, touches no DE10 file and moves no pc386 closure — but forfeits
   lockstep, per 3(i). A seam in fes-common gets lockstep and conflicts with
   both open slices. These cannot both be had. Which does Deano want?
3. **Does the backend change go in my PR?** Point 2(c) means unmasked results
   need `fes_x87_round.sv` edited and #1892's bench and 200 MHz fit re-run.
   #1892 is open and not a draft. Mine, or a change asked of #1892?

## 5. Things I will commit to before measuring, once 1-3 are answered

- Fit target and pass rule, fixed in writing before the first fit, and the CPU
  clock the sibling `i386-agilex5-size` session is using, so my figure adds to
  its. I will report raw ALMs / DSPs / M20Ks and percentage of
  `A5EB013BB23BE4SCS`, labelled with device, Quartus Pro version, seed and
  corner set, and **no DE10 ratio**.
- The bench's pass rule, including back-to-back operation pairs as a named
  requirement rather than a nice-to-have.
- Transcendentals and the remainder stop as **not built**, said in the record.
