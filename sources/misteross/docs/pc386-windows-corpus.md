# pc386 Windows corpus

Windows for Workgroups 3.11 is a standing check of `fes.pc386`, beside the
[first acceptance games](i386-cpu-design.md#first-acceptance-games). It runs
WIN386's 386 enhanced mode, which none of the games need: paging, V86 virtual
machines at CPL 3, ring transitions through gates, the TSS I/O bitmap, the
PS/2 mouse through Windows' own driver, and WDCTRL, WfW's 32-bit disk driver,
writing the ATA disk itself. Every CPU slice and machine change is checked
against it, in two parts:

| Check | What | Where | Cost |
| --- | --- | --- | --- |
| [Kit acceptance](#kit-acceptance) | Program Manager, a mouse click, a DOS window, a Notepad save read back from the disk | a kit, on the change's seal | about 5 minutes on the kit |
| [Simulation](#simulation) | `win` to Program Manager on the RTL alone, from the DOS prompt | `sim_fes_pc386.py --suite wfw --cpu rtl`, on a host with the drive | about an hour; a periodic check, not a per-PR gate |

The kit check is part of a CPU slice's or machine change's own kit evidence,
next to DOOM's `-timedemo demo3` and the acceptance games. The merge gate's
HIL stays N/A for pc386 (`AGENTS.md`). The simulation is too slow for every
PR: run it periodically, and on any change to protected mode, paging, V86,
interrupts or the ATA path. CI has no drive, so the suite skips there.

Windows is commercial. **No Windows bytes, frames of its screens beyond kit
records, or files ever enter the repository, a PR or an upload.** The repository
holds the drive's SHA-256, its recipe, the checks and their records.

## The drive

`wfw-msdos` is a 260/16/63 (128 MiB) hard-disk image: MS-DOS 6.22 and WfW 3.11
in 386 enhanced mode, 32-bit disk access on and a permanent swap file. It is an
**image drive** in the [drive manifest](../cores/fes-pc386/sim/game-drives.toml):
a user-supplied installed system pinned by its SHA-256, not built by the drive
builder.

| Drive | SHA-256 | Title | What |
| --- | --- | --- | --- |
| `wfw-msdos` | `a2afd3c7689526eba919cfbfe77810f4751525dea1827d0c196dd81e0c8d0c6c` | `wfw` (`win`) | MS-DOS 6.22 and WfW 3.11 as their SETUPs left them; `HIMEM.SYS /TESTMEM:OFF`; SYSTEM.INI with `32BitDiskAccess=on`, `device=*int13` and `device=*wdctrl`; a 20,480 KB permanent swap file `C:\386SPART.PAR` that Windows' own Virtual Memory dialog made |

The hash is the drive's identity. SETUP stamps file times, so the recipe below
makes an equivalent drive, not the same bytes: a remade drive gets a new hash,
a manifest change and a fresh run of both checks.

Put a copy in the store (`~/fes-drives/pc386` on Powerboat, mode 0700;
`FES_PC386_DRIVES` overrides it). The import checks the hash and size, writes
the record and links `by-name/wfw-msdos`:

```sh
python3 scripts/build_pc386_game_drive.py --import /path/to/wfw-msdos.img wfw-msdos
python3 scripts/build_pc386_game_drive.py --resolve wfw-msdos
```

A plain `build_pc386_game_drive.py` run skips an image drive the store lacks.

### Recipe

The media is on the NAS's read-only share
`smb://192.168.10.223/beast/MiscStorage/Retro/PC/Discs/OS`: the MS-DOS 6.22
setup floppies and the WfW 3.11 OEM floppies. It was installed in QEMU
(`qemu-system-i386 -m 16 -cpu 486 -vga std`, an IDE disk), driven by keyboard
through the QEMU monitor; any PC emulator with an IDE disk works.
1. A blank 134,184,960-byte image, given to QEMU as an IDE disk with 260
   cylinders, 16 heads and 63 sectors per track.
2. MS-DOS 6.22 SETUP from its three floppies onto the whole disk (one FAT16
   partition, 2 KiB clusters).
3. WfW 3.11 SETUP from its floppies, Custom: VGA, PS/2 mouse, no network and no
   printer. SETUP writes `C:\WINDOWS`, CONFIG.SYS, AUTOEXEC.BAT and its
   SMARTDRV line.
4. CONFIG.SYS: `HIMEM.SYS /TESTMEM:OFF` (pc386's memory needs no test).
5. SYSTEM.INI `[386Enh]`: `32BitDiskAccess=on`, `device=*int13`,
   `device=*wdctrl`.
6. Boot it and run `win`. QEMU's IDE fails WDCTRL's validation ("phase 07,
   30"), so press a key to go on without it. In Control Panel, 386 Enhanced,
   Virtual Memory, Change: type Permanent, the offered 20,480 KB, "Use 32-Bit
   Disk Access" checked, OK, Yes, Restart Windows. Windows writes
   `C:\386SPART.PAR`, `C:\WINDOWS\SPART.PAR` and `PermSwapDOSDrive=C`,
   `PermSwapSizeK=20480` in place of `PagingFile` and `MaxPagingFileSize`.
7. Exit Windows, run `smartdrv /c`, check that `chkdsk c:` reports no errors,
   and power off.

On pc386 WDCTRL passes its validation
([#1726](https://github.com/DeanoC/fes/pull/1726)), and Control Panel reads
"Disk Access: Using 32-bit Disk Access" with this permanent swap file
([kit record, `w3`](../../../docs/validation/2026-11-12-pc386-windows-kit/summary.md)).

### FreeDOS variant

WfW 3.11's 386 enhanced mode needs FreeDOS kernel 2046 and SHARE
([#1727](https://github.com/DeanoC/fes/pull/1727)); the FreeDOS fixture moves
there in [#1729](https://github.com/DeanoC/fes/issues/1729). Once that lands, a
`wfw-freedos` drive joins the manifest as an ordinary FreeDOS games-image drive:
- a `[sources.wfw311]` tree source (`FES_PC386_WFW311`): the `C:\WINDOWS` tree
  of `wfw-msdos`, extracted with `mcopy -s`, pinned by its tree SHA-256;
- one title that copies it to `WINDOWS`, launched with `c:\windows\win|`;
- FDCONFIG.SYS's `VERSION=6.22` and SHARE, as #1729 sets them.

Both checks then take `--drive wfw-freedos`.

## Simulation

```sh
python3 scripts/sim_fes_pc386.py --suite wfw --cpu rtl
```

- The oracle boots MS-DOS 6.22 from the drive, and `fes_i386_cpu` takes over at
  the `C:\>` prompt (`machine_tb --rtl-at-prompt`). `win` is typed there.
- `machine_tb` writes the captured frame every 0.25 simulated seconds
  (`--frame-every`). The suite reads each with
  [`scripts/pc386_wfw.py`](../scripts/pc386_wfw.py) as it appears.
- Two frames in a row showing Program Manager with its Main group window end
  the run (`machine_tb --stop-file`).
- **Pass:** Program Manager was seen, the run ended there, and the RTL ran all
  of Windows itself: no stop, no gap and no oracle step after the handover. A
  time limit (12 simulated seconds) without Program Manager fails.
- Without the drive in the store, the suite prints `SKIP wfw` and passes.
- `--cpu oracle` runs everything on Bochs, as a reference. `--drive` takes
  another WfW drive; `--preset` runs it under the governor.
- The report is `build/sim/fes-pc386/wfw-rtl/report.json`: `wfw.program_manager`
  (the first frame and its time), `wfw.program_manager_after_win`, each frame's
  verdict, and the RTL counters. The frames stay in `wfw-rtl/frames/` and are
  never committed.

The checks are structural, so they need no pictures of Windows: the 640×480
frame must show a dark blue title bar at least 300 pixels wide over a white
menu bar, an open group window's title bar inside it, and the grey desktop.
The WfW logo, the hourglass desktop, a dialog, Notepad, Control Panel and text
screens all fail it.

**Cost.** The first run's timing is in the [corpus record](../../../docs/validation/2026-11-12-pc386-windows-corpus/summary.md). Run it on a host with spare cores (chip-30x0-ai-dev under
`/data`), in the background.

## Kit acceptance

[`scripts/pc386_wfw_kit_acceptance.py`](../scripts/pc386_wfw_kit_acceptance.py)
runs a sealed package on a kit and judges four goals from HDMI grabs and the
disk:

| Goal | Passes when |
| --- | --- |
| `program-manager` | `win` at the DOS prompt reaches Program Manager |
| `mouse` | a double-click on Program Manager's title bar maximizes it. The pointer is parked in the top-left corner and moved in small steps, whose pixels per count the script measures from the grabs, so the click lands on the bar whatever the acceleration |
| `dos-window` | File > Run `DOSPRMPT.PIF` opens a full-screen MS-DOS Prompt, which writes a line holding the run's nonce to `C:\WFWDOS.TXT`, and `exit` returns to Program Manager |
| `notepad` | Notepad saves a line holding the nonce to `C:\WFWNOTE.TXT` and exits back to Program Manager |

After the goals the script exits Windows, stops the machine (which saves the
run's ATA disk delta on the kit), and reads the delta back over SSH. It applies
the delta to a private copy of the drive. Both files must hold the nonce: the
DOS window's write and Notepad's save reached the disk.

The kit rules apply ([CLAUDE.md](../../../CLAUDE.md), "Shared kits"):
- Claim the kit in its status file before the run. The script refuses to start
  unless the latest state line is an IN USE claim naming `--owner`.
- It is a core-only run: the kit's baseline software launches library games
  with a disk, so nothing is installed and the image is not touched.
- The script holds the agent's kit lease and releases it on every exit path.
  It then removes the FogCast data trees the run created (its game records,
  disk delta and staged drive; never one that was there before). It checks
  that the agent is idle with a free lease and has not rebooted.
- Check the baseline and log the restore and release in the status file
  afterwards.

```sh
python3 scripts/pc386_wfw_kit_acceptance.py --kit B --owner Claude \
    --package /path/to/<package-id>.fcore --out runs/wfw-k1 \
    --kit-ssh /path/to/kit-b-ssh
```

- `--kit-ssh` names a command that runs its one argument as a shell command on
  the kit.
- `--scrub` boots a memory-scrub drive first, for a kit whose baseline does
  not zero RAM.
- `OUT/result.json` holds each goal's verdict and evidence, the files read
  back, what the run created on the kit, and the state after.
- `OUT/frames/` holds the grabs. Records may keep a few small frames, as the
  kit records do; never the drive or its files.
- It exits 0 only when every goal passed and the cleanup was verified.

A run takes about 3.5 minutes on the kit, a scrub boot included
([corpus record](../../../docs/validation/2026-11-12-pc386-windows-corpus/summary.md)).

## Records

- [2026-11-11 plan](../../../docs/validation/2026-11-11-pc386-windows-plan/summary.md):
  WfW 3.11 on the oracle, and the RTL's first stops.
- [2026-11-12 #UD](../../../docs/validation/2026-11-12-i386-ud-1701/summary.md):
  Program Manager on the RTL in simulation.
- [2026-11-12 kit](../../../docs/validation/2026-11-12-pc386-windows-kit/summary.md):
  Program Manager, the DOS window, Minesweeper and a Notepad save on Kit B,
  driven by hand.
- [2026-11-12 corpus](../../../docs/validation/2026-11-12-pc386-windows-corpus/summary.md):
  this page's drive, suite and kit check, first runs.
