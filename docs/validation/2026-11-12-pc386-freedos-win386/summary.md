# pc386: why WIN386 refuses pc386's FreeDOS, 12 November 2026

Windows for Workgroups 3.11 reaches Program Manager in 386 enhanced mode on
MS-DOS 6.22 on the Bochs oracle. On pc386's FreeDOS games image, WIN386 exits
with "ERROR: Unsupported MS-DOS version", even with `VERSION=6.22`
([Windows plan record](../2026-11-11-pc386-windows-plan/summary.md), tracker
[#1702](https://github.com/DeanoC/fes/issues/1702)). This record finds why and
decides whether pc386 can fix it. Issue:
[#1725](https://github.com/DeanoC/fes/issues/1725).

**Result.**
- **pc386's kernel does not answer WIN386's DOS callouts.** FreeDOS 1.4's
  kernel 2043 (`KERNL386.SYS`, the FAT32 build that pc386 ships) leaves two
  of WIN386's INT 2Fh calls unanswered:
  - AX=1605h, the Windows start-up broadcast;
  - AX=1607h BX=0015h, DOSMGR's request for the DOS patch table.

  MS-DOS 6.22 answers both. Without the patch table, DOSMGR quits with
  "Unsupported MS-DOS version".
- **The reported version is not the cause.** Kernel 2043 reports true version
  7.10 (INT 21h AX=3306h). A TSR that made AX=3306h report 6.22 changed
  nothing: WIN386 still quit with the same message.
- **Newer FreeDOS kernels answer the callouts but still fail.** Kernels
  2044-2046 build in the Windows 3.x support (`WIN31SUPPORT`). Kernel 2046's
  386 FAT16 build answers both callouts, and WIN386 runs further on it. It
  still did not reach Program Manager in any of the configurations tried:

  | Kernel 2046 configuration | Where WIN386 stops |
  | --- | --- |
  | games image, boot entry 1 (`DOS=UMB`, `DOSDATA=UMB`, UMBM, HimemX) | "ERROR: Unsupported data configuration." |
  | entry 2 (`DOS=HIGH,UMB`, UMBM, HimemX) | exits to the prompt with no message |
  | entry 2 with `InDOSPolling=TRUE` in SYSTEM.INI | "Either memory is insufficient to run KRNL386.EXE or the value of the WindowMemSize entry in the SYSTEM.INI file is too large." |
  | a bare `FDCONFIG.SYS`: `FILES=30`, `BUFFERS=16`, HimemX only (no UMBM, no `DOS=` lines), with `InDOSPolling=TRUE` | the same insufficient-memory message |
- **Decision: a FreeDOS limitation, documented, not fixed in pc386.** The
  missing support is in the FreeDOS kernel itself, so pc386 does not patch
  FreeDOS. Upstream tracks it as
  [FDOS/kernel#50](https://github.com/FDOS/kernel/issues/50) and
  [FDOS/kernel#151](https://github.com/FDOS/kernel/issues/151). In those
  threads, the kernel's maintainer says enhanced mode needs `InDOSPolling`
  and has been tested only without FAT32 and with Windows' own memory
  managers.
  Microsoft's HIMEM.SYS is commercial, so pc386 cannot ship it. Windows on
  pc386 needs a DOS the user supplies, such as MS-DOS or PC DOS 5 or later.
  pc386's FreeDOS fixture is unchanged: moving it to kernel 2046 alone does
  not run Windows, and it would change every pc386 suite and game drive.

## How WIN386 talks to DOS

WIN386 identifies its DOS through INT 2Fh AH=16h, the Windows multiplex
calls, while it starts:

- **AX=1605h** (Windows initialisation). Each TSR and the DOS kernel may add
  a startup-information structure to the chain in ES:BX. DOS uses it to name
  its instance data, the per-virtual-machine areas WIN386 must copy for each
  DOS box.
- **AX=1607h BX=0015h** (the DOSMGR device callout). DOS 5 and later answer
  with CX≠0 and a pointer to a patch table: the DOS version, where the
  kernel keeps DS and BX during an INT 21h call, the InDOS flag, the machine
  ID and the critical-section patch offsets. DOSMGR uses the table to run
  several virtual machines over one DOS.

## Method

- **Bench.** The Windows record's `machine_tb --cpu oracle` build and main's
  BIOS, run on chip-30x0-ai-dev, with the scratch drives from that record:
  `win-msdos` (MS-DOS 6.22 and WfW 3.11) and `win-freedos` (the games image
  with WfW's `C:\WINDOWS` and `VERSION=6.22`). Copies only: the media are
  commercial and stay in scratch.
- **The trace.** A scratch TSR, run before `win`, logged every INT 2Fh AH=16h
  call and every INT 21h AH=30h, AH=52h and AX=3306h call to port E9h. It
  logged registers in and out, so the bench printed each call. WIN386
  reflects these calls from V86 mode to the TSR, so the log covers WIN386's
  probes as well as WIN.COM's. Neither the TSR nor its logs are committed.
- **Kernel 2046.** The `ke2046_386f16` build from the
  [FDOS/kernel 2046 release](https://github.com/FDOS/kernel/releases/tag/ke2046)
  (`ke2046_386f16.zip`, SHA-256 `1e672ddd…46aa`), copied over `KERNEL.SYS`
  on scratch copies of `win-freedos`. Its boot banner reads "Kernel
  compatibility 6.22 - WATCOMC - 80386 CPU required". Kernel 2043's reads
  "Kernel compatibility 7.10 - WATCOMC - 80386 CPU required - FAT32 support".
- **The 3306h test.** A 57-byte TSR that answered INT 21h AX=3306h with
  BX=1606h (6.22) and passed every other call on, run before `win` on
  `win-freedos`.

## The callouts, side by side

Registers as AX BX CX DX ES DI DS SI, in hex. Out is the call's return.

| Call | MS-DOS 6.22 | FreeDOS 2043 | FreeDOS 2046 |
| --- | --- | --- | --- |
| INT 2Fh AX=1605h, out | ES:BX = 05EC:12EF (startup info) | unchanged: no chain | ES:BX = 00DA:009A |
| INT 21h AX=3306h, out | BX=1606h (6.22) | BX=0A07h (7.10) | BX=1606h (6.22) |
| INT 2Fh AX=1607h BX=0015h CX=0, out | CX=0001h, BX=1022h | unchanged (BX=0015h, CX=0) | CX=0001h, DX:BX=00DA:00B8 (the kernel map's `_winPatchTable`) |
| next | DOSMGR's further 0015h calls (CX=4, 1, 3) are answered, then Windows starts | DOSMGR asks twice more (CX=4, 1), gets no answer, sends AX=1606h (Windows exit), and WIN386 quits: "ERROR: Unsupported MS-DOS version." | DOSMGR's further 0015h calls are answered; WIN386 stops later (table above) |

Before the AX=1605h broadcast, the three runs make the same calls with the
same results (AX=160Ah, AX=1600h, AH=52h and AH=30h, with `VERSION=6.22`
giving 6.22 on FreeDOS).

## Follow-ups

- When FreeDOS's kernel runs WfW 3.11 in enhanced mode with free memory
  managers, upstream will show it in FDOS/kernel#50 or #151. Moving pc386's
  kernel lock then is a deliberate change with its own suites.
- Standard mode is not an alternative. WfW 3.11 has none, and pc386's share
  has no Windows 3.0 or 3.1 media
  ([Windows record](../2026-11-11-pc386-windows-plan/summary.md#versions-run)).
