# i386 Phase 5: what the blocked titles need, 8 November 2026

This record plans CPU Phase 5 (V86, paging and task switches) for
`fes_i386_cpu`
([`i386-cpu-design.md`, "Phase 5: V86, paging and task switches"](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches)).
It takes the titles that the pc386 compatibility census
([#1533](https://github.com/DeanoC/fes/pull/1533)) found blocked on Phase 5,
runs each on the Bochs oracle with a finer instruction census, and lists
exactly which Phase 5 features each one uses. It also runs JEMM386, the
Phase 5 exit's memory manager, under Wolfenstein 3D, DOOM and Master of
Orion. It changes no CPU behaviour, and no RTL ran. Tracker:
[#1548](https://github.com/DeanoC/fes/issues/1548).

**Result.**
- **Paging alone unlocks nothing that was blocked on Phase 5.** Every title
  that pages also needs a ring-3 transition (Phase 2's third slice,
  [#1535](https://github.com/DeanoC/fes/issues/1535)), a task switch, V86, or
  all of them. The brief's "paging alone unlocks CWSDPMI and DOS/4GW
  Professional" does not hold. CWSDPMI enters and leaves ring 3 by far JMP
  to a TSS. DOS/4GW Professional takes its page faults through a task gate.
- **No title arms a breakpoint.** CWSDPMI, U.S. Navy Fighters and Strike
  Commander write the debug registers, but DR7 never changes from 400h and no
  #DB is raised. They need MOV to and from DR0–DR7, not the breakpoint
  compares.
- **No title uses INVLPG.** It is a 486 instruction and stays #UD.
- **Every V86 monitor runs DOS at IOPL 3.** JEMM386, Pinball Fantasies'
  eXceed and Strike Commander's own manager all do. So CLI, STI, PUSHF and
  POPF never fault, and every IN and OUT in V86 reads the TSS I/O bitmap.
  Under JEMM386, Wolfenstein 3D makes 26.5M bitmap-checked I/O accesses and
  about 1M V86 interrupt round trips in 24 simulated seconds. The V86
  interrupt delivery and the IRETD back to V86 are therefore hot paths.
- **JEMM386 boots on the oracle,** and Wolfenstein 3D runs with EMS under it.
  It makes 781 EMS calls (INT 67h) and reaches its options menu. DOOM reaches
  `I_StartupTimer` through VCPI, as it does without JEMM386.
- **A 16-entry DTLB and an 8-entry ITLB miss on at most 0.47% and 1.1% of
  accesses.** The worst cases are Angband for the DTLB and Master of Orion's
  DOS prompt for the ITLB. An 8-entry DTLB misses on 2.86% of Wolfenstein 3D's
  data accesses under JEMM386; a 16-entry one on 0.033%.
- **Strike Commander is a Phase 5 title.** The census counted it as a DOS
  setup failure (`FILES`), which the games image now fixes. On the oracle it
  then runs its own V86 monitor under paging, entered by IRETD with NT set: a
  task switch into a V86 task.

## Method

- **Tree.** #1533's head `0a847f73f`, which has the game drives and
  `sim_fes_pc386.py --suite game --instruction-census`, in a scratch
  worktree. Its census (`machine_tb --census`) was extended there and not
  committed. The extensions:
  - **Stems under paging.** The census read an instruction's bytes at its
    linear address as if it were physical, so with paging on its stems were
    wrong (U.S. Navy Fighters' "00" and Descent's "FF.7" in #1533's runs).
    The scratch census walks the oracle's page tables, without setting A or
    D, for the stem and for the IDT gate it decodes.
  - **System registers by number:** MOV CRn, DRn and TRn split by register.
  - **Task switches by cause:** the instruction or event, the new TSS's type,
    NT, and whether CR3, LDTR or VM changed. Also the TSS's I/O-map base and
    limit at each TR change.
  - **V86:** steps by IOPL; CLI, STI, PUSHF, POPF, INT n and IRET by IOPL; I/O
    in V86 or at CPL > IOPL; and events delivered from V86 with their gate.
  - **TLB models.** Every paged step's instruction page and data pages,
    run through LRU models: fully associative DTLBs of 2, 4, 8, 16 and 32
    entries, 4-way DTLBs of 64 and 128, a 2-way of 256 and a direct-mapped
    64, and fully associative ITLBs of 1, 2, 4, 8 and 16. A CR3 that
    changes, or a PG toggle, flushes them. A CR3 write of the same value
    does not, so the miss rates are low by at most the TLB's size per such
    write: for Strike Commander, whose 18,960 CR3 writes are the most, by at
    most 0.13 percentage points at 16 entries.
- **Runs.** 20 simulated seconds each (Wolfenstein 3D 24, Quake 25), the
  title's launch and keys from its drive record, `--cpu oracle`, Powerboat.
  - The titles on their #1533 drives: Jump 'n Bump, Angband, NetHack,
    Chris's 3D Benchmark, Quake's timedemo, Time Circle, Descent, U.S. Navy
    Fighters, Pinball Fantasies and Strike Commander.
  - Quake's launch line has `+`, which `machine_tb` cannot type. Its run
    used a scratch copy of the `bench` drive with the line in a `Q.BAT`.
  - **JEMM386.** Scratch copies of the `real`, `pmode` and `xms` drives with
    `FDCONFIG.SYS`'s UMBM line replaced by `HIMEMX.EXE` then
    `JEMM386.EXE I=C800-EFFF`. JEMM386 is v5.85 from the pinned FreeDOS
    Jemm package (`jemm` in `freedos.lock`, file SHA-256 `a7baf962…`). The
    frames show "Jemm386 loaded".
- **Not committed.** The drives, the scratch census patch and the frames
  stay on Powerboat in `~/fes-scratch/p5plan`. [`census.jsonl`](census.jsonl)
  has one row per run: the counts below and the TLB models' accesses and
  misses. It holds no game bytes, frames or instruction bytes.

## What each title uses

Counts are per run. "Ring 3" is Phase 2's third slice (#1535): IRET to CPL 3,
deliveries to CPL 0 through the TSS's stack, call gates, and the
IOPL-sensitive instructions and I/O bitmap outside V86.

| Title (host) | Paging | Task switches | V86 | Debug registers | Ring 3 (#1535) |
| --- | --- | --- | --- | --- | --- |
| Jump 'n Bump (CWSDPMI) | yes; PG off and on around every DOS call (3,867 toggles), 1,934 CR3 writes; 245 #PF, error 6 (a CPL-3 write to a not-present page) | 5,796 far JMPs to 32-bit TSSs: CPL 0→3 (1,932), 3→0 (1,451), 0→0 (2,411) | no | 11,604 MOV DRn writes (DR0–DR3, DR6, DR7), 1,933 reads; DR7 stays 400h | IRETD to CPL 3, DPL-3 interrupt gates, far CALL through a call gate |
| Angband (CWSDPMI) | yes; 3,224 toggles; 656 #PF, errors 4 and 6 | 4,830 far JMPs to TSSs | no | 9,672 writes | as above |
| NetHack (CWSDPMI) | yes; 546 #PF, error 6 | 3,426 far JMPs to TSSs | no | 6,864 writes | as above |
| Chris's 3D Benchmark (CWSDPMI) | yes; 238 #PF, error 6 | 1,495 far JMPs to TSSs | no | 2,495 writes | as above; 164 #NM (x87 emulation) |
| Quake timedemo (CWSDPMI) | see below | | | | then needs an x87 |
| Time Circle (Borland 32RTM) | yes; 2,655 CR3 writes; 126 #PF, error 2 (a CPL-0 write) | none (LTR once) | no | none | IRETD to CPL 3 (7,296) |
| Descent (DOS/4GW Professional) | yes, at CPL 0 only; 829 #PF, errors 0 and 2 | every #PF through a task gate to a 32-bit TSS (829, 441 of them switching LDT), and back by IRET with NT (829) | no | none | none |
| U.S. Navy Fighters (its own extender) | yes; no #PF | none (LTR once) | no | 726 writes, 363 reads | IRET to CPL 3 (1,356), call gate back (891); then needs its CD |
| Pinball Fantasies (eXceed) | no | none (LTR once) | at IOPL 3, entered by IRETD (889); INT n through DPL-0 gates raises #GP(vector × 8 + 2), which eXceed reflects (641); 1,845 I/O accesses through the bitmap | none | IRETD to V86, delivery from V86 to CPL 0 |
| Strike Commander (its own manager) | yes; 18,960 CR3 writes; no #PF | one: IRETD with NT into a V86 TSS, switching LDT | at IOPL 3; INT n through DPL-0 gates as #GP reflections (about 15,000 of them, INT 67h's the most); 13.1M I/O accesses through the bitmap, 47 refused | 18 writes | delivery from V86 to CPL 0 |
| Wolfenstein 3D under JEMM386 | yes; 959 CR3 writes; no #PF | none (LTR once) | at IOPL 3; 984,044 INT n straight through DPL-3 gates to JEMM386 and 998,776 IRETDs back; 26.5M I/O accesses through the bitmap, 3,535 trapped as #GP(0) | none | none |
| DOOM under JEMM386 (VCPI) | yes; 13,315 CR3 writes | none; DOS/4GW's VCPI switches load TR 8,532 times | as above; 221,539 INT n | none | none |
| Master of Orion under JEMM386 | yes | none | as above; 2.1M INT n at the DOS prompt | none | none |

Quake's rerun is in [`census.jsonl`](census.jsonl) when it finished; its
host is the CWSDPMI the other four use.

**JEMM386's results.**
- **Wolfenstein 3D** makes 781 EMS calls (INT 67h) and reaches its options
  menu.
- **DOOM** starts through VCPI to `I_StartupTimer` at the 20-second limit,
  as without JEMM386.
- **Master of Orion** passes the EMS check that stopped it in #1533, then
  prints "reconfigure hardware options" and returns to the prompt. That is
  its hardware setup on the drive, not the CPU, and is not followed up here.
  The 2.1M INT n are FreeDOS's prompt waiting for a key.

**No title** uses INVLPG, arms DR7, raises #DB, or uses a 16-bit TSS. All
use 32-bit (type 9/B) TSSs.

## TLB sizes

Misses per 100 accesses. The DTLB columns count data accesses (a REP
string's every element, both pages of a page-crossing access); the ITLB
columns count instructions.

| Run | Data accesses | DTLB 8 | DTLB 16 | DTLB 32 | 64, 4-way | ITLB 1 | ITLB 4 | ITLB 8 | ITLB 16 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Jump 'n Bump | 162.1M | 0.062 | 0.033 | 0.030 | 0.028 | 12.11 | 0.011 | 0.009 | 0.008 |
| Angband | 22.6M | 1.124 | 0.469 | 0.240 | 0.168 | 1.82 | 0.393 | 0.182 | 0.026 |
| NetHack | 3.3M | 0.972 | 0.429 | 0.418 | 0.417 | 1.47 | 0.215 | 0.173 | 0.159 |
| Chris's 3D Benchmark | 123.5M | 0.757 | 0.140 | 0.034 | 0.016 | 0.29 | 0.010 | 0.001 | 0.001 |
| Time Circle | 155.3M | 0.038 | 0.023 | 0.017 | 0.016 | 7.71 | 0.019 | 0.008 | 0.005 |
| Descent | 102.8M | 1.196 | 0.098 | 0.062 | 0.060 | 3.07 | 0.354 | 0.035 | 0.020 |
| U.S. Navy Fighters | 2.3M | 0.444 | 0.219 | 0.205 | 0.202 | 2.92 | 1.067 | 0.653 | 0.508 |
| Strike Commander | 228.1M | 0.138 | 0.033 | 0.006 | 0.002 | 0.52 | 0.107 | 0.051 | 0.011 |
| Wolfenstein 3D, JEMM386 | 322.5M | 2.862 | 0.033 | 0.007 | 0.003 | 2.51 | 1.179 | 0.136 | 0.014 |
| DOOM, JEMM386 | 239.7M | 0.534 | 0.091 | 0.080 | 0.080 | 3.15 | 0.454 | 0.205 | 0.068 |
| Master of Orion, JEMM386 | 363.1M | 2.598 | 0.001 | 0.000 | 0.000 | 5.08 | 3.280 | 1.095 | 0.003 |

- **DTLB.** Sixteen entries take every run but Angband and NetHack, the two
  smallest, below 0.1%. A 64-entry 4-way second level would save at most
  0.3 points (Angband), about a walk per 750 data accesses. So the second
  level stays a Phase 7 option, built only if walks show in a profile.
- **ITLB.** One entry, the current page, misses on up to 12% of
  instructions: CWSDPMI's hot loop spans three pages. Eight entries miss on
  at most 1.1%, Master of Orion's idle DOS prompt; on 0.21% or less in every
  title that runs.

## test386.asm

From the pinned source (`cfd052d1e64d…`):
- POST 08 builds a page directory and two page tables and enters protected
  mode with paging on. Its whole protected-mode part therefore waits for
  paging.
- POST 11 checks #PF's error codes over every PDE and PTE combination of P,
  U/S and R/W, at CPL 0 and CPL 3 (supervisor writes ignore R/W), and that a
  write sets D.
- POST 20 is ring 3 (#1535).
- POST 21 is V86: CLI, STI, PUSHF(D), POPF(D), INT n and IRET at IOPL 0
  (#GP(0)), I/O through the TSS bitmap, INT n at IOPL 3 to a non-CPL-0 or a
  conforming handler (#GP(selector)), HLT, and 16-bit interrupts in V86.
- POST 22 is task switching: 16- and 32-bit TSSs, JMP and CALL through task
  gates, INT through task gates, the busy bit, the back link and NT, a ring-2
  handler from each TSS type, and a switch into V86 through a task.
- It does not touch the debug registers.

## Follow-ups

- Land the census extensions (paged stems, system registers by number, task
  switches, V86, TLB models, a flush on every CR3 write) in
  `machine_tb --census`, so Phase 5's slices can rerun this record.
- Recheck Master of Orion's hardware setup on the `xms` drive once JEMM386
  runs on the RTL.
