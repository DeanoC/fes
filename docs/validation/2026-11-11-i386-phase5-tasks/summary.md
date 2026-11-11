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
  71 more protected-mode cases and 5 more paging cases. Main's one failure,
  [#1663](https://github.com/DeanoC/fes/issues/1663), now passes: the harness takes a
  fault at a transfer's first fetch into the transfer's step, as the 386
  and Bochs do (`fetch_rule`).
- **Mutants.** 24 of 26 one-line mutants of the new RTL fail directed
  cases; one is equivalent, and one (NMI unblocked by a task return) is
  out of the lockstep's reach ([mutants.txt](mutants.txt)).
- **Seal.** Quartus 17.0.2 at seed 1, from clean checkout `fcb780f78`
  (this branch with main merged), meets every analysis: setup +0.400 ns,
  hold +0.080 ns. ALMs 36,132 (main 36,002 the same day, +130) and 269
  M10K (main 268, +1) ([Seal](#seal)).
- **Kit A, max.** DOOM v1.9 `-timedemo demo3` gives 40.03 fps, and 3DBench,
  PCPBench and TopBench match main's exactly, run the same hour. Descent now reaches its
  main menu; on main it stops after the DOS/4GW banner. Jump 'n Bump reaches
  its title screen and NetHack its first prompt; on main both hang at launch.
  Angband now gets as far as DJGPP's "Coprocessor not available" exit,
  where on main it hung: it needs a 387 or an emulator.

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
  bits. The fit reports put the 512×133 ROM in 7 M10K blocks, where main's
  512×119 takes 6: that is the one more M10K. A far JMP's or CALL's descriptor check and a delivery's gate
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
250 task-switch programs, the full corpus) at `fdf79c057`. That is this
branch with main `1d45ba6e5` merged in, which moves the i386 lock's Yosys to
`0a7927bf` (#1664), plus the fix to Codex's review (IRET waits for NT; see
below). It ran on the RTL and on that Yosys's netlist, with the lock's
Verilator 5.054:

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (7,977,648 retirements) | same |
| Protected-mode programs | 8,000 of 8,000 (432,695 retirements) | same |
| Task-switch programs | 4,000 of 4,000 (202,130 retirements) | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 349, 77, 1,933 and 27, all pass | same |

All 996 trace hashes match between the RTL and the netlist
([lockstep-report.json](lockstep-report.json),
[lockstep-netlist-report.json](lockstep-netlist-report.json)). The same
runs at `fcb780f78`, before the fix, passed on both with 76 paging cases.
Earlier runs on the unmerged branch, at `5083205bd` (RTL) and `03e92ea45` (Yosys
`238eac9a4`'s netlist), gave the same results; they had 347 and 348
protected-mode cases. `ts-jmp-ti`'s LDT entry was overwritten by the LDT's own
setup there, which the testbench fixes; the fix also adds `ts-gate-ti-ldt`.

**An IRET that DC decoded by a stale NT** (Codex's review): a POPF in
flight that sets NT leaves DC picking the stack return. The RTL's first
uop used to replay only in EX, after its stack load had issued, and under
paging that load's walk set the stack page's accessed bit. A 386 with NT
set reads no stack. D now holds a protected-mode IRET's first uop while
any uop that may write NT is in flight (`nt_busy`), and replays it with no
access when the committed NT differs. `pg-ts-popf-nt-iret` (POPFD setting
NT, then IRETD with ESP on a page no access has walked; the new task reads
that page's PTE) fails on the old RTL (PTE 13027h against Bochs's 13007h)
and passes now.

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

**New directed cases** (71 protected-mode, 5 paging; the old task-gate and
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
  present, a TSS across a page boundary, an IRET right after the POPFD
  that sets NT.

## test386.asm

`scripts/test_fes_i386_test386.py --cpu rtl` at `8337808e9`
([test386-rtl-report.json](test386-rtl-report.json), its output text
trimmed to a hash). The runner now passes its 200,000,000-retirement limit
to the testbench (`FES_I386_LIMIT`). Before, the testbench's default of
30,000,000 ended the RTL inside POST EEh. The RTL is `214cb3886`'s, the
seal's logic. A run of the same testbench by a scratch script, before the
runner change, gave the same POST codes, end and output text:
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
  Each is the `IDIV AX` or `IDIV EAX` form (test386 divides EDX:EAX by the
  register; the neighbouring lines' results show which). For all four, the
  vectors runner's `idiv-unchecked-overflow` predicate holds: the quotient
  overflows and the 386EX's division check does not fault. The RTL follows
  the 386EX there (`divergences.toml`, `[[vectors]]
  idiv-unchecked-overflow`). No slice-4
  RTL touches the divider (`fes_i386_muldiv` is main's); main's RTL never
  reached POST EEh before.
- `--cpu rtl` does not compare POST EEh's output with the reference, so
  these four lines were compared by hand from the run's output file.

## Seal

The seal is Quartus 17.0.2, seed 1, from clean checkout `fcb780f78`
([seal-summary.json](seal-summary.json)). That is the PR's RTL, with main
`1d45ba6e5` merged in; main changed no RTL. An earlier seed-1 seal of
`214cb3886` ([seal-summary-214cb3886.json](seal-summary-214cb3886.json))
built the kit's package. Its RTL differs from the seal's only in how
`dc_q.tsw` reads the decoder's routine address (the same logic, rewired so
Verilator 5.032 sees no loop). Main's `d56f0e558` was sealed the same way
the same day for the kit's A/B
([seal-summary-main-d56f0e558.json](seal-summary-main-d56f0e558.json)).

| | Seal (`fcb780f78`) | Kit package (`214cb3886`) | Main (`d56f0e558`) |
| --- | ---: | ---: | ---: |
| Package | `5565fff4…` | `b3af9d7a…` | `bc4ab0d9…` |
| Setup | +0.400 ns | +0.175 ns | +0.753 ns |
| Hold | +0.080 ns | +0.093 ns | +0.077 ns |
| Recovery | +7.343 ns | +6.897 ns | +7.097 ns |
| Removal | +0.399 ns | +0.501 ns | +0.577 ns |
| Minimum pulse width | +1.122 ns | +1.122 ns | +1.122 ns |
| ALMs | 36,132 | 36,155 | 36,002 |
| Registers | 32,671 | 32,552 | 32,413 |
| M10K blocks | 269 | 269 | 268 |
| Block memory bits | 1,349,500 | 1,349,500 | 1,342,310 |
| DSP blocks | 38 | 38 | 38 |

The system clock is 61.44 MHz. The seal's worst setup path runs from
`cpu_dfwd_ready[15]` through D's load forwarding (`dfwd` base value) to
`cpu_r_c1`. On `214cb3886`'s fit it ran from `cpu_ex_ag_result` through
`dfwd` and the store-to-load adder (`st_e`, `st_near`) to the issue enable.
Both are Phase 4 paths with none of this slice's logic on them, and their
slack moves with the fit.
The plan gave slices 2-5 about 2,400 ALMs. This slice takes 130 over
main's fit of the same day, inside the ±200 fit noise the area audit
measured. 5,778 ALMs (13.8%) are left, 2,368 under the working ceiling of
38,500.

## Kit

Runs `k1`-`k3` (this slice) and `m1`-`m3` (main) used Kit A, core-only,
on 11 November from 11:24 to 12:28 ([kit-scores.json](kit-scores.json)).
- **Packages.** `b3af9d7a…` (this slice's seed-1 fit of `214cb3886`, the seal's logic) and `bc4ab0d9…`, main
  `d56f0e558`'s seal of the same day, at max (preset 0). Each launch came after a
  SCRUBX scrub.
- **Kit.** Baseline image `179affbb…` and software `a2756499`, with no
  install, no image change and no reboot. After each run Kit A was checked
  against its snapshot and logged FREE.
- **Cleanup.** Lander's own recovery at 12:35 had already removed the
  game records and media trees the six runs left (restoring the kit's
  listing). My cleanup then removed the three staged disk images, and the kit
  was logged FREE on its baseline.
- **Disks.** The Phase 4 bench disk, and the #1533 game drives `dpmi`
  (`9e6ae3a3…`) and `pmode` (`b4be6e29…`), from Powerboat. None is
  committed.

| Benchmark | Main (m1) | This slice (k1) |
| --- | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,378 realtics, 40.03 fps | same |
| 3DBench | 76.9 | 76.9 |
| PCPBench | 13.3 | 13.3 |
| TopBench | 307 | 307 |

| Title | Drive | Main (m2, m3) | This slice (k2, k3) |
| --- | --- | --- | --- |
| Descent (DOS/4GW 1.97) | `pmode` | Stops after the DOS/4GW banner | Its main menu |
| Jump 'n Bump (CWSDPMI) | `dpmi` | Hangs at launch, blank screen | Its title screen |
| NetHack 3.4 (CWSDPMI) | `dpmi` | Hangs at launch, blank screen | "Who are you?" |
| Angband (CWSDPMI) | `dpmi` | Hangs at launch | Exits: SIGNOFP, "Coprocessor not available at eip=00112a07" |
| Time Circle (32RTM) | `dpmi` | Its main menu | Its main menu |

**Angband's exit is what a 386 without a 387 does.** The design plan
listed Angband as unlocked by this slice. The census's oracle runs, which
reached its character menu, were on Bochs with a working 387: the machine
testbench never empties the oracle's socket. Without a 387, a DJGPP
program sets CR0.EM, and its first ESC raises #NM. DJGPP then loads
`EMU387.DXE` if it can find it, else exits with SIGNOFP ("Coprocessor not
available"). The `dpmi` drive has no `EMU387.DXE` anywhere. The machine
sim on Bochs (with its 387) reaches the character menu here too; on main the
runtime never got as far as the ESC. Each title was launched and watched
for 80-180 s; this is a check of how far they get, not acceptance.

## What this record does not cover

- **The T bit's #DB** on a switch, and breakpoints (slice 5).
- **NMI unblocked by a task return.** The RTL unblocks NMI at the commit
  of an IRET to a nested task. The 386's microcode clears the block at
  IRET's start, in the delay slot of the branch to TASK_RETURN (673-674),
  so the two differ only when the switch faults inside an NMI handler,
  where the 386 has unblocked NMI and the RTL has not (as for a plain IRET
  that faults). The lockstep raises NMI in real mode only, so no case
  reaches either (mutant `nmi-kept`).
- **A far JMP through a call gate,** which stays not built (no task
  switch is involved).
- **nextpnr.** No route was made (fes#1235).
