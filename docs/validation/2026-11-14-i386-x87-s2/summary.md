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
- **ESC instructions run through the CPU with a 387 fitted, and all eight
  directed programs pass.** The 387's whole state -- the words, the tag
  word, the opcode, the pointers and the eight registers -- matches the
  oracle's at every retirement, across the constants and the stack moves,
  the loads with FXAM and FTST, the stores of every format, the control and
  status words, FNSTENV and FLDENV in both operand sizes, FNSAVE and FRSTOR
  with TOP moved, the stack faults, and WAIT. Getting there, with Codex's
  review, took twelve more faults
  ([below](#what-the-identity-on-run-found)), two of them edits of mine
  that had silently not applied: the pointer-clear pulse that was never
  deasserted, which is why the instruction pointer read zero in every
  program, and the CPU's ERROR# that never reached the slave PIC's IRQ 13.
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
| the pointer-clear pulse was never deasserted, an edit of mine that silently had not applied | after the first FINIT every commit cleared the pointers, which is why the instruction pointer read zero in every program (Codex P1) |
| and then it arrived a cycle after the commit, where the CPU keeps the pointers | unless something else committed in that cycle the clear was lost; the unit says so in the commit's own cycle now |
| the CPU's ERROR# never reached the slave PIC's IRQ 13, another edit that had not applied | a guest handler could never clear the board's latch, and the record already claimed it was wired (Codex P2) |
| a second no-wait form was silently dropped, and a dropped one's commit ran the younger instruction's path | FNINIT and FNCLEX skip the board's latch, not the unit, so one behind another waits (Codex P2) |
| FLDENV and FRSTOR load the control word before the status word, so the exception summary could rise on the old flags part way through and latch BUSY# for nothing | the latch waits for the whole environment, and its apply sets it if ES is still active (Codex P1) |
| with CR0.EM or CR0.TS set, an ESC form a later slice builds stopped as not built instead of raising #NM | lazy context switching relies on #NM coming first (Codex P2) |
| a store's own first uop waited for the unit, and a store's conversion makes the unit busy before that uop issues | the store forms hung; a first uop now waits for the unit only while the unit has not taken it |
| a 32-bit environment's reserved halves read zero | they are all ones, with a 387 as without one, which is what the oracle writes |
| the uop carried the x87 flag whatever the identity, so this slice's pointer restore ran with no 387 fitted | six of the 1,933 no-387 directed cases regressed, FLDENV and FRSTOR in each environment: with the identity clear the flag is clear too, so those forms load and discard as the empty socket does |

## Checks

Every gate below is of **`bf581e303`**, the branch's frozen head: one run,
in the order the seal needs, on `chip-30x0-ai-dev` with CUDA
(`/data/x87-s2-fits/gates3.log`).

| Check | Where | Result |
| --- | --- | --- |
| `test_fes_i386_x87_unit.py` (84 cases: loads, stores, FXAM, FTST, the constants, the state forms, the protocol, the holds) | chip, the lock's Verilator | **84 passed, 0 failed** |
| the same with `--netlist`, the seal's Yosys netlist of the unit | chip | **84 passed, 0 failed** |
| the lockstep on the RTL, every bucket | chip | **0 fail**: the 1,933 no-387 directed cases (5,355 ESC instructions under the no-387 rule), the 8 identity-on programs with the 387's whole state compared at 13 retirements, unreal, protected mode, paging, tasks, debug and the undefined sweep |
| the lockstep on the locked-Yosys netlist, the same buckets | chip | **0 fail**, the same counts |
| the lockstep at governor preset 3, `--same-as` the max run | chip | **0 fail**, 16 retirements compared; every bucket agrees with the max run |
| `test386.asm` on the RTL | chip | POST 00 … E0 EE **FF**, 79,641,629 steps, no EE difference |
| the Quartus 17.0.2 seal of the frozen head | chip, CUDA | **passed**, package `25ba71ce`, below |
| the two machine analysis fits, the identity overlaid on and left off | chip | below |
| a kit run on that seal | Kit B | below |
| `tests.test_pc386_machine` (82 tests: the whole machine simulated with the unit in it) | Powerboat | OK |
| `tests.test_fes_i386_x87_unit`, `tests.test_fes_i386_ucode`, `tests.test_build_fes_pc386_quartus`, `tests.test_audit_fes_i386_case`, `tests.test_quartus_fes_i386` | Powerboat | OK, 145 tests with the machine's |
| Verilator `-Wall` lint (the lock's 5.054) | Powerboat and chip | clean |

The identity-off lockstep is the regression this slice turns on: with the
identity clear the CPU must be the empty socket it was, and it is, across
the random, protected-mode, task, debug and undefined programs and the
per-encoding unbuilt sweep.

**The head before this one failed one gate.** On `bb7b10c0c` the lockstep at
governor preset 3 deadlocked at FNINIT: a no-wait control form took the one
no-wait slot and then waited on it, and only its own commit clears it. Without
the governor it issued in the same cycle, so nothing showed. `bf581e303`'s
`nw_waiting` is the fix and the last row of the fault table above; every gate
was re-run from the start on it, and the seal recorded here is of that head.

## Area and timing

Standalone fits of the unit in the timing envelope, Quartus 17.0.2 at the
seal's settings, 61.44 MHz. The envelope's stimulus and signature are about
90 ALMs of these totals.

| Variant | Seed | ALMs | % of the device | DSP | M10K | CPU setup (ns) | CPU hold (ns) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `unit` | 1 | 1,888 | 4.5% | 9 | 4 | **+0.656** | +0.085 |
| `unit` | 2 | 1,891 | 4.5% | 9 | 4 | **+0.515** | +0.042 |
| `unit-noregs` | 1 | 1,757 | 4.2% | 9 | 4 | **+1.451** | +0.049 |
| `unit-noregs` | 2 | 1,755 | 4.2% | 9 | 4 | **+1.243** | +0.068 |

These are of `bf581e303`, the frozen head, so they include the protocol
fixes and the pointer restore. Both variants meet the clock on both seeds.
`unit-noregs` keeps +1.2 to +1.5 ns, the margin the slice-1 spike's option A
had standalone (+1.7 to +1.9); `unit` is tighter at +0.5 to +0.7 because the
envelope reads all eight registers out combinationally, which the sealed
machine does not.
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
| `bf581e303`, the frozen head | 1,888 / 1,891 | +0.656 / +0.515 | 1,757 / 1,755 | +1.451 / +1.243 |

The first build's worst path ran from the register stack's M10K output
through the shift, a 65-bit adder's carry chain and the range compares into
the stored value: 18 levels of logic and 21.0 ns against a 16.3 ns period.
The second's ran from the same output through the class decode, the
exponent subtract and the shift amount into the multiplier's one-hot
multiplicand, 9 levels.

### The seal

The Quartus 17.0.2 seal of `bf581e303`, built on `chip-30x0-ai-dev` from a
clean checkout of that commit with `--gpu-router CUDA` and the full lockstep
`--netlist` report in front of it, as #1782 requires:

| | |
| --- | --- |
| package | `25ba71ceb4b2ae22fd5f4d7ccd6521767d5795fb9a6564643a5c13398e059fc6` |
| `BUILD_ID` | `e48f617bcd2df180070ba4c18625e023` |
| `core.rbf` | `b01d1663de9313a780bce1f1ba790101cf651243ba6747aa981c2c0d855a1595` |
| ALMs | 36,406 of 41,910 (86.9%) |
| DSP / M10K / registers | 40 / 274 / 32,727 |
| system clock | 61.44 MHz, setup **+0.588 ns**, hold +0.115, pulse width +1.122, recovery +6.542, removal +0.258, every analysis 0 ns TNS |
| pixel / audio clocks | 74.27 MHz / 12.29 MHz, both met |
| the lockstep report it carries | netlist `39f9a82b`, report `d8324c44`, Verilator 5.054 of the i386 lock, Yosys `e240a465` |

Main's last pc386 seal is 36,803 ALMs, but it was taken before the
keyboard's M10K win (#1802, −796) landed, so it is not this seal's
counterpart. The like-for-like comparison is the pair of analysis fits
below, which run main and this branch through one flow.

### The machine, measured

A seal cannot measure this slice: the package ties the identity to `1'b0`
until slice 5, so Quartus trims most of the unit out and the seal's delta
against main says little. The measurement is the pair of machine analysis
fits, with the identity overlaid to `1'b1` and left at `1'b0`
(`quartus_x87_spike.py --mode machine --variant base [--identity-on]`),
Quartus 17.0.2 at the seal's settings, seed 1. `origin/main` is fitted
through the same flow in a clean worktree, so the cost against main is
measured rather than estimated:

| The machine | ALMs | % of device | vs the 92% ceiling | DSP | M10K | CPU setup (ns) | CPU hold (ns) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `origin/main` (`53681975f`), the same flow | 36,625 | 87.4% | −1,932 | 40 | 274 | **+0.435** | +0.074 |
| this branch, the identity clear, as the package has it | 36,532 | 87.2% | −2,025 | 40 | 274 | **+0.558** | +0.090 |
| this branch, the identity set, so the unit is kept | 38,118 | 91.0% | **−439** | 49 | 278 | **+0.804** | +0.072 |

- **Against main the unit costs +1,493 ALMs, 9 DSP blocks and 4 M10K**, and
  the machine meets the clock in all three fits.
- **With the identity clear this branch is 93 ALMs smaller than main**, which
  is inside the flow's own seed-to-seed spread: tying the identity to `1'b0`
  really does remove the unit, and the changes that are not behind it — the
  shared FNSAVE and FRSTOR microcode, the sequencer's offsets, the wider
  part field — cost nothing measurable.
- At 38,118 the machine with a 387 fitted is **439 ALMs under the 92%
  ceiling**, and its margin is +0.804 ns against main's +0.435 in the same
  flow.

### What this says about the budget

This is the plan's "stop and report" case.

- The [fit spike](../2026-11-12-i386-x87-fit-spike/summary.md) measured its
  whole option-A unit, slow path and all, at **2,134 ALMs** standalone (mean
  of the fits that routed), and **+1,687** inside pc386.
- This slice's unit, which is the interface, the state and the conversions
  and **no arithmetic at all**, measures **1,757**: 82% of that figure.
- The spike's unit was built to be fitted, not run: its microcode,
  dispatch, constant and seed ROMs held a hash of the address and its
  conversions were structural. The real conversions, their rounding and the
  environment and save layouts are what the difference buys.
- The machine fits put it beyond doubt: against main, this slice alone adds
  **+1,493** ALMs, where the spike predicted **+1,687** for the whole of
  option A, slow path and all.
- So slices 3 to 5 — the 68-bit add and subtract, compare, the Goldschmidt
  divide and square root with their seed ROMs, FPREM, and the
  transcendentals with their microcode ROM and constants — have **439 ALMs**
  of headroom under the ceiling, and the spike's own standalone figures put
  the slow path alone above that.
- Against the ceiling: main fits at **36,625** in this flow and the 92%
  working ceiling is 38,557, so main itself has 1,932 ALMs of room and this
  slice takes 1,493 of them. OPL2's MLAB win
  ([#1795](https://github.com/DeanoC/fes/pull/1795), about −160) is not
  merged yet and would buy a little of it back.
- What to do about it is Deano's call, and the spike's own
  [open questions](../2026-11-12-i386-x87-fit-spike/summary.md#open-questions-for-deano)
  list the options: the audit's remaining wins, a second core variant
  (`fes.pc386-387`), or a seal above 92%. Nothing here trims the
  architecture to make room.

### The kit run

Kit B, on the sealed package `25ba71ce` itself, core-only: nothing installed,
no image or boot-config change, the baseline claimed in
`~/kit-status/kitB-STATUS.md` before each pass and verified restored after it
(bundle `7ddb0bfb` active, image `179affbb`, boot id unchanged, the run's
FogCast data trees removed). The driver is
`~/fes-scratch/x87s2/kit/run-x87.sh` around `hil_x87.py`, a copy of the
keyboard slice's, on the user-supplied FreeDOS drive `a6977555` (never
committed). **The identity is clear in this package**, so the run is a
no-regression check of the machine around the unit, not a check of the 387;
the 387's own behaviour is the unit's bench and the lockstep.

| Pass | What it did | Result |
| --- | --- | --- |
| x1 | boot, `echo X87:Ok! [1@#]`, a held key's typematic, Keen 4 | FreeDOS boots, the prompt echoes every character including the shifted ones, Keen 4 launches and its menus take input; the game itself was not reached, the sequence stayed in the menus |
| x2 | the same, then Keen 4 played, then a second boot for Wolfenstein 3D | Keen 4 reaches the world map and Keen moves; Wolfenstein's Options menu takes input |
| x3 | Wolfenstein 3D from a fresh boot | its episode and difficulty screens take input |
| x4 | Wolfenstein 3D from a fresh boot, a grab at every menu step | **Floor 1, health 100%, and the view moves and turns** |

Four boots of FreeDOS, two games played, the keyboard right at the DOS
prompt and in both games, and the kit logged free on its baseline after
every pass. The frames are in `~/fes-scratch/x87s2/kit/runs/x1..x4/fr`.

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

- **The lockstep's 387 coverage is the eight directed programs.** The rule
  is in and they pass, but the generator still emits no ESC instruction, as
  slice 0's record notes, so with the identity on everything outside those
  programs leaves the 387 alone. Wider coverage comes from the generator
  emitting the slice's forms, or from the no-387 directed cases
  (`run_no387_cases`) run again with the identity on, where the expectation
  comes from the oracle rather than the socket's hand-computed image. Doing
  that pulls in the 387's state after a CPU reset, the open
  `cpu-reset-387-state` divergence (Bochs leaves CW 0040h with every tag
  valid; this unit resets as FINIT does, 037Fh with every tag empty): either
  the bench starts each program with FNINIT on both sides, as these eight
  do, or it adopts one side's reset state. **TestFloat's conversion vectors**,
  which the plan's gate names, are the other piece: the conversions here are
  checked against an x86-64 host's own x87 over the PRM's operand classes,
  random bits and all four rounding modes, which is the same oracle, but the
  vectors as guest programs are a scope of their own.
- **The identity's record word and FogCast setting**, which slice 0 left to
  this slice. It is not in this PR on purpose: the setting words 0-6 are a
  seven-decimal wire format in `FogCast/protocol/pc386.go` with generated
  constants, a `fes.pc386.machine` contract, a parser, the kit software and
  the mister-packages document behind it, so adding one is a cross-stack
  contract change and its own scope, not part of an RTL slice. Until it
  lands a kit can only run the sealed package with the identity off, which
  is what this slice's kit run checks; the 387's own behaviour is covered
  by the unit's bench and, once it has programs, the lockstep.
- **Exception 9 is not built, and it is in this slice's scope.** It is
  raised when a coprocessor operand's middle part crosses into a
  not-present or protected page or segment: the transfer has begun, so the
  387 is partly updated and the fault cannot be restarted. Nothing this
  slice runs reaches it — the lockstep's x87 programs use flat, present
  memory, and the bench drives the unit's interface rather than the memory
  path — so it is stated here rather than built blind. What it needs is a
  directed lockstep case that straddles a page boundary with the second
  page absent, and a decision on what the unit leaves behind when it faults
  part way, which is also the `divergences.toml` entry that case would
  settle (a real 387 keeps what the bus transferred; Bochs applies the
  operand at once). It is the one named piece of the slice that is not in
  the PR.

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
