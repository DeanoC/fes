# i386 Phase 2, second slice: protected-mode interrupts, exceptions and system segments at CPL 0, 2 November 2026

Issue [#1406](https://github.com/DeanoC/fes/issues/1406), the i386 design's
Phase 2 after the first slice ([#1370](https://github.com/DeanoC/fes/issues/1370),
PR #1373).

**Result:**
- `fes_i386_cpu` delivers exceptions, INTR and NMI with PE set through 386
  and 286 interrupt and trap gates at CPL 0, with the gate, CS and offset
  checks, error codes with EXT, IF for interrupt against trap gates, and RF
  in the frame. A fault in a delivery is delivered in its place; two
  contributory faults make a double fault, and a fault in its delivery is
  shutdown, which the machine turns into a CPU reset, in both modes. It
  builds INT n, INT3, INTO, IRET and IRETD, far CALL and RETF (imm16) to the
  same privilege, LLDT and TI=1 loads, LTR, SLDT, STR, LAR, LSL, VERR, VERW
  and ARPL ([What the slice builds](#what-the-slice-builds)).
- The RTL and the i386 lock's Yosys netlist match Bochs on every lockstep
  run: 1,758,699 corpus states, 32,000 random programs, 8,000 protected-mode
  programs with 13,640 deliveries through the IDT and 1,427 shutdowns, 51
  new directed protected-mode cases, and the directed, unreal-mode and memory
  cases. Against slice 1's report only timing differs: one corpus test
  (23 cycles) and the two real-mode nested-fault cases, which now run on to
  shutdown ([Lockstep](#lockstep)).
- A protected-mode IRET and RETF read their stack slots in Bochs's order,
  from the top, so a frame past a 16-bit stack's limit raises #SS(0) before
  any read ([#1424](https://github.com/DeanoC/fes/issues/1424), found by the
  CS-limit follow-up's random programs; [Review](#review)).
- The SingleStepTests runner gives slice 1's results. test386.asm on the RTL
  passes its real-mode part and stops at POST 08h, which turns paging on
  with protected mode; none of its protected-mode part can run before
  Phase 5 ([Oracle runners](#oracle-runners-and-test386)).
- FreeDOS boots with HIMEMX's full 15,360K of XMS on the RTL CPU in the
  machine sim (0 stops, 0 oracle steps) and in `system_tb`; the boot and
  system suites pass, the governor bench equals slice 1's field for field,
  and the governor suite passes but for 386DX-25, which meets the bench gap
  #1372 after its test finishes, as on main ([Machine](#freedos-with-himemx-and-the-pc386-suites)).
- Quartus 17.0.2 seals `fes.pc386` from a clean checkout of `7224b11f6`
  (with the review fix) at 61.44 MHz with every analysis met: setup
  +1.231 ns (the pixel clock; the system clock has +1.889 ns, 69.51-69.88 MHz
  at its slow corners), hold +0.085 ns, 28,831 ALMs, 520 more than slice
  1's merge seal ([Seal](#quartus-seal)).
- **DOOM under DOS/4GW, the target that sets the boundary**, uses only CPL 0
  without paging, V86 or task switches: INT 31h, 21h, 10h and 33h and IRQ 0
  through 386 interrupt gates, far CALL and RETF, LLDT, LAR and LSL, and
  3,366 mode switches in its first 10 seconds on the Bochs CPU, all of them
  instructions this slice builds. It does not start on the RTL, for two
  things outside this slice: its FPU probe (`FNINIT`, `FNSTSW`, `FNSTCW` in
  real mode; the design's empty-387-socket ESC forms are not built) and its
  16-bit code segments, whose limits are below FFFFh (the general CS-limit
  check is not built). The second stops every instruction of DOS/4GW's
  kernel, so DOOM exercises none of this slice on the RTL yet
  ([Boundary](#the-boundary-from-a-real-target-doom-under-dos4gw)).
- On Kit B the sealed package boots FreeDOS with HIMEMX's 15,360K of XMS;
  DOOM halts at its FPU probe, as the sim shows
  ([Kit B](#kit-b-smoke-test-freedos-with-himemx-and-dooms-start)).

## Inputs

- FES branch `deano/i386-phase2-idt-8f1dd2`, from main `d1d6c67e9` (after
  slice 1's merge, `8de99b4bb`). Main `3d2e17f84` (#1385 and #1387, ZX81
  and PC/XT only, no i386 or pc386 source) is merged in as `43bff581b`.
- `7baa2a6f3` is Codex's review fix: a protected-mode IRET with NT set
  stops as not built ([Review](#review)). The final seal, the lockstep and
  the netlist lockstep are of `7224b11f6`, which has it.
- The first seal and the kit runs are of `43bff581b`, before the fix, which
  changes nothing they reached. The other runs used these trees, whose i386
  and pc386 RTL equals `43bff581b`'s but for a `FES_I386_DEBUG` block in
  `fes_i386_cpu.sv`, which nothing defines and `9314cad59` removed:
  - `32cbe9b30` (whose `pipe_t.sdisp` was still declared signed, the same
    bits): test386 on the RTL;
  - `10956ac5c`: the HIMEMX machine run, the DOOM census and RTL machine
    runs and the governor bench;
  - `9314cad59`'s lockstep testbench on `10956ac5c`'s RTL (Verilator had
    read the CPU before the block was removed): the lockstep; `9314cad59`:
    the HIMEMX `system_tb` run;
  - `ad2ab3544`: the vectors runner, the pc386 suites and the 386DX-25
    rerun, the unit tests and `make test-changed`.

  Later commits change only this record, the design doc's Phase 2 row, the
  sim README, the review fix and `d9773b8f2`.
- `d9773b8f2` is the #1424 fix: protected-mode IRET and RETF read their slots
  in Bochs's order. The lockstep and the netlist lockstep below are of it
  (the RTL run's testbench was built just before a comment in
  `fes_i386_decode.sv` was reworded for the commit). The seal is of
  `7224b11f6`, before it: under the pc386 reseal exemption a post-seal RTL
  fix needs no reseal before merge, and the next sealing PR covers it
  ([Seal](#quartus-seal)).
- Oracle: Bochs 3.1 at CPU level 3 (the `fes-i386-oracle` build), corpus
  SingleStepTests/80386 `459d49fbe628`, test386.asm `cfd052d1e64d`.
- Verilator 5.032 on Powerboat. The netlist lockstep used the i386 lock's
  Yosys `238eac9a4`. The seal used Quartus Prime Lite 17.0.2 with the
  netlist from the pc386 lock's Yosys `0a7927bf`.
- DOOM v1.9 (DOOM.EXE 709,905 bytes, DOOM.WAD 11,159,840 bytes, DOS/4GW) from
  `/mnt/emuhd/Games/PC DOS/Collections/eXoDOS 6.04 Full/eXoDOS/eXo/eXoDOS/DOOM (1993).zip`
  (SHA-256 `06ee36b4…`), copied onto scratch disks only. No game bytes
  are in the repository.

## What the slice builds

The [RTL README](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#protected-mode)
has the detail. In short, at CPL 0, following Bochs 3.1's
`protected_mode_int`, `exception`, `call_protected`, `return_protected`,
`iret_protected` and the group 6 and LAR-kin handlers:

- **Delivery through the IDT** (`pvec`, `pint`). Exceptions, INTR and NMI
  with PE set, and INT n, INT3 and INTO, read the gate at IDTR's base plus
  the vector times 8. The checks and their error codes: the entry past
  IDTR's limit #GP(vector × 8 + 2 + EXT); not an interrupt or trap gate
  (286 or 386) #GP(entry); not present #NP(entry); then the target CS: null
  #GP(0), not code or DPL not 0 #GP(selector + EXT), not present
  #NP(selector + EXT), the offset past its limit #GP(0). Each pushed slot is
  probed before anything is stored; then the CS descriptor's accessed bit,
  EFLAGS, CS, EIP and the error code at the gate's size, and the far jump.
  An interrupt gate clears IF, a trap gate keeps it; TF, NT and RF are
  cleared. The pushed EFLAGS image has RF set for a fault, and for an INTR
  or NMI between a REP string's elements. A delivery through a task gate
  stops as not built.
- **Nested faults, #DF and shutdown.** A fault in a delivery is delivered
  in its place with EXT set in its error code. A contributory fault (#DE,
  #TS, #NP, #SS, #GP) in a contributory fault's delivery is a double fault
  (#DF, error code 0); a fault in #DF's delivery is shutdown. The CPU halts
  and raises `shutdown`, which `pc386_machine` turns into a CPU reset, as the
  AT's shutdown detect does. Real mode uses the same rules (an IVT read past
  IDTR's limit is #GP there).
- **IRET and IRETD** (`piret`) to the same privilege, with
  `iret_protected`'s CS and offset checks. They read EFLAGS's slot, then
  CS's, then the offset's, as Bochs does, so a frame past the stack's limit
  raises #SS(0) before any read. IRETD loads RF; an image with VM set (a
  return to V86 mode) stops as not built, as does a return to RPL above 0.
- **Far CALL** direct and through m16:16 or m16:32 (`pcall`, `pcallm`) and
  **RETF** and **RETF imm16** (`pretf`) to a code descriptor at the same
  privilege, with `call_protected`'s and `return_protected`'s checks. RETF
  reads CS's slot before the offset's, as Bochs does. A call gate, TSS or
  task gate, or a return to an outer privilege, stops as not built.
- **LLDT and SLDT** (`plldt`); selectors with TI=1 now read the LDT that
  LLDT loaded, checked against its limit. A null LLDT leaves LDTR invalid,
  so every TI=1 selector then faults.
- **LTR and STR** (`pltr`), with the TSS descriptor checks; LTR stores the
  descriptor's busy bit. TR is held only: no task switch is built.
- **LAR, LSL, VERR, VERW and ARPL** (`plar`, `plsl`, `pverr`, `pverw`,
  `parplr`, `parplm`), with ZF and the results Bochs gives at CPL 0. ARPL
  writes nothing when it does not raise the RPL (divergence
  `arpl-no-write-no-fault`).
- **Group 6, LAR, LSL and ARPL raise #UD in real mode**, as the 386's do.
- **LDTR and TR reset** to Bochs's values: selector 0, base 0, limit FFFFh,
  access rights 82h and 8Bh.

**Not built** (each stops with `trace_unimplemented`, and now its reason in
`trace_unbuilt`, `why_e`): task gates and task switches, inter-privilege
transitions (RETF and IRET to RPL above 0, and calls and jumps through call
gates), IRET to V86 mode, paging, a 16-bit stack slot past 64 KiB under a
limit that reaches there, and code under a CS limit other than FFFFh or
FFFFFFFFh. IOPL, the I/O bitmap and debug registers are not built either; at
CPL 0 IOPL never faults. The third slice takes privilege, call gates and
task gates; Phase 5 takes V86 and paging.

**Microcode.** `ucode.toml` gains `pvec`, `pint`, `piret`, `pretf`,
`pcall`, `pcallm`, `plldt`, `pltr`, `plar`, `plsl`, `pverr`, `pverw`,
`parplr` and `parplm`: 130 words of 90 bits, from 59 of 79. New fields:
`dmode` (how a descriptor is read: load, probe or GDT only), `gsz` (a word
at the gate's size), `errp` (dropped without an error code), `nwrap` (a
16-bit stack slot without the 64 KiB wrap), `vmchk` and `iimm`. A fifth
temporary (T4) holds IRET's EFLAGS image.

## The boundary from a real target: DOOM under DOS/4GW

**Census on the Bochs CPU.** `machine_tb --cpu oracle --census` on a scratch
disk (the games image, HIMEMX's full XMS, DOOM in `C:\DOOM`, SHA-256
`fa26f60b…`), typing `cd doom` and `doom` at the prompt, 10 simulated
seconds. DOS/4GW comes up and DOOM reaches `R_Init` (its screen shows
`DOOM System Startup v1.9` through `DPMI memory: 0xdba000, 0x800000
allocated for zone`, `W_Init`, `M_Init` and the `R_Init` progress bar), so
the disk, FreeDOS and HIMEMX are not in the way.

In those 10 seconds the CPU retires 6,120,051 instructions with PE set, 320
stems, all at CPL 0, under CS 0018h, 0070h, 0080h and 0088h (DOS/4GW's
16-bit kernel), 0170h (DOOM's 32-bit code) and 029Eh. Nothing pages, enters
V86, loads TR, changes IOPL or changes CPL. What it uses of this slice:

| What | Count |
| --- | --- |
| INT n through 386 interrupt gates (type 0Eh, DPL 0): 31h (DPMI), 21h, 10h, 33h | 4,302 |
| IRQ 0 through a 386 interrupt gate at vector 08h | 4 |
| far CALL ptr / far JMP ptr | 4,020 / 6,110 |
| RETF / RETF imm16 | 12,516 / 773 |
| MOV to CR0, MOV from CR0, SMSW, LMSW (its mode switches) | 3,367, 3,366, 3,366, 3,367 |
| LLDT (selector 68h) | 3,366 |
| LAR / LSL | 1,548 / 2 |
| MOV Sreg | 95,390 |

The rest are ordinary instructions. Every protected-mode stem it ran is one
the lockstep covers.

**What the RTL stops at.** The same disk on `machine_tb --cpu rtl`, where the
bench steps each not-built instruction on Bochs and resumes the RTL:
- In real mode, before protected mode, DOS/4GW's FPU probe: `FNINIT`
  (DB E3), `FNSTSW m16` (DD /7) and `FNSTCW m16` (D9 /7) at 0727:68B0, then
  `FNINIT` again at 07A2:021D. The design's [FPU section](../../../sources/misteross/docs/i386-cpu-design.md#fpu)
  specifies a 386 with an empty 387 socket (FNINIT does nothing, FNSTSW
  writes FFFFh), but no ESC form is built yet, so each stops the CPU.
- In protected mode, every instruction under CS 0070h and 0080h
  (`MOV AH, 30h`, `INT 21h`, `SUB SP, 6`, `PUSHA`, `PUSH ES`…): DOS/4GW's
  16-bit code segments have limits below FFFFh, and the general CS-limit
  check is not built (`WHY_CS_LIMIT`, slice 1's `cs_odd` rule).

Through 4.6 simulated seconds (the run went on; this is where it stood when
this record was written) the bench printed 789 stops of 271 stems: the five
FPU-probe stops in real mode, and every other one an ordinary instruction
under CS 0070h, 0080h or 0088h. It stepped 687,098 one-instruction stretches
with PE set on Bochs, under CS 0070h, 0080h, 0088h, 0018h and 0090h: the RTL
stops at each instruction of DOS/4GW's kernel for its code segment's limit,
before anything of this slice runs. So DOS/4GW's use of this slice is
measured on Bochs (the census), and the slice is checked against Bochs by
the lockstep, not by DOOM on the RTL.

So DOS/4GW at CPL 0 needs no protected-mode instruction beyond this slice,
but needs two more pieces before DOOM can start on the RTL: the
empty-socket ESC forms and the general CS-limit check. Both are follow-ups
([Not in this record](#not-in-this-record)); the CS-limit check belongs to
Phase 2, which the design means to be enough for DOS extenders.

## Lockstep

`scripts/test_fes_i386_lockstep.py` at `d9773b8f2`, with the review's fix
and #1424's ([report](lockstep-report.json)), and with `--yosys` on the
i386 lock's Yosys `238eac9a4` ([report](lockstep-netlist-report.json)).
The two reports are identical but for the netlist's `yosys` field. Against
the run at `7224b11f6`, before #1424's fix, the protected-mode cases differ
(four new), and the random and protected-mode programs' timing moves with
the new read order: 417 more cycles of 115,115,194 in the random programs
and 1,914 fewer of 9,543,771 in the protected-mode ones, with a few more
port 0 reads (74 and 100) as their loads reach the cache in another order.
Every pass, retirement, delivery and stop count is unchanged, and so is
every corpus, directed, unreal-mode and memory field.

Sixteen more seeds of random programs (17-32, 2,000 each, `d9773b8f2`'s
testbench), past the 16 the lockstep runs and among them #1424's
`random#17#229`, give 31,999 passes and one failure, `random#26#1633`. It
fails the same way on the RTL before the #1424 fix and without fetch
latency: an INTR delivered in real mode on an unreal stack (SS base
C5000000h, ESP 0, the frame past the lockstep's 16 MiB of RAM), after whose
three pushes Bochs nests a #GP(0) that the RTL does not raise. Bochs's
`real_mode_int` raises #GP there only for a vector whose IP is past CS's
cached limit, yet the reported CS limit was FFFFh throughout, so whether the
RTL, the harness or the oracle is at fault is still open:
[#1428](https://github.com/DeanoC/fes/issues/1428).

| Run | Result |
| --- | --- |
| Corpus (SingleStepTests/80386, 941 files) | 1,758,699 pass, 0 fail; 3,517,401 retirements compared; 147,537 in an exception's handler |
| Random programs (16 seeds × 2,000) | 32,000 pass, 0 fail; 8,347,724 retirements; 2,832 end at a shutdown, 781 at a fault in a delivery delivered |
| Protected-mode programs (16 seeds × 500) | 8,000 pass, 0 fail; 481,484 retirements, 379,145 of them (with the random programs') with PE set; 13,640 deliveries through the IDT; 1,427 shutdowns; 201 not-built stops |
| Unbuilt encodings | 52,320 outside SCOPE, all stopped as not built |
| Directed cases | 40 pass |
| Unreal-mode cases | 6 pass |
| Protected-mode cases (new) | 51 pass |
| Memory cases (DMA, FLUSH) | 26 pass |

The latency-free reruns of seeds 1 and 2 retire the same trace, and the
sequential-gap check passes: every instruction that is not a branch, a
memory read after a store, a flag consumer after a shift, a microcoded
protected-mode instruction or I/O retires one cycle after the last.

**Against slice 1's report.** Compared field by field with slice 1's
`lockstep-merge-max-report.json` (at `b7b4cb7dc`, whose i386 RTL and sim
sources equal this branch's base `d1d6c67e9`):
- **Corpus:** every field is equal, but BT r/m, r's (0FA3h) cycles: 240
  fewer of 1,024,960. One test differs on its own, `67660FA3#2237`
  (`bt [ds:esi], ebp`, which faults #GP): 23 fewer cycles and two fewer
  DDR reads. A trace of the DDR model's reads shows why: main's front end
  reads one more code line past the faulting BT while the vector is read,
  and this branch's does not. The rest of the 240 is that test's latency
  draws moving the corpus file's later ones. The results are unchanged.
- **Directed cases:** the trace hash is slice 1's (`68d44755edca35dc`);
  the cycles grow from 5,691 to 5,781 because the two cases whose delivery
  faults (an INT and a fault through a vector past IDTR's limit) now run on
  through the double fault's delivery to shutdown, where slice 1 stopped
  them as not built at the first nested fault.
- **Unreal-mode and memory cases:** equal, trace hashes
  (`fdcd18e3edf8dcdb`, `14650fb0739d0383`) and cycles included.
- **Random and protected-mode programs:** not comparable. The generator's
  protected-mode blocks now draw INT n, far CALL and RETF, LLDT, LTR and the
  rest, so the random stream after a program's first block differs. In
  slice 1 each PE=1 exception stopped the run; now it is delivered, so runs
  go on past it. `double_faults` becomes `shutdowns` and `nested`, and
  `pm_stops` becomes `pm_deliveries` and `unbuilt_stops`.

**The protected-mode programs** (seeds 1001-1016, 30 blocks per hundred
items) now set up an IDT of 48 gates (386 and 286 interrupt and trap gates
to IRET handlers in 32- and 16-bit code, now and then one that fails a check
or a task gate), an LDT and a TSS beside the 24-entry GDT, and draw INT n,
INT3 and INTO, far CALLs (direct and through memory, 16- and 32-bit) to RETF
or RETF imm16, LLDT, LTR, SLDT, STR, LAR, LSL, VERR, VERW and ARPL, STI and
loads of TI=1 selectors. Each exception, INTR and NMI with PE set is
delivered through the IDT on both sides, and the delivery is compared as a
retirement: its frame, error code and EXT bit, the gate's target, and the
CS, EIP, EFLAGS and SS:ESP it leaves. The 13,640 deliveries, by vector:

| Vector | Deliveries |
| --- | --- |
| #GP (13) | 7,143 |
| INTR (vectors 8-15 from the PIC) | 4,378 |
| #NP (11) | 1,121 |
| #SS (12) | 645 |
| #DF (8) | 274 |
| NMI (2) | 67 |
| #UD (6) | 12 |

1,427 runs end in shutdown on both sides (a fault in #DF's delivery), and
481 faults in a delivery are delivered in its place, EXT set. Instructions
of this slice retired with PE set:

| Kind | Retired | Kind | Retired |
| --- | --- | --- | --- |
| LLDT | 7,029 | LAR | 1,178 |
| IRET, IRETD | 5,150 | LSL | 1,176 |
| far CALL | 3,800 | LTR | 645 |
| RETF, RETF imm16 | 3,533 | VERR | 615 |
| TI=1 segment loads | 2,129 | STR | 600 |
| INT n, INT3, INTO | 1,902 | SLDT | 598 |
| ARPL | 1,280 | VERW | 574 |

The 201 not-built stops, by reason: 147 deliveries through a task gate
(`WHY_TASK_GATE`), 16 IRETDs to V86 mode (`WHY_V86`) and 38 16-bit stack
slots past 64 KiB (`WHY_STACK64K`). For a task gate, an outer return, a
transfer through a gate or TSS, and V86, the oracle's step must show the
like: a fault, or the task switch, ring change or V86 entry. Paging, the
64 KiB slot and a CS limit other than FFFFh or FFFFFFFFh stop the RTL
wherever they arise and are not compared with the oracle's step
([Known limits](#known-limits)).

Two harness rules are new:
- **Relaxed writes for a nested fault or a far CALL that faults.** The RTL
  probes each slot before it stores any, as the 386EX does, where Bochs
  pushes and then faults. Where a step's delivery faults, or a protected-mode
  far CALL faults, the oracle's writes before the fault that the RTL did not
  make are left out of the comparison; every write the RTL makes must be
  the oracle's.
- **`ss_error_rule`** (5 times in the random programs): a protected-mode
  far CALL whose push faults #SS. Bochs's `write_new_stack_*` gives the
  error code from SS's hidden RPL (its real-mode load sets the RPL to 0,
  `set_state` to the selector's), which the RTL does not keep; the RTL
  gives #SS(0), and the harness takes the oracle's error code from the
  frame at the handler's SS:ESP.

**The protected-mode cases** (`--mode pmode`, new, 51): from real mode,
LGDT, LIDT, PE set and a far jump to 16-bit code, then each of:
- `#UD`; `#NP(sel)`; `#GP(sel)` past the GDT; `#GP(sel)` and `#SS(sel)` for
  SS; `#GP(0)` and `#SS(0)` at FFFFh; `#DE` through a 386 and a 286 gate;
- INT n past IDTR's limit (`#GP(vec × 8 + 2)`), through a not-present gate
  (`#NP(vec × 8 + 2)`), a data descriptor (`#GP(vec × 8 + 2)`), a null CS
  (`#GP(0)`), a data CS (`#GP(sel)`), a not-present CS (`#NP(sel)`) and an
  offset past CS's limit (`#GP(0)`);
- a hardware exception's gate not present (`#NP` with EXT,
  `6 × 8 + 3`), a contributory fault in a contributory delivery (`#DF(0)`),
  and one in the double fault's (shutdown);
- INT n through a 286 interrupt gate and a 386 trap gate whose handlers
  IRET and IRETD back, INTO, INT3, a trap gate keeping IF; a task gate (not
  built); an IRETD whose image sets RF returning to a 16-bit IRET (added
  after Codex's review, [Review](#review));
- IRET after POPFD sets NT, a return from a nested task (not built; added
  after Codex's review);
- far CALL ptr16:16, ptr16:32 and m16:16 to RETF or RETF imm16, past CS's
  limit, to a not-present CS, and with SP 2 (no 64 KiB wrap: #SS(0), then
  #DF, then shutdown); RETF to data, IRET to a not-present CS, RETF to RPL 3
  (not built);
- IRET, IRETD, RETF and 32-bit RETF whose frame runs past a 16-bit stack's
  limit at 10000h, with SS based at A0000h so the stack is in the MMIO
  window: #SS(0) with no read, as Bochs reads the top slot first (added for
  #1424, [Review](#review));
- LLDT and LDT loads, a not-present LDT descriptor, a TI=1 load after a null
  LLDT, LLDT of a data descriptor; LTR with STR, LTR of a busy TSS, LTR of a
  null selector; LAR, LSL, VERR and VERW of code, data, a TSS and a selector
  past the GDT; ARPL by memory (raising RPL and not) and register;
- LLDT and ARPL in real mode (#UD).

Each case's ending (HLT after the software interrupt it names, the
exception its handler halts in with its error code from the frame,
shutdown, or the stop as not built) is checked, and every retirement is
compared with Bochs. Nine of them deliver a fault in a delivery.

## Oracle runners and test386

The adapter (`bochs_oracle.cpp`, `i386_oracle.h`), `vectors_tb.cpp`,
`test386_tb.cpp` and `divergences.toml` are unchanged from main.
- `test_fes_i386_vectors.py`: 1,758,700 tests, 1 revoked, 1,437,596 pass
  and 321,103 fail, as in slice 1's record; the fails are the divergence
  list's known cases.
- `test_fes_i386_test386.py` (the oracle): POST codes to FFh, 44,926 EE
  lines with none differing from the reference, final RAM SHA-256
  `44597188…`, as before.
- `test_fes_i386_test386.py --cpu rtl` (new: the lockstep bench's `test386`
  mode runs the ROM on the RTL alone; [report](test386-rtl-report.json)):
  POST 00h-06h pass (real-mode initialisation, conditional jumps and loops,
  multiply and divide, segment moves, strings, calls, LES and its kin), with
  no error. POST 08h's set-up then enters protected mode with
  `or eax, CR0_MSW_PE | CR0_PG` / `mov cr0, eax` (`src/test386.asm:625`)
  and the RTL stops there, at linear `000F267C`, as not built
  (`WHY_PAGING`), after 793,777 retirements. test386 has no build option
  without paging: its protected-mode part (POST 08h onward) builds page
  tables, rings and TSSes, so none of it runs before Phase 5's paging and
  slice 3's privilege levels.

## FreeDOS with HIMEMX, and the pc386 suites

Scratch disks, not committed:
- **Games image with HIMEMX's full XMS**: this tree's `freedos-games.img`
  with its FDCONFIG.SYS line `DEVICE=C:\FDOS\BIN\HIMEMX.EXE /VERBOSE` (no
  `/MAX=0`), SHA-256 `d3a97f59…`.
- **Boot fixture with HIMEMX**: the system bench's `freedos.img` with the
  games image's HIMEMX.EXE and MEM.EXE in C:\, FDCONFIG.SYS `DOS=HIGH` and
  `DEVICE=C:\HIMEMX.EXE /VERBOSE`, and MEM after VER in AUTOEXEC.BAT,
  SHA-256 `d7bf77f0…`.

| Run | Result |
| --- | --- |
| Machine sim, RTL CPU (`machine_tb --cpu rtl`), games image with HIMEMX, `mem` typed ([report](machine-himem-report.json)) | Pass: the prompt at 2.40 s and MEM's output by 3.65 s, with 15,360K of XMS, 159K used and 15,201K free. All 98,354,970 instructions retired on the RTL: 0 oracle steps, 0 stops, 1 protected-mode entry and 6 retirements with PE set (HIMEMX's unreal-mode set-up) |
| `system_tb` (`pc386_system`, RTL CPU, no oracle), boot fixture with HIMEMX, `--seed 1 --exercise-input 0` ([report](system-himem-report.json)) | Pass: `C:\>` at 2.896 s after 47,178,783 instructions. HIMEMX reports `XMS 3.00 80386 4G` and `Fast A20 method used`, MEM shows 15,360K of XMS, 159K used and 15,201K free, and all 12 checks pass: the dirty map, the save, the record and its round trip |
| `sim_fes_pc386.py --suite boot` (oracle) ([report](boot-oracle-report.json)) | `C:\>` at 4.3 s; the VGA replay has 0 mismatches |
| `--suite boot --cpu rtl` ([report](boot-rtl-report.json)) | Pass: `C:\>` at 2.35 s; all 113,096,898 instructions retired on the RTL, with 0 oracle steps and 0 stops; the VGA replay has 0 mismatches |
| `--suite system` ([report](system-report.json)) | Pass: `C:\>` at 3.309 s, as in slice 1's record |
| `--suite devices` ([report](devices-report.json)) | 132 checks pass |
| `--suite governor` ([report](governor-report.json)) | `max`, 286-12, 386SX-16 and 386DX-40 pass: TP 210,892, 27,453, 39,932 and 104,609, as in slice 1's record, each within 0.04% of the table. 386DX-25 prints `GOV TP=0000FF5F DC=04A4 C18=0011F492` (65,375, 1,188, 1,176,722: within one count of slice 1's), then `system_tb` throws `port 2: a write burst cut short, then another begun at clock 119811804`, the bench gap [#1372](https://github.com/DeanoC/fes/issues/1372) that main's A20 record documents for the same run at the same clock. `--seed 1` gives TP 65,376 and the same throw |

These pc386 runs are of trees before the review fix (`7baa2a6f3`), which
changes only a protected-mode IRET with NT set, and before the #1424 fix
(`d9773b8f2`), which changes only the order of a protected-mode IRET's and
RETF's stack reads. The boot retires nothing with PE set (`pe_entries` 0),
and the HIMEMX boots run only HIMEMX's unreal-mode set-up with PE set, no
IRET or RETF among it, so none of them reaches either change. A rerun of
`--suite boot --cpu rtl` at `7224b11f6` hit this session's 90-minute cap
on a loaded host before it finished, so it gives no result either way.

The misteross unit tests pass (1,627, 74 skipped), and `make test-changed`
against main `3d2e17f84` passes every lane it plans: the planner's tests,
whitespace, the parent Python regressions, generated-consumer consistency,
misteross's tests and the pc386 RTL simulation.

Since slice 1, main's #1391 gates A20 off in the BIOS, so HIMEMX now picks
its fast (port 92h) method and the instruction counts differ from slice 1's
record.

## Governor

The new SCOPE stems each have a cost class in `governor.toml`, from the
table's own sources (`CLASS_BITS` grows from 7 to 8; `fes_i386_gov_pkg.sv`
and `fes_i386_gov_rom.sv` are regenerated):

| Class | Stem | 286, 386, 486 |
| --- | --- | --- |
| `sldt_r`, `sldt_m` | 0F00 /0 | 2, 2, 2; 3, 2, 3 |
| `str_r`, `str_m` | 0F00 /1 | 2, 2, 2; 3, 2, 3 |
| `lldt_r`, `lldt_m` | 0F00 /2 | 17, 20, 11; 19, 24, 11 |
| `ltr_r`, `ltr_m` | 0F00 /3 | 17, 23, 20; 19, 27, 20 |
| `verr_r`, `verr_m` | 0F00 /4 | 14, 10, 11; 16, 11, 11 |
| `verw_r`, `verw_m` | 0F00 /5 | 14, 15, 11; 16, 16, 11 |
| `group6_ud` | 0F00 /6, /7 (#UD; never charged) | 2, 2, 1 |
| `lar_r`, `lar_m` | 0F02 | 14, 15, 11; 16, 16, 11 |
| `lsl_r`, `lsl_m` | 0F03 | 14, 23, 10; 16, 24, 10 (the 386's 20 or 25 by G, 21 or 26 from memory: the middle, rounded up) |
| `arpl_r`, `arpl_m` | 63 | 10, 20, 9; 11, 21, 9 |

These run only in protected mode (in real mode they raise #UD, which is
charged as INT's delivery), so their counts are the protected-mode ones. A
protected-mode INT n, IRET, far CALL or RETF is charged its real-mode
class's count, as every class is.

`test_fes_i386_governor.py` ([report](governor-bench-report.json)) passes,
and its report equals slice 1's merge report field for field: this branch
changes no cycle the bench measures, governed or not.

## Quartus seal

`make build-fes-pc386-quartus` at seed 1 from a clean checkout of
`7224b11f6`, the review fix's head ([`seal-summary.json`](seal-summary.json)),
Quartus Prime Lite 17.0.2 on the pc386 lock's Yosys `0a7927bf` netlist. All
109 input hashes match the branch head. Every analysis passes, with no
illegal or unconstrained clocks. Two earlier seals of this branch are data
points: `43bff581b`, before the fix, whose package the kit ran
([summary](seal-43bff581b-summary.json)), and `10956ac5c`.

| | Final, `7224b11f6` | `43bff581b` (kit runs) | `10956ac5c` | Slice 1's merge seal, `b7b4cb7dc` |
| --- | --- | --- | --- | --- |
| Build ID, package | `b70597c6…`, `ad844e72…` | `80b66665…`, `faf63b2f…` | `da4662c5…`, `fb241da6…` | `530f136f…`, `604cab9b…` |
| System Fmax, slow corners (61.44 MHz) | 69.51-69.88 MHz, setup +1.889 ns | 67.59-71.13 MHz, +1.481 ns | 71.51-72.22 MHz | 72.43-74.83 MHz, +2.470 ns |
| Pixel Fmax (74.27 MHz) | 81.75-82.56 MHz | 81.29-81.96 MHz | 80.60-81.69 MHz | 80.55-81.01 MHz |
| Setup, hold slack | +1.231 (pixel), +0.085 ns | +1.162 (pixel), +0.077 ns | +1.057 (pixel), +0.091 ns | +1.049 (pixel), +0.080 ns |
| Recovery, removal, pulse width | +7.183, +1.190, +1.122 ns | +7.292, +0.268, +1.122 ns | +7.990, +0.265, +1.122 ns | +5.948, +0.264, +1.122 ns |
| ALMs | 28,831 of 41,910 (69%) | 28,535 | 28,674 | 28,311 |
| Registers | 25,755 | 25,714 | 25,699 | 25,527 |
| RAM blocks | 259 | 259 | 259 | 257 |
| DSP blocks | 32 | 32 | 32 | 32 |
| RBF | 3,644,880 bytes, SHA-256 `12f96a06…` | 3,631,716 bytes | 3,628,660 bytes | 3,576,248 bytes |

Against slice 1's merge seal the slice costs 220-520 ALMs across the three
seals (the two seals of one design differ by 139), 170-230 registers and two
RAM blocks. The microcode ROM's words grew from 79 bits to 90, and 130
of its 256 are used (59 before). The system clock's slowest corner falls
from 72.43 MHz to 67.6-71.5 MHz across the three seals, which keeps
1.48-2.0 ns of setup slack at 61.44 MHz. The `10956ac5c` and `43bff581b`
seals' inputs differ only in `fes_i386_cpu.sv`'s `FES_I386_DEBUG` block,
which nothing defines; main's merge changes no input. Their spread is
Quartus's placement noise on one design.

`d9773b8f2`, the #1424 fix, comes after the final seal and is not sealed:
it changes two microcode words and the decoded first uop of a
protected-mode IRET and RETF (its slot, its temporary, and `vmchk` on
IRET's). The pc386 reseal exemption lets a post-seal RTL fix merge without
a reseal; the next sealing PR covers the tree.

## Kit B smoke test: FreeDOS with HIMEMX, and DOOM's start

Two runs, claimed and released in Kit B's status file on 2 November 2026:
p2k1 at 11:30 and p2k2 11:30-11:33 EET. Each was core-only on Kit B's
named baseline, software `f786f7b3` (main `30595667`, OS `f904961e`): the
sealed package `faf63b2f…` (`43bff581b`) went through the agent's library
launch, with no install, image change, reboot or host change, and Kit B's
`usb-storage.quirks=152d:0583:i` line untouched. The baseline was unchanged
after each (same image, previous, pending, idle RBF, launcher, quirk and
boot ID as before the run; agent idle, lease free). The disk was the census
disk above (SHA-256 `fa26f60b…`). p2k1 staged the disk and then stopped on
a bug in the scratch console driver before it launched anything. Both runs
left the staged disk, and p2k2 the game's record, in FogCast's data
directories, as their claims said.

**FreeDOS with HIMEMX passes** ([MEM](kit-p2k2-mem.png)): the prompt within
33 s of the launch, HIMEMX's `Fast A20 method used`, and MEM showing
15,360K of XMS, 159K used and 15,201K free, as in the machine sim.

**DOOM does not start** ([60 s after `doom`](kit-p2k2-doom-60s.png)):
nothing follows `C:\DOOM>doom` in the 60 seconds the run watched; the five
frames differ only in the cursor's blink. The RTL machine sim of the same
disk shows why: DOS/4GW's FPU probe runs `FNINIT` in real mode, which this
build does not build, and a not-built stop halts the CPU on a kit. Past
that, DOS/4GW's 16-bit code segments would stop it too
([census](#the-boundary-from-a-real-target-doom-under-dos4gw)).
[`kit-p2k2-result.json`](kit-p2k2-result.json) has the run's commands and
acknowledgements.

## Review

Codex reviewed `b541e25e1` with one P2 finding: a 16-bit IRET that begins
with RF set (after an IRETD that set it) clears RF at its flags-load uop,
where it "does not load RF and must retain the existing bit". The RTL keeps
its behaviour, which is Bochs's and the architecture's:
- Bochs sets RF with `assert_RF()`, which invalidates the prefetch queue,
  and `prefetch()` (`cpu/cpu.cc`) clears RF before the next instruction
  runs. A 16-bit IRET after an IRETD that set RF therefore starts with RF
  clear and leaves it clear; RF is architecturally cleared as each
  instruction completes, except by an IRETD that loads it.
- In the RTL every uop but IRET's last clears RF, so the 16-bit IRET's
  first uops clear it and its last keeps the 0. The architectural EFLAGS
  shows IRETD's RF until the next instruction retires, as Bochs's does.
- A new directed case, `iretd-rf-iret16`, runs exactly this: an IRETD
  whose image sets RF returns to a 16-bit IRET. It passes, comparing RF
  after each retirement: Bochs has RF set after the IRETD and clear after
  the IRET, as the RTL does. With the RTL changed never to load RF, it
  fails at the IRETD (Bochs has EFLAGS `10082h`), so the case does reach
  RF set.

Codex's review of `bd86096a7` found a real gap (P2): a protected-mode IRET
with EFLAGS.NT set returns from a nested task through the TSS backlink,
which this build does not do, yet it ran the same-privilege IRET. NT is
reachable at CPL 0 through POPFD or IRETD. `7baa2a6f3` stops it as not
built:
- IRET's first uop in protected mode carries `ntchk`. EX checks NT in the
  in-order EFLAGS as it runs that uop, so a POPFD or IRETD just ahead of
  the IRET counts, and stops it with `WHY_TASK_RETURN`.
- The stop stands over that uop's stack-read fault, since Bochs takes the
  task return before it reads the stack.
- The lockstep's not-built rule takes a fault or a TR change as the like for
  this reason.
- A directed case, `iret-nt` (POPFD with NT set, then IRET), passes: the
  RTL stops as not built and Bochs faults on TR's backlink. Without the
  check it fails: Bochs raises #TS and the RTL returned and then took #GP.

The CS-limit follow-up's random programs (validating #1414) found one more,
filed as [#1424](https://github.com/DeanoC/fes/issues/1424): with an
injected interrupt's frame wrapped to the top of a 16-bit stack, the
handler's IRET reads its slots at SP, SP+2 and 10000h, past the limit.
Bochs's `iret_protected` reads EFLAGS's slot first, then CS's, then the
offset's, so it raises #SS(0) before reading anything; the RTL read the
offset's and CS's slots first. With the stack in the lockstep's MMIO window
those were device reads Bochs never made (`random#17#229`, a seed past the
16 the lockstep runs, fails `mmio-reads.0.4`). `return_protected` likewise
reads RETF's CS slot before its offset's, which the RTL read first.
`d9773b8f2` reads both in Bochs's order:
- In protected mode DC's first uop reads IRET's EFLAGS slot into T4,
  carrying `vmchk` (under `ntchk`, which still wins in EX), or RETF's CS
  slot into T0. `piret` then reads CS's slot and the offset's, and `pretf`
  the offset's. No routine grows; two ROM words change.
- `random#17#229` now passes: #SS(0) with no device read.
- Four directed cases (`iret-ss-limit`, `iretd-ss-limit`, `retf-ss-limit`,
  `retf32-ss-limit`) put SS at A0000h and the frame past its limit. They
  pass, and with the RTL before the fix they fail, the RTL reading 4, 8, 2
  and 4 device bytes before the #SS.

The follow-up also filed [#1425](https://github.com/DeanoC/fes/issues/1425),
a harness gap for a REP string that stores over its own bytes in a run
without the window; it is the lockstep's, not this slice's, and is not in
this record.

## Yosys workarounds

None. `10956ac5c` made a packed struct's 8-bit stack-slot displacement
(`pipe_t.sdisp`) unsigned, M1 reading its sign from bit 7, as a precaution
before the first seal. The locked Yosys builds
(i386 `238eac9a4`, pc386 `0a7927bf`) elaborate and evaluate a signed member
of a packed struct correctly (a repro sign-extends -2 into a 32-bit sum), so
no fork issue is filed.

## Known limits

The [sim README](../../../sources/misteross/cores/fes-common/sim/i386/README.md#known-limits)
lists them; new or changed in this slice:
- **Probe before push.** The RTL probes every slot of a delivery or a far
  CALL before it stores any, as the 386EX does; Bochs pushes and then
  faults. Where the push faults, the RTL writes less, and the lockstep
  compares only the writes the RTL makes.
- **Far CALL's #SS error code** comes from SS's hidden RPL in Bochs
  (`ss_error_rule`); the RTL gives #SS(0), as a 386 at CPL 0 does once SS
  is loaded in protected mode.
- **ARPL's memory form** is a read-modify-write in Bochs, which the adapter
  logs as a write even when ARPL writes nothing; the lockstep leaves that
  write out when ARPL leaves ZF clear and nothing is delivered.
- **The not-built check.** A not-built stop for a task gate, an outer
  return, a transfer through a gate or TSS, or V86 must match a fault or
  that transition in Bochs's step; paging, a 16-bit stack slot past 64 KiB
  and a CS limit other than FFFFh or FFFFFFFFh stop the RTL wherever they
  arise and are not compared with Bochs's step.
- **TR is held only.** LTR loads it and marks the TSS busy; nothing reads it
  until task switches (Phase 5).

## Not in this record

- **Privilege transitions, call gates, task gates, IOPL and the I/O bitmap,
  debug registers** (the third slice), **paging, V86 and task switches**
  (Phase 5). Each stops the CPU as not built with its reason.
- **The empty-387-socket ESC forms** (the design's FPU section): DOS/4GW's
  FPU probe needs them before DOOM can start on the RTL.
- **The general CS-limit check**, for code under a limit other than FFFFh
  (16-bit) or FFFFFFFFh (32-bit): DOS/4GW's kernel runs in such segments.
- **The bench gap #1372**, which 386DX-25's governor run meets.
- **[#1428](https://github.com/DeanoC/fes/issues/1428)**, `random#26#1633`
  from the extra seeds ([Lockstep](#lockstep)), and the CS-limit
  follow-up's harness gap [#1425](https://github.com/DeanoC/fes/issues/1425).
