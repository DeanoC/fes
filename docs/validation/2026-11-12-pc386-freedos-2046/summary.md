# pc386: FreeDOS kernel 2046 and SHARE, 12 November 2026

This record moves pc386's FreeDOS fixture from FreeDOS 1.4's kernel 2043 to
kernel 2046's 386 FAT12/FAT16 build. It also ships FreeDOS's SHARE, so that
Windows for Workgroups 3.11 runs in 386 enhanced mode on pc386's own DOS. It
then reruns everything the fixture feeds. Issue:
[#1729](https://github.com/DeanoC/fes/issues/1729). The finding that led here
is the [WIN386 on FreeDOS record](../2026-11-12-pc386-freedos-win386/summary.md)
([#1725](https://github.com/DeanoC/fes/issues/1725), PR
[#1727](https://github.com/DeanoC/fes/pull/1727)).

**Result.**
- **The fixture now runs kernel 2046.** `freedos.lock` pins the
  [FDOS/kernel ke2046 release](https://github.com/FDOS/kernel/releases/tag/ke2046)'s
  `ke2046_386f16.zip`, and its source archive `ke2046s.zip` for the boot
  sector source and the licence. Both are pinned by archive SHA-256 and by
  the SHA-256 of each file used. The 2043 paths are gone. The other
  packages stay at FreeDOS 1.4.
- **SHARE loads only on a new boot entry 4, "Windows 3.x".** Entry 4 is
  entry 2 (`DOS=HIGH,UMB`) plus `INSTALLHIGH=C:\FDOS\BIN\SHARE.COM`
  (FreeDOS 1.4's SHARE 08/2006a). Entries 1 to 3, which the games use, are
  unchanged and run without SHARE.
- **WfW 3.11 reaches Program Manager from entry 4 on the oracle.** It needs
  no `VERSION=` line: kernel 2046 reports DOS 6.22.
- **Every pc386 suite and acceptance game that has finished passes on the
  new fixture.** Conventional memory on entries 1 and 3 grows by 1,552
  bytes, because the FAT16 build has no FAT32 code.
- **Kit A runs the fixture and three acceptance games** on a CUDA seal of
  this branch.
- **Saving a new file from Windows fails under FreeDOS's SHARE, as
  upstream reported.** Re-saving an existing file works, but Save As to a
  new name stops with "This file is on a network drive with create but no
  modify privileges" and leaves the new file at 0 bytes. The same keys on
  MS-DOS 6.22 save both files. The games never load SHARE, so this does
  not affect them; it limits Windows on entry 4
  ([below](#saving-files-from-windows), issue [#1777](https://github.com/DeanoC/fes/issues/1777)).

## The lock

| Package | Before | After |
| --- | --- | --- |
| Kernel | FreeDOS 1.4 package `kernel` at `FreeDOS-v1.4` (2043, `BIN/KERNL386.SYS`, the FAT32 build) | `ke2046_386f16.zip` (SHA-256 `1e672ddd…46aa`): `bin/kernel.sys` (`b17aefa1…`); `ke2046s.zip` (`0ea71c92…e5c3`): `boot/boot.asm`, `boot/magic.mac`, `COPYING` |
| SHARE | not shipped | FreeDOS 1.4 package `share` at `FreeDOS-v1.4` (`4b98bf9a`): `BIN/SHARE.COM` (`b51a1af2…`), `LICENSE` (GPL v2) |

- **Why the FAT16 build.** The 386 FAT32 build is what FreeDOS's own
  `kernel` package ships. It reports DOS 7.10, which Windows 3.x refuses
  (#1725). The FAT16 build reports 6.22 and answers WIN386's INT 2Fh
  AX=1605h and AX=1607h BX=0015h callouts. pc386's disks are FAT16 of at
  most 128 MiB, and its floppies are FAT12, so FAT32 is not needed.
- **Why release archives.** FreeDOS's package archive has no 386 FAT16
  build of any kernel. The FDOS/kernel release is the upstream's own
  binary, with its source archive beside it. The builder downloads each zip
  only with `--download`, checks its SHA-256 and unpacks only the files the
  lock names.
- **Boot sector.** 2046's `boot.asm` includes `magic.mac` from its own
  directory, so the builder now passes NASM that directory as an include
  path. The boot sector is still assembled with `-dISFAT16` and the
  test386 runner's pinned NASM.
- **Licences.** The design doc's component table
  ([FreeDOS in the FogCast disk builder](../../../sources/misteross/docs/pc386-machine-design.md#freedos-in-the-fogcast-disk-builder))
  gains the kernel's release source and a SHARE row. Kernel 2046 is GPL v2
  or later (`COPYING` in the source archive), and SHARE is GPL v2.

## Where SHARE loads, and why

WIN386's DOSMGR runs several virtual machines over one DOS. On FreeDOS it
needs a SHARE resident: without one, `win /3` exits to the prompt with no
message (#1725). The choices were SHARE on every entry, on entry 2, or on a
new entry. This record adds a new entry:

- **The games do not need SHARE, and it would sit in their file path.**
  SHARE hooks INT 21h's open, close and lock calls and checks each open
  against its sharing table. No acceptance or census title needs it.
  Ultima VII saves through those calls, and an upstream report says saving
  from Windows fails under FreeDOS's SHARE. Keeping SHARE off entries 1 to
  3 leaves every game's DOS exactly as before, apart from the kernel.
- **Entry 2 keeps its purpose.** Entry 2 is for programs that need the most
  conventional memory (Falcon 3.0). SHARE costs 9K of upper memory, not
  conventional memory, but it would still change that entry's DOS for
  programs that never asked for it.
- **Entry 4 is entry 2 plus SHARE, loaded high by `INSTALLHIGH`.** Entry 2
  is the setup #1725 found Windows needs: entry 1's `DOSDATA=UMB` gives
  "Unsupported data configuration". `INSTALLHIGH` puts SHARE in upper
  memory, and MEM shows the same 622K free as entry 2.
- **Nothing else changes.** HIMEMX, UMBM, CuteMouse, MOUSEDEF, FESENV,
  `FILES` and `BUFFERS` are as they were, and `MENUDEFAULT=1,3` still takes
  entry 1 after 3 seconds. The `SYSTEM.INI` changes #1725 found
  (`InDOSPolling=TRUE`, and no `device=ifsmgr.386`, `vcache.386` or
  `vshare.386` lines) belong to the user's own WfW install, not to the
  fixture.

## Memory

MEM on the games image, oracle, before and after:

| Entry | Kernel 2043 | Kernel 2046 | Upper memory used |
| --- | --- | --- | --- |
| 1 (default) | 580K (594,256 bytes) | 582K (595,808 bytes) | 12K, both |
| 2 | 622K (637,104 bytes) | 622K (637,152 bytes) | 6K, both |
| 3 (EMS) | 580K (594,256 bytes) | 582K (595,808 bytes) | 16K, both |
| 4 (Windows) | none | 622K (637,152 bytes) | 15K (SHARE 9K) |

Entry 1 still meets Frontier's own figure (about 594,000 bytes) and Serpent
Isle's 573K plus the 1,024 bytes of headroom that the games-image boot
suite's MEM check requires.

## Suites

All on the new fixture, on Powerboat (oracle) and chip-30x0-ai-dev (RTL).
Each fixture-fed suite was rerun:

| Suite | Oracle | RTL |
| --- | --- | --- |
| boot (boot image; VGA replay) | PASS: the banner reads "FreeDOS kernel 2046 … Kernel compatibility 6.22"; replay 0 mismatches | PASS: 0 oracle steps, no stops, 145.6M instructions |
| boot, games image (`mem`, Serpent Isle and Frontier memory check) | PASS: 595,808 bytes | PASS (`dir`): 0 oracle steps, no stops; VGA replay 0 mismatches |
| a20 | PASS | PASS |
| c2xx | PASS | PASS |
| ems | PASS | PASS |
| wait | PASS | PASS |
| speaker | PASS | PASS |
| scroll | PASS | PASS |
| sound | PASS | — |
| cpu-profile | PASS | — |
| system (pc386_system, to the prompt and the input exercise) | PASS | — |

No suite changed result. The only output that changed is what the kernel
prints: its banner, `VER`, the size of `KERNEL.SYS` in `DIR` (46,128 bytes,
not 46,256), and MEM's figures above. No suite compares those.

## Game drives

The six drives were rebuilt from the same manifest (`c6a61087…`) on the
new games image (`eeea865f…`). The titles' files are unchanged, so only
the FreeDOS part of each drive differs. The drives went to a private store
for this branch. `docs/pc386-game-drives.md` lists the new hashes, which
replace the shared store's drives once this merges.

| Drive | Kernel 2043 | Kernel 2046 |
| --- | --- | --- |
| `real` | `a6977555…` | `d65eb762…` |
| `xms` | `5053c36b…` | `e5eb47bf…` |
| `pmode` | `b4be6e29…` | `6ee147a3…` |
| `dpmi` | `9e6ae3a3…` | `31acf283…` |
| `fpu` | `ff322ddc…` | `ad1197b6…` |
| `bench` | `e69a4cb6…` | `82647975…` |

The game suite on the oracle, with its own checks (no stop, I/O timing and,
for the OPL2 titles, OPL2 against Nuked-OPL3), and the last frames
inspected:

| Title | Entry | Result |
| --- | --- | --- |
| Wolfenstein 3D | 1 | PASS; OPL2 1,193,181 samples exact |
| Ultima VI | 1 | PASS |
| DOOM | 1 | PASS |
| Ultima VII: The Black Gate | 1 | PASS; title screen and intro |
| Ultima VII: Serpent Isle | 1 | PASS; intro ("Eighteen months after…") |
| Ultima Underworld | 3 | PASS; intro |
| Darklands | 3 | PASS; main menu |
| Frontier: Elite 2 | 3 | PASS; its sound setup prompt, as before |
| Master of Orion | 3 | PASS; back at the prompt at 4.6 s with its "reconfigure hardware options" message, as on kernel 2043 (4.7 s on the old `xms` drive). Its drive still lacks the `CONFIG.MOO` sidecar its own `INSTALL` writes ([EMS board record](../2026-11-08-pc386-ems-board/summary.md)), which is not a kernel matter |

RTL game suite (`--cpu rtl`, `real` drive): Wolfenstein 3D runs 24 s with
713.8M instructions, and Ultima VI runs 16 s with 448.0M. Both have 0
oracle steps, and their OPL2 output matches Nuked-OPL3 sample for sample.
Both PASS.

## Windows for Workgroups 3.11

The scratch drive is the new games image on 260 cylinders plus WfW 3.11's
`C:\WINDOWS` from the Windows plan's `win-freedos` drive. Its `SYSTEM.INI`
`[386Enh]` gains `InDOSPolling=TRUE` and loses the `device=ifsmgr.386`,
`device=vcache.386` and `device=vshare.386` lines that WfW's setup wrote.
This record does not describe that change as turning 32-bit file access
off. The Windows-kit session (PR #1760) found that Control Panel's dialog
on these drives already shows "16-Bit", with the 32-bit box unchecked,
while the lines are present. This PR corrects the same wording in #1727's
record and in the design doc. Its `FDCONFIG.SYS` and `AUTOEXEC.BAT` are the fixture's, and
it has no `VERSION=` line. The drive is commercial and stays in scratch.

- **Oracle, entry 4:** `win /3` reaches Program Manager by 14 s simulated.
- **RTL:** #1728 (#UD) has not merged, so there is no RTL run.

### Saving files from Windows

Notepad, on the oracle, with the same keystrokes on entry 4 of the
FreeDOS 2046 drive and on the Windows plan's MS-DOS 6.22 drive
(`win-msdos`, Microsoft's SHARE-free setup with WfW's own VSHARE). The
keys are spaced 6 to 12 s apart, frames every 0.5 s, and the disk is
written back at the end (`machine_tb --disk-out`). This takes a scratch
change to the bench, `<f10>`, `<esc>` and `<tab>` named keys, which is not
committed. `win /3 notepad c:\save1.txt` (`SAVE1.TXT` holds `ORIGINAL`),
type `EDITED` and Enter, then File Save, then File Save As `c:\save2.txt`:

| Step | FreeDOS 2046 + SHARE | MS-DOS 6.22 |
| --- | --- | --- |
| File Save of the existing `SAVE1.TXT` | saved: `EDITED\r\nORIGINAL\r\n` on the disk | saved, the same bytes |
| File Save As a new `SAVE2.TXT` | "c:\save2.txt — This file is on a network drive with create but no modify privileges. Ask the administrator of this network to change this condition." `SAVE2.TXT` is created with 0 bytes | saved: the same 18 bytes |
| After | the message box stays up to the 92 s limit | Windows exits to `C:\>` at 78.7 s |

That is the upstream report's failure. Notepad created the new file but
could not write to it. Why SHARE refuses the write is not traced here. FreeDOS 1.4's SHARE (08/2006a) is the
newest official SHARE. FDOS/share has made no release since, and its source
has not changed since January 2025. The kernel 2046 source tree carries a
newer SHARE source but no binary. Write: pending (a rerun with keys the bench can type; the first runs used `*`, which it cannot type).

## Kit

- **Package.** `040329c9b61db9542f634591d269179768852e1620c5d22c82fab4075903838f`
  (`.fcore` SHA-256 `ca7c9b8e…`, `build_id` `9f7f0d4aa011470f7a196a69af94cc0a`),
  a Quartus 17.0.2 seal (seed 1) of this branch at `030bf9321`, built in a
  clean detached worktree on chip-30x0-ai-dev with
  `scripts/build_fes_pc386_quartus.py --gpu-router CUDA` (slot `a4d627f7`).
  Timing passed: setup +0.880 ns, hold +0.116 ns, recovery +6.703 ns,
  removal +0.261 ns, minimum pulse width +1.122 ns. 36,813 of 41,910 ALMs.
  [`seal-summary.json`](seal-summary.json) is its `build-summary.json`.
  The branch changes no RTL, so this is the core of main at `df88b3ca8`, which
  the branch was cut from. The simulations ran at that same commit.
- **Kit A, run a1**, core-only on its baseline (image `179affbb…`, software
  `a2756499`), at max. The claim, restore check, cleanup and release are in
  Kit A's status file. The run staged the fixture and the `real` and
  `pmode` drives, and a housekeeping claim then removed exactly the 13
  data trees it created. The FAT's free space was 353.6M before and after.
  - The games image boots kernel 2046: `ver /r` prints "DOS version 6.22"
    and "FreeDOS kernel 2046 (build 2046 OEM:0xfd)", and the menu shows
    four entries.
  - Entry 4 prints "SHARE installed.", and MEM shows 622K (637,152 bytes)
    free with FreeDOS in the HMA, as in the simulation.
  - Wolfenstein 3D reaches the first floor.
  - Ultima VI plays its introduction.
  - DOOM `-timedemo demo3`: "timed 3863 gametics in 3362 realtics", 40.2 fps
    (Phase 4's last Kit A figure was 40.05 on kernel 2043).

## Reproducing

```sh
cd sources/misteross
python3 scripts/build_pc386_freedos_image.py --download
python3 scripts/build_pc386_freedos_image.py --profile games --download
python3 scripts/sim_fes_pc386.py --suite boot --download
python3 scripts/sim_fes_pc386.py --suite game --game wolf3d --drive real
python3 scripts/build_pc386_game_drive.py --download
```

`sim_fes_pc386.py --suite game --boot-entry 4` takes the Windows entry.
