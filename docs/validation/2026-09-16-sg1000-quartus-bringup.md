# FES SG-1000 Quartus oracle bring-up

This note records the Quartus Prime Lite 17.0.2 compiler/oracle bring-up of
package `fes.sg1000` on Powerboat. It does not claim an OSS/nextpnr lane, FES
parent pin, or kit HIL.

## Scope

SG-1000 is a Coleco sibling. The new tree is `cores/fes-sg1000`. TV80, the
bounded TMS9918-style VDP, dual-port RAM wrappers, the `fes.simple-computer`
mailbox, both PLL wrappers and the 720p HDMI shell stay the Coleco modules.
SG-1000-specific RTL is the memory map and the 8255 joystick ports.

| Item | First slice |
| --- | --- |
| CPU | TV80 via Coleco `T80pa`, 52 MHz enable shape |
| Cartridge | 1–16 KiB mailbox blob at `0000–3fff` |
| RAM | 1 KiB at `c000–c3ff`, mirrored through `ffff` |
| VDP | Coleco `0xbe`/`0xbf` Graphics I / bounded Graphics II |
| Joystick | SG-1000 8255 `0xdc`/`0xdd` from keyboard bits 0..9 |
| Video | Coleco 1650×750 HDMI shell |
| Audio / mappers / 32–48 KiB carts | out of slice |

`make build-fes-sg1000-quartus` seals a format-2 package from a clean committed
tree. This bring-up compiled with `--compile-only` from a dedicated dirty
worktree so the oracle RBF exists before any commit. A later sealed package
will embed a real `BUILD_ID` and will not bit-match this compile-only RBF.

## Host simulation

`make sim-fes-sg1000` on Powerboat with cached Verilator 5.051
(`v5.050-268-g5e4151e3`) passed:

- 16 KiB mailbox copy into `0000–3fff`
- unmapped `4000`/`8000` reads `ff`
- CPU write `5a` at `c000` with 1 KiB mirror at `c400`
- `IN (DC)`/`IN (DD)` with P1 Up
- open Graphics I diagnostic (998 bytes, entry `0000`) stores `a5` at `c000`
  and captures neutral `DC=ff` at `c001`

Diagnostic SHA-256:

| Image | Bytes | SHA-256 |
| --- | ---: | --- |
| `graphics-i.rom` | 998 | `2ed0543d8885b12b8b1e0604df8b9bed1da764f8974d721c35c72e65672ff131` |
| `graphics-i-16k.rom` | 16384 | `f02f307938d12edea33aa7d092ed578e7179eba190c315e605e8b0fad9bfd5ee` |

Icarus + `altera_mf.v` was not run: Powerboat has no `iverilog`. The Verilator
machine check is the cheap Coleco-style diagnostic for this job.

## Quartus 17.0.2 oracle

Host: Powerboat (`192.168.10.202`), user `deano`.
Worktree: `/home/deano/fes-worktrees/misteross-sg1000-quartus` (not the live
FES tree). Tool: `/home/deano/intelFPGA_lite/17.0`,
`Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition`.
Device: `5CSEBA6U23I7`. Seed 1. Macros `QUARTUS=1` and
`FES_SG1000_BUILD_ID` (compile-only id all zeros).

Quartus Full Compilation completed successfully at ~13:27 Europe/Sofia
(`0` errors, `37` warnings). Evidence is on Powerboat under
`build/fes-sg1000-quartus/` (`quartus.log`, `top.sta.rpt`,
`build-summary.json`, `core.rbf`).

| Field | Value |
| --- | --- |
| Errors | `0` (Full Compilation successful; 37 warnings) |
| Worst-case slack setup/hold/recovery/removal/pulse (ns) | `2.492` / `0.164` / `11.263` / `1.468` / `0.961` (multi-corner TimeQuest / `build-summary.json`) |
| Fast −40 °C model corner (ns) | `6.256` / `0.164` / `15.407` / `1.468` / `0.961` (one model in `quartus.log`; not the multi-corner minimum setup) |
| RBF path | `/home/deano/fes-worktrees/misteross-sg1000-quartus/build/fes-sg1000-quartus/core.rbf` |
| RBF SHA-256 | `cc6888089410b378cd6c246aeb6a1b7b60bee2ee19697a0bf58f227832986984` |
| RBF bytes | `2403252` |
| Summary | `build/fes-sg1000-quartus/build-summary.json` (`status=pass`, `sealed=false`) |
| Sealed package | not sealed (dirty tree; analog recipe requires a clean commit) |

This is compiler evidence only. It does not establish HDMI, cartridge delivery
or native kit acceptance.

## Suspected later OSS/Mistral/formic gaps

Step-2 inventory and misteross test ladder:
`docs/validation/2026-09-16-sg1000-oss-gap-ladder.md`. These do not block
Quartus GREEN:

- Same Coleco registered M10K TDP wrappers and media-load flush.
- Same four coherent VDP VRAM copies and sprite line banks.
- OSS I²C via `MISTRAL_IO` plus BEL `cyclonev_hps_interface_peripheral_i2c.52.60.0`.
- `TV80_REFRESH=1` and the accepted 50 MHz OSS constraint subset
  (`constraints-oss.qsf` / `clocks-oss.sdc`) are not in this Quartus recipe.
- No SG-1000 HIP nextpnr recipe, lock or seed yet.
- 16 KiB ABI vs retail 32/48 KiB SG-1000 images.
- No SN76489 audio.

## Out of scope here

Yosys/nextpnr finish, formic gap execution, FES parent pin, Codex kit HIL,
Herd host, `.4`/expand/`WRITE_GO`, Bugbot.
