# pc386: 486-only games for the DE25 486SX, census and proposal, 23 November 2026

Deano, 23 Nov 2026: "get it working then add 486 only games to the set (Duke
Nukem 3d being the obvious candidate)". The 486SX DE25 build is
[#2171](https://github.com/DeanoC/fes/pull/2171) (record
[`2026-11-23-de25-486sx`](../2026-11-23-de25-486sx/summary.md)). Tracker
[#1962](https://github.com/DeanoC/fes/issues/1962).

This is a **proposal for Deano's decisions**. Nothing here is built: no VBE,
no drive manifest change, no acceptance rows.

## Method

- **Oracle first.** Each title runs in `sim_fes_pc386.py --suite game
  --instruction-census` on the level-4 (486) oracle, and where it matters on
  the level-3 (386) oracle, as the i486 plan's census did
  ([`2026-11-17-i486-plan`](../2026-11-17-i486-plan/summary.md#demand)).
  A title **needs a 486** when the 386 run stops at a 486 instruction (or the
  title refuses a 386) and the 486 run goes on past it.
- **Then the RTL.** A title that needs a 486 runs on the RTL at `I486 = 1`
  with no 387 (`--cpu rtl --cpu-level 4`, #2171), the DE25's identity.
- **The drives** are scratch copies, not committed: the `pmode` drive with
  Duke's `DUKE3D.CFG` set to a VGA mode, and two drives built by the drive
  builder from a scratch manifest with five more titles. No game bytes, frames
  or pixels are in this record.
- **Candidates.** The 53-title level-4 census of the existing drives (i486
  plan) already found Duke Nukem 3D the only title that retires a 486
  instruction other than CPUID. The new candidates are 1995–97 titles from
  the eXoDOS set that fit a 128 MiB drive without their CD: three Build-engine
  or Build-era games and two others. A static scan of each zip's executables
  for a "486 required" string found one, Warcraft II's `WAR2.EXE` ("A 486 or
  better processor is required"); most executables are packed, so the scan
  proves little either way.

## Results

Simulated seconds are from machine reset. "386" is the level-3 oracle,
"486" the level-4 oracle with the 387 fitted (as the i486 plan's census ran),
"RTL" `fes_i386_cpu` at `I486 = 1` with no 387.

| Title (eXoDOS) | 386 | 486 | RTL, I486 = 1 | Verdict |
| --- | --- | --- | --- | --- |
| **Duke Nukem 3D 1.3D**, `DUKE3D.CFG` set to `ScreenMode = 0` (chained VGA) at 320×200 | DOS/4GW "exception 06h (invalid opcode) at 180:00222BE2" at 21.45 s: BSWAP EAX | Runs the 50 s window: BSWAP 32 times at 0180:00222BE2, ring 0; 3D Realms logo, title, then "LOADING…" into a level at 49 s. `ScreenMode = 2` (screen-buffered) runs the same way | From reset, frames only: the same logo, title and "LOADING…" at 40 s. The run was stopped there, so it has no end report: any RTL stop would have run on the oracle (a DX4 with a 387) without showing in the frames. **Not yet established** that the RTL alone gets there; see [the RTL runs](#the-rtl-runs) | **Needs a 486. Runs without VBE** on the oracle; on the no-387 RTL not yet established |
| Duke Nukem 3D 1.3D, as the `pmode` drive configures it (VESA 2.0, 1024×768) | the same exception 6 | exits at 21.85 s: "VESA driver for ( 1024 * 768 ) not found/supported" (i486 plan) | not run | the config, not the CPU, is what stops it |
| **Shadow Warrior 1.2 shareware** | DOS/4GW exception 6 at 0180:00271C08 at 28.6 s: BSWAP | BSWAP 32 times at 0180:00271C08. With eXoDOS's SB16 setup: "Playback failed … 16-bit DMA channel", then stuck loading. With an SB 2.0 setup (`BlasterType = 3`, 8-bit mono, 11 kHz) the sound plays, then the game asks for a VESA mode and offers "Continue anyway in standard 320 x 200? (Y / N)". A run answering `Y` is running. | not yet established; see [the RTL runs](#the-rtl-runs) | **Needs a 486** (the 386 stop is BSWAP) |
| Death Rally (1996) | not run | exits at 14.1 s: "Your VGA-adapter is not fully VESA (VBE 1.0) compliant". CPUID twice and an AC toggle (a 486 test), no 486 instruction | not run | **Needs VBE 1.0**, not a 486, in this window |
| Powerslave (1996) | not run | exits at 11.0 s: "640 * 400 Vesa mode not supported" (eXoDOS's setup). CPUID and an AC toggle, no 486 instruction | not run | needs VBE as configured; its VGA modes were not tried |
| Z (1996) | not run | "Critical File Missing: main.pac": the data is on the CD | not run | needs its CD; not judged |
| Warcraft II (1995) | not run | the Bochs oracle panics ("prefetch: getHostMemAddr vetoed direct read", physical 100BC852h) 2 s after launch, after DOS/16M's INT 15h AH=BFh probes | not run | not judged. `WAR2.EXE` carries "A 486 or better processor is required" |

**Sound.** The machine's card is a Sound Blaster 2.0. Duke's digitised sound
plays under eXoDOS's SB16 setup (the DSP took DMA and raised IRQs), but its
music is silent: the OPL2 took 9 writes and no non-zero sample in 50 s.
Shadow Warrior needs an SB 2.0 setup to get past loading.

## The RTL runs

**Rule, written before any result is read.** Each title runs from reset on
the RTL at `I486 = 1` with no 387 (`--cpu rtl --cpu-level 4`, no
`--x87-fitted`, no handover), on chip-30x0-ai-dev with Verilator 5.032:
Duke Nukem 3D (chained VGA) to 30 s, past its 386 stop at 21.45 s; Shadow
Warrior (SB 2.0, answering `Y` at its 320×200 prompt) to 36 s, past its 386
stop at 28.6 s. A title passes if the suite's verdict is `PASS game`, its end
reason is `limit` (not `prompt`: on a 386 both exit to the prompt), and the
RTL report shows 0 oracle steps and no stops.

**Handover runs are not this evidence.** Runs with `--rtl-from` start the RTL
from the oracle's state, after the title's FPU detection has run on the
oracle's 387. Duke executes x87 instructions of its own (the level-3 census
saw `x87exc` at 0180:0026C2D5 and 0026C4F3), so a handover run cannot answer
whether a no-387 486SX runs it. Such runs can show only that the RTL retires
the BSWAP at its site.

<!-- RTL-RESULTS -->

## What a title needs

| Title | Needs, beyond the 486SX DE25 build (#2171) | Size |
| --- | --- | --- |
| Duke Nukem 3D | A `DUKE3D.CFG` sidecar on the drive (the manifest's `config` mechanism, as Rise of the Triad's `SOUND.ROT`): 320×200 chained VGA, an SB 2.0 sound setup and an FM music device; then the acceptance row (launch, keys to start a level, a goal). No VBE | small |
| Shadow Warrior shareware | An `SW.CFG` sidecar with the SB 2.0 setup, and either the screen mode that skips the VESA question or a `Y` key; then the row | small |
| Death Rally | VBE 1.0 in the VGA BIOS and the hardware behind it (below) | large |
| Powerslave | Its VGA mode tried first; VBE if it has none | small, or large |
| Z, Blood, Redneck Rampage (full) | Their CD on the drive: the ATAPI CD exists (#1695), but game drives do not attach a CD image | medium |
| Warcraft II | The oracle's panic understood (it may be the oracle, or the title probing past RAM) before any judgement | unknown |

**VBE, priced.** Today the VGA BIOS is LGPL VGABios built without `-DVBE`
(`build_pc386_firmware.py`), and `pc386_vga.sv` has no Bochs DISPI ports
(1CEh/1CFh) and no linear framebuffer. VBE means:
- the BIOS built with `-DVBE` (its VBE code exists and targets DISPI);
- DISPI index and data registers in the VGA (resolution, bpp, enable,
  bank, virtual width, offsets);
- a linear framebuffer window in HPS DDR at a PCI-style address, mapped by
  the CPU's attribute map, plus banked access at A0000h for VBE 1.x;
- the scanout reading 8 bpp at 640×480 and up from DDR, and the DE25
  presenter scaling it to 720p;
- the governor's charge for LFB writes (local-bus or ISA timing, #2005).

That is a new video path, larger than any change in #2171. Area on the DE25
is not the limit (5,236 ALMs free at the median); the work is the scanout and
BIOS integration, and VBE's own oracle (Bochs's VGA with VBE) for the replay
check.

## Decisions for Deano

1. **Duke Nukem 3D joins the DE25 486SX acceptance set at 320×200 VGA, with
   a config sidecar, and no VBE.** It needs a 486 and runs without VBE on the
   486 oracle. On the RTL as a no-387 486SX it is not yet established (the
   from-reset runs below). Recommended, conditional on that run.
2. **Shadow Warrior shareware joins too**, with an SB 2.0 sidecar, once its
   from-reset RTL run passes the rule below. Recommended; the full game (88 MB) fits
   a drive too.
3. **VBE is a separate plan, not part of the 486 work.** It unlocks Death Rally
   and the higher-resolution modes of the Build games, not a 486 requirement.
   Recommended: defer, and write its plan when a VBE-only title is wanted.
4. **CD titles** (Z, Blood, Redneck Rampage, the full Build games' music):
   attach CD images to game drives as a separate change. Deano's call on
   priority.
5. **Warcraft II**: look at the oracle panic before classifying it, as a
   small follow-up.
6. **A wider census** (the rest of the 1995–97 DOS/4GW titles) runs kit-first
   once a DE25 kit path exists (slice 4), as the census rule says; this
   record's oracle runs were the targeted first pass.

## Tools and runs

- [`tools/scan486.py`](tools/scan486.py): the static string scan of the zips.
- [`tools/census486.toml`](tools/census486.toml): the scratch manifest for drives
  `c486a` (Warcraft II, Shadow Warrior shareware, Z) and `c486b` (Death Rally,
  Powerslave), built with `build_pc386_game_drive.py --manifest` into a
  scratch store; zips pinned by SHA-256.
- The Duke drives are copies of `pmode` (`b4be6e29…`) with only `DUKE3D.CFG`'s
  `ScreenMode`, `ScreenWidth` and `ScreenHeight` changed (0 or 2, 320, 200).
  The Shadow Warrior drive is `c486a` with `SW.CFG` set to `BlasterType = 3`,
  `NumChannels = 1`, `NumBits = 8`, `MixRate = 11000` and `ScreenMode = 0`
  at 320×200.
- [`tools/census486_l4.sh`](tools/census486_l4.sh) ran the level-4 batch on
  Powerboat (Verilator 5.032). [`tools/chip_duke486.sh`](tools/chip_duke486.sh)
  and [`tools/chip_sw486d.sh`](tools/chip_sw486d.sh) run on chip-30x0-ai-dev,
  whose Verilator was upgraded to 5.032 for them (23 Nov 2026), from #2171's
  `a9d6cb4f5`.
