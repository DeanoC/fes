# i386 Phase 5 slice 4: task switches, 11 November 2026

This record covers CPU Phase 5's slice 4
([`i386-cpu-design.md`, "Phase 5: V86, paging and task switches"](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches),
tracker [#1548](https://github.com/DeanoC/fes/issues/1548)). `fes_i386_cpu`
now switches tasks: far JMP and CALL to a TSS or through a task gate; INT n,
exceptions, INTR and NMI through a task gate; IRET with NT set; 16-bit and
32-bit TSSs; the busy bits, the back link, NT, CR3, CR0.TS and DR7's local
enables; and a switch into a V86 task.

**Result:**
- **test386.asm** on the RTL runs to POST FFh and halts where Bochs does,
  with no error, after 79,640,765 retirements. That includes POST 22h
  (task switching) and 11h. Main stopped at POST 22h, at `int 0x28`
  through a task gate. Four of POST EEh's 44,926 output lines differ from
  test386's reference, all IDIV overflows that the RTL completes as the
  386EX does ([test386.asm](#test386asm)).
- **Lockstep.** Everything passes against Bochs on the RTL and on the
  i386 lock's Yosys netlist, with identical retirement traces. That covers the full corpus,
  the random and protected-mode programs, 4,000 new task-switch programs,
  71 more protected-mode cases and 4 more paging cases. Main's one failure,
  [#1663](https://github.com/DeanoC/fes/issues/1663), now passes: the harness takes a
  fault at a transfer's first fetch into the transfer's step, as the 386
  and Bochs do (`fetch_rule`).
- **Mutants.** 24 of 26 one-line mutants of the new RTL fail directed
  cases; one is equivalent, and one (NMI unblocked by a task return) is
  out of the lockstep's reach ([mutants.txt](mutants.txt)).
- **Seal.** Quartus 17.0.2 at seed 1, from clean checkout `214cb3886`,
  meets every analysis: setup +0.175 ns, hold +0.093 ns. The RTL has not
  changed since; later commits touch only docs and the testbench. ALMs
  36,155 (main 36,002 the same day, +153) and 269 M10K (main 268, +1)
  ([Seal](#seal)).
- **Kit A, max.** KIT_RESULT

## Design as built

The RTL README's
[Task switches](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#task-switches)
has the full description, and the design doc's slice 4 lists where it
departs from the plan. In short:
- **The 386's order, not Bochs's, where the two differ visibly.** The source is the die-extracted
  microcode ([reenigne/x86_microcode](https://github.com/reenigne/x86_microcode),
  `80386/microcode_notes.txt` 65C-7E4, and `tests.txt`'s protection
  tests):
  - The new TSS's busy bit is set before the old task's save (74B-74C).
  - An IRET with NT clears NT and saves the old task before it reads and
    checks the back link (65F-667), so a back link that faults leaves the
    old TSS written. Bochs checks first and writes nothing.
  - The new task's segments load LDT, SS, CS, then DS, ES, FS and GS
    (7AB-7CE). Bochs loads CS last.
  - SS's privilege is tested before its presence (`TST_DES_SS`). Bochs
    tests presence first.
  - The switch does not check the new EIP against CS's limit (7D0-7E2):
    the first fetch raises #GP(0). Bochs raises it inside the switch.
  - A JMP or CALL to a TSS through a TI=1 selector is #GP(selector) ahead
    of a not-present #NP (`TST_SEL_TASKGT`).
- **Bochs's order where the 386's can't be established.** These are the TSS limit checks up
  front (new TSS #TS(selector), old #TS(TR)), the new TSS's first and last
  bytes read before anything is written, and CPL 3 through the LDT's and SS's
  loads.
- **Routines.** `tsjg`, `tsgate`, `tsw`, `tssave`, `tsload` and `tsv86`
  hold 103 ROM words, 435 of 512 in all. The word grows from 119 to 133
  bits and still fits in 7 M10K blocks; main's ROM takes 6, hence the one
  more M10K. A far JMP's or CALL's descriptor check and a delivery's gate
  are tgo words that go on at the switch for a TSS or task gate
  (`fes_i386_seq`). An IRET with NT starts at `tssave` from DC, and replays
  if an older POPF changed NT under it.
- **The commit** is a far jump to the new EIP that does not end the
  instruction. It sets CS's selector, CPL 3, CR0.TS, DR7 &= FFFFFEAAh, CR3
  (a 386 TSS under PG, when it differs) and the gate size. DC then takes the
  new task's descriptor loads (`tsload`, or `tsv86` for VM) as it takes a
  fault's entry, at the new EIP and length 0. A fault there is therefore
  the new task's, and a delivery's exception stays active through it
  (nesting as Bochs does).
- **Speed.** About 70 uops a switch, none on a fast path.

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of
2,000 random programs, 16 seeds of 500 protected-mode programs, 16 seeds of
250 task-switch programs, the full corpus). The RTL run was at `5083205bd`
and the netlist run (Yosys `238eac9a4`, the i386 lock's) at `03e92ea45`, both with the
lock's Verilator 5.054. Both have the sealed RTL. The protected-mode and paging cases
then ran again, on the RTL and the netlist, with the final testbench at
`e75343855`. That testbench fixes `ts-jmp-ti`, whose LDT entry was overwritten by the
LDT's own setup, and adds `ts-gate-ti-ldt`.

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (7,977,648 retirements) | same |
| Protected-mode programs | 8,000 of 8,000 (432,695 retirements) | same |
| Task-switch programs | 4,000 of 4,000 (202,130 retirements) | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, no-387 and memory cases | 7, 1,933 and 27, all pass | same |
| Protected-mode cases (final testbench) | 349 of 349 | same |
| Paging cases (final testbench) | 76 of 76 | same |

Of 996 trace hashes in the full runs, 995 match between RTL and netlist
([lockstep-report.json](lockstep-report.json),
[lockstep-netlist-report.json](lockstep-netlist-report.json)). The other
is the protected-mode cases', where the RTL run had 347 cases and the
netlist run 348 (`ts-jmp-ti` was added between them). On the final
testbench both give the same hashes for the protected-mode, paging and
directed cases
([directed-report.json](directed-report.json),
[directed-netlist-report.json](directed-netlist-report.json)).

**#1663.** In `random#1010#118`, an INTR delivery's handler's first fetch
is on a page that is not present. Bochs takes that #PF inside the delivery's step;
the RTL takes it as its next event. On a 386 the fetch is the transfer's
own, so the lockstep now takes the RTL's next event into the step when it
is a #PF at the target's first byte (CR2 the RTL's CS.base + EIP) or a
#GP(0) for an EIP past CS's limit (`fetch_rule`). The program passes; the
rule fired once in the protected-mode programs and once in the cases.

**Task-switch programs** (`FES_I386_TASKS`, `--task-programs`, seeds from
3000) add a handler task (a 386 TSS at 400h), task gates in the GDT and
the IDT (vectors 1 and 16 and a software vector), and a block of JMP,
CALL, INT and V86 switches with NT and busy-bit cleanup. They draw from
the generator's side stream, so every earlier seed's programs are
unchanged. Over 16 seeds they made 200 JMP, 95 CALL, 290 delivery, 133
IRET, 47 V86 and 12 286-TSS switches, and 53 IRETs whose back link faulted
after the old task's save (`iret_save_rule`).

**Harness rules.**
- `fetch_rule` (above).
- `iret_save_rule`: when an IRET with NT faults on its back link, the
  oracle adopts the RTL's writes into the old TSS's save area, which the
  386 has written by then and Bochs has not.
- A shutdown after a committed switch (TR changed): the RTL's writes must
  be a subset of the oracle's, and the state is compared but for RF, TF,
  IF and NT.
- `why_e` drops the task-switch reasons nothing raises any more.

**New directed cases** (71 protected-mode, 4 paging; the old task-gate and
IRET-NT placeholders are gone):
- Round trips: JMP and CALL to 386 and 286 TSSs from 32-bit and 16-bit
  code, through task gates, INT, INT3 and exceptions (#UD, #GP with an
  error code, #NP) through task gates, a chain of nested CALLs back by
  IRET, an EFLAGS image with NT, a V86 task by JMP and CALL, CPL 3 and
  back, FS and GS through a 286 TSS, the LDT, CR3 with PG off, DR7's local
  enables.
- Checks: busy, not present, RPL 3, TI=1 (a TSS in the LDT), limits below
  67h and 2Bh, the old TSS's limit, task gates not present, RPL 3, naming
  a busy TSS, TI=1, null, a data segment, a TSS not present, an IDT task
  gate not present or busy.
- IRET back link: null, TI=1, available, not present, past the GDT.
- The new task's loads: LDT TI=1, not an LDT, not present; SS null,
  read-only, RPL, not present, DPL before present; CS null, data, DPL, not
  present; DS RPL, not present; ES a system descriptor; FS and GS not
  present; DS null (allowed); a delivery's task with DS or CS not
  present; EIP past CS's limit.
- Paging: #PF through a task gate and back by IRET with NT (as Descent
  does), CR3 switched with the GDT on a mapped page, a new TSS not
  present, a TSS across a page boundary.

## test386.asm

`lockstep_tb`'s test386 mode, the run `scripts/test_fes_i386_test386.py
--cpu rtl` makes, with the step limit raised to the script's 200,000,000
(the runner now passes `FES_I386_LIMIT`; before, the testbench's default of
30,000,000 ended the RTL inside POST EEh). The testbench was `03e92ea45`'s, with the sealed RTL
([test386-rtl-report.json](test386-rtl-report.json)):
- **POST codes** 00, 01, 02, 03, 04, 05, 06, 08, 09, 20, 21, 22, 0B-1C,
  E0, EE and FF, no error, no check stepped over. The run halts at
  00D0h:0000FFB9h after 79,640,765 retirements; Bochs halts at the same place.
- **POST EEh's output** (44,926 lines; test386's reference is from
  emulators, and its README says it was never run on real hardware) differs from the
  reference in four lines, two word and two dword IDIVs that overflow.
  The reference and Bochs raise #DE; the RTL completes them with a quotient
  of 8000h (80000000h):

  | Reference | RTL |
  | --- | --- |
  | `F7 IDIV W EAX=00007FFF EDX=00008000 PS=0000 #DE EAX=00007FFF EDX=00008000` | `... EAX=00008000 EDX=0000FFFF` |
  | `F7 IDIV W EAX=00008001 EDX=00007FFF PS=0000 #DE EAX=00008001 EDX=00007FFF` | `... EAX=00008000 EDX=00000001` |
  | `F7 IDIV D EAX=7FFFFFFF EDX=80000000 PS=0000 #DE ...` | `... EAX=80000000 EDX=FFFFFFFF` |
  | `F7 IDIV D EAX=80000001 EDX=7FFFFFFF PS=0000 #DE ...` | `... EAX=80000000 EDX=00000001` |
  These are overflows the 386EX's division check lets through, which the RTL
  follows from the corpus (`divergences.toml`, `[[vectors]]
  idiv-unchecked-overflow`, decided on an 8-bit case). No slice-4
  RTL touches the divider (`fes_i386_muldiv` is main's); main's RTL never
  reached POST EEh before.
- `--cpu rtl` does not compare POST EEh's output with the reference, so
  these four lines were compared by hand from the run's output file.

## Seal

The seal is Quartus 17.0.2, seed 1, from clean checkout `214cb3886`
([seal-summary.json](seal-summary.json)). The RTL is the PR's: the commits
after it change only docs, the testbench and test scripts. Main's
`d56f0e558` was sealed the same way the same day for the kit's A/B
([seal-summary-main-d56f0e558.json](seal-summary-main-d56f0e558.json)).

| | This slice (`214cb3886`) | Main (`d56f0e558`) |
| --- | ---: | ---: |
| Package | `b3af9d7a…` | `bc4ab0d9…` |
| Setup | +0.175 ns | +0.753 ns |
| Hold | +0.093 ns | +0.077 ns |
| Recovery | +6.897 ns | +7.097 ns |
| Removal | +0.501 ns | +0.577 ns |
| Minimum pulse width | +1.122 ns | +1.122 ns |
| ALMs | 36,155 | 36,002 |
| Registers | 32,552 | 32,413 |
| M10K blocks | 269 | 268 |
| Block memory bits | 1,349,500 | 1,342,310 |
| DSP blocks | 38 | 38 |

The system clock is 61.44 MHz. The slice's worst setup path goes from
`cpu_ex_ag_result` through D's load forwarding (`dfwd` base and row) and
the store-to-load adder (`st_e`, `st_near`) to the issue enable. That path
was built in Phase 4 and none of this slice's logic is on it; its slack
moves with the fit.
The plan gave slices 2-5 about 2,400 ALMs. This slice takes 153 over
main's fit of the same day, inside the ±200 fit noise the area audit
measured. 5,755 ALMs (13.7%) are left, 2,345 under the working ceiling of
38,500.

## Kit

KIT_SECTION

## What this record does not cover

- **The T bit's #DB** on a switch, and breakpoints (slice 5).
- **NMI unblocked by a task return.** The RTL unblocks NMI at the commit
  of an IRET to a nested task, as IRET does; the lockstep raises NMI in
  real mode only, so no case reaches it (mutant `nmi-kept`).
- **A far JMP through a call gate,** which stays not built (no task
  switch is involved).
- **nextpnr.** No route was made (fes#1235).
