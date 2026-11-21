# pc386: deletes and renames fail under FreeDOS's SHARE, 21 November 2026

Issue: [#1777](https://github.com/DeanoC/fes/issues/1777). On boot entry 4
of the FreeDOS 2046 games image, WfW 3.11 Notepad's Save As to a new name
reports "network drive with create but no modify privileges". Write's Save
As leaves a 0-byte file ([2046 record](../2026-11-12-pc386-freedos-2046/summary.md#saving-files-from-windows)).
This record finds the fault. Everything here ran on the Bochs oracle
(`machine_tb --cpu oracle`). No RTL, ATA or CPU change is involved.

**Result.**
- **The fault is in FreeDOS, not in pc386.** On entry 4, every INT 21h
  delete (41h) and rename (56h) fails with error 5, "access denied", even
  for a closed file or a file that does not exist. The same DOS calls work
  on MS-DOS 6.22 with its own `SHARE.EXE`, and on FreeDOS entry 2, which
  has no SHARE. Creating, opening, writing and closing behave the same on
  both DOSes, in every sharing mode tried.
- **Defect 1: the kernel and SHARE versions do not match.** Kernels 2044 to
  2046 ask SHARE before every delete and rename (FDOS/kernel 5b9e8fed,
  September 2021), using `INT 2Fh AX=10A6h` ("is this file open?"). SHARE
  gained that call the same day (FDOS/share 9c8db61c), but no SHARE release
  has it. FreeDOS 1.4's `SHARE.COM`, 08/2006a, the newest release and the
  one entry 4 loads, handles only 10A0h to 10A5h. 10A6h comes back as
  AX=0001h with CF set. The kernel ignores CF and reads AX=1 as "open".
  Kernel 2043, which pc386 shipped before the 2046 record, makes no such
  call.
- **Defect 2: the kernel's 10A6h stub corrupts SI.** It returns with SI
  holding the DS segment value, but its Watcom `pascal` callers expect SI to
  be preserved. `DosRenameTrue` keeps the source path in SI across the call
  and passes the corrupted SI to `dos_rename` (disassembly below). Answering
  10A6h with "not open" therefore turns the error 5 into a rename of a
  garbage path. On the oracle that gave "Error reading from drive A: DOS
  area: drive not ready" for a rename on C:. Upstream master still has this
  stub.
- **Neither defect can be fixed from the fixture or the config.** Fixing
  only SHARE (a build from current source) or only the answer to 10A6h
  exposes defect 2. Dropping SHARE stops WIN386 (#1725), and kernel 2043
  has no Windows 3.x support. The decisions for the coordinator are
  [below](#proposal). Draft upstream reports are in
  [`upstream-report.md`](upstream-report.md).
- **The link to Notepad and Write is still to be traced.** Defect 1 fails
  every delete and rename on entry 4, and the Windows symptoms fit it. The
  trace of Notepad's own INT 21h calls is still running (see
  [The Windows trace](#the-windows-trace)).

## The repro

[`shrrepro.asm`](shrrepro.asm) (`SHRREPRO.COM`, 304 bytes, NASM) runs six
steps at the DOS prompt and prints each call's AX and CF. It is the minimal
repro for the upstream reports. The bench reads its lines from port E9h.

| Step | MS-DOS 6.22 + `SHARE.EXE` | FreeDOS 2046, entry 4 (SHARE 08/2006a) |
| --- | --- | --- |
| create `C:\SHRREPRO.TMP` (3Ch) | AX=0005h (handle) CF=0 | AX=0005h (handle) CF=0 |
| close (3Eh) | CF=0 | CF=0 |
| rename to `C:\SHRREPR2.TMP` (56h), no handle open | CF=0 | **AX=0005h CF=1** |
| delete it (41h) | CF=0 | **AX=0005h CF=1** |
| delete a file that does not exist (41h) | AX=0002h CF=1 | **AX=0005h CF=1** |
| `INT 2Fh AX=10A6h` | AX=0001h CF=1 | AX=0001h CF=1 |

Both runs used the drives below and `machine_tb --type 'c:\shrrepro|'`.
FreeDOS took `--boot-key 1.2:4`.

- **MS-DOS 6.22.** A scratch copy of `win-msdos` (`c5d77840…`) with
  `C:\DOS\SHARE.EXE` added at the end of `AUTOEXEC.BAT`. MS-DOS's `SHARE`
  does not implement 10A6h either, so the AX=0001h CF=1 is just "not
  supported". MS-DOS itself never makes that call.
- **FreeDOS.** The scratch drive from the 2046 record's Windows runs (see
  [Method](#method)). FreeDOS entry 2 (no SHARE) will be added when its run
  ends.

A wider scratch probe, not committed, ran ten sequences on the same two
drives. It covered create then reopen in deny-all, deny-write and deny-none
modes, `DUP` then close both handles, 5Bh create-new, 6Ch extended
open/create, and re-creating an existing file. Both DOSes gave the same
result at every step except rename and delete, and the steps those failures
left behind. On FreeDOS the failed delete left the file in place, so a later
create-new (5Bh) and 6Ch create got 0050h, "file exists".

## Defect 2 in the running kernel

The bench dumped guest memory at the FreeDOS prompt (entry 4, `DOS=HIGH`).
The 386 FAT16 build's map (`kwc38616.map` in `ke2046_386f16.zip`) puts
`HMA_TEXT` at 020c, `SHARE_IS_FILE_OPEN` at 020c:16de and `DosRenameTrue_`
at 020c:35f0. The stub's bytes are at linear 1016CEh, so the segment base is
FFFF0h. `DosRenameTrue_` is at 1035E0h. Disassembled (objdump, i8086):

```
SHARE_IS_FILE_OPEN:              DosRenameTrue_:
  mov si, ds                       push bx / push si / push di
  mov es, si                       enter 2, 0
  pop ax                           mov si, ax        ; path1
  pop si                           mov di, dx        ; path2
  pop ds                           ...
  push ax                          push ds
  mov ax, 0x10a6                   push si
  int 0x2f                         call SHARE_IS_FILE_OPEN
  mov si, es    ; SI = DS          test ax, ax
  mov ds, si                       jz .rename
  ret                              mov ax, 0xfffb    ; DE_ACCESS
                                   ...
                                 .rename:
                                   mov cx, [bp-2]
                                   mov dx, di
                                   mov ax, si        ; path1 = DS value
                                   call dos_rename
```

`SHARE_OPEN_CHECK`, beside it in `kernel/int2f.asm`, saves SI through DI.
`DosDelete` reads the global `PriPathName` after the call, so it is not
affected.

To reach that path, a scratch TSR (`A6SHIM`, not committed) answered 10A6h
with AX=0 ahead of SHARE, which is kernel 2043's behaviour. The wider probe
then created and closed a file and renamed it. FreeDOS stopped at "Error
reading from drive A: DOS area: drive not ready / (A)bort, (I)gnore,
(R)etry, (F)ail?". The same probe without the TSR got error 5 at that step.
So the TSR is not a workaround: it trades error 5 for renames on garbage
paths.

## The Windows trace

`machine_tb` gained a scratch INT 21h/2Fh trace, not committed. It logs
each `INT 21h` and each `INT 2Fh` with AH 10h to 12h, from protected mode
(Windows KERNEL) and V86 mode (the DOS kernel's own calls to SHARE). For
each it records the registers, the path at DS:DX or DS:SI, and AX and CF
when control returns. It replays the 2046 record's Notepad keys on entry 4:
`win /3 notepad c:\save1.txt`, type `EDITED`, File Save, then File Save As
`c:\save2.txt`. The trace sees Windows' INT 21h calls (find-first of
`C:\WINDOWS\WIN.INI` at 35.4 s) and the kernel's V86 INT 2Fh calls. The
runs are slow, about 10 minutes of wall time per simulated second on the
shared host. Save As lands at about 73 s simulated, so this section is
pending.

- **Pending:** the trace of Save and Save As on entry 4, and the same keys
  with `A6SHIM` loaded. The second run shows whether Save As needs a delete
  (it should then save) or a rename (it should then hit defect 2).

## Proposal

Nothing in pc386 changes. The choices are:

1. **File the two upstream reports** in [`upstream-report.md`](upstream-report.md):
   - FDOS/kernel: the stub should preserve SI, and should treat CF set as
     "SHARE cannot say";
   - FDOS/share: please release a build with `is_file_open`.

   Filing is outward-facing, so it needs Deano's OK. This is the
   recommendation.
2. **Carry locally built FreeDOS binaries on entry 4.** This needs a kernel
   2046 built from `ke2046s.zip` with the stub fixed, and SHARE built from
   current FDOS/share source. That adds pinned OpenWatcom and ia16-gcc
   toolchains to `build_pc386_freedos_image.py`, and it patches FreeDOS, so
   it needs Deano's OK. Until then entry 4 keeps this limit: no delete or
   rename works under SHARE.
3. **Not proposed:**
   - a TSR answering 10A6h, which hits defect 2;
   - dropping SHARE, which stops WIN386;
   - kernel 2043, which has no Windows 3.x support.

The games are unaffected: entries 1 to 3 do not load SHARE, and kernel 2046
asks SHARE only when it is installed.

## Method

- **Bench.** `machine_tb` from `main` at `b9dd1ff47`, built with
  `sim_fes_pc386.py`'s `machine_testbench` (`-j 4`). It had scratch
  additions for this record only: the trace, `<f10>`, `<esc>` and `<tab>`
  keys, and a `PC386_DUMP_MEM` guest-memory dump at the end of a run. None
  is committed. Everything ran on Powerboat, at most four sims at once.
- **FreeDOS drive.** A scratch copy of `win-freedos` (`40f587c9…`, the
  Windows plan's games image with WfW 3.11). It was brought to the 2046
  games image (`eeea865f…`, built by `build_pc386_freedos_image.py --profile
  games`):
  - its `KERNEL.SYS`, `FDCONFIG.SYS`, `AUTOEXEC.BAT` and `FDOS` tree;
  - its boot code, with the drive's own BPB kept;
  - the 2046 record's `SYSTEM.INI` edits: `InDOSPolling=TRUE`, and no
    `ifsmgr`/`vcache`/`vshare` lines.

  Entry 4 printed "SHARE installed." The media are commercial and stay in
  scratch.
- **SHARE source.** The released SHARE is FreeDOS 1.4's `share` package at
  `4b98bf9a` (`SOURCE/SHARE/SHARE.C`, 08/2006a). The newer source is the
  `share` tree in `ke2046s.zip`. Its open, close, access and lock rules and
  tables match 08/2006a. It adds `is_file_open` (10A6h), the GCC build and
  uninstall support.
- **Upstream history.** `gh api repos/FDOS/kernel/compare/5b9e8fed...<tag>`
  gives `behind` for ke2043 and `ahead` for ke2044, ke2045 and ke2046. FDOS/share
  has no release after 08/2006a, and its CI keeps no artifacts.

```sh
cd sources/misteross
python3 scripts/build_pc386_freedos_image.py --profile games --download
build/sim/fes-i386-test386/nasm-2.16.03/nasm -f bin -o SHRREPRO.COM \
    ../../docs/validation/2026-11-21-pc386-share-1777/shrrepro.asm
# copy SHRREPRO.COM to a drive with SHARE loaded, then:
build/sim/fes-pc386/machine/obj/Vpc386_machine --bios build/firmware/pc386/pc386-bios.bin \
    --vgabios build/firmware/pc386/pc386-vgabios.bin --disk DRIVE.img --limit-seconds 40 \
    --boot-key 1.2:4 --type 'c:\shrrepro|' --expect 'R DONE' --cpu oracle
```
