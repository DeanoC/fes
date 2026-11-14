# i386 x87 slice 2: the 387's state, interface and conversions, 14 November 2026

Slice 2 of the [x87 plan](../2026-11-12-i386-x87-plan/summary.md#slices)
([#1768](https://github.com/DeanoC/fes/issues/1768)) builds the 387's unit
inside `fes_i386_cpu`: its state, its interface to the CPU, the AT board's
error path, and the loads, stores and register forms the slice names.
Arithmetic is slices 3 to 5. Deano's decisions on #1768 shape it: everything
in the fabric, a nine-DSP multiply, and the shifts done as multiplies.

This record covers the unit and its verification. **It does not yet cover the
whole slice:** the CPU side, the machine side, the lockstep's 387-present
rule, the seal and the kit run are not in it. [What is left](#what-is-left)
lists them.

**Result.**
- **The unit is built and checked against a real 80-bit FPU.** An x86-64
  host's FPU is the same 80-bit unit a 387 is, so the bench drives the unit's
  CPU interface and compares every result and status word with the host's own
  instruction. **75 cases pass on the RTL**, and the 70 cases the bench had
  at the time passed on the seal's Yosys netlist of the unit as well. The
  bench found twenty faults, each fixed in the commit that found it.
- **The CPU and the machine are in, and the lockstep says the identity-off
  path is unchanged**: the full run passes every bucket, including the 1,933
  no-387 directed cases, which matters because the shared FNSAVE and FRSTOR
  microcode and the sequencer's offsets changed.
- **The identity-on path runs, and is being brought up.** The first ESC
  instructions through the CPU with a 387 fitted, together with Codex's
  review, found eleven more faults
  ([below](#what-the-identity-on-run-found)), two of them edits of mine
  that had silently not applied: the pointer-clear pulse that was never
  deassserted, which is why the instruction pointer read zero in every
  program, and the CPU's ERROR# that never reached the slave PIC's IRQ 13.
  The directed programs are not all passing yet. **Until they do the slice
  is not mergeable.**
- **The register stack lands in M10K**, as the plan assumed: 4 blocks, two
  copies of eight 80-bit words, with the tag word read from the class bits
  each write stores rather than from the registers.
- **It costs more fabric than the plan's share, and the first fit missed the
  clock.** The numbers are in [Area and timing](#area-and-timing). This is
  the plan's "stop and report" case, not something to trim here.

## Method

- **Tree.** This branch, from main at `53681975f`. The RTL is
  `cores/fes-common/rtl/i386/fes_i386_x87.sv` with its multiply in
  `fes_i386_x87_mul.sv`.
- **Sources.** Intel's 80387 Programmer's Reference Manual 231917-001 (the
  registers and the words in section 2, the operand rules and the exceptions
  in 3.1.7, the RESET state in 4.8.1 and Appendix C.1, the PC/AT interface in
  Appendix F), the 80386 Hardware Reference Manual section 5.1 (the wait for
  BUSY# before every ESC but FNINIT and FNCLEX), the 386 DX Programmer's
  Reference Manual, and IBM's PC AT Technical Reference for ports F0h and
  F1h. The [plan record](../2026-11-12-i386-x87-plan/summary.md#sources) has
  the links.
- **The bench.** `cores/fes-common/sim/i386/x87_unit_tb.cpp`, run by
  `scripts/test_fes_i386_x87_unit.py`, on `chip-30x0-ai-dev` with the i386
  lock's Verilator (5.054) from the CUDA toolchain slot. Each case sets the
  host's control word, clears its status word, runs the one instruction and
  reads the status word back, so every comparison is against a masked run.
  Where the host and a 387 differ the case follows the manual, not the host.
- **The fit.** `scripts/quartus_x87_spike.py --mode standalone --variant unit`
  puts the unit in the slice-1 timing envelope (`spike_x87_unit.sv`) and fits
  it with the seal's settings, Quartus 17.0.2 b602 at 61.44 MHz. `unit` keeps
  the eight registers' readout live; `unit-noregs` trims it, as the sealed
  machine does.

## What the unit is

- **The state.** Eight 80-bit registers in M10K with two read ports, the
  control and status words, and the tag word, which is derived: one empty bit
  a register and the two class bits each write stores. That is what FNSTENV
  stores and what the oracle's accessor compares ("empty stays empty, any
  other from the value", `bochs_oracle.cpp`). A 387 keeps the tag word
  itself, so an FLDENV whose tags disagree with its registers reads back
  differently; that is a divergence to record when FLDENV lands on the CPU.
- **The interface.** One ESC instruction is in flight at a time, so a store
  always reads committed state and a squashed instruction leaves nothing to
  undo. Every architectural change happens at the instruction's commit, as
  the x87 pointers already do. BUSY# is a hold in D, not a freeze on the
  pipeline: the unit holds the next ESC until its own work is done, and a
  load's work finishes after the instruction has retired, as a real 387's
  does with BUSY# still active.
- **The AT error path.** An unmasked exception sets ES, which is ERROR# and
  IRQ 13's level, and latches BUSY# in the same cycle, so an ESC already
  waiting in D cannot slip past it. While latched, every ESC but FNINIT and
  FNCLEX, and WAIT, holds before it runs. Port F0h clears the latch; F1h
  resets the unit to control 037Eh and status 8081h and leaves the latch
  alone, as slice 0's `f1-reset` case has it.
- **The forms.** FLD and FILD of every size, FST, FSTP, FIST and FISTP of
  every size, FLDCW, FNSTCW, FNSTSW, the environment and save images' words,
  FLD ST(i), FST and FSTP ST(i), FXCH, FFREE, FINCSTP, FDECSTP, FNINIT,
  FNCLEX, the seven constants, FCHS, FABS, FTST, FXAM, FNOP, FENI, FDISI,
  FSETPM, and the stack faults. A form a later slice builds reports `unimpl`,
  which becomes the CPU's `FAULT_UNIMPLEMENTED`.
- **No barrel shifter.** A left shift by n is a multiply by 2^n and the
  product's low half; a right shift by n a multiply by 2^(64-n), with the
  product's high half and its low half as the sticky bit. The rounding
  happens at one of four fixed positions, so its own shifts are muxes.
- **Ready for slice 3.** The multiplier has operand registers of its own,
  which is the fit spike's remedy for the one shared operand register its
  standalone fits could not route. The adder's are slice 3's.

## What the bench found

Twelve faults, each fixed in the commit that found it. They are the reason
the bench exists, and they are listed because they say where this kind of
unit goes wrong:

| What | Why it mattered |
| --- | --- |
| BUSY# dropped before the register write landed | the next ESC read the stack before the write |
| a load's normalising shift started before the operand's last part committed | the shift ran on the part before it |
| a store raised the denormal exception | the 387's FST and FIST report invalid, overflow, underflow and precision only |
| an integer store's invalid cases stored nothing | they store the integer indefinite |
| a masked overflow's largest-finite value and its exponent check | the exponent field was built wrong, and the check truncated to 12 bits |
| FNINIT and FNCLEX were taken only when the unit was idle and the latch clear | which is exactly when they are needed: they do not wait for BUSY# |
| a new instruction was not held while the one before it was still worked on | it issued without the unit taking it |
| a squashed instruction left the unit busy | nothing after it could issue |
| a flush after an instruction had retired threw its work away | a retired FILD lost its push |
| a write to port F1h set the board's latch | it must leave it alone |
| the latch set a cycle after ES | an ESC already in D slipped past it |
| the control forms cleared C1, and C1 was wrong for a masked overflow and a masked invalid integer store | FNSTSW and its kin leave the condition codes alone |

## What the sources and the oracle settled

Four questions the plan left to the manuals, and where each was settled.

- **A store raises no denormal exception.** Storing a denormal or a
  pseudo-denormal to a 32- or 64-bit real sets underflow and precision but
  not the denormal flag: the 387's FST and FIST report invalid, overflow,
  underflow and precision only, and the host's own FSTP agrees. FTST's
  operand does raise it, because a compare is an arithmetic operand, and the
  host agrees there too. The unit followed the plan's reading of section
  3.1.7 at first and raised it on stores; the bench caught it.
- **ES is the exception summary, not a bit of its own.** It is any flag that
  is not masked, so it follows every change to the flags and to the control
  word. That is what the oracle has: Bochs re-evaluates
  `status & ~control & exceptions` and sets or clears ES and B together from
  its FLDENV, FRSTOR, FNINIT, FNCLEX and FNSTENV
  (`clear_unmasked_fpu_exception`, `cpu/fpu/ferr.cc`). It is also what the
  387's RESET state says, where IM is clear and IE set so ES and B come out
  set (Appendix C.1). FNSTENV therefore clears ES by masking everything, and
  FLDCW unmasking a pending flag sets it with no special case.
- **A save image holds ST(0) to ST(7), not the physical registers.** The
  387's FSAVE image labels its register area ST(0) upward (387 PRM, the
  FSAVE and FRSTOR image), so the unit's chunk index is TOP plus the
  image's. It moved the physical registers at first, which only shows when
  TOP is away from zero; the bench's FNSAVE with TOP at 5 caught it.
- **The tag word is derived, and a 387's is not.** The unit keeps one empty
  bit a register and reads the other three classes out of the value, which
  is what the oracle's accessor compares ("empty stays empty, any other from
  the value"). A 387 keeps the tag word itself, so an FLDENV whose tags
  disagree with its registers would read back differently on real hardware
  than on either of these. It costs no read of the stack and it matches the
  oracle, so it is what the unit does; nothing in lockstep can see the
  difference.

**No new `divergences.toml` entry is justified yet.** The two candidates are
not exercised by anything this slice runs: the 387's state after a CPU reset
is already the open `cpu-reset-387-state` entry from slice 0, and the x87
directed programs start with FNINIT so they do not depend on it; and a
multi-part operand that faults part way leaves this unit partly updated, as
the coprocessor bus's transfers leave a real 387, where Bochs applies its
operand at once -- but exception 9 is not built yet, so that is a slice
item, not an observed difference. An entry goes in when a run shows one.

## What the identity-on run found

The lockstep's own programs run no ESC at all, so these faults could only
show once the slice's own directed programs went through the CPU. Each was
fixed in the commit that found it.

| What | Why it mattered |
| --- | --- |
| FNINIT and FNCLEX do not wait for BUSY#, so a following ESC was taken before FNINIT committed, and FNINIT's reset then wiped it | the first program's FLD1 lost its push; a new instruction's first uop now waits for a pending no-wait form, which is what a 387 does while FNINIT runs |
| the flag that says the unit's instruction has issued its first uop was cleared in the branch that takes the instruction, and that branch ran later | a load form, which does not hold, takes and issues in one cycle, so the flag stayed clear and the next ESC issued un-taken |
| WAIT took the ESC path, where its opcode is 9Bh's low bits and the byte after it | that usually decodes as a form no slice builds, which killed its hold; WAIT now waits for the latch and the unit's work and no more |
| FLDENV and FRSTOR discarded the environment's pointer fields | the 387's state could not be restored; they are read back now, including the opcode, which only the real-mode and 32-bit protected layouts carry |
| FINIT did not clear the x87 pointers | the 387 PRM's FINIT clears them with the words, and so does FNSAVE |

## Checks

| Check | Where | Result |
| --- | --- | --- |
| `test_fes_i386_x87_unit.py` (70 cases: loads, stores, FXAM, FTST, the constants, the state forms, the protocol) | chip, lock's Verilator | 70 passed, 0 failed |
| the same with `--netlist` (the seal's Yosys netlist of the unit) | chip | 70 passed, 0 failed |
| the lockstep with the identity off, the full run | chip | every bucket 0 fail, the 1,933 no-387 directed cases included; the unbuilt sweep 53,008 encodings, 0 wrong |
| `tests.test_pc386_machine` (82 tests: the whole machine simulated with the unit in it) | Powerboat | OK |
| `tests.test_fes_i386_x87_unit`, `tests.test_fes_i386_ucode`, `tests.test_build_fes_pc386_quartus`, `tests.test_audit_fes_i386_case`, `tests.test_quartus_fes_i386` | Powerboat | OK |
| Verilator `-Wall` lint (the lock's 5.054) | Powerboat and chip | clean |

Not run yet, and owed before this leaves draft:

| Check | Why it is owed |
| --- | --- |
| the unit's bench on the netlist, and the full and netlist lockstep, on the frozen head | the runs above are of earlier heads; the netlist bench was 70 cases, before the five protocol faults and the pointer restore |
| the lockstep's x87 bucket | the first ESC instructions through the CPU with the identity on |
| the lockstep at a governor preset with `--same-as` the max run | the slice changes the issue condition, which the governor also holds |
| test386 to POST FFh | the integer regression |
| a Quartus 17.0.2 seal of the frozen head, and a kit run on it | the slice's own gates |

The identity-off run is the regression this slice turns on: with the
identity clear the CPU must be the empty socket it was, and the short run
says it is, across the random, protected-mode, task, debug and undefined
programs and the per-encoding unbuilt sweep. The full run, the netlist run
and test386 are in flight.

## Area and timing

Standalone fits of the unit in the timing envelope, Quartus 17.0.2 at the
seal's settings, 61.44 MHz. The envelope's stimulus and signature are about
90 ALMs of these totals.

| Variant | Seed | ALMs | % of the device | DSP | M10K | CPU setup (ns) | CPU hold (ns) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `unit` | 1 | 1,878 | 4.5% | 9 | 4 | **+1.358** | +0.060 |
| `unit` | 2 | 1,889 | 4.5% | 9 | 4 | **+1.226** | +0.063 |
| `unit-noregs` | 1 | 1,733 | 4.1% | 9 | 4 | **+1.754** | +0.049 |
| `unit-noregs` | 2 | 1,734 | 4.1% | 9 | 4 | **+1.639** | +0.040 |

These fits are of the head that closed the clock, before the five protocol
faults and the pointer restore; the figures are the unit's shape rather than
its final size, and a fit of the frozen head is owed with the rest.

Both variants meet the clock on both seeds, with +1.2 to +1.8 ns, which is
the margin the slice-1 spike's option A had standalone (+1.7 to +1.9).
The eight registers' readout costs about 145 ALMs and the sealed machine
trims it, so `unit-noregs` is the figure to carry forward.

**The register stack is in M10K.** Four blocks: two copies of eight 80-bit
words, for the unit's two read ports. The tag word costs no read of them.

**It took two rounds to close the clock,** and both rounds cut area as well:

| Head | `unit` ALMs | `unit` setup (ns) | `unit-noregs` ALMs | `unit-noregs` setup (ns) |
| --- | ---: | ---: | ---: | ---: |
| the first build | 2,191 / 2,177 | −7.568 / −6.905 | 2,075 / 2,084 | −5.716 / −5.552 |
| the rounding split in two | 1,926 / 1,927 | −0.661 / +0.366 | 1,803 / — | +0.924 / — |
| the shift's operands registered | 1,878 / 1,889 | +1.358 / +1.226 | 1,733 / 1,734 | +1.754 / +1.639 |

The first build's worst path ran from the register stack's M10K output
through the shift, a 65-bit adder's carry chain and the range compares into
the stored value: 18 levels of logic and 21.0 ns against a 16.3 ns period.
The second's ran from the same output through the class decode, the
exponent subtract and the shift amount into the multiplier's one-hot
multiplicand, 9 levels.

### A seal cannot measure this

The package ties the identity to `1'b0` until slice 5, so Quartus trims the
whole unit out of the machine: a seal of this branch has about main's area,
and its delta says nothing about the slice. The measurement is a machine
analysis fit with the identity overlaid to `1'b1`
(`quartus_x87_spike.py --mode machine --variant base --identity-on`) against
the same fit without the flag; that pair is owed with the gates. The figures
below are the standalone unit's, and the machine number in them is an
extrapolation at the slice-1 spike's standalone-to-machine ratio.

### What this says about the budget

This is the plan's "stop and report" case.

- The [fit spike](../2026-11-12-i386-x87-fit-spike/summary.md) measured its
  whole option-A unit, slow path and all, at **2,134 ALMs** standalone (mean
  of the fits that routed), and **+1,687** inside pc386.
- This slice's unit, which is the interface, the state and the conversions
  and **no arithmetic at all**, measures **1,733**: 81% of that figure.
- The spike's unit was built to be fitted, not run: its microcode,
  dispatch, constant and seed ROMs held a hash of the address and its
  conversions were structural. The real conversions, their rounding and the
  environment and save layouts are what the difference buys.
- So slices 3 to 5 — the 68-bit add and subtract, compare, the Goldschmidt
  divide and square root with their seed ROMs, FPREM, and the
  transcendentals with their microcode ROM and constants — land on top of
  1,733 rather than inside 2,134, and option A will not fit the spike's
  **+1,687** estimate.
- Against the ceiling: main's last pc386 seal is 36,803 ALMs less the
  keyboard's 796 (#1802), about **36,007**; the 92% working ceiling is
  38,557, so about **2,550** are free, and OPL2's MLAB win
  ([#1795](https://github.com/DeanoC/fes/pull/1795), about −160) is not
  merged yet. This slice alone, at the spike's standalone-to-machine ratio
  (1,687/2,045, about 0.82), extrapolates to roughly **1,400** of them. The
  machine fit above replaces that extrapolation with a measurement.
- What to do about it is Deano's call, and the spike's own
  [open questions](../2026-11-12-i386-x87-fit-spike/summary.md#open-questions-for-deano)
  list the options: the audit's remaining wins, a second core variant
  (`fes.pc386-387`), or a seal above 92%. Nothing here trims the
  architecture to make room.

## The CPU and the machine

All of it behind the identity, so with the identity clear every path is the
empty socket's exactly as before.

- **Decode.** `fes_i386_decode` takes the identity and, with it set, gives
  the forms the unit builds: the same accesses as the socket's, with the
  unit's value where the socket gave all ones; real stores where it made
  none (FST, FSTP, FIST and FISTP of every size, in one to three uops);
  FNSTSW AX as a move of the status word; and a form a later slice builds
  stopped as not built, through the package's `x87_decode`, which decode and
  the unit now share.
- **The pipeline.** `fes_i386_cpu` instantiates the unit, drives its issue
  side from the uop at D's head and its commit side from WB, puts its hold
  in D's issue condition beside the other waits, makes an unmasked
  exception's store no-access, and substitutes the unit's words and its
  opcode for the all-ones fields in the environment and save layouts, which
  are otherwise the no-387 microcode's.
- **The save image.** It moves from twenty dwords to each register's two
  dwords and its top word, ten bytes a register, so no chunk straddles two
  registers and the unit needs no byte muxing; `fes_i386_seq` computes those
  offsets and the part each uop moves.
- **The readout.** The CPU reports the 387's words, opcode, registers and
  pointers through `arch_x87_*`, which the lockstep builds `X87State` from
  and the sealed machine leaves unused.
- **The board.** `pc386_at_core` decodes the write-only ports F0h and F1h
  without claiming the cycle, so a read of either still floats FFh as an
  unclaimed port does; `pc386_machine` passes them through; `pc386_system`
  wires them to the CPU and the unit's ERROR# into the slave PIC's IRQ 13.
  The identity stays `1'b0` in the package.

## What is left

- **The lockstep needs x87 programs.** The 387-present rule is in (the
  state accessor, the wait on BUSY#, the per-form not-built rule and the
  comparison at every retirement), but the lockstep's random programs
  contain no ESC instruction, as slice 0's record notes, so with the
  identity on the comparison runs over programs that leave the 387 alone.
  The coverage has to come from the generator emitting the slice's forms,
  or from the no-387 directed cases (`run_no387_cases`) run again with the
  identity on, where the expectation comes from the oracle rather than the
  socket's hand-computed image. Two things come with it: the 387's state
  after a CPU reset, which is the open `cpu-reset-387-state` divergence
  (Bochs leaves CW 0040h with every tag valid; this unit resets as FINIT
  does, 037Fh with every tag empty), so either the bench starts each
  program with FNINIT on both sides or it adopts one side's reset state;
  and TestFloat's conversion vectors, which the plan's gate names.
- **The identity's record word and FogCast setting**, which slice 0 left to
  this slice. It is not in this PR on purpose: the setting words 0-6 are a
  seven-decimal wire format in `FogCast/protocol/pc386.go` with generated
  constants, a `fes.pc386.machine` contract, a parser, the kit software and
  the mister-packages document behind it, so adding one is a cross-stack
  contract change and its own scope, not part of an RTL slice. Until it
  lands a kit can only run the sealed package with the identity off, which
  is what this slice's kit run checks; the 387's own behaviour is covered
  by the unit's bench and, once it has programs, the lockstep.
- exception 9, for a coprocessor operand whose middle part crosses into a
  protected or not-present page or segment;
- CR0.ET following the identity at reset;
- the lockstep's 387-present rule on the RTL and on the netlist, with the
  plan's directed cases, and test386 to POST FFh;
- a Quartus 17.0.2 seal of the head with every analysis met, and its area
  against main;
- a kit run on that seal.

## Reproducing

From `sources/misteross`, on a host with the i386 lock's toolchain slot:

```bash
python3 scripts/test_fes_i386_x87_unit.py --cache-root /abs/fes-toolchain-cache
```

```bash
python3 scripts/test_fes_i386_x87_unit.py --cache-root /abs/fes-toolchain-cache --netlist
```

```bash
QUARTUS_ROOTDIR=/abs/intelFPGA_lite/17.0/quartus python3 scripts/quartus_x87_spike.py --cache-root /abs/fes-toolchain-cache --gpu-router CUDA --mode standalone --variant unit --seed 1 --output /abs/scratch/unit-s1
```
