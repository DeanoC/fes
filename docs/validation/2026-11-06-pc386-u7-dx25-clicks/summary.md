# pc386 Ultima VII at 386DX-25: character-screen clicks, 6 November 2026

Issue [#1427](https://github.com/DeanoC/fes/issues/1427). Kit B run `w1`,
on the 386DX-25 governor preset (preset 3).

**Result.** #1427 is the dangling INT 33h handler from
[#1485](https://github.com/DeanoC/fes/issues/1485), which MOUSEDEF.COM in the
games image fixed (PR [#1495](https://github.com/DeanoC/fes/pull/1495), on
main). It is not a CPU, governor, PS/2 or `fes.mouse.relative` fault, and no
RTL changed.

- **Main's games image at 386DX-25.** The first click on Start New Game
  opens the character screen. After a name is typed, the first click on
  Journey Onward ends name entry and the second starts the game, which opens
  in Trinsic. This is the same as at max in #1485's run `v1`.
- **The stock image (before MOUSEDEF) at 386DX-25, with the same steps.**
  - **Keys only during the replay.** If the menu idles back to its intro
    and only keys are pressed during the replay, the game still starts.
  - **Mouse motion during the replay.** The intro freezes on its title, and
    Esc no longer does anything. This is #1485's state: MAINMENU has left
    its own handler armed in CuteMouse, so INTRO's code is called at the
    stale address.
- **The MOUSEDEF image, the same motion during the replay.** The replay
  keeps running, Esc returns to the menu, and the game starts.

## Why the original run failed only at 386DX-25

Run `g1409-k7` (2 November, #1409's stock image) took this path:

1. **The menu idled out.** The driver sent its click on Start New Game
   about 23 s after the Esc that skipped the intro (sleeps of 9, 0.5, 0.5,
   1, 8, 0.8, 0.8 and 1.5 s, plus the motion packets). The menu returns to
   its intro after about 20 s without a selection, and its fade-in is slower
   at 386DX-25. So the click arrived after MAINMENU had exited (frame `n00`
   is black, and `n01` is the intro title).
2. **Mouse input reached the replay.** The driver then typed "Avatar" and
   sent a −16, 11 move and two clicks into the intro replay. With the stock
   image, CuteMouse called MAINMENU's stale handler address, 203B:000D,
   which now held INTRO's code. This time it did not fall into the #GP loop:
   the replay kept animating (`g03`–`g15`).
3. **Clicks no longer landed.** Esc reloaded MAINMENU (`r00`), and a click
   opened the character screen (`f4`, the issue's `073a496c156b`). From
   then on, keys reached the name field (`esc1`, the issue's
   `9f1b72a8ad94`) but no click landed.

The character screen was therefore reached only after the stale handler had
run inside INTRO. At max (`g1409-k1`) the click beat the idle return, so the
same script started the game. That is the whole preset dependence. #1486's
run `r1` later got the same character-screen state at max too.

`w1`'s keys-only leg shows that the stock image is fine at 386DX-25 when no
mouse packet reaches the replay. Its motion leg shows that the stale call
breaks the session. We did not trace exactly how the non-crashing form in
`k7` stopped later clicks while keys kept working. One likely reason is
that the stray call left CuteMouse's callback state corrupt. With MOUSEDEF
the stale address is never armed (#1485's record has the handler readings),
so both forms are gone.

## Inputs

- **Package.** `fes.pc386` `499f4805ca07ddc13f04e35146fc822e340c49babc3694fdae0b813fce933d2c`,
  #1481's seal of main `ee7ea9bb4`, as in #1485's `v1`. So `w1` tested the
  RTL of that seal, not current main's.
  - **Since then, on main.** #1483 added the status ring
    (`pc386_status.sv`) and wired it into `pc386_system.sv`. It is a
    read-only DDR-window writer and does not change the CPU or the mouse
    path. #1495 changed only the games image, and #1499 only the runtime.
  - **No ring evidence.** This seal has no status ring, so `w1` has none.
- **Kit.** Kit B ran core-only on baseline software `a8cd30c4` (main
  `bb51f4454`) and OS `f904961e`. There was no install, no image change and
  no reboot. A SCRUBX scrub ran before the first game. The claim (08:05
  EET), release (08:25) and housekeeping are in Kit B's status file. The 12
  FogCast trees that `w1` created were removed, and the baseline was
  unchanged.
- **Disks.** These stay outside the repo. Game files come from Deano's
  eXoDOS 6.04 zip, SHA-256 `75e6eb68…`, as in #1409 and #1485.
  - **MOUSEDEF.** `eae30dea165fb0ded69b1075b36d9a69609582a820c565e86747c39555e7789d`
    is #1495's games image (`36f50d52…`) plus The Black Gate. The image
    builder and `mousedef.asm` are unchanged on main since #1495.
  - **Stock.** `5eba644557b9765cf7d88e93774a17649ace91e4cd9b2a9f85f8d2e836d88f0a`
    is #1486's `m-u7bg.img`, the games image before MOUSEDEF, plus The
    Black Gate.
- **Settings.** Every game id was written `3,0,0,0,544,7,1` (preset 3,
  SB 220h/IRQ 7/DMA 1) before its first load.
- **Input.** The agent's input stream, using `v1`'s packet scripts.
  - **Aim.** Eleven `dy=3` packets at 0.05 s take the cursor from the
    menu's default spot to Start New Game. Then 20 `dx=−3` and 7 `dy=3`
    packets at 0.04 s reach Journey Onward.
  - **Clicks.** Each click is two button-down packets and two button-up
    packets, 0.08 s apart.

## Runs

All four legs ran in one lease, at preset 3.

| Leg | Disk | Steps | Result |
| --- | --- | --- | --- |
| `bg25` | MOUSEDEF | Esc past the intro, menu, click, "Avatar", two clicks on Journey Onward | Pass. Start New Game on the first click, game start on the second Journey Onward click. |
| `bg25s` | stock | Esc, wait 40 s (the menu idles back to the intro), Esc (keys only), then as `bg25` | Pass. The menu returns, and the clicks and game start behave as in `bg25`. |
| `bg25t` | stock | Esc, wait 55 s (intro replay), 10 `(+5, +5)` packets, Esc | Fail. The title frame is frozen from the motion on, and Esc does nothing. The later clicks have no effect. |
| `bg25f` | MOUSEDEF | the same as `bg25t`, then Esc and the clicks | Pass. The replay keeps running and Esc returns to the menu. The first click missed only because the reverse nudge put the cursor on the title (`f-aim`). After the next idle return, the clicks at Start New Game and Journey Onward started the game. |

## Frames

No frames are committed, because they are all game screens. The `w1` frames
are on Powerboat in `~/fes-scratch/u7menu/runs/w1/fr/`, and the `k7` frames
are in `~/fes-scratch/games/runs/g1409-k7/fr/`.

| SHA-256 | Frame | What it shows |
| --- | --- | --- |
| `02112e655c4f3bf7ec5f6a47c0ac21c013cdda2c5cae0d67bda3fc934b0f2cbf` | `w1/bg-cs` | MOUSEDEF, 386DX-25: the character screen after the first menu click. |
| `59a18cb9b5f297e06e268acbf44a0572ffbfe2859af34315bc7f143b1e68591f` | `w1/bg-jo1` | The same after "Avatar" and the first Journey Onward click: name entry ended, cursor on Journey Onward. |
| `dd62335bd81c135895546a4ce33f26cc9a7108dbf7b127bd7924629f7ea4a069` | `w1/bg-jo3` | 36 s after the second click: the opening in Trinsic. |
| `4248faccbc28f63aafe55be1b099b56ac92e6fab93a5289acbcf311ca9ee542f` | `w1/s-menu2` (= `w1/f-menu2`) | Stock and MOUSEDEF: the menu again after an idle return and Esc. |
| `f0ec187b7bbbb6148b9a2b15273dec8a851c1ed001472e7ff8fae934941e43e3` | `w1/s-jo3` (= `w1/f-jo4`) | Stock with keys only, and MOUSEDEF after motion: the opening in Trinsic. |
| `d11be65fb63ea0a7b3908b8c4d66fbdd021568a033e32aad4cb62757810ad197` | `w1/t-idle`, `w1/t-move2`, `w1/t-menu2` | Stock after motion in the replay: the same title frame before the motion, 15 s after it and 15 s after Esc. |
| `8000855920dfc06dbb679700cdf3ad4f230937a52e508b8ba5d3637d99a272cb` | `w1/f-move2` | MOUSEDEF, 15 s after the same motion: the replay has moved on. |
| `6363614695b43c4fdd74b80ea9ce9c5be232d8d770a0eed75a5fa172e1dba85f` | `w1/f-aim` | MOUSEDEF: the cursor on the title letters after the reverse nudge (the missed aim). |
| `1002c3dda6a321064b88b36b184feb2783f400d8d2e42eb281177ccb039a3682` | `w1/f-cs3` | MOUSEDEF: the character screen after the retry. |
| `4c6d2c8d8bbb3756757eacd65f2e662ac9a6f56594ea0e34760218bc21f02037` | `k7/n00` | 6 s after k7's menu click: black, so the menu had exited. |
| `9917ee50fdb127f222a233aee3a4845386ae9051d8c94f24bf953c5d18c719d9` | `k7/n01` | The intro title, after "Avatar" and the −16, 11 move. |
| `7b0a9c60ed9754442aae28f16952ebb8c8eede244d9e86547baafc20dd2adafe` | `k7/g15` | The replay still animating, about 75 s after the stray packets. |
| `ec5e597723e5160c9ea2e615c8d386577c6e34f28cfab9a7837d67e70b80b09c` | `k7/r00` | The menu fading in after Esc, which reloaded MAINMENU. |
| `073a496c156b1cfd2a047df33886d059ee9da1fb915607d8eaa47eec338817d4` | `k7/f4` | The character screen that then ignored clicks. This is #1427's first frame. |

## Not done

- **No max rerun.** `w1` has no max rerun with the MOUSEDEF image; #1485's
  `v1` already covers that.
- **#1427's other notes.** The low menu hit rate and the menu's idle return
  were explained in #1485's record: the cursor aim at y=144, the first Enter
  only highlighting, and the first click ending name entry.
