# pc386 Darklands and the VGA BIOS mode set, 8 November 2026

Issue [#1553](https://github.com/DeanoC/fes/issues/1553). Machine bench on the
Bochs oracle CPU and the RTL, a Quartus seal, and Kit B run `d1`.

**Result.** The fault is in the VGA BIOS. Darklands and its Art Gallery both
call a valid mode set, and the BIOS returns from it with DX changed.

- **Cause.** Darklands and `ARTGLLRY.EXE` load MicroProse's `MGRAPHIC.EXE`
  driver. It sets mode 13h with INT 10h AH=00h, keeps the mode it asked for
  in DL, and compares DL with the AL that INT 10h AH=0Fh returns. On a
  mismatch it sets mode 3 and prints "MCGA is not supported" through INT 21h
  AH=09h, with DS from its own data, and exits. The upstream LGPL VGABIOS
  (0.9d) saves only DS, BX, CX and AX around `biosfn_set_video_mode`, and the
  C mode set returns with DX changed. So the driver saw a failed mode 13h,
  and its message came out of the wrong segment as garbage. IBM's VGA BIOS
  changes only AL on AH=00h.
- **Fix.** The AH=00h dispatch saves ES, DS and every general register around
  the mode set (`pusha`/`popa`), so only AL changes. The change is marked in
  `vgabios.c` and recorded in `vgabios/source.json`. The VGA BIOS image is
  `8522568e…` (was `e0fae3ec…`).
- **Not a CPU or Phase 5 fault.** Both CPUs fail the same way on the old
  BIOS, and nothing protected mode is involved: the driver runs in real
  mode before the game's EMS use.

## Bench

`scripts/sim_fes_pc386.py --suite scroll` runs `SCROLLT.COM`
([source](../../../sources/misteross/cores/fes-pc386/sim/scroll_test.asm)).
Its new `modeset` check sets modes 13h, 93h and 03h with BX, CX, DX, SI, DI,
BP and ES loaded with marker values. It counts the registers kept, DS, and
AH=0Fh reporting the mode: 9 checks for each mode, 27 in all.

| BIOS | CPU | Result |
| --- | --- | --- |
| `e0fae3ec…` (main) | Bochs | modeset 24/27 FAIL: DX changed in all three modes |
| `8522568e…` (this PR) | Bochs | modeset 27/27, mode13 2/2, mode12 2/2, mode04 2/2, DONE ok |
| `8522568e…` (this PR) | RTL (`fes_i386_cpu`) | no result: BIOS POST stalls after `ata0 master` within the 8 s limit |
| `e0fae3ec…` (main) | RTL (`fes_i386_cpu`) | the same stall, same screen and counters within a few hundred instructions |

The RTL stall is on main and doesn't depend on the VGA BIOS. It is
[#1589](https://github.com/DeanoC/fes/issues/1589). The kit run below runs
the same RTL CPU in the sealed core, and it covers the fix on the RTL.

`--suite game` on the shared store's xms drive `e83b8185…`, with `ARTGLLRY` typed at the prompt, run on
the oracle CPU. The Art Gallery needs no EMS, so it shows the fault and the
fix on the bench. Darklands itself asks for EMS before its mode set, and its
check is the kit run below.

| BIOS | Title | Result |
| --- | --- | --- |
| `e0fae3ec…` (main) | `ARTGLLRY.EXE` | garbage text, then back to the prompt |
| `8522568e…` (this PR) | `ARTGLLRY.EXE` | the Art Gallery menu in mode 13h at 16 s |

## Seal

Quartus 17.0.2, seed 1, from a clean checkout of `582b0aa0c`: package
`909144171cee897d671a693522ed7fe3a096846ff83a70d259897ce2e18d75b1`. Every
timing analysis passed. The worst slacks over the four corners: setup
0.806 ns, hold 0.081 ns, recovery 8.053 ns, removal 0.274 ns, minimum pulse
width 1.122 ns.

## Kit check (`d1`)

Kit B, core-only, this PR's package `90914417…`, boot entry 3 (FESEMS.SYS,
2,048 KiB of EMS at E000h).

| Step | Frame SHA-256 (first 16) |
| --- | --- |
| `MEM /E`: 2,097,152 bytes of EMS free | `ea8e86cf9cbe229b` |
| `DARKLAND`: the MicroProse intro in mode 13h | `185b17e9dbbd5fb1` |
| Title menu (Quickstart, Create a New World, The Story Continues) | `fb5954a1a9193298` |
| Quickstart: the party in Zürich's Gasthaus | `f7292451043d39a2` |
| The "you decide to…" choice | `a16f6d093173de26` |
| Leave Zürich (a mouse click): the overworld map, the party on the road | `5c988d41c5fc145f` |
| `ARTGLLRY` from the prompt | `5e9d5c601048c85d` |
| The Art Gallery menu | `d494e48d6a823297` |

Darklands reaches gameplay: a mouse-driven choice in the city, then the
overworld map. The 5 FogCast data trees `d1` created were removed afterwards
(`d1-clean`). The baseline is unchanged, and Kit B was logged FREE.

## Inputs

- Kit B, core-only on baseline software `a8cd30c4`: no install, image change
  or reboot. The claim and release are in Kit B's status file.
- Drive xms `5053c36bf89cf797…`, built by `build_pc386_game_drive.py` from
  main's manifest (games image with FESEMS.SYS from
  [#1556](https://github.com/DeanoC/fes/pull/1556)) in a private store.
  Darklands is from Deano's eXoDOS zip `f74246f52943e20d…`, the hash pinned
  in `game-drives.toml`. The shared store's xms drive `e83b8185…` predates
  #1556 and has no EMS boot entry.

Nothing from the game is committed: no frames, no bytes. Frames stay on
Powerboat in `~/fes-scratch/dl1553/kit/runs/d1`.
