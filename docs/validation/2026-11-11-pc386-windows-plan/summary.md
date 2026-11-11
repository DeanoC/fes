# pc386 and early Windows: what it needs and how far it gets, 11 November 2026

This record plans pc386's next CPU and machine work after i386 Phase 5
(tracker [#1548](https://github.com/DeanoC/fes/issues/1548)) for early
Windows. It installs Windows on scratch drives and runs it on the Bochs
oracle with `machine_tb --census`, as the
[Phase 5 plan](../2026-11-08-i386-phase5-plan/summary.md) did for DOS games.
It records which 386 features and which machine devices Windows uses, and
how far main's RTL gets. It changes no CPU behaviour. Tracker:
[#1702](https://github.com/DeanoC/fes/issues/1702).

**Result.**
- **Only Windows for Workgroups 3.11 could be run.** The OS share holds
  WfW 3.11 (OEM, 1.44 MB floppies) and Windows 98 SE. It has no Windows 3.0,
  3.1 or 95. WfW 3.11 has no standard mode: `WIN /S` prints "Windows for
  Workgroups 3.11 does not run in real or standard modes". So only 386
  enhanced mode was run.
- **On the oracle, WfW 3.11 reaches Program Manager in 386 enhanced mode.**
  On MS-DOS 6.22, Program Manager is up less than 2.7 simulated seconds
  after `win` is typed, with VGA 640×480×16 and the PS/2 mouse. With a
  Bochs CPU, pc386's machine and devices are therefore enough.
- **Main's RTL runs WIN386 itself, then stops at two deliberate invalid
  opcodes.** In `--cpu rtl --rtl-at-prompt`, the RTL retired 39.0M
  instructions after the DOS prompt, 11.3M of them in protected mode:
  WIN386's switch to protected mode, paging, V86 at IOPL 3 and Windows'
  ring 3. It stopped twice, both times where a 386 raises #UD:
  - `0F A6` in WIN386's real-mode loader (0D83:0E46). This was XBTS on
    B-step 386s and is #UD on later ones; the loader probes it.
  - `0F FF` in KRNL386 at 0117:AF69, at CPL 3 under paging, where the
    oracle also takes a #UD.
  The RTL treats undefined encodings as not built and stops on them
  (`FAULT_UNIMPLEMENTED`) instead of raising #UD. That is the first
  blocker. Filed as [#1701](https://github.com/DeanoC/fes/issues/1701).
- **Windows needs no task switch, no armed breakpoint, no 486 instruction
  and no x87.**
  - WIN386 loads TR once. It writes DR0–DR3, DR6 and DR7 once at start
    and never changes DR7 (no debugger is loaded).
  - No CPUID, BSWAP, CMPXCHG, XADD or INVLPG runs.
  - x87 appears only as one real-mode FPU probe (FNINIT, FNSTCW, FNSTSW).
  So slices 4 (task switches) and 5 (breakpoints and TF) of Phase 5 are
  not on Windows' path. Slices 1–3 and Phase 2's ring 3 are.
- **Windows is the heaviest V86 user so far.** In 10.7 simulated seconds
  of Windows:
  - 621,160 IRETDs enter V86, all at IOPL 3 bar 406 at IOPL 0.
  - 613,278 #UD exceptions come out of V86 code: WIN386's V86 call-backs,
    which are invalid opcodes by design.
  - 490,416 LIDTs run at CPL 0.
  - The 16-entry DTLB and 8-entry ITLB models walk on 3.0% and 3.3% of
    accesses: 6.5 and 3.0 times the DOS titles' worst.
- **The host DOS matters.** On pc386's FreeDOS games image, WIN.COM
  refuses the reported version. With `VERSION=6.22`, WIN386 starts,
  enables paging and runs 18M V86 steps, then exits with "ERROR:
  Unsupported MS-DOS version". MS-DOS 6.22 works.
- **32-bit disk access (WDCTRL) fails its validation** at "phase 03, 3F" on
  pc386's BIOS (QEMU's SeaBIOS fails at "phase 07, 30"). Windows then runs
  without it, through the BIOS's INT 13h in V86.

## Versions run

| Version and mode | Media | How far it got | First blocker |
| --- | --- | --- | --- |
| Windows 3.0 (real, standard, 386 enhanced) | none on the share | not run | media |
| Windows 3.1 (standard, 386 enhanced) | none on the share | not run | media |
| WfW 3.11, standard mode | OEM floppies | `WIN /S` refuses (QEMU check) | none: WfW 3.11 has no standard mode |
| WfW 3.11, 386 enhanced, MS-DOS 6.22, oracle | as above | Program Manager by 6.0 s simulated (`win` at 3.3 s) | none |
| WfW 3.11, 386 enhanced, MS-DOS 6.22, MS-DOS Prompt from Program Manager, oracle | as above | a full-screen DOS virtual machine at `C:\WINDOWS>` by 14.0 s; no new CPU feature (619,955 V86 entries, 379 of them at IOPL 0; 2,114 CR3 writes, no task switch) | none |
| WfW 3.11, 386 enhanced, MS-DOS 6.22, RTL from the prompt | as above | 39.0M retired, 11.3M in protected mode, into KRNL386 at CPL 3 | `0F A6` in WIN386's loader, then `0F FF` in KRNL386: #UD not built ([#1701](https://github.com/DeanoC/fes/issues/1701)) |
| WfW 3.11, 386 enhanced, 32-bit disk access, oracle | as above | WDCTRL "validation failed at phase 03, 3F"; a key goes on without it | WDCTRL's BIOS check (machine side) |
| WfW 3.11, 386 enhanced, FreeDOS games image, oracle | as above | WIN386 enters paging and V86, then "Unsupported MS-DOS version" | FreeDOS as WIN386's DOS |
| Windows 95 | none (98 SE only, on CD; pc386 has no CD-ROM) | not run | media |

No kit run yet: in simulation, main's RTL stops deterministically at the
loader's `0F A6`, so a kit on main's seal would only show that stop.

## Method

- **Tree.** Main at `bb5f1bfc1` (Phase 5 slices 1–3 merged). The census is
  main's `machine_tb --census` ([`pc386-game-drives.md`, "Instruction
  census"](../../../sources/misteross/docs/pc386-game-drives.md#instruction-census)),
  summarized by `scripts/pc386_census_summary.py`.
- **Install.** The media are commercial and never leave scratch. MS-DOS 6.22
  was installed from its three setup floppies by its SETUP, then WfW 3.11
  by its SETUP, in QEMU (486, 16 MiB) on a blank 128 MiB disk of pc386's
  geometry (260/16/63).
  - Custom setup picked what SETUP detected: VGA, the Microsoft or IBM PS/2
    mouse, the US 101/102-key keyboard. No network, no printers. The
    tutorial was skipped, and SETUP edited CONFIG.SYS and AUTOEXEC.BAT.
  - SETUP's own CONFIG.SYS loads SETVER, WfW's HIMEM, `DOS=HIGH`, FILES=30,
    IFSHLP and STACKS, and AUTOEXEC.BAT runs SMARTDRV. The only edit after
    SETUP is HIMEM's `/TESTMEM:OFF`, to skip the memory test in simulation.
  - SETUP enabled 32-bit file access (VFAT, VCACHE) and not 32-bit disk
    access. A second drive adds the three SYSTEM.INI lines that turn
    WDCTRL on.
  - SETUP ran on a QEMU 486 with an FPU; it picks the same drivers for a
    386.
- **Drives** in `~/fes-drives/pc386/` by SHA-256, with `by-name` links and
  records that name each title's launch:
  - `win-msdos` `c5d77840…`: MS-DOS 6.22 and WfW 3.11.
  - `win-msdos-32bit` `54dad425…`: the same with WDCTRL on.
  - `win-freedos` `40f587c9…`: the `dpmi` game drive (FreeDOS games image)
    plus the same `C:\WINDOWS`, with `VERSION=6.22` first in FDCONFIG.SYS.
- **Runs**, all with `--cpu oracle` unless named, on Powerboat and a second
  build host:
  - `wfw-386`: `win` at the prompt, 14 simulated seconds, census.
  - `wfw-386-fd`: `c:\windows\win` on the FreeDOS drive, census (ends at
    the prompt at 8.2 s).
  - `wfw-dosbox`: `win`, then the arrow keys to the MS-DOS Prompt icon and
    Enter, census. The bench ends a run at a DOS prompt, so it ended at
    13.95 s on the DOS virtual machine's `C:\WINDOWS>`.
  - `wfw-rtl`: `--cpu rtl --rtl-at-prompt 1`, 14 s. The oracle boots DOS;
    the RTL takes over at the prompt. The bench gives the CPU back to the
    RTL only in real mode or at CPL 0 without paging, but once running, the
    RTL runs paging, V86 and CPL 3 itself until a stop. The bench's STOP
    line reads the stopped instruction's bytes as if its linear address
    were physical; a rerun to 4.2 s with a scratch, uncommitted bench that
    walks the page tables there read the second stop's bytes as `0F FF`,
    with the same retirement counts.
  - I/O: the `wfw-386` and `wfw-386-32bit` launches again with
    `PC386_TRACE_IO=1`, counted per port into [`io.jsonl`](io.jsonl), not
    stored raw.
- **The oracle has a 387.** Bochs's FPU answers the probe, so on the oracle
  Windows sees a coprocessor. pc386 has an empty socket. No x87
  instruction but the probe ran, so this does not change the census, but
  WIN87EM's emulation path for applications was not exercised.
- **Files.** [`census.jsonl`](census.jsonl) has one summary row per census
  run and [`io.jsonl`](io.jsonl) the per-port I/O counts. Neither holds
  Windows bytes, frames or file contents.

## What WfW 3.11 uses from the CPU

Counts are from `wfw-386`, 14 simulated seconds: DOS boot, `win` at 3.3 s,
Program Manager by 6.0 s, then idle. 234.2M of its 311.7M steps ran with
paging on: 31.6M in V86 mode, 26.2M at CPL 3 in protected mode and the
rest at CPL 0.

| Feature | Use | Provided by |
| --- | --- | --- |
| Protected mode entry and paging | WIN386's loader loads CR3 and the GDT in real mode, sets PE and PG; 1,666 CR3 writes (one per VM switch), 279 #PF (error 6: CPL-3 writes to not-present pages, 16 from V86) | Phase 5 slice 1 (paging) |
| V86 mode | entered by IRETD 621,160 times, at IOPL 3 (406 at IOPL 0, during start-up); CLI, STI, PUSHF, POPF and IRET run there 1.6M times without faulting; INT n through DPL-3 386 interrupt gates to CPL 0 (4,712) | slice 3 (V86) |
| #UD | 613,278 from V86 code: WIN386's V86 call-backs and breakpoints are invalid opcodes by design. Also KRNL386's `0F FF` at CPL 3 and WIN386's `0F A6` probe in real mode | slice 3 delivers from V86; #UD for undefined encodings is not built ([#1701](https://github.com/DeanoC/fes/issues/1701)) |
| I/O bitmap | 25,065 V86 accesses allowed and 2,812 trapped as #GP(0); 4,178 allowed and 401 trapped at CPL 3 in protected mode. One TSS, I/O map base 68h, limit 2069h | slice 3 (V86), Phase 2 slice 3 (#1535, CPL 3) |
| Ring 3 | Windows' KRNL386, USER and GDI run at CPL 3: INT 30h to CPL 0 through a DPL-3 386 gate (124,571); INT 2Fh, 31h, 21h, 33h, 15h and 10h through DPL-3 286 trap gates to CPL-3 handlers (123,231); INT 28h through a DPL-0 gate as #GP(142h) (121,958); #NP on segment loads (3, most likely segments loaded on demand) | Phase 2 slice 3, Phase 2's gates |
| VMM calls | INT 20h at CPL 0 (1,358): VxD service calls, which the VMM patches in place on first use | Phase 2 delivery; self-modifying code under paging |
| LAR, LSL, VERW | 14,887 LAR and 2,504 LSL at CPL 3 and 0; 12 VERW | Phase 2 slice 2 |
| LIDT | 490,416 at CPL 0, and 1,675 SGDT | Phase 2 |
| Debug registers | MOV to DR0–DR3, DR6 and DR7 once each, DR6 read twice, DR7 once; DR7 never changes, no #DB | slice 2 (built) |
| Task switches | none: LTR once; LLDT three times, of one LDT | slice 4 not needed |
| Breakpoints, TF | none | slice 5 not needed (WDEB386 would need them) |
| 486 instructions | none: no CPUID, BSWAP, CMPXCHG, XADD, INVD, WBINVD or INVLPG | — |
| x87 | FNINIT, FNSTCW and FNSTSW once each, in real mode before WIN386 | the empty socket's answers (as built) |

**TLB pressure.** Walks per 100 accesses in the census's LRU models of the
planned TLBs:

| Run | DTLB 16 | ITLB 8 |
| --- | ---: | ---: |
| `wfw-386` | 3.034 | 3.294 |
| DOS titles' worst ([Phase 5 plan](../2026-11-08-i386-phase5-plan/summary.md#tlb-sizes)) | 0.469 (Angband) | 1.095 (Master of Orion's prompt) |
| `wfw-dosbox` (a DOS virtual machine from Program Manager) | 3.043 | 3.291 |

Windows runs three working sets at once: the VMM's linear pages above
80000000h, the V86 machine's low megabyte and Windows' ring-3 segments.
Every V86 call-back crosses all three. A 386's own TLB is 32 entries,
4-way, unified.

## What it needs from the machine

From the per-port I/O counts of `wfw-386` (the whole run, DOS boot
included):

| Area | What Windows does | pc386 today |
| --- | --- | --- |
| Memory | HIMEM's XMS; DOS=HIGH; WIN386 takes the rest of the 16 MiB | 16 MiB is plenty for WfW 3.11 |
| A20 | the 8042's output port (D1h, once) and port 92h (19 writes, 2,566 reads) | both built (#1386) |
| PIT and PIC | 245,245 channel-0 latches (OUT 43h, 0) with 490,488 reads of 40h, and 245,246 IRR reads (OUT 20h, 0Ah; IN 20h): the timer and PIC VxDs reading time | built; every access is an ISA I/O at CPL 0, so its cost is the machine's ISA charge (#1362) |
| Interrupt vectors | VPICD moves the master PIC to 50h and the slave to 58h | built |
| RTC and CMOS | registers 00h–3Fh read at start; NMI masked through bit 7 | built |
| Keyboard and mouse | 8042 commands ADh/AEh, A8h and D4h: the PS/2 mouse through the 8042, Windows' own MOUSE.DRV and VMD | built; Program Manager shows the pointer |
| Display | VGA.DRV and VDDVGA at 640×480×16 (planar): 38,315 writes to 3CFh; the DAC is read back (VGA.3GR saves it for DOS boxes) | built; pc386's VGA has no SVGA mode, so VGA is the only display driver |
| Disk | through the BIOS's INT 13h in V86: 1.36M word reads from 1F0h (no 32-bit disk access) | built; WDCTRL fails its validation (phase 03, 3F) |
| Probes | COM 2E8h/3E8h, LPT 278h/378h, Hercules 3BFh, network cards 2Exh/6Exh, DMA page registers | answer as empty ISA space |
| Sound | not installed. WfW 3.11 ships SNDBLST2.DRV with VSBD.386 (Sound Blaster 2.0) and MSADLIB.DRV with VADLIBD.386 (OPL2) | pc386's SB 2.0 at 220h, IRQ 7, DMA 1 and its OPL2 match; not exercised |

## Plan to Program Manager in 386 enhanced mode on the RTL

Ranked by what blocks first:

1. **#UD for undefined encodings** ([#1701](https://github.com/DeanoC/fes/issues/1701)).
   Both RTL stops are deliberate invalid opcodes (`0F A6`, `0F FF`). A 386
   raises #UD for every encoding it does not define; the RTL stops on
   them. Nothing else stopped main's RTL in 11.3M protected-mode
   instructions, and it is a decode change, not a Phase 5 slice.
2. **Rerun `wfw-rtl` and a kit run after 1.** The RTL already runs WIN386's
   paging, V86 and ring 3 for 11.3M instructions. The next stop, if any,
   decides what follows.
3. **The host DOS** (a decision, not CPU work). WIN386 needs a DOS it
   accepts. MS-DOS 6.22 works. pc386's FreeDOS kernel does not pass
   WIN386's checks even when it reports 6.22. Either Windows needs a
   user-supplied MS-DOS or PC DOS, or the FreeDOS kernel's Windows
   support is a separate piece of work.
4. **Speed on Windows' hot paths**, once it runs:
   - the V86 round trip (IRETD into V86, and the #UD or INT delivery out);
   - LIDT;
   - the TLB. Sixteen DTLB and eight ITLB entries walk on about 3% of
     accesses here. A larger or second-level TLB, which the Phase 5 plan
     left to Phase 7, is worth measuring on Windows first.
5. **Machine follow-ups** that Program Manager does not need:
   - WDCTRL's validation, for 32-bit disk access;
   - Sound Blaster 2.0 and AdLib drivers under Windows;
   - an x87 run of WIN87EM's emulation (pc386 has no FPU).
6. **Other versions.** Windows 3.0 and 3.1 (standard mode runs KRNL286 under
   DOSX) and Windows 95 need media. Windows 95 also
   needs its setup run from a hard disk, since pc386 has no CD-ROM.

Phase 5's slice 4 (task switches) and slice 5 (breakpoints and TF) are not
on this path. WDEB386, Windows' kernel debugger, would need slice 5.

## Divergences

None. Where the RTL and Bochs could differ here, on `0F A6`, both a
post-B-step 386 and Bochs at CPU level 3 raise #UD.

## Follow-ups

- [#1701](https://github.com/DeanoC/fes/issues/1701): #UD for undefined encodings.
- The machine bench's STOP line reads the instruction's bytes at its linear
  address as physical, so under paging its bytes and stem are wrong (the
  second stop prints `0000`). The census already walks the page tables.
- The bench gives the CPU back to the RTL only in real mode or at CPL 0
  without paging. Slices 1–3 run paging, V86 and CPL 3, so after a stop
  in one of those (here KRNL386's `0F FF`) the rest of a run is the
  oracle's. A stop in real mode, like the loader's `0F A6`, hands back at
  once.
