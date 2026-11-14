# i386 x87 slice 2: the 387's state, interface and conversions, 14 November 2026

Slice 2 of the [x87 plan](../2026-11-12-i386-x87-plan/summary.md#slices)
([#1768](https://github.com/DeanoC/fes/issues/1768)) builds the 387's unit
inside `fes_i386_cpu`: its state, its interface to the CPU, the AT board's
error path, and the loads, stores and register forms the slice names.
Arithmetic is slices 3 to 5. Deano's decisions on #1768 shape it: everything
in the fabric, a nine-DSP multiply, and the shifts done as multiplies.

This record covers the whole of what the PR builds: the unit and its
verification, the CPU and machine sides, the lockstep's 387-present rule,
the seal and the kit run on it. [What is left](#what-is-left) states what
the slice's scope names and this PR does not have.

**Result.**
- **The unit is built and checked against a real 80-bit FPU.** An x86-64
  host's FPU is the same 80-bit unit a 387 is, so the bench drives the unit's
  CPU interface and compares every result and status word with the host's own
  instruction. **87 cases pass on the RTL**, and all 87 pass on the seal's
  Yosys netlist of the unit as well. The bench found the sixteen faults its
  table [below](#what-the-bench-found) lists, each fixed in the commit that
  found it.
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
  review, took fourteen more faults
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

Sixteen faults, each fixed in the commit that found it. Four came from
Codex's review: three from its review of the clock-closing head
`bf581e303`, fixed in `641f06583`, and the fourth, the FLDCW latch at the
foot of the table, from its review of `e440a62d`, fixed in `06c324ac0`.
Three of the four — `latch-idle-edge`, `fstp-st0-empty` and
`latch-ldcw-unmask` — have a bench case each that fails on the RTL before
the fix and passes after. They are the reason
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
| the BUSY# latch was set a cycle after the unit went idle | an unmasked exception makes the unit idle on the same edge that raises ERROR#, so for one cycle an ESC saw neither busy nor latched and ran with a fault pending. The apply now sets the latch itself rather than leaving it to the ES edge detector (`latch-idle-edge`) |
| `FSTP ST(0)` left the popped register non-empty | the destination's write cleared the empty bit after the pop had set it, so the tag word read `fffe` where FNINIT plus a stack-underflow pop must leave `ffff` (`fstp-st0-empty`) |
| a flush in the same cycle did not cancel a take | D still held the wrong-path ESC that cycle, so taking it would leave `started`, or the no-wait slot, set for an instruction that can never commit |
| FLDCW's own apply did not latch BUSY# | ES is derived from the *registered* control word, so a word that unmasks a flag already set raised ES only the cycle after the apply had made the unit idle, and the edge detector latched a cycle after that. An ESC issued in between with an unmasked exception pending. FLDCW now latches against the word it is loading (387 PRM section 2.3.1); every other apply that writes the control word was checked for the same window and FLDCW was the only one left (`latch-ldcw-unmask`) |

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

Every gate below is of **`06c324ac0`**, the branch's frozen head: one run,
in the order the seal needs, on `chip-30x0-ai-dev` with CUDA
(`/data/x87-s2-fits/gates5.log`). The only commit after it is this record,
which is Markdown, so there are no RTL, recipe, firmware source or ABI
header changes after the sealed revision and the seal and the kit run stand
for the branch head. Two earlier heads had a full run and a seal of their
own and both are superseded. `bf581e303` (seal `25ba71ce`) was superseded
when Codex's review of it found three real RTL faults, and `641f06583`
(seal `ff11f812`) when a review thread on `e440a62d`, which had gone
unanswered, turned out to name a real fourth one: the FLDCW latch. Each
time every gate, the seal and the kit run were redone from the start on the
new head; the pc386 reseal exemption does not cover an i386 seal. The
`ff11f812` seal never got a valid kit run — see the kit section.

| Check | Where | Result |
| --- | --- | --- |
| `test_fes_i386_x87_unit.py` (87 cases: loads, stores, FXAM, FTST, the constants, the state forms, the protocol, the holds) | chip, the lock's Verilator | **87 passed, 0 failed** |
| the same with `--netlist`, the seal's Yosys netlist of the unit | chip | **87 passed, 0 failed** |
| the lockstep on the RTL, every bucket | chip | **0 fail**: the 1,933 no-387 directed cases (5,355 ESC instructions under the no-387 rule), the 8 identity-on programs with the 387's whole state compared at 13 retirements, unreal, protected mode, paging, tasks, debug and the undefined sweep |
| the lockstep on the locked-Yosys netlist, the same buckets | chip | **0 fail**, the same counts |
| the lockstep at governor preset 3, `--same-as` the max run | chip | **0 fail**, 16 retirements compared; every bucket agrees with the max run |
| `test386.asm` on the RTL | chip | POST 00 … E0 EE **FF**, 79,641,629 steps, no EE difference |
| the Quartus 17.0.2 seal of the frozen head | chip, CUDA | **passed**, package `81ab96a1`, below |
| the two machine analysis fits, the identity overlaid on and left off | chip | below |
| a kit run on that seal | Kit B | below |
| `tests.test_pc386_machine` (82 tests: the whole machine simulated with the unit in it) | Powerboat | OK |
| `tests.test_fes_i386_x87_unit`, `tests.test_fes_i386_ucode`, `tests.test_build_fes_pc386_quartus`, `tests.test_audit_fes_i386_case`, `tests.test_quartus_fes_i386` | Powerboat | OK, 145 tests with the machine's |
| the whole `sources/misteross/tests` tree, which is the tree CI's lanes discover and the one this PR's test change is in | Powerboat | OK, **2,193 tests**, 86 skipped |
| Verilator `-Wall` lint (the lock's 5.054), in the bench's own build | chip | clean |

The identity-off lockstep is the regression this slice turns on: with the
identity clear the CPU must be the empty socket it was, and it is, across
the random, protected-mode, task, debug and undefined programs and the
per-encoding unbuilt sweep.

**One earlier head failed a gate.** On `bb7b10c0c` the lockstep at
governor preset 3 deadlocked at FNINIT: a no-wait control form took the one
no-wait slot and then waited on it, and only its own commit clears it. Without
the governor it issued in the same cycle, so nothing showed. `bf581e303`'s
`nw_waiting` is the fix, and every gate was re-run from the start on it. The
three review fixes forced the same thing again on `641f06583`, and the FLDCW
fix once more on `06c324ac0`, which is the head sealed here.

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

These are of `bf581e303`, the clock-closing head, so they include the protocol
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
| `bf581e303`, the clock-closing head | 1,888 / 1,891 | +0.656 / +0.515 | 1,757 / 1,755 | +1.451 / +1.243 |

The first build's worst path ran from the register stack's M10K output
through the shift, a 65-bit adder's carry chain and the range compares into
the stored value: 18 levels of logic and 21.0 ns against a 16.3 ns period.
The second's ran from the same output through the class decode, the
exponent subtract and the shift amount into the multiplier's one-hot
multiplicand, 9 levels.

### The seal

The Quartus 17.0.2 seal of `06c324ac0`, built on `chip-30x0-ai-dev` from a
clean checkout of that commit with `--gpu-router CUDA` and the full lockstep
`--netlist` report in front of it, as #1782 requires:

| | |
| --- | --- |
| package | `81ab96a11ba3d8969fadf19306856c78e77642f5229ec9ef1b47412671d22b45` |
| `BUILD_ID` | `9a960e227f2200d681051b280f4e8264` |
| `core.rbf` | `6b58880a8b892f745ad967d19cc2fcfdc5fda329425228f0ae79d81635bd7361` |
| ALMs | 36,323 of 41,910 (86.7%) |
| DSP / M10K / registers | 40 / 274 / 32,883 |
| system clock | 61.44 MHz, setup **+0.752 ns**, hold +0.072, recovery +6.618, removal +2.463, minimum pulse width +1.162 |
| pixel / audio clocks | 74.27 MHz setup +1.273, hold +0.077, pulse width +1.122 / 12.29 MHz setup +11.798, hold +0.181, removal +0.510 |
| every analysis | the worst slack of each clock over all four corners (Slow and Fast, 100C and -40C), every one positive, 0 ns TNS throughout |
| the lockstep report it carries | netlist `9270e41d`, report `d242bf47`, Verilator 5.054 of the i386 lock, Yosys `e240a465` |

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
| this branch, the identity clear, as the package has it | 36,434 | 86.9% | −2,123 | 40 | 274 | **+0.532** | +0.106 |
| this branch, the identity set, so the unit is kept | 38,233 | 91.2% | **−324** | 49 | 278 | **+0.603** | +0.076 |

- **Against main the unit costs +1,608 ALMs, 9 DSP blocks and 4 M10K**, and
  the machine meets the clock in all three fits.
- **With the identity clear this branch fits 191 ALMs smaller than main**, a
  negative delta, so tying the identity to `1'b0` really does remove the
  unit and the changes that are not behind it — the shared FNSAVE and FRSTOR
  microcode, the sequencer's offsets, the wider part field — cost nothing
  measurable. How small a difference means nothing here is worth stating:
  the seal and this fit are the same RTL at the same seed through two flows
  and differ by 111 ALMs (36,323 against 36,434), so a hundred either way is
  below this measurement's resolution. The 92% ceiling is a device figure
  (38,557 of 41,910) and applies to both, but 38,233 is a fit-flow number,
  so read the 324 with that 111 in mind.
- At 38,233 the machine with a 387 fitted is **324 ALMs under the 92%
  ceiling**, and its margin is +0.603 ns against main's +0.435 in the same
  flow.
- **How firm those two figures are.** The previous head `641f06583` was
  fitted the same way and gave +1,534 against main and 398 under the
  ceiling. The only RTL between the two heads is the FLDCW latch, a
  condition on an existing register, which is a handful of ALMs at most, so
  the 74-ALM difference is this flow's spread between two fits of nearly the
  same design. Read the headroom for slices 3 to 5 as **roughly 300 to 400
  ALMs**, not as an exact 324; the direction, which is that it is a few
  hundred and not a few thousand, is what the numbers settle.

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
  **+1,608** ALMs, where the spike predicted **+1,687** for the whole of
  option A, slow path and all.
- So slices 3 to 5 — the 68-bit add and subtract, compare, the Goldschmidt
  divide and square root with their seed ROMs, FPREM, and the
  transcendentals with their microcode ROM and constants — have **about 324
  ALMs**, call it three to four hundred, of headroom under the ceiling, and
  the spike's own standalone figures put the slow path alone above that.
- Against the ceiling: main fits at **36,625** in this flow and the 92%
  working ceiling is 38,557, so main itself has 1,932 ALMs of room and this
  slice takes 1,608 of them. OPL2's MLAB win
  ([#1795](https://github.com/DeanoC/fes/pull/1795), about −160) is not
  merged yet and would buy a little of it back.
- What to do about it is Deano's call, and the spike's own
  [open questions](../2026-11-12-i386-x87-fit-spike/summary.md#open-questions-for-deano)
  list the options: the audit's remaining wins, a second core variant
  (`fes.pc386-387`), or a seal above 92%. Nothing here trims the
  architecture to make room.

### Where the ALMs go (the per-module audit)

Deano asked for this before slices 3-5 are re-planned. It changes no RTL.
The method is the [area audit's](../2026-11-08-i386-area-audit/summary.md):
the producer's Yosys program without `flatten`, so the fit report lists ALMs
per entity (`quartus_x87_spike.py --hierarchy`), Quartus 17.0.2, seed 1.
Its fits are of `bf581e303`, the head the audit was asked for. The three
review fixes in `641f06583` move the machine by +41 ALMs with the identity
on and −37 with it off, and the FLDCW latch in `06c324ac0` by a further +74
and −61; both are inside this flow's spread, and neither adds a module or a
path, so the breakdown stands.

**In the machine**, the identity overlaid on against off, the whole
difference is three rows; every other module moves by less than 40 ALMs,
which is this flow's noise:

| Entity | identity on | identity off | delta |
| --- | ---: | ---: | ---: |
| `fes_i386_x87`, its own logic | 1,413.6 | 0 | **+1,414** |
| `fes_i386_x87_mul` | 177.6 | 0 | **+178** |
| `fes_i386_cpu`, its own logic | 9,409.0 | 9,266.2 | **+143** |
| the whole machine | 38,170 | 36,529 | +1,641 |

So the CPU-side cost of having a 387 at all — the issue and hold
conditions, the unit's words substituted into the environment and save
layouts, the `arch_x87_*` readout — is **143 ALMs**, and the rest is the
unit. (This flow's delta is 1,641 where the flattened one gives 1,586 and
the comparison against main on the sealed head gives 1,608; the three
differ by about the spread stated above.)

**There is no untrimmable residual.** With the identity off both entities
are 0.0 ALMs in the table. The ~520 ALMs this audit was asked to explain
were an artefact of comparing a fit against an estimated baseline; measured
against a fit of main, the identity-off machine is 191 ALMs *smaller*. The
unit's ERROR# reaching the slave PIC costs nothing when `present` is
constant 0.

**Inside the unit**, by forcing an output constant so its driver chain
trims (scratch edits on a worktree of the frozen head, never committed;
standalone `unit-noregs`, hierarchy kept):

| Fit | `fes_i386_x87` own | registers | M10K | `_mul` | DSP |
| --- | ---: | ---: | ---: | ---: | ---: |
| as built | 1,491.7 | 1,248 | 4 | 179.4 | 9 |
| the register stack's write data forced constant | 1,062.8 | 1,051 | 2 | 179.1 | 9 |
| the value the CPU stores forced constant | 1,405.5 | 1,262 | 4 | 187.4 | 9 |
| both | 791.1 | 958 | 2 | 176.1 | 9 |

- **The load result path is about 429 ALMs**: unpack and classify, the
  normalising shift's control, the rounding, and the pack into 80 bits.
- **The store-only logic and the environment and save image mux is about
  86 ALMs**, which is small because the store reuses the load's shifter and
  rounding rather than carrying its own.
- **About 186 ALMs are shared between the two directions** — they survive
  either ablation alone and go only when both do (429 + 86 = 515 against the
  701 the two together remove).
- **The floor is about 967 ALMs** (791 of the unit plus the multiplier's
  176) and it is not idle logic: it is the eight registers' control, the
  tag, class, control and status words, TOP, the BUSY# latch and the board's
  error path, the issue, hold and no-wait interlocks, the sequencer — **and
  the conversion machinery itself**, because the exception flags IE, DE, OE,
  UE and PE are derived from it and they drive ES, ERROR# and IRQ 13. A unit
  that reports exceptions cannot gate its arithmetic away, so the shifter
  and its nine DSPs survive even with both results dead. That is why the
  DSPs cannot be attributed to one direction.

**What the DSPs and M10K are, in a slice with no arithmetic.**
- The **9 DSPs** are all in `fes_i386_x87_mul`: nine 22x22 products forming
  the 64x64 multiply. They are not arithmetic for the program -- they are
  the **shifter**. Option A has no barrel shifter, so every normalising and
  denormalising shift is a multiply by a one-hot constant. 178 ALMs is the
  logic wrapping them.
- The **4 M10K** are the register stack: two copies of the eight 80-bit
  registers, one per read port, and each copy takes two blocks because an
  M10K port is at most 40 bits wide. The derived tag word is what keeps it
  at two ports rather than three. Forcing the writes constant drops it to
  two blocks, which is the measurement's own confirmation that both copies
  are live.

**What looks dear, and what is inherent.** Nothing in the breakdown is
idle: the three largest pieces are the conversions (429 + 186 shared), the
state and interface, and the shift-as-multiply wrapper. The 1,248 registers
are mostly 80-bit datapath stages added in the two rounds that closed the
clock, and those rounds cut ALMs as well as time, so they are not the place
to look. What the numbers say for slices 3-5 is simply that the interface,
the state and the conversions are **not** a small base on which arithmetic
adds its own cost: at 1,414 + 178 they are already most of what the fit
spike projected for the whole of option A. This record does not propose a
way out; the options are the spike's
[open questions](../2026-11-12-i386-x87-fit-spike/summary.md#open-questions-for-deano).

**A method note worth keeping.** Ablating at the decode — making
`x87_decode` return `X87_UNIMPL` for a group of forms — measures nothing:
three such fits all landed within 8 ALMs of the unmodified unit. The op
reaches the datapath through a register (`cur <= iss_dec`), and neither
Yosys nor Quartus reasons about which values a state register can hold, so
nothing is proved dead. Ablations have to cut at an output, where constant
propagation works.

### The kit run

Kit B, on the sealed package `81ab96a1` itself, core-only: nothing
installed, no image or boot-config change, the baseline claimed in
`~/kit-status/kitB-STATUS.md` before the run and verified restored after it
(bundle `7ddb0bfb` active, image `179affbb`, boot id unchanged, the run's
three FogCast data trees removed, kit logged free). The driver is
`~/fes-scratch/x87s2/kit/run-x87.sh` around `hil_x87.py`, a copy of the
keyboard slice's, on the user-supplied FreeDOS drive `a6977555` (never
committed). **The identity is clear in this package**, so the run is a
no-regression check of the machine around the unit, not a check of the 387;
the 387's own behaviour is the unit's bench and the lockstep.

| Pass | What it did | Result |
| --- | --- | --- |
| x6b | boot FreeDOS from the sealed package, type at the prompt, launch Wolfenstein 3D, step through its menus, then move and turn in the game | **FreeDOS boots to `C:\>` with HimemX, µXMS and CuteMouse loaded; Wolfenstein reaches Floor 1 at health 100% and ammo 8, and the view both moves forward and turns** |

The admission of the package was checked rather than assumed: `result.json`
carries no `error`, the `package` command returned the package id, the load
went `active` with `fes.pc386.machine 1.0` among its interfaces, and every
grab returned `ok`. The frames are in `~/fes-scratch/x87s2/kit/runs/x6b/fr`.

Two earlier kit sessions are superseded and are recorded only so the kit log
reads straight. Passes x1 to x4 ran the same way on the `25ba71ce` seal of
`bf581e303` — four boots, Keen 4 and Wolfenstein both played, the keyboard
right at the prompt and in both games — and pass x5b, on the `ff11f812` seal,
was **void**: it was packed with a plain `tar cf`, which is not the canonical
`.fcore` layout, so the kit refused it at admission with
`HTTP 422 INVALID_ARCHIVE` while the driver still reported `rc=0` and ran
the rest of its script, whose frames therefore say nothing about that seal.
Nothing was installed and the kit never left its baseline. The runner now fails the run
when `result.json` holds an `error`, and the packer builds the archive with
`scripts/core_package.py`'s own USTAR header so the package id matches the
store digest before anything is sent.

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

Three things the slice's scope names are not in this PR. Exception 9 is the
one that would need RTL, and building it here would void this PR's seal and
kit run, so Deano has placed it on its own branch, stacked on this one
(`deano/i386-x87-exc9`), with its own gates, seal and kit run.

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
- **Exception 9 is not built here; it is on its own branch.** It is
  raised when a coprocessor operand's middle part crosses into a
  not-present or protected page or segment: the transfer has begun, so the
  387 is partly updated and the fault cannot be restarted. Nothing this
  slice runs reaches it — the lockstep's x87 programs use flat, present
  memory, and the bench drives the unit's interface rather than the memory
  path — so it is stated here rather than built blind. What it needs is a
  directed lockstep case that straddles a page boundary with the second
  page absent, and a decision on what the unit leaves behind when it faults
  part way, which is also the `divergences.toml` entry that case would
  settle (a real 387 keeps what the bus transferred; Bochs never raises
  vector 9 at all, so the rule is RTL-only and needs a `divergences.toml`
  entry). The 386's own microcode settles when it fires: every ESC memory
  entry point sets `IND = EA + size − 1` before the transfer
  (`IND_DELTA` 9, 7, 3, 1 for m80, mq, md, mw), which is the manual's
  "both ends valid" precondition implemented in hardware, so a violation
  found once the transfer has begun aborts with no error code and no
  restart. It is the one named piece of the slice that is not in this PR.

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
