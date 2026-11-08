# Atari ST gameplay references for Xenon and Gomoku, 8 November 2026

Issue [#1528](https://github.com/DeanoC/fes/issues/1528). This record adds the
Hatari gameplay references that the earlier records listed as owed. The
earlier records' Hatari only compared the no-input boot for these two games,
because Hatari's command interface has no joystick or mouse-move events:

- the [7 November record](../2026-11-07-atari-st-game-acceptance/summary.md)
- the [main-package record](../2026-11-08-atari-st-main-package/summary.md)

Here Hatari 2.4.1 runs on its own X display, and xdotool drives its window as
a player would: relative mouse moves, and cursor keys as the joystick. Data is
in [`refs.json`](refs.json).

**Result.** Both references pass within the tolerances below, judged on game
timing, rules and outcomes, never on brightness.

- **Xenon.** The game's own fixed-length events take the same time on the kit
  and in Hatari.
  - From the launch bay appearing to plain plating: 8.0 s on both.
  - From the bay to the static the game shows after about two idle minutes:
    115.2–115.3 s on the kit and 115.0–115.5 s in Hatari.
  - Play shows the same rules on both.
- **Gomoku.**
  - The computer opens at the centre on both.
  - Every result matches a five in a row on the final board.
  - White wins 84% of kit games and 88% of Hatari games.
  - Three kit games end on final boards identical to two of Hatari's.

Nothing else is owed on #1528. Work that continues elsewhere:

- **The tenfoot swap** moved to #946 (see below).
- **Protected originals** are tracked separately in
  [#1541](https://github.com/DeanoC/fes/issues/1541).
- **The host input-path key repeat** is filed as
  [#1582](https://github.com/DeanoC/fes/issues/1582).

## Setup

- **Hatari.** Same binary (`dab1d8a0…`) and machine options as the earlier
  references: 520ST, 8 MHz, TOS 1.04 UK (`c005b0ae…`), real floppy timing.
- **Image.** [`hatari-x11.Dockerfile`](hatari-x11.Dockerfile) adds Xvfb and
  xdotool to the earlier Hatari image, and [`hx.sh`](hx.sh) starts Hatari on
  that display.
- **Driving it.** [`hlib.py`](hlib.py) drives Hatari like the kit runners
  drive the kit. Hatari's window is twice low resolution, so:
  - mouse moves are 2 host pixels per ST count;
  - the keyboard joystick (`--joy1 keys`) uses the cursor keys, with Right
    Ctrl as fire;
  - frames come from Hatari's own screenshot command, laid out like the
    kit's 960×540 capture so the kit's readers apply unchanged.
- **Disks.** The same `.st` images as the kit runs: Xenon `660e544f…`,
  Gomoku `0805b45c…`.
- **The kit side.**
  - Gomoku and Xenon play come from the counted runs of the two earlier
    records.
  - One extra Kit A claim, `r1` (22:58–23:08), recorded Xenon on main's
    package `2b54eb67` with no input after fire, twice.
  - It reused `m2`'s Xenon library entry, because creating a new entry
    returned `STALE_REVISION`.
  - It ran core-only on baseline `4efd34bf`/`31b39063`, removed the 2 trees
    it created, and left Kit A FREE.

## Xenon

**Tolerances.**
- **Timing.** The game's fixed-length events must agree within 1 s. Samples
  are every 0.25–0.5 s on the kit recording and every 0.5 s in Hatari.
- **Play.**
  - Score values are multiples of 50, and a hit costs a life.
  - The third loss shows "GAME OVER PRESS FIRE TO START", and fire starts a
    new game with 3 lives.
  - Level structures scroll in during play.
- **Why scores are only ranges.** The level advances with the player's input,
  so score totals are compared as ranges only. With no input after fire, it
  never leaves the plating in two minutes.

**How the timing was read.** Each frame's playfield is scored by how much of
it is not plain blue plating. That marks:
- the launch bay;
- its two breaks;
- the switch to plain plating;
- the burst of screen static the game shows after about two idle minutes.

| | Bay to breaks | Bay to plating | Bay to idle static |
| --- | --- | --- | --- |
| Kit `r1` launch 1 | — | — | 115.3 s |
| Kit `r1` launch 2 | 3.0 s, 5.5 s | 8.0 s | 115.2 s |
| Kit `m1`, `m2` (with input) | 3.0 s, 5.5 s | 8.0 s | — |
| Hatari `i1` | 3.0 s, 5.5 s | 8.0 s | 115.5 s |
| Hatari `i2` | 3.0 s, 5.5 s | 8.0 s | 115.0 s |

The largest difference is 0.5 s over 115 s, which is the sampling interval.

**Play outcomes.** Hatari ran [`xplay_h.py`](xplay_h.py), which sends the kit
`xplay.py`'s input pattern (same seed) as host keys, for two 11-minute runs:

- **`r1`** continued a game started by hand, during the setup checks.
- **`r2`** was a clean run from fire with [`xref.py`](xref.py).

Both sides show the same rules:

- Score moves in multiples of 50.
- Lives fall on hits, and there are 1–2 game overs per run.
- "GAME OVER PRESS FIRE TO START" is followed by a fire restart.
- Turrets and buildings scroll in when the player keeps advancing.

Maximum scores:

| | Maximum scores |
| --- | --- |
| Kit: `x2` and `x4` (factory package `2dd2271c`), `m1` and `m2` (main's `2b54eb67`) | 850, 600, 250, 750 |
| Hatari | 3050, 950 |

The range is wide on both. Hatari's higher top score comes from reaching
further into the level, which depends on input timing.

## Gomoku

**Tolerances.**
- The computer (Black) opens at the centre.
- Every declared result has the winner's five in a row on the final board.
- White's win share agrees within 20 points.
- At least one final board is identical between kit and Hatari.

**Hatari.** [`hgomoku.py`](hgomoku.py) played eight games. Each one is a cold
reset, then File > Open of drive A and `GOMOKU.PRG` with the mouse. Then
[`gomoku_h.py`](gomoku_h.py) plays White: it is the kit's `gomoku.py` with
the same board reader and move rule, taking frames from Hatari and moving
Hatari's mouse.

- Seven games ended in "Game won by White", with 19, 20, 10, 10, 7, 9 and 45
  White stones.
- Game 7 ended in "Game won by Black", with 15 White stones.
- Board play totals 15:18.

**Comparison.** The kit pool is `g3` and `g4` from the 7 November record and
`m1` and `m2` from the main-package record: 32 decided games.

| | White wins | Black wins |
| --- | --- | --- |
| Kit | 27 (84%) | 5 |
| Hatari | 7 (88%) | 1 |

- **Identical final boards.** Hatari's game 6 ended on exactly the same board
  as kit games `g3` #5 and #9. Hatari's game 7, a Black win, ended on exactly
  the same board as kit game `g4` #1. So the program on the kit chose the
  same moves as in Hatari.
- **Opening.** Black opens at the centre (7,7) on both.
- **Results.** Every result reads as a five in a row of the winner's colour
  on the final board.

**Observation.** At boot the kit opens the `A:\` window that the disk's
`DESKTOP.INF` saves (`#W … A:\*.*`). Hatari 2.4.1 didn't, in three boots with
and without fast floppy timing, so its runner opens drive A itself. The
kit's behaviour matches the `DESKTOP.INF` entry. The cause of Hatari's
difference wasn't isolated, and it isn't treated as a core defect.

## Counts corrected in the 7 November record

The 7 November record took each game's White-move count from the player log's
last move index. That index is one short for every won game. This PR replaces
those figures with White stones counted on each final board, where the
winner's five in a row is checked as well:

- **`g1` game 1:** 16 White stones, not 14.
- **`g3` White wins:** 6, 17, 12, 12, 9, 10, 10 and 9 stones, not 5, 16, 11,
  11, 8, 9, 9 and 8.
- **`g4` White wins:** 7, 6, 8, 9, 9 and 29 stones, not 6, 5, 7, 8, 8 and 28.

The Black-win counts and every result are unchanged. The main-package record
was corrected the same way in #1583.

## Disk swap from tenfoot

On Deano's direction, this item moved to
[#946](https://github.com/DeanoC/fes/issues/946#issuecomment-6726826255).

- **Why it can't run today.** On a kit, tenfoot offers its running-machine
  media selector only when the package reports the session-display plane.
  The ST package doesn't report it yet; that's #946's "Atari ST: plane after
  the part" slice.
- **What exists already.** The swap itself passes through the host API on
  both packages: `n1` in the 7 November record, `m1` and `m2` in the
  main-package record.

## What stays on Powerboat

These stay in `~/fes-scratch/st1528`:
- the Hatari screenshots and timelines (`hat/`)
- the kit `r1` recording
- the Docker images

No disk, ROM or frame is committed.
