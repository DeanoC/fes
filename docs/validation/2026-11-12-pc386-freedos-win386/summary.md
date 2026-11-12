# pc386: WIN386 on pc386's FreeDOS, 12 November 2026

Windows for Workgroups 3.11 reaches Program Manager in 386 enhanced mode on
MS-DOS 6.22 on the Bochs oracle. On pc386's FreeDOS games image, WIN386 exits
with "ERROR: Unsupported MS-DOS version", even with `VERSION=6.22`
([Windows plan record](../2026-11-11-pc386-windows-plan/summary.md), tracker
[#1702](https://github.com/DeanoC/fes/issues/1702)). This record finds why, and
which FreeDOS configuration runs it. Issue:
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
- **Kernel 2046 runs WfW 3.11 to Program Manager, with FreeDOS's SHARE.**
  Kernels 2044-2046 build in the Windows 3.x support (`WIN31SUPPORT`).
  Kernel 2046's 386 FAT16 build answers both callouts. With four more
  settings, WfW 3.11 reaches Program Manager in 386 enhanced mode on the
  games image's own boot entry 2 (`DOS=HIGH,UMB`, UMBM, HimemX), with no
  Microsoft DOS component:
  - FreeDOS 1.4's `SHARE.COM`, run before `win`;
  - `InDOSPolling=TRUE` in SYSTEM.INI's `[386Enh]`;
  - WfW's 32-bit file access off: no `ifsmgr.386`, `vcache.386` or
    `vshare.386` in `[386Enh]`. WfW's SETUP turns it on, and its IFSMGR
    relies on MS-DOS's IFSHLP.SYS, which FreeDOS does not load;
  - `win /3`, to start in enhanced mode.

  This matches a July 2026 FreeDOS report that Windows 3.11 runs on the
  T2607 test release, which has kernel 2046
  ([both.org, "Yes, FreeDOS runs Windows 3.11"](https://www.both.org/?p=14420)).
  That report also says saving a file from Windows then fails ("This file is
  on a network drive with create but no modify privileges"), which it puts
  down to FreeDOS's SHARE. This record did not test saving.

  Every run below is kernel 2046 with WfW 3.11. Each row that leaves out one
  of the settings fails:

  | Configuration | SHARE | `InDOSPolling` | 32-bit file access | Result |
  | --- | --- | --- | --- | --- |
  | games entry 1 (`DOS=UMB`, `DOSDATA=UMB`, UMBM, HimemX) | no | no | on | "ERROR: Unsupported data configuration." |
  | games entry 2 (`DOS=HIGH,UMB`, UMBM, HimemX) | no | no | on | exits to the prompt, no message |
  | games entry 2 | no | yes | on | "Either memory is insufficient to run KRNL386.EXE or the value of the WindowMemSize entry in the SYSTEM.INI file is too large." |
  | bare `FDCONFIG.SYS` (`FILES=30`, `BUFFERS=16`, HimemX only) | no | yes | on | the same insufficient-memory message |
  | bare | yes | yes | on | the same insufficient-memory message |
  | games entry 2 | yes | yes | on | the same insufficient-memory message |
  | bare | no | yes | off | exits to the prompt, no message |
  | bare | yes | yes | off | **Program Manager** by 8 s simulated; runs to the 16 s limit |
  | games entry 2 | yes | yes | off | **Program Manager**; runs to the 12 s limit |

- **Decision: fixable in pc386's FreeDOS configuration, with a kernel
  move.** Nothing needs patching in FreeDOS, but kernel 2043, which pc386
  locks today, cannot run WIN386 under any configuration. Running Windows
  on pc386's FreeDOS needs two fixture changes:
  - move the kernel lock from FreeDOS 1.4's 2043 to 2046's 386 FAT16 build;
  - ship SHARE on the games image.

  The Windows settings live in the user's own WfW install. The kernel move
  changes every pc386 suite and game drive, so it is a separate PR with its
  own suites and game acceptance, not part of this record. Upstream's
  Windows enhanced-mode work is tracked in
  [FDOS/kernel#50](https://github.com/FDOS/kernel/issues/50) and
  [FDOS/kernel#151](https://github.com/FDOS/kernel/issues/151).

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
- **SHARE and SYSTEM.INI.** `SHARE.COM` from FreeDOS 1.4's `share`
  package (version 08/2006a, GPL), copied to `C:\FDOS\BIN` on the scratch
  drives and typed at the prompt before `cd \windows` and `win /3`. The
  32-bit file access runs drop `device=ifsmgr.386`, `device=vcache.386` and
  `device=vshare.386` from `[386Enh]`; the `InDOSPolling` runs add
  `InDOSPolling=TRUE` there.
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

- [#1729](https://github.com/DeanoC/fes/issues/1729): move pc386's FreeDOS kernel lock to 2046 (386 FAT16 build) and ship
  FreeDOS SHARE on the games image, with the pc386 suites and game
  acceptance rerun. Saving files from Windows under FreeDOS SHARE needs
  checking then.
- Standard mode is not an alternative. WfW 3.11 has none, and pc386's share
  has no Windows 3.0 or 3.1 media
  ([Windows record](../2026-11-11-pc386-windows-plan/summary.md#versions-run)).
