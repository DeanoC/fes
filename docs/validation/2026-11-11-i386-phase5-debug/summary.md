# i386 Phase 5 slice 5: breakpoints and single step, 11 November 2026

This record covers CPU Phase 5's slice 5
([`i386-cpu-design.md`, "Phase 5: V86, paging and task switches"](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches),
tracker [#1548](https://github.com/DeanoC/fes/issues/1548)). `fes_i386_cpu`
now has DR7's instruction and data breakpoints, RF, DR6's B0-B3, BS and BT
bits, the TSS's T bit and TF's single step. No title in the Phase 5 census
needs them; the compatibility contract does.

**Result:**

RESULT

## Design as built

The RTL README's
[Breakpoints and single step](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#breakpoints-and-single-step)
has the full description, and the design doc's slice 5 lists where it
departs from the plan. In short:
- **The 386, not Bochs, where the two differ.** The sources are the
  80386 Programmer's Reference Manual's chapter 12 and the die-extracted
  microcode ([reenigne/x86_microcode](https://github.com/reenigne/x86_microcode),
  `80386/microcode_notes.txt`):
  - A #DB never clears DR6's bits, it only ORs in its own (manual 12.2.3:
    "the bits of DR6 are never cleared by the processor"; microcode GD_HIT
    3A3, SINGLE_STEP 93F and the task switch's 7E0 are ORs). Bochs's
    `exception()` clears B0-B3, bit 12 and BT first. Slice 2's GD #DB,
    which followed Bochs there, now keeps DR6's bits too.
  - A #DB's EFLAGS image has RF as it is: the microcode's BREAKPOINT_CMN
    goes to INTERRUPT_X (94D) past FAULT's setting of RF (890-899). RF clears
    as an instruction completes (manual 12.3.1.1), so GD's #DB from the
    instruction after an IRETD that set RF pushes RF set. Bochs clears RF
    before an instruction runs, so its image has RF clear. Slice 2's
    `dr-gd-rf` case expected Bochs's; it now expects the 386's.
  - Debug traps of an instruction, then the next instruction's breakpoint,
    then NMI, then INTR (the manual's table 9-2), which Bochs also has.
- **Bochs's where the 386's can't be established.** The microcode's
  SINGLE_STEP and BREAKPOINT entries are requested by hardware (its small
  PLA), so it shows what a #DB does, not which conditions raise one. The
  RTL takes Bochs's: B bits of a disabled breakpoint's match only with a
  #DB at that edge, and none while no breakpoint is enabled; data
  breakpoints on a descriptor's, gate's and TSS's accesses too; LEN 10
  as eight bytes, and LEN other than 00 on an instruction breakpoint as a
  range; RF cleared by POPF and by a 16-bit IRET (the manual's wording
  allows either); a single step pending at a task switch dropped; an
  instruction breakpoint passed over after a MOV SS or POP SS that shadows
  it; data breakpoints always reported exactly (the 386 does so with GE or
  LE set, and may report late without them).
- **Instruction breakpoints** compare the fetched linear address the fetch
  check already carries at AG, on an instruction's first uop. A match with
  a breakpoint enabled stops the uop touching anything from M1 on (no read,
  no page walk, no fault of its own), and EX makes it `FAULT_DBI` while RF
  is clear and no MOV SS shadows it, else a replay after which AG skips the
  breakpoint once.
- **Data breakpoints** compare each uop's accesses at M1, the memory
  operand's and a string store's, with byte masks over the field's 8-byte
  block and the next. The matches gather at WB as uops commit.
- **Traps** (single step, data breakpoints, the T bit) are boundary events
  beside NMI and INTR, ahead of them, and held off only by MOV SS's and POP
  SS's shadow. At an event the CPU takes two cycles to compare the next
  instruction's address with the instruction breakpoints, so one #DB
  reports both, as Bochs's does.
- **The T bit** costs one ROM word: `tssave` reads a 386 TSS's word at 64h
  into T5, and the commit arms the trap. The ROM holds 436 of 512 words.

## Lockstep

LOCKSTEP

## Mutants

MUTANTS

## test386.asm

TEST386

## Seal

SEAL

## Kit

KIT

## What this record does not cover

- **Breakpoints in the random protected-mode and task-switch programs.**
  The debug programs run their blocks in real mode; protected mode, V86
  mode and task switches are covered by the directed cases.
- **GD with TF set.** The 386's GD_HIT ORs only BD into DR6; Bochs adds
  BS. The RTL reports BD alone; no case compares it.
- **HLT under TF.** Bochs waits in HLT for an interrupt before it takes the
  trap; the RTL traps after the HLT. No program halts with TF set.
- **The T bit with the new EIP past CS's limit.** The 386 takes the #DB
  before the new task's first fetch; Bochs raises the #GP within the
  switch. Not compared.
- **A T bit in the task-switch programs.** The programs' #DB vector is a
  task gate to their handler task; a T bit there loops on a 386 too.
- **nextpnr.** No route was made (fes#1235).

## Commands

From `sources/misteross`:

```sh
python3 scripts/test_fes_i386_lockstep.py --jobs 12
python3 scripts/test_fes_i386_lockstep.py --jobs 12 --yosys <the i386 lock's Yosys>
FES_I386_MODE=pmode build/sim/fes-i386-lockstep/rtl/Vfes_i386_cpu   # the protected-mode cases alone
python3 scripts/test_fes_i386_test386.py --cpu rtl --no-cross-check
python3 scripts/build_fes_pc386_quartus.py --root "$PWD" --cache-root "$CACHE_ROOT" --seed 1
```
