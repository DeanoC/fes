# pc386: Windows for Workgroups 3.11 on a kit, 12 November 2026

This record boots Windows for Workgroups 3.11 to Program Manager in 386
enhanced mode on a real kit, with a seal of main's pc386 core. Two changes
on main made this possible:
- [#1728](https://github.com/DeanoC/fes/pull/1728) raises #UD for undefined
  encodings, and with it WfW 3.11 reaches Program Manager on the RTL in
  simulation ([`2026-11-12-i386-ud-1701`](../2026-11-12-i386-ud-1701/summary.md)).
- [#1726](https://github.com/DeanoC/fes/pull/1726) makes WDCTRL, WfW's 32-bit
  disk driver, pass its validation on pc386's BIOS.

The plan record ([`2026-11-11-pc386-windows-plan`](../2026-11-11-pc386-windows-plan/summary.md))
had no kit run. This record changes no RTL.
Refs [#1702](https://github.com/DeanoC/fes/issues/1702).

**Result.**
- **WfW 3.11 runs on Kit B with main's core.** MS-DOS 6.22 boots from reset
  on `fes_i386_cpu`. `win` reaches Program Manager in 386 enhanced mode, and
  every task passed with 32-bit disk access off and on:
  - a full-screen MS-DOS Prompt opens, runs `ver` and `mem`, and `exit`
    returns to Program Manager;
  - Minesweeper opens and uncovers cells on mouse clicks, and its timer runs;
  - Notepad saves a typed line. After exiting Windows, `type` at the DOS
    prompt prints the same line.
- **Program Manager is up 2.0–2.5 s after `win`** at max (preset 0),
  measured frame by frame from a 60 fps HDMI capture (table below).
- **32-bit disk access works on the kit.** With WDCTRL on and a permanent
  swap file, Control Panel reports "Disk Access: Using 32-bit Disk Access".
  The DOS window and the Notepad save work in that state, and `chkdsk`
  afterwards reports no errors.
- **No core bug was found**, so no issue was filed. Nothing the kit did
  contradicts the sim. The kit opened three things the sim never ran: the
  Control Panel, the swap-file setting and Windows' own restart. What they
  showed is listed below under "Kit against the sim".

## Package

- **Package** `27f478e09e8fe94e4c6ba84a6eab10d65712f23629bee51977c2bf887354b5b3`:
  - `.fcore` SHA-256 `9eb0f09f…8106`, `build_id` `6ef05309a77ece6da52af3bc26ab5ea5`;
  - a Quartus 17.0.2 seal (seed 1) of main `7e08be726`, in a clean detached
    worktree, with `scripts/build_fes_pc386_quartus.py --gpu-router CUDA`.

  [`seal-summary.json`](seal-summary.json) is its `build-summary.json`.
- **Host and backend.** chip-30x0-ai-dev, CUDA (`gpu_backend: cuda`), main's
  pc386 lock slot `a4d627f7…` (a cache hit). The sealer takes only the slot's
  Yosys (`0a7927bf`); Quartus places and routes. The seal took 13 minutes.
- **Timing.** Every analysis passed. Worst slacks:

  | Analysis | Worst slack |
  | --- | --- |
  | Setup | +0.375 ns |
  | Hold | +0.057 ns |
  | Recovery | +7.006 ns |
  | Removal | +0.267 ns |
  | Minimum pulse width | +1.122 ns |

  Clocks: system 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.
- **Resources.** 36,694 of 41,910 ALMs, 269 of 553 RAM blocks, 38 DSP
  blocks. BIOS `37a1b6ba…` (main's lock, with #1726's AH=08h change), VGA
  BIOS `8522568e…`.
- **Why this is main's core.** `7e08be726` was main's head when the seal
  started; it has #1726, #1728 and #1749 (ARPL). Main has since moved to
  `56a15f9c6` (#1753's record), which changes nothing under
  `sources/misteross/cores` or `sources/misteross/scripts`.

## Kit and drives

- **Kit.** Kit B, core-only on its baseline: image `179affbb…`, software
  bundle `7ddb0bfb…`. Nothing was installed, and there was no reboot or host
  change.
  - Kit B's status file logs each run's claim, restore check and release
    (`w1`, `w2`, `w3`), and a housekeeping claim after each (`w1-clean`,
    `w2-clean`, `w3-clean`).
  - Each housekeeping claim removed exactly the five trees its run created:
    two game records, two ATA disk deltas and the staged drive. Each run's
    other new paths were inside those trees, and nothing was left under
    `saves/`.
  - `/media/fat` had 354 MB free before the first run and after each cleanup.
- **Drives.** These are the plan record's scratch drives, by SHA-256 in
  `~/fes-drives/pc386/` on Powerboat. The software is commercial, so the
  drives are never committed. They were staged on the kit only through the
  agent's disk-image path, one per claim, and removed afterwards. Each is
  260/16/63 (128 MiB).

  | Drive | SHA-256 | Contents |
  | --- | --- | --- |
  | `win-msdos` | `c5d77840c984ccb36caf6930ae0aa913301f3fe402ef9db8de0c9db2062c8f59` | MS-DOS 6.22 and WfW 3.11 as their SETUPs left them; HIMEM `/TESTMEM:OFF` |
  | `win-msdos-32bit` | `54dad4252538b6dc26672b62637c0af686e3bdc4e4a0e8ef4c61488001422234` | the same, plus `32BitDiskAccess=on`, `device=*int13` and `device=*wdctrl` in SYSTEM.INI |

- **Runs.** Each run took one kit lease, at max (preset 0, the machine
  settings `0,0,0,0,544,7,1`). It first booted the shared memory-scrub disk
  (SCRUBX), then launched the package as a fresh library game on the drive.
  Input was typed and moved through the agent's input stream: keyboard
  through `fes.keyboard.hid`, mouse through `fes.mouse.relative`. HDMI was
  grabbed on Powerboat, and each step was driven after reading the previous
  frame.
  - `w1`, `win-msdos`: DOS boot, `ver`, `mem`; timed `win`; MS-DOS Prompt;
    Minesweeper with the mouse; Notepad save; exit and `type` the file; a
    second timed `win`; Control Panel, 386 Enhanced, Virtual Memory.
  - `w2`, `win-msdos-32bit`: the same tasks with 32-bit disk access on, plus
    SYSTEM.INI's WDCTRL lines checked with `find` at DOS.
  - `w3`, `win-msdos-32bit`: made a 20 MB permanent swap file through
    Control Panel (Virtual Memory, Change, OK), and let Windows restart
    itself. Then it read the disk status, timed `win` again, and repeated the
    MS-DOS Prompt and the Notepad save. It ended with `chkdsk c:`.

  The scratch runner, all frames and the raw captures are in
  `~/fes-scratch/winkit` on Powerboat.

## What the kit shows

| Task | 32-bit disk access off (`w1`) | 32-bit disk access on (`w2`, `w3`) |
| --- | --- | --- |
| MS-DOS 6.22 from reset | BIOS, `Starting MS-DOS...`, SMARTDRV, `C:\>`; `ver` 6.22; `mem`: 639K conventional, 15,360K XMS, DOS in the HMA | the same |
| `win` to Program Manager | 2.33 s, then 2.00 s | 2.48 s and 2.28 s (temporary swap), 2.35 s (permanent swap) |
| MS-DOS Prompt | full-screen `C:\WINDOWS>`, `mem /c` lists win386 resident; `exit` returns | the same; `dir` of `C:\WINDOWS\SYSTEM\*.386` in `w3` |
| Minesweeper, mouse | three clicks: two uncover numbers, the third lands outside the board; timer counts | two clicks uncover numbers; timer counts |
| Notepad save, then `type` at DOS | `WINKIT.TXT`, 65 bytes, same line | `WINKIT2.TXT` 61 bytes; `WINKIT3.TXT` 68 bytes with 32-bit disk access in use; same lines |
| Exit Windows | "This will end your Windows session", OK, `C:\>` | the same |
| Control Panel, Virtual Memory | Disk Access "Using MS-DOS", File Access "Drive C: 16-Bit", 49,900 KB temporary swap | temporary swap: "Using MS-DOS", with "Use 32-Bit Disk Access" checked. Permanent swap: "Using 32-bit Disk Access", 20,475 KB permanent |
| `chkdsk c:` after `w3` | — | no errors; 92,917,760 bytes free, the 20.9 MB `386SPART.PAR` among 3 hidden files |

**Timing** ([`timing.json`](timing.json)). `win` is typed without Enter. The
capture is then started, and Enter is tapped 2 s later. On Kit B the 4KPRO
captures HDMI at 1920×1080 and 60 fps, so one frame is 16.7 ms.
- The start is the first frame that changes by more than a blinking cursor,
  when the prompt's cursor drops a line or the screen clears.
- The end is the last frame that changes, with Program Manager fully drawn.
- The screen in between is black, then the WfW logo, then the grey desktop
  with the hourglass.

| Run | Drive | Boot | Start → end frame | Seconds |
| --- | --- | --- | --- | --- |
| `w1` | `win-msdos` | first `win` after the DOS boot | 140 → 280 | 2.33 |
| `w1` | `win-msdos` | second `win`, same DOS session | 145 → 265 | 2.00 |
| `w2` | `win-msdos-32bit` | first `win`, temporary swap | 148 → 297 | 2.48 |
| `w2` | `win-msdos-32bit` | second `win`, temporary swap | 146 → 283 | 2.28 |
| `w3` | `win-msdos-32bit` | `win` with the permanent swap, 32-bit disk access in use | 143 → 284 | 2.35 |

The second `win` of a DOS session is faster: SMARTDRV already holds what the
first one read. Each time is good to about a frame, except `w1`'s second
`win`: that capture delivered 1,793 of its 1,800 frames, so a few frames
may be missing between its start and end, and its 2.00 s is good to about
±0.1 s.

Frames (640×480, cut from the 1080p capture, 64 colours):
- [`kit-b-msdos-boot.png`](kit-b-msdos-boot.png): MS-DOS 6.22 boots.
- [`kit-b-program-manager.png`](kit-b-program-manager.png): Program Manager.
- [`kit-b-dos-window.png`](kit-b-dos-window.png): the MS-DOS Prompt.
- [`kit-b-minesweeper.png`](kit-b-minesweeper.png): after the mouse clicks.
- [`kit-b-notepad-saved.png`](kit-b-notepad-saved.png): Notepad saved as
  `WINKIT.TXT`.
- [`kit-b-notepad-file-at-dos.png`](kit-b-notepad-file-at-dos.png): `type`
  and `dir` at DOS after exiting Windows.
- [`kit-b-32bit-temporary-swap.png`](kit-b-32bit-temporary-swap.png) and
  [`kit-b-32bit-permanent-swap.png`](kit-b-32bit-permanent-swap.png): the
  Virtual Memory dialog with WDCTRL on, before and after the permanent swap
  file.

## Kit against the sim

- **Same path, no new stop.**
  - The sims ran WfW 3.11 to Program Manager on the oracle (plan record) and
    on the RTL from the DOS prompt (#1728).
  - #1726 showed WDCTRL passing validation and driving the controller itself
    on the oracle.
  - The kit runs the whole machine on the RTL from reset, MS-DOS 6.22's boot
    included, which the sims handed to Bochs. It reached every screen the
    sims reached.
- **"Using MS-DOS" with WDCTRL on is the swap file, not the driver.** With
  32-bit disk access on and the temporary swap file that SETUP set up, the
  Virtual Memory dialog says "Disk Access: Using MS-DOS". This looked like
  WDCTRL failing. Three things show it was not:
  - no WDCTRL validation message appeared between `win` and the logo (the
    capture has no text screen there);
  - the change dialog had "Use 32-Bit Disk Access" checked;
  - after the swap file was made permanent, the dialog said "Using 32-bit
    Disk Access".

  Observed: both drives read "Using MS-DOS" with the temporary swap file
  that SETUP set up. The 32-bit drive read "Using 32-bit Disk Access" once
  the swap file was permanent. No run had a permanent swap file with 32-bit
  disk access off. The sims never opened this dialog.
- **32-bit file access is off on both drives.** The Virtual Memory dialog
  shows "File Access: Drive C: 16-Bit", and its "Use 32-Bit File Access" box
  is unchecked. The plan record says WfW's SETUP turned 32-bit file access
  on. For these drives, Windows' own dialog says it is off. This run did not
  try 32-bit file access.
- **The 386 Enhanced applet says "Unable to initialize ports."** It says
  this on every drive before opening, and its Device Contention list is
  empty. That is what WfW does on a PC with no serial or parallel port, and
  pc386 has neither (no UART or LPT in its RTL). A QEMU control on a
  `-snapshot` of `win-msdos` showed this:
  - with `-serial none -parallel none`, the same message;
  - with a COM1 and an LPT1, no message, and the list shows `Com1` and
    `LPT1`.

  The sims never opened the applet.
- **Time to Program Manager is not compared with the sim.** The oracle run
  had Program Manager by 6.0 simulated seconds with `win` at 3.3 s, about
  2.7 s. The kit takes 2.0–2.5 s. These are different clocks:
  - the oracle's simulated second is Bochs' instruction rate;
  - `machine_tb` runs the RTL at 100 MHz (`CLOCK_HZ = 100e6`);
  - the seal runs at 61.44 MHz.

  The kit time is the real one.

## Follow-ups

- None from the kit. Untried here: 32-bit file access (VFAT and VCACHE),
  printing, sound in Windows (the drive has no Sound Blaster driver set up),
  and anything that pages heavily to the swap file.
