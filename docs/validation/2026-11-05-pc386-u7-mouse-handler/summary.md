# pc386 Ultima VII menus and the mouse driver's default handler, 5 November 2026

Issue [#1485](https://github.com/DeanoC/fes/issues/1485) (and
[#1427](https://github.com/DeanoC/fes/issues/1427)). Kit B runs `u1`, `u2`
and `v1`, with guest memory read live through the HPS (busybox `devmem` on
the DDR window, read-only) and, for `u1` and `u2`, #1483's CPU status ring.

**Result.** The CPU and the machine deliver every click; the fault is a
guest-software mismatch between CuteMouse and Ultima VII.

- **Cause.** MAINMENU.EXE installs its mouse event handler with INT 33h
  function 14h (swap), through Borland's `int86`, so a search for `CD 33`
  misses it. It keeps the old handler only while its saved copy is 0:0.
  CuteMouse's handler starts as 0:0 (Microsoft's driver and DOSBox's start
  non-null; DOSBox uses 6362:0000), so the first swap saves nothing and the
  second saves MAINMENU's own handler. On exit MAINMENU "restores" that, with
  mask 3Fh. The next program loaded at the same address, INTRO.EXE after the
  menu's 20 s idle return or View Introduction, or U7.EXE after Journey
  Onward, is then called at a stale address on every mouse event.
- **What that does on the kit.** The first mouse packet during the intro
  replay sends CuteMouse into INTRO's code at 203B:000D, mid-instruction. It
  ends in a #GP loop on a `rep movsw` to 0078:FFFF in stale code in free
  upper memory (C897:EA1B). Timer interrupts still run, so music and the
  picture's last frame stay. This is #1485's "intro replay froze" state and
  explains everything after it in run `r1`.
- **The menu's own clicks work.** With the handler probed live, every press
  and release reaches MAINMENU's handler (AX 2, then 4, busy flag clear) and
  the menu loop keeps running. In `r1`'s launch 4 the cursor sat at y=144,
  just above Start New Game's hit box; at y=149 the same click opens the
  character screen. The first Enter only highlights View Introduction. The
  first click after typing a name ends name entry; the second starts the
  game.
- **Not the clock.** The kit's clock was synced (guest `DATE` 11-05-2026)
  and the fault still reproduced.
- **Fix.** `MOUSEDEF.COM` ([source](../../../sources/misteross/cores/fes-pc386/sim/mousedef.asm)),
  run by the games image's `AUTOEXEC.BAT` right after CuteMouse, sets the
  default handler to a RETF in the BIOS ROM with mask 0. MAINMENU then saves
  and restores that, and the intro replay plays through mouse motion.
- **Kit check with the fixed image (`v1`).** Both games start a new game.
  Serpent Isle saves ("fes1485" in slot 1). Two new faults, past the menus,
  stop the rest: #1493 and #1494 (see "Not done").

## Inputs

- **Packages.**
  - `u1`, `u2`: `fes.pc386` `01e1eebb1ab03acd…`, #1483's status-ring seal
    (main `ee7ea9bb4` plus the ring).
  - `v1`: `499f4805ca07ddc1…`, #1481's seal of main `ee7ea9bb4`. The pc386
    and common cores are unchanged from `d646bcd7e` (whose sealed inputs
    #1486 checked) to main `2c0acc241`.
- **Kit.** Kit B, core-only on baseline software `a8cd30c4` and OS
  `f904961e`: no install, image change or reboot. Every claim and release is
  in Kit B's status file. The 21 FogCast trees these runs created were
  removed afterwards (`uall-clean`), with the baseline unchanged.
- **Disks** (outside the repo; game files from Deano's eXoDOS 6.04 zips, as in
  #1486):
  - `u1`, `u2`: #1486's `m-u7bg.img` (`5eba6445…`), and a copy with a scratch
    `MFIX.COM` (`23eef5a2…`) that made the same INT 33h call with F000:9908.
  - `v1`: this branch's games image (`36f50d52…`, MOUSEDEF.COM `04fb3170…`)
    plus The Black Gate (`eae30dea…`) or Serpent Isle with #1443's
    `SERPENT.CFG` (`6b4b0619…`).
- **Scrubs.** A SCRUBX scrub ran before each launch except two noted below.

## Evidence

Addresses are real-mode segment:offset; MAINMENU and INTRO both load with
PSP 106Bh, image 107Bh.

| What | Where | Value |
| --- | --- | --- |
| MAINMENU's handler install | file offset 11D36h–11D83h | `int86x(0x33)`, AX=14h, CX=3Fh, ES:DX = image+0FC0h:000Dh; saves CX, ES, DX only if its saved ES:DX is 0:0 |
| MAINMENU's handler | 203B:000D | busy flag at CS:0008h; own stack; `sti`; dispatches the queued callbacks |
| Its saved "old" handler, clean boot, stock image | DS 2854:133C | 203B:000D, mask 3Fh (its own) |
| The same with MOUSEDEF | DS 2854:133C | F000:1264, mask 0 |
| CuteMouse's handler after MAINMENU exits, stock | C897:00B8, mask at C897:00C9 | 203B:000D, 3Fh |
| The same with MOUSEDEF | same | F000:1264, 0 |
| CuteMouse's reset (function 00h) keeps the handler address | MAINMENU calls 00h (file offset 11B10h) before its swaps | with MOUSEDEF its first swap still gets F000:1264 back; CuteMouse's state clear starts at 00BCh (C897:08C4), after the handler |
| #GP loop after a mouse packet in the intro replay | frame at 04FE:0784 | IP=EA1B CS=C897 FLAGS=0216; `rep movsw` with ES:DI = 0078:FFFF, DS:SI = 0078:0005, 10 bytes |
| Vector of that loop | status ring | 0Dh from the first packet on; INT 0Dh → FreeDOS 0070:006B → BIOS F000:E9E6 (dummy master IRQ handler), IRET to the fault |

- **Leftover RAM.** On a load without a scrub, CuteMouse came up with the
  previous run's handler and mask (203B:000D, 3Fh) already set, before any
  game ran: it does not initialise them, and a kit load keeps guest RAM
  (#1408). MOUSEDEF sets both.
- **Free memory.** MOUSEDEF is not resident. The games image's MEM in the
  machine simulation still shows 575K (588,544 bytes) conventional and
  154K upper free.

## Runs

| Run | Image | What it showed |
| --- | --- | --- |
| `u1` | stock | Idle return to the intro, then mouse motion: #GP loop (three times, once without a scrub). With MFIX: the intro replay survives motion, Start New Game, Journey Onward and the start of play all work. |
| `u2` | stock, then MFIX | `r1`'s launch 4 replayed exactly: both images ignore the clicks at y=144 and highlight View Introduction on Enter, and both menu loops idle back to the intro after 20 s. After MAINMENU exits, stock leaves 203B:000D armed and MFIX leaves F000:9908, mask 0. |
| `v1` | this branch | The Black Gate: intro replay through motion, Start New Game, Journey Onward, the opening in Trinsic. Serpent Isle: Start New Game, Journey Onward, the ship scene, save to slot 1. |

## Not done

- **The Black Gate: save.** After Iolo's "I shall accompany thee." the
  picture creeps down a few lines each frame, and clicks no longer advance
  the conversation. `u1`, with MFIX, reached the same state. No earlier run
  got that far: #1409 stopped at Iolo's first line.
- **Serpent Isle: load after relaunch.** Journey Onward from a relaunch with
  the save starts SI.EXE (it installs its handler), but the picture stays
  black for 60 s while audio plays.
- They are [#1493](https://github.com/DeanoC/fes/issues/1493) and
  [#1494](https://github.com/DeanoC/fes/issues/1494). Their frames stay on
  Powerboat (`~/fes-scratch/u7menu/runs/v1/fr`).

## Frames

No frames are committed: they are all game screens. They are on Powerboat in
`~/fes-scratch/u7menu/runs/{u1,u2,v1}/fr`.
