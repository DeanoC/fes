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
- **Bochs's where the 386's can't be established** (below).
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

## Not established for the 386; follows Bochs

The microcode's SINGLE_STEP (93F) and BREAKPOINT (941) entries are requested
by hardware through its small PLA (`small_pla.txt`), so the microcode shows
what a #DB does, not which conditions raise one; the 386 manual's chapter 12
(12.2.2-12.3.1.5) is silent or allows either reading on each item below. On
11 November Deano confirmed Bochs's behaviour for each ("Yes, Bochs is
fine", relayed by the coordinator). Each is easy to change later; its case
would show the change.

| | Item | What was checked | The RTL (Bochs's) | Lockstep cases |
| --- | --- | --- | --- | --- |
| (a) | RF after POPF, POPFD and a 16-bit IRET | Manual 12.3.1.1 (RF is cleared after every instruction "except IRET, POPF" and task-switching JMP, CALL and INT, which set it from the image); microcode 804 (BITS16 after POPFD's read, which the notes' annotation takes as leaving RF and VM as they were, with a question mark); Bochs's `POPF_Fd` clears RF | RF clears as POPF or a 16-bit IRET completes. RF is cleared as any instruction completes, so for POPF the microcode's reading and Bochs's give the same result. Only the manual's literal reading differs: POPFD loading RF from its image and keeping it, which would pass over the next instruction's breakpoint | `db-popfd-rf`, `iretd-rf-iret16` |
| (b) | A single step pending at a task switch | Manual 12.3.1.4 (TF in the new TSS's image traps after the new task's first instruction; silent on the old task's pending step); microcode 7D0-7E2 (the switch's end takes only the T bit's trap); Bochs's `task_switch` drops the pending step | Dropped: no trap for the JMP, CALL or IRET that switched; the new task's TF steps after its first instruction | `ts-tf-jmp` |
| (c) | Data breakpoints on a descriptor's, gate's or TSS's access | Manual 12.2.4 (a data breakpoint matches "any of the bytes participating in a memory access"; silent on the CPU's own table accesses); Bochs's `system_read_*` and `system_write_*` check a page that holds a data breakpoint as any access | Matched, as any access; the walker's page-table accesses are physical and match none | `db-dbp-gdt` |
| (d) | B bits of a disabled breakpoint's match | Manual 12.2.3 ("the processor sets Bn regardless of whether Gn or Ln is set"; silent on when it is reported); Bochs's `hwdebug_compare` | Reported only with a #DB at that edge, and none while no breakpoint is enabled | `db-ibp`, `db-tf-dbp-disabled`, `db-tf-none-enabled`, `db-dbp-rep-own` |
| (e) | LEN 10, and LEN other than 00 on an instruction breakpoint | Manual 12.2.2 (LEN 10 undefined; an instruction breakpoint's LEN other than 00 undefined) | LEN 10 is eight bytes; an instruction breakpoint's LEN masks its address's low bits | `db-dbp-len8`, `db-ibp-len2` |
| (f) | HLT with TF set | Manual 12.3.1.4 (silent); Bochs waits in HLT for an interrupt before it takes the trap | The RTL traps after the HLT. No program halts with TF set, so this one is not compared | none |

Two more follow Bochs where the manual leaves them open: an instruction
breakpoint on the instruction after a MOV SS or POP SS that shadows it is
passed over (Bochs's `BX_INHIBIT_DEBUG`; `db-ibp-movss`), and every data
breakpoint is reported exactly, as the 386 does with GE or LE set
(12.3.1.2; without them it "may" report late).

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of
2,000 random programs, 16 of 500 protected-mode programs, 16 of 250
task-switch programs, and now 16 of 250 debug programs, and the full corpus)
at `5a74d5598`: this branch with #1684's tip, main `25d5eec67` and the NMI
fix (#1705, below) merged in. It ran on the RTL and on the netlist of the i386 lock's Yosys
(`0a7927bf`), with the lock's Verilator 5.054, on Powerboat. The same runs at
`c0a6a2682`, before those merges, passed too, with the same counts but for
the protected-mode cases (398, the merges add `nmi-in-nmi-handler`,
`ts-nmi-iret-link-null` and this slice's last three):

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (7,977,648 retirements) | same |
| Protected-mode programs | 8,000 of 8,000 (432,695 retirements) | same |
| Task-switch programs | 4,000 of 4,000 (202,130 retirements) | same |
| Debug programs (new) | 4,000 of 4,000 (2,185,253 retirements) | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 403, 77, 1,933 and 27, all pass | same |

All 1,012 trace hashes match between the RTL and the netlist
([lockstep-report.json](lockstep-report.json),
[lockstep-netlist-report.json](lockstep-netlist-report.json)). The random,
protected-mode and task-switch programs' hashes are slice 4's: no program
before this slice arms a breakpoint or sets TF. 
**Debug programs** (`--debug-programs`, seeds from 5001, `FES_I386_DEBUG`):
real-mode random programs with a #DB handler through vector 1 and blocks that
arm instruction and data breakpoints and run stretches under TF (the
lockstep README has the generator). Over 16 seeds they took 70,960 #DBs:
3,378 reporting B bits (instruction and data breakpoints, alone or with BS)
and the rest single steps, 3,987 inside a REP string; 342,402 instructions
ran with TF set and 63,758 with a breakpoint enabled. The `dr6_rule` kept
DR6's bits past 26,185 #DBs. They also took 258,392 injected INTRs and
114,547 NMIs, some meeting a trap at the same edge.

**Harness changes.**
- `dr6_rule` and `db_rf_rule` (divergences.toml's new `[[lockstep]]`
  section, which the vectors and test386 runners do not read). `db_rf_rule`
  fails the step when the RTL's image has RF clear where it should be set.
- A #DB whose delivery shuts down: the shutdown check takes the DR6 and DR7
  the #DB set first.
- The oracle adapter: `set_state` keeps Bochs's pending `debug_trap`, which
  the lockstep's rules hand back between instructions (it dropped single
  steps after an `imul-flags` adoption), and a trap due between a REP
  string's iterations is taken within the step, as an interrupt raised
  there is.
- `stop_at_exception` goes on past a #DB in the debug programs and the
  `past_db` cases, and TF no longer stops a debug program as stray.

**Bugs the lockstep found on the way** (each fixed before the run above):
an instruction breakpoint's uop read the MMIO window before its fault, and
could have walked a page (now it touches nothing from M1 on); its flags
reached EX's running copy, and so its #DB's frame; an NMI edge that came
while the CPU chose between events was lost; a POP SS shadow carried a
single step into INT n; a fetch check's replay of a matched instruction
skipped its breakpoint (`random#5014#237`, after a prediction a program 67
runs earlier had left in the BTB).

**New directed cases** (52 protected-mode cases, 49 of them in the run above): see the
lockstep README's protected-mode row and the "Not established" table above
for each, and [mutants.txt](mutants.txt) for which mutant each catches.

## Mutants

23 one-line mutants of the new RTL ([mutants.txt](mutants.txt)): 21 fail
directed cases; `edge-cbp-off` (no next-instruction breakpoint at an
interrupt's edge) fails only the debug programs, since no directed case
raises INTR; `rf-per-uop` is unreachable, since the replay that suppresses
a breakpoint issues its instruction one uop at a time, so no later uop of
it replays. Five mutants failed only the debug programs at first; the cases
added for them (`db-ibp-movss`, `db-dbp-movss`, `db-dbp-movss-int`,
`db-dbp-rep-own`, `db-dbp-gdt`) fail them now.

## test386.asm

`scripts/test_fes_i386_test386.py --cpu rtl --no-cross-check` at `c0a6a2682`
on chip-30x0-ai-dev (Powerboat was loaded; the i386 lock's Verilator 5.054
built there), [test386-rtl-report.json](test386-rtl-report.json), its output
text trimmed to a hash:
- **POST codes** 00, 01, 02, 03, 04, 05, 06, 08, 09, 20, 21, 22, 0B-1C, E0,
  EE and FF, no error, no check stepped over; the run halts at
  00D0h:0000FFB9h after 79,640,765 retirements, where Bochs halts, as on
  slice 4.
- **POST EEh's output** (44,926 lines) is byte for byte slice 4's (SHA-256
  `83634482…`): the same four IDIV overflows that the RTL completes as the
  386EX does (`idiv-unchecked-overflow`; slice 4's record), and nothing
  else differs from test386's reference.
- An earlier run on Powerboat at `92d08101b`, before the last two RTL
  changes (system accesses matching data breakpoints, the skip after a
  suppressed breakpoint only), gave the same POST codes, end and output.

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
