# Core status

This is the current described-core matrix. It is taken from
`config/core-recipes.toml`, `profiles/native-integration-dev.toml`, and the
misteross producers that emit format-2 manifests, except ZX81, SMS, SG-1000,
Apple II, C64, Spectrum and Atari ST format-3 manifests, and Coleco format-4
manifests with sealed ROM maps.
Boards, OSS versus Quartus toolchains and support tiers are the
[core support matrix](support-matrix.md). This page stays ABI, interfaces
and package standing. Whether a core matches the current HIP pin, CPU and
display choices is [core alignment](core-alignment.md). A dated note under
[validation/](validation/) describes the artifact it names. It does not accept
a bitstream built later.

How to build or change one of these cores is
[misteross cores](../sources/misteross/docs/cores.md). How to prepare one
package without an image rebuild is the
[core developer workflow](core-development.md). Install, inspect and library
selection are [core packages](core-packages.md).

## Standing

| Standing | Meaning |
| --- | --- |
| Factory | `profiles/native-integration-dev.toml` installs it in the ordered image set. |
| Package-only | Registered in `config/core-recipes.toml`. Not in that image. Prepare it with `make core-dev`. |
| Unregistered | A misteross producer exists. FES will not prepare or install it until a recipe row is added. |
| Reference | A producer exists for experiments and examples. No recipe row. |
| Board firmware | A pinned RBF. Not a described play package and not a `fes.*` recipe. |

The current [default profile](../profiles/native-integration-dev.toml) installs
only `fes.menu` display firmware and the locked idle RBF. All playable cores
are package-only installations. Image inclusion is not hardware acceptance;
the dated evidence below applies only to the artifacts tested.

The following optional-core behavior applies when an operator explicitly adds
Atari ST to an image selection; the default no longer selects it.

`fes.atari-st` is optional (`optional = true` in its recipe). Every other recipe
stays required. When the Atari ST producer misses timing, the image build
continues and writes `unavailable-cores.json` with reason `timing-not-met`, the
seeds, and the per-seed clock figures. The kit lists that core as
`Hardware unavailable` and does not launch it. A matching functional-key cache
hit is still the source-derived package and is installed before a producer
runs. A failed producer is left out of the image. `FES_REQUIRE_ALL_CORES=1`
makes that omission fail the build; use it for a release or factory image that
must contain every selected core. `FES_SKIP_OPTIONAL_CORES=1`, or
`FES_SKIP_CORES=fes.atari-st`, skips the producer before place-and-route for a
fast image of an unrelated change. Verify reuses the recorded omission, ignores
skip flags, and does not rewrite `unavailable-cores.json`.
`FES_REQUIRE_ALL_CORES=1` on verify rejects an image that left an optional core
out.

The image selector supports ten IDs: `fes.menu`, `fes.pong`, `fes.zx81`,
`fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.c64`, `fes.spectrum`, `fes.ramtest`, `fes.atari-st`.
`fes.riscv`, `fes.spectrum128`, `fes.spectrum-plus3`, `fes.pentagon`, `fes.zx80`, `fes.ts1000` and `fes.ts1500` are registered for `make core-dev` only.
SMS, SG-1000, Spectrum and Atari ST have sealed packages for independent installation. C64 remains registered and package-only. Its shared-pin retirement
is complete on the recorded CUDA seal, package `1d599353eaef5502…` (build
`6f865fddebe62184…`). The current 0.4.0 native-drive core is sealed as CUDA
package `3678ad9e7135e327…` and needs hardware acceptance. The original firmware/probe
[functional diagnostic](validation/2026-10-06-c64-functional-closure.md)
used nextpnr `0c5ed400` and does not accept those bytes, and there is no
factory-image acceptance or promotion. Keyboard transposition
([#750](https://github.com/DeanoC/fes/issues/750)) is unrelated. The factory
packages, Apple II and Catch seal with HIP/nextpnr. The accepted policy allows
labelled Quartus product variants alongside OSS builds; current registered
recipes and package selection still use HIP/nextpnr only. Quartus remains an
oracle where a recipe says so until variant support lands. A failed OSS route
never falls back to Quartus.

## Matrix

| Package | Version | Standing | ABI | Required interfaces | Optional | What it implements |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.ramtest` | 1.1.0 | Package-only | `fes.application` 1.0 | `fes.gamepad` 1.0, `fes.video.fixed-720p60` 1.0, `fes.memory.hps-ddr` 1.0 | — | ROM-less RAM testing utility for the SDRAM addon and all three HPS DDR ports. After the scans it measures single-line read latency on each memory ([2026-10-25 kit record](validation/2026-10-25-hps-ddr-latency.md)). Ships the OSS 100 MHz variant using `sources/misteross/cores/fes-ramtest/toolchain.lock`; 130 MHz timing closure remains open (#264). No exact-image hardware acceptance is claimed. |
| `fes.pong` | 1.1.0 | Package-only | `fes.simple-game` 1.0 | `fes.gamepad` 1.0, `fes.video.fixed-720p60` 1.0, `fes.persistence.words` 1.0, `fes.pong.progress` 1.0 | — | ROM-less Pong. Paddle speed and best rally persist. Lock `toolchain.lock`. |
| `fes.zx81` | 1.7.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.zx81-tape` 1.0, `fes.media.zx81-tape-write` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.zx81-bus` 2.0 | Sinclair ZX81. 1 KiB RAM, HID keyboard mapped to the 40-key matrix, exact 8192-byte `machine-rom` linked before download, and a 1..16384-byte `.p` media unit. Completed BASIC SAVE images remain pending until FogCast stores them for the library game and acknowledges that package generation. HDMI passes through the shared [display plane](display-pipeline.md), which blends premultiplied launcher frames over the machine picture and restores machine pixels on close. This is host-software coverage; the 2 Oct note does not accept these bytes. Still the first-party NMOS Z80. Lock `toolchain.lock`. |
| `fes.zx80` | 0.1.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.zx81-tape` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.zx81-bus` 2.0 | Sinclair ZX80 from the ZX81 shell. PAL, 4 KiB `zx80-rom`, 1 KiB internal RAM, no NMI. Raw image load at `$4000`. Experimental and package-only. Not in the factory image. Lock `cores/fes-zx81/toolchain.lock`. |
| `fes.ts1000` | 0.1.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.zx81-tape` 1.0, `fes.media.zx81-tape-write` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.zx81-bus` 2.0 | Timex Sinclair 1000 from the ZX81 shell. NTSC, 8 KiB `ts1000-firmware`, 2 KiB internal RAM, NMI, and the ZX81 ROM BASIC SAVE path. Experimental and package-only. Not in the factory image. Lock `cores/fes-zx81/toolchain.lock`. |
| `fes.ts1500` | 0.1.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.zx81-tape` 1.0, `fes.media.zx81-tape-write` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.zx81-bus` 2.0 | Timex Sinclair 1500 from the ZX81 shell. NTSC, 8 KiB `ts1500-firmware`, 16 KiB internal RAM, NMI, and the ZX81 ROM BASIC SAVE path. Experimental and package-only. Not in the factory image. Lock `cores/fes-zx81/toolchain.lock`. |
| `fes.coleco` | 1.5.0 | Package-only | `fes.application` 1.0 | `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.gamepad.ports` 1.0, `fes.keypad.ports` 1.0, `fes.video.fixed-720p60` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.coleco-bus` 2.0, `fes.fabric.video.native-pixels` 1.0 | Format 4 ColecoVision. Exact 8 KiB `coleco-bios` at source offset 0 and 16 KiB `coleco-cart` at offset 8192; the cartridge map is mirrored at `0x8000`. Pad a shorter cart with `0xff` before import. Two gamepads, two 12-key keypads, SN76489, and an optional SGM socket. No blob, stream, or firmware mailbox. The factory shell is the native-pixel video lane and requires a linked part; the shared session-display plane follows the part. Not a retail-complete core. Lock `cores/fes-coleco/toolchain.lock` (Yosys `5391eeb1`; nextpnr `bec82e36` and Mistral `8fcc4cb4`). |
| `fes.sms` | 1.4.0 | Package-only | `fes.simple-computer` 1.0 | `fes.keyboard` 1.0, `fes.video.fixed-720p60` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | Sega Master System slice. Do not use `fes.mastersystem`. Exact 32 KiB `cartridge-rom` is linked through a sealed ROM map before download; pad shorter fixed-map images with `0xff`. 8 KiB RAM at `0xc000`, Mode 4 VDP, SN76489 on `0x7E`/`0x7F`, shared PCM-to-I2S into the ADV7513. No host audio-stream mailbox or Sega mapper. Lock `toolchain.lock` (shared pin). |
| `fes.catch` | 1.0.0 | Package-only | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.gamepad` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | ROM-less paddle game on the shared application shell. No BIOS, cartridge, or factory-image entry. Lock `toolchain.lock`. |
| `fes.atari-st` | 0.2.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.atari-st-floppy` 1.0, `fes.mouse.relative` 1.0, `fes.media.atari-st-floppy-write` 1.0, `fes.media.atari-st-floppy-geometry` 1.0 | `fes.expansion.atari-st-bus` 1.0, `fes.fabric.video.raster-rgb888` 1.0 | Atari 520ST: 8 MHz 68000, 512 KiB SDRAM, exact 192 KiB `atari-st-firmware` link, original MMU aliases, MFP/ACIA/IKBD/YM2149, writable 80–82 track, one/two-sided, nine/ten-sector `.st` drive A, relative mouse and all three video modes. Library-bound disk saves and independently selected video parts pass host tests. Stock EmuTOS runs the original GEMDOS write/read/rename/delete diagnostic through the real CPU and modeled SDRAM. The earlier 0.1.0 shell seals at seed 4 (74.45/53.55/276.63 MHz), with separate sealed Direct/Scanlines parts. Both physical memory diagnostics, stock GEM/mouse and independent Direct/Scanlines video checks pass on Kit A. Automatic initial-disk boot and save/Stop/cold-reload/restore pass in the same runtime and agent lifetime, with exact complete payload comparisons against the guest model. See the [automatic-boot record](validation/2026-10-06-atari-st-auto-boot.md) and the [startup budget follow-up](validation/2026-10-06-atari-st-startup-budget.md). See the [current bring-up record](validation/2026-10-06-atari-st-bringup.md). Lock `toolchain.lock` (shared pin); the [earlier qualification](validation/2026-10-04-atari-st-nextpnr-head.md) records the read-only shell. |
| `fes.sg1000` | 1.3.0 | Package-only | `fes.simple-computer` 1.0 | `fes.keyboard` 1.0, `fes.video.fixed-720p60` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | Sega SG-1000 with the first-party NMOS Z80 at native cadence. Exact 16 KiB `cartridge-rom` linked before download at `0x0000`; pad shorter fixed-map images with `0xff`. 1 KiB RAM at `0xc000`, joysticks on `0xdc`/`0xdd`, SN76489 PSG at `0x40–0x7f` through HDMI I2S. No startup media blob. Lock `toolchain.lock` (shared pin). |
| `fes.c64` | 0.5.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.c64-disk` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.c64-bus` 1.0 | [Resealed after #1001](validation/2026-10-24-c64-hps-ddr-reseal.md) moved the HPS DDR bridge into the shell: HIP package `3d5bfc914a91798f…` (build `795666e322409e56…`), seed 2, system 53.10/52.224, pixel 78.09/74.25 MHz; booted to BASIC on kit A with kit software built from `main` (run `kswA1`). Session display and live D64 insert/eject passed the [Kit A diagnostic](validation/2026-10-23-c64-session-display/README.md) on the earlier HIP package `713b811056616b15…` (build `b7bf0ddbbf358a3b…`); system 52.89/52.224, pixel 76.49/74.25 MHz. Fidelity evidence below describes its named earlier packages. Commodore 64 pathfinder: 6510 at 0.985248 MHz PAL (312×63-cycle beam), 64 KiB RAM, VIC-II native matrix/glyph/sprite DMA, graphics shift/mode pipelines, sprite pixels/priority/collision IRQs, CPU DMA stalls, enabled-cycle IRQ polling and complete 384×272 PAL frames scaled into fixed 720p60 HDMI, three-voice MOS6581 digital oscillators and eight-bit ADSR with the default fixed-point state-variable filter (MOS6581-shaped cutoff and resonance, LP/BP/HP, voice-3 off and master volume); the reSID DAC, nonlinear filter and external output network stay behind `RESID_FILTER` and are off in the package, CIA keyboard, joystick and time-of-day clocks/alarms. Exact 36 KiB `c64-firmware` (BASIC, KERNAL, character ROM, 1541 ROM) linked before download. Native 1541 CPU, two VIAs, GCR read channel and track cache for read-only 174,848-byte D64 on media unit 0. Two cartridge sockets (ROM window and I/O window) on one shared port. Open diagnostic and external-firmware host observation; build-specific checkpoints and a development PRG fixture accelerate host iteration. Native loading, matched intro frames and Frogger control/respawn outcomes have host evidence. Opt-in reSID PCM matches the reference chain; the default state-variable filter is checked against its own fixed-point equations, not that PCM; routed timing is sealed on CUDA package `139d53f88b210e6e…` (build `0d507d2bc92176ba…`), seed 4, system 55.33/52.224, pixel 100.60/74.25, audio 213.72/12.288; whole-demo, physical analogue audio and current-package hardware acceptance remain open. No Commodore ROM is shipped. The [bounded Kit A diagnostic](validation/2026-10-06-c64-functional-closure.md) passed both probes, input and a read-only D64 on that exact package. Version 0.4.0 inherits no seal or hardware acceptance from that earlier package. The earlier core's shared-pin move completed on recorded seals: CUDA package `1d599353eaef5502…` (build `6f865fddebe62184…`) and the HIP package with its Kit B READY check in the core README. No factory-image acceptance. Lock `cores/fes-c64/toolchain.lock` (Yosys `3a5accbd`, nextpnr `5fe55f3d`, Mistral `8fcc4cb4`). The 6 Oct kit diagnostic used nextpnr `0c5ed400` and does not accept a later bitstream. |
| `fes.apple2` | 0.1.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.apple2-floppy` 1.0 | `fes.expansion.apple2-bus` 1.0 | Apple II pathfinder: 6502 at 1.0205 MHz, 64 KiB RAM with language card, text/lores/hires, speaker. Exact 16 KiB `apple2-firmware` ($C000–$FFFF window) linked before download. Built-in Disk II in slot 6 reads a live-swappable 143,360-byte DOS-order image; no writes. Physical card sockets 2, 4, 5 and 7 take independently built cards composed by the target's Go linker. Open diagnostic firmware and probe card only; no Apple ROM is shipped. [Sealed build](validation/2026-09-26-apple2-pathfinder-seal.md) and a [kit hardware diagnostic](validation/2026-09-27-apple2-kit-diagnostic.md) with linked firmware, four cards and live disk swap, [re-checked](validation/2026-09-27-apple2-cart-clock-recheck.md) after the general cart-merge fix; no image acceptance. Those notes are the older pin. Product default on `toolchain.lock` remains HIP. The CUDA route reported with the lock move was exploratory as seal evidence only: its route outcomes are interchangeable with HIP, but backend-specific `BUILD_ID` and package identity make it a distinct artifact and it is not a recorded seal. The shared-pin HIP seal on this lock remains open. |
| `fes.spectrum` | 0.6.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.spectrum-tape` 2.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.spectrum-bus` 1.0, `fes.media.spectrum-snapshot` 1.0 | ZX Spectrum 48K: first-party NMOS Z80 at 3.5 MHz with ULA contention by CPU clock stretching, the floating bus and the exact interrupt window and fetch timing; 48 KiB RAM; exact 16 KiB `spectrum-firmware` (`$0000–$3FFF`) linked before download. Border, beeper and keyboard on port `$FE`, built-in Kempston on `$1F`, Sinclair joystick 1 on controller port 1; complete 50 Hz frames shown 3x3 through the shared display plane. Unit 0 plays a PZX stream of up to 16 MiB from HPS DDR, which the host converts from a `.tzx`, `.tap`, `.csw` or `.pzx` image of up to 32 MiB; Fuse's loader detection starts and stops it, and while it plays T states take 5 system clocks instead of 14 or 15, so tapes load about three times faster, exactly in T states, with the sound muted; there is no MIC write-back. The same unit loads `.z80`, `.sna` and `.szx` snapshots (48K ones), which the host converts to a restore stream the CPU runs from reset. The edge socket takes one independently built card. Open diagnostic firmware and probe card only; no Sinclair or Amstrad ROM is shipped. Timing matches Fuse in simulation ([models record](validation/2026-10-22-spectrum-models-seal.md)); [2026-10-26 turbo loading seal](validation/2026-10-26-spectrum-turbo-loading.md): package `057a422d…` (build `6a69d61b…`), seed 5, 59.30 / 92.16 / 231.8 MHz from `9da56232`, which restored real `.z80`/`.sna` snapshots on kit B (run `i2B2`); the first turbo seal `eb002504…` from `a8e165fc`, whose RTL differs only in two disk controller fixes, loaded a turbo-loader `.tzx` to its stripes in 2.5 s (10.5 s before) and loaded Manic Miner and Bubble Bobble in turbo on kit B (run `tbB3`) and restored real `.z80`/`.sna` snapshots (run `tbB4`); the previous [+3 disk seal](validation/2026-10-25-spectrum-plus3-disk.md) `35b0041b…` (build `572d1e9c…`), seed 5, 70.67 / 90.20 / 227.3 MHz from `a5de9abb`, loaded real `.z80`/`.sna` snapshots and a turbo-loader `.tzx` from DDR on kit B (run `p3B1`); the previous [TR-DOS seal](validation/2026-10-25-spectrum-trdos.md) `baa042cc…` loaded the same snapshots and tape on kit B (run `trdB2`); the previous [snapshot seal](validation/2026-10-25-spectrum-snapshots.md) `08ada8a6…` did the same with snapshots on kit A (runs `snapA2`, `tapeA2`); the previous [timing-margin seal](validation/2026-10-24-spectrum-timing-margin.md) `226617fe…` loaded a turbo-loader `.tzx` on kit A (run `kswA1`); the previous [tape seal](validation/2026-10-22-spectrum-tape-ddr.md) `71731792…` loaded a turbo-loader `.tzx` from DDR on kit B (run `c997i`); on kit A the superseded `7589e281` archive also loaded a turbo-loader `.tzx` (BASIC, code and turbo blocks) from DDR through `LOAD ""`; joysticks on hardware are open; the beeper and the turbo mute were heard on kit B (run `tbB5`). Lock `cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`). The [previous TV80 shell](validation/2026-09-28-spectrum-pathfinder-seal.md) and the [kit ROM link](validation/2026-09-28-spectrum-basic-kit.md) of the 48K BASIC ROM do not accept these bytes. |
| `fes.spectrum128` | 0.4.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.spectrum-tape` 2.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.spectrum-bus` 1.0, `fes.media.spectrum-snapshot` 1.0, `fes.media.spectrum-snapshot-128` 1.0 | ZX Spectrum 128K/+2: NMOS Z80 at 3.5469 MHz with ULA contention (odd banks) and the floating bus; 128 KiB paged through `$7FFD` (shadow screen, paging lock); AY-3-8912 on `$FFFD`/`$BFFD`; exact 32 KiB `spectrum128-firmware` (editor, then 48 BASIC). Border, beeper and keyboard on port `$FE`, built-in Kempston on `$1F`, Sinclair joystick 1 on controller port 1; complete 50 Hz frames shown 3x3 through the shared display plane. Unit 0 plays a PZX stream of up to 16 MiB from HPS DDR, which the host converts from a `.tzx`, `.tap`, `.csw` or `.pzx` image of up to 32 MiB; Fuse's loader detection starts and stops it, and while it plays T states take 5 system clocks instead of 14 or 15, so tapes load about three times faster, exactly in T states, with the sound muted; there is no MIC write-back. The same unit loads `.z80`, `.sna` and `.szx` snapshots (48K and 128K ones), which the host converts to a restore stream the CPU runs from reset. The edge socket takes one independently built card. Open diagnostic firmware and probe card only; no Sinclair or Amstrad ROM is shipped. Timing matches Fuse in simulation ([models record](validation/2026-10-22-spectrum-models-seal.md)); [2026-10-26 turbo loading seal](validation/2026-10-26-spectrum-turbo-loading.md): package `ee164e9f…` (build `5c8f2002…`), seed 5, 65.59 / 89.12 / 189.8 MHz from `9da56232`, which restored real `.z80`/`.sna` snapshots on kit B (run `i2B2`); the first turbo seal `f07a38f5…` from `a8e165fc`, whose RTL differs only in two disk controller fixes, loaded a turbo-loader `.tzx` to its stripes in 2.7 s (9.7 s before) and loaded Head Over Heels in turbo on kit B (run `tbB3`) and restored real `.z80`/`.sna` snapshots (run `tbB4`); the previous [+3 disk seal](validation/2026-10-25-spectrum-plus3-disk.md) `9aa51a5d…` (build `bae7ae99…`), seed 4, 66.14 / 99.21 / 131.6 MHz from `a5de9abb`, loaded real `.z80`/`.sna` snapshots and a turbo-loader `.tzx` from DDR on kit B (run `p3B1`); the previous [TR-DOS seal](validation/2026-10-25-spectrum-trdos.md) `f7b2bdad…` loaded the same snapshots and tape on kit B (run `trdB2`); the previous [snapshot seal](validation/2026-10-25-spectrum-snapshots.md) `0320fe38…` did the same with snapshots on kit A (runs `snapA2`, `tapeA2`); the previous [timing-margin seal](validation/2026-10-24-spectrum-timing-margin.md) `c651c074…` loaded a turbo-loader `.tzx` on kit A (run `kswA1`); the previous [tape seal](validation/2026-10-22-spectrum-tape-ddr.md) `11241760…` loaded a turbo-loader `.tzx` from DDR on kit B (run `c997i`); on kit A the superseded `7589e281` archive also loaded a turbo-loader `.tzx` from DDR through its menu's tape loader; joysticks on hardware are open; AY title music was heard on the Bubble Dizzy snapshot on kit B (run `tbB5`); the beeper and turbo mute were not recorded on this core. Head Over Heels was silent through the turbo load and joystick menu (no sound was shown after the load). Lock `cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`). |
| `fes.spectrum-plus3` | 0.5.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.spectrum-tape` 2.0, `fes.media.spectrum-disk` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.spectrum-bus` 1.0, `fes.media.spectrum-snapshot` 1.0, `fes.media.spectrum-snapshot-128` 1.0, `fes.media.spectrum-snapshot-plus3` 1.0, `fes.media.spectrum-disk-plus3` 1.0 | ZX Spectrum +2A/+3 with its disk drive: NMOS Z80 at 3.5469 MHz with gate-array contention (banks 4–7, memory T1 only) and the floating bus on ports 1+4n; 128 KiB paged through `$7FFD` and `$1FFD` with the four all-RAM configurations; AY-3-8912; exact 64 KiB `spectrum-plus3-firmware` (ROMs 0–3). Border, beeper and keyboard on port `$FE`, built-in Kempston on `$1F`, Sinclair joystick 1 on controller port 1; complete 50 Hz frames shown 3x3 through the shared display plane. Unit 0 plays a PZX stream of up to 16 MiB from HPS DDR, which the host converts from a `.tzx`, `.tap`, `.csw` or `.pzx` image of up to 32 MiB; Fuse's loader detection starts and stops it, and while it plays T states take 5 system clocks instead of 14 or 15, so tapes load about three times faster, exactly in T states, with the sound muted; there is no MIC write-back. The same unit loads `.z80`, `.sna` and `.szx` snapshots (48K, 128K and +2A/+3 ones), which the host converts to a restore stream the CPU runs from reset. Unit 1 holds a raw track disk image in HPS DDR, which the host converts from a standard or extended `.dsk` of up to 1 MiB (weak sectors, Speedlock sectors as Fuse reads them, FM tracks); a uPD765A on `$2FFD`/`$3FFD` reads it as Fuse's `upd_fdc.c` does, the disk turning at its real speed, write protected; a launch disk boots through the menu's Loader. The edge socket takes one independently built card. Open diagnostic firmware and probe card only; no Sinclair or Amstrad ROM is shipped. Timing matches Fuse in simulation ([models record](validation/2026-10-22-spectrum-models-seal.md)); [2026-10-26 turbo loading seal](validation/2026-10-26-spectrum-turbo-loading.md): package `a1fa87c5…` (build `9b0ab581…`), seed 4, 58.85 / 95.95 / 229.9 MHz from `9da56232`, which restored real `.z80`/`.sna` snapshots and booted `.dsk` disks on kit B (run `i2B2`); the first turbo seal `4d501a44…` from `a8e165fc`, whose uPD765 left the seek interrupt pending after Sense Interrupt Status, loaded a turbo-loader `.tzx` to its stripes in 7.7 s (15.7 s before) on kit B (run `tbB3`), restored real `.z80`/`.sna` snapshots and booted `.dsk` disks (run `tbB4`); the previous [+3 disk seal](validation/2026-10-25-spectrum-plus3-disk.md) `d0318f1e…` (build `621e33eb…`), seed 5, 61.27 / 93.60 / 199.4 MHz from `a5de9abb`, loaded real `.z80`/`.sna` snapshots, booted `.dsk` disks (Speedlock ones too) through the menu's Loader, and a turbo-loader `.tzx` from DDR on kit B (run `p3B1`); the previous [TR-DOS seal](validation/2026-10-25-spectrum-trdos.md) `b82212e9…` loaded the same snapshots and tape on kit B (run `trdB2`); the previous [snapshot seal](validation/2026-10-25-spectrum-snapshots.md) `b71f5edc…` did the same with snapshots on kit A (runs `snapA2`, `tapeA2`); the previous [timing-margin seal](validation/2026-10-24-spectrum-timing-margin.md) `15abb75c…` loaded a turbo-loader `.tzx` on kit A (run `kswA1`); the previous [tape seal](validation/2026-10-22-spectrum-tape-ddr.md) `c58dc83e…` loaded a turbo-loader `.tzx` from DDR on kit B and held HDMI at the −0.8 ns ADV7513 input clock delay the runtime now programs (#1011; run `c997i`). Joysticks and AY on hardware are open. Lock `cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`). |
| `fes.pentagon` | 0.5.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.spectrum-tape` 2.0, `fes.media.spectrum-disk` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0, `fes.fabric.video.display-plane` 1.0 | `fes.expansion.spectrum-bus` 1.0, `fes.media.spectrum-snapshot` 1.0, `fes.media.spectrum-snapshot-128` 1.0, `fes.media.spectrum-disk-trdos` 1.0 | Pentagon 128: NMOS Z80 at 3.5 MHz, uncontended 71,680 T-state frame with a 36 T interrupt and video data on port `$FF`; 128 KiB paged through `$7FFD`; AY-3-8912; exact 48 KiB `pentagon-firmware` (128, 48 BASIC, then TR-DOS); a Beta 128 FD1793 with TR-DOS paged in from `$3Dxx`. Border, beeper and keyboard on port `$FE`, built-in Kempston on `$1F`, Sinclair joystick 1 on controller port 1; complete 50 Hz frames shown 3x3 through the shared display plane. Unit 0 plays a PZX stream of up to 16 MiB from HPS DDR, which the host converts from a `.tzx`, `.tap`, `.csw` or `.pzx` image of up to 32 MiB; Fuse's loader detection starts and stops it, and while it plays T states take 5 system clocks instead of 14 or 15, so tapes load about three times faster, exactly in T states, with the sound muted; there is no MIC write-back. The same unit loads `.z80`, `.sna` and `.szx` snapshots (48K and 128K ones), which the host converts to a restore stream the CPU runs from reset. Unit 1 holds a raw track disk image in HPS DDR, which the host converts from a `.trd` or `.scl` image of up to 1 MiB; a launch disk boots through TR-DOS and the disk is write protected. The edge socket takes one independently built card. Open diagnostic firmware and probe card only; no Sinclair, Amstrad or TR-DOS ROM is shipped. Timing matches Fuse in simulation ([models record](validation/2026-10-22-spectrum-models-seal.md)); [2026-10-26 turbo loading seal](validation/2026-10-26-spectrum-turbo-loading.md): package `effa14fb…` (build `b172d9cd…`), seed 5, 64.53 / 81.38 / 154.5 MHz from `9da56232`, which restored real `.z80`/`.sna` snapshots and booted a `.trd` on kit B (run `i2B2`); the first turbo seal `0afd828c…` from `a8e165fc`, whose Beta 128 raised I2's INTRQ as the index pulse started, loaded a turbo-loader `.tzx` to its stripes in 2.7 s (9.7 s before) on kit B (run `tbB3`), restored real `.z80`/`.sna` snapshots and booted a `.trd` (run `tbB4`); the previous [+3 disk seal](validation/2026-10-25-spectrum-plus3-disk.md) `34be15bb…` (build `0d8954d1…`), seed 5, 57.81 / 88.82 / 204.5 MHz from `a5de9abb`, loaded real `.z80`/`.sna` snapshots, booted a `.trd`, and a turbo-loader `.tzx` from DDR on kit B (run `p3B1`); the previous [TR-DOS seal](validation/2026-10-25-spectrum-trdos.md) `1e9b0a6d…` loaded the same snapshots and tape on kit B (run `trdB2`) and booted TR-DOS disks; the previous [snapshot seal](validation/2026-10-25-spectrum-snapshots.md) `3eec9f7a…` did the same with snapshots on kit A (runs `snapA2`, `tapeA2`); the previous [timing-margin seal](validation/2026-10-24-spectrum-timing-margin.md) `5e0cdec5…` loaded a turbo-loader `.tzx` on kit A (run `kswA1`); the previous [tape seal](validation/2026-10-22-spectrum-tape-ddr.md) `8b4ce348…` loaded a turbo-loader `.tzx` from DDR on kit B (run `c997i`); on kit A the superseded `7589e281` archive also loaded a turbo-loader `.tzx` from DDR through its menu's tape loader; joysticks on hardware are open; AY title music was heard on the Bubble Dizzy snapshot on kit B (run `tbB5`); the beeper and turbo mute were not recorded on this core. Lock `cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`). |
| `fes.riscv` | 0.1.0 | Package-only | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.gamepad` 1.0 | — | Original first-party RV32I machine-mode CPU (Zicsr, Zicntr, three interrupts) with 32 KiB M10K RAM, a 160x120 RGB332 framebuffer shown 6x in the 720p raster, a timer and a gamepad register, running a hand-assembled demonstration firmware. CPU, system and board shell pass host simulation; the producer seals a HIP route at 74.25 MHz from `toolchain.lock` ([2026-10-04 host seal](validation/2026-10-04-riscv-seal.md): seed 1, 75.18 MHz). A [kit A development load](validation/2026-10-04-riscv-kit-a.md) of package `e5c4bd79…` (build `55de600f…`) showed the picture, the timer-driven colour cycle and dpad-right. The qualified ALU package `c6c11fcd…` (build `a7118693…`, [receipt](validation/riscv-timing-alu-2026-10-11.json)) showed the scene, timer colour and Stop on both kits; that load does not test the gamepad and does not accept an image. The [11 Oct video diagnostic](validation/2026-10-11-riscv-video-diagnostic.md) of baseline `72f3b2be…` (build `39ccdf04…`) is a static-scene and Stop check; candidate `85bf8676…` there is a video comparison only. See [the core README](../sources/misteross/cores/fes-riscv/README.md). |
| `fes.demo` | 1.0.0 | Reference | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0 | — | Autonomous video. Not registered. |
| `fes.demo-media` | 1.0.0 | Reference | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.gamepad` 1.0, `fes.media.blob` 1.0 | — | Palette-from-blob reference. Not registered. |
| `fes.demo-audio` | 1.0.0 | Reference | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.gamepad` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | Stereo tone reference. Not registered. Catch is the registered game on this shell. |
| splash / idle | sealed file | Board firmware | none | none | — | `sources/misteross/sealed/fes-splash.rbf`, pinned by image policy for splash and Stop-idle. 720p60 HDMI. No MiSTer user-io: nothing answers Probe `0x0014` or HPS framebuffer `0x002f`. |

`fes.media.blob` 1.0 is 1–16384 bytes. `fes.media.blob-stream` 1.0 is a
different interface and admits 1–32768 bytes on the cores that require it.
Host storage can hold larger files. Storage size is not cartridge capacity.
See [media capacity](core-media-evolution.md).

The named `fes.sg1000` 1.2.0 package, open sound ROM and installed image have
[exact-artifact kit 1 audio diagnostic acceptance](validation/2026-09-29-sg1000-shared-audio-hil.md):
captured tone/noise, checkerboard, Stop mute and silent Pong transition. This
does not qualify a later artifact or commercial cartridge.

The current native-Z80 SG-1000 1.3.0 has a sealed package and an
exact-package development diagnostic from 2026-10-03; its identity and clock
results are in the
[SG-1000 README](../sources/misteross/cores/fes-sg1000/README.md#memory-and-host-interfaces).
The Spectrum 0.2.0 packages diagnosed that day (native and the deleted 56 MHz
variant) are superseded by the current models below. Their [22 Oct Kit A
diagnostic](validation/2026-10-22-spectrum-models-seal.md) records each model
booting its named ROM and running a typed BASIC line, beeper edges captured on
all four, and FLASH at 16-frame intervals on the 128K, +3 and Pentagon models.
That diagnostic did not test tape, joysticks, AY music, expansion or any game
or demo. Tape and snapshot loading on the later packages, games included, are in
the Spectrum rows above. The [26 Oct turbo-loading record](validation/2026-10-26-spectrum-turbo-loading.md)
also records AY title music heard on the Bubble Dizzy 128K snapshot on Kit B
(run `tbB5`); joysticks and expansion remain untested on hardware. These checks
did not assemble or replace the appliance image.
Physical NMOS pin equivalence remains open.

The named `fes.sms` 1.4.0 package, open Mode 4 ROM and installed image have
[exact-artifact kit 1 audio diagnostic acceptance](validation/2026-09-29-sms-shared-audio-hil.md):
captured checkerboard, approximately 399 Hz tone and Stop mute. This does not
qualify a later artifact or commercial cartridge.

The Coleco v2 shell and independently linked SGM expansion have
[exact-artifact kit diagnostic acceptance](validation/2026-09-25-coleco-sgm-v2-hil.md)
for the named artifacts in that record. The factory image built from FES
`3aa69308` has separate [exact-artifact kit acceptance](validation/2026-09-25-coleco-v2-main-image-acceptance.md)
for its named Coleco v2 package and linked SGM probe. The bounded
[retail-cartridge baseline](validation/2026-09-25-coleco-v2-retail-baseline.md)
does not qualify a later bitstream or general game compatibility.

The separate `fes.coleco` 1.3.0 MegaCart producer is a development lane. It
requires an exact private 8 KiB BIOS and exact 128 KiB cartridge as two linked
ROM inputs, with an optional SGM archive built against that exact shell. Its
factory recipe and image selection are unchanged. The producer must pass a
fresh authenticated route, all three timing gates, ROM-map authentication and
socket containment before any package is imported; synthetic bank simulation
does not qualify a retail mapper or a kit artifact. The sealed synthetic
BIOS/MegaCart pair has [exact-artifact kit diagnostic acceptance](validation/2026-09-26-coleco-megacart-two-rom-hil.md)
with and without SGM, including Stop/relaunch and retained selections after
host restart. That record does not qualify a retail mapper, a proprietary
BIOS, or an appliance image.

The factory Coleco [video-parts lane](../sources/misteross/docs/cores.md#video-parts-development-lane)
emits the TMS9918's active indexed 256×192 pixels through a checked clock
crossing. Its mandatory linked Direct or Scanlines part owns both frame banks
and 720p HDMI timing; the system shell owns no framebuffer. The native marker,
map and wider physical layout are distinct from the earlier raster profile.
The native link preserves every in-fence routing bit; legacy whole-column
exclusions are not applied to this wider socket.

FES builds the matching Direct/Scanlines pair and retains its exact-shell
inventory separately from the canonical package. Normal catalog Install imports
both companions. FogCast stores the household video preference and normal
library Play composes that choice with the title's optional Coleco bus 2.0
expansion before download. Missing Scanlines may fall back to a valid installed
Direct part. Native shells have no built-in Direct fallback; an unavailable or
invalid selected output blocks launch. Older installed raster shells retain
built-in Direct output and cannot use native parts. Settings changes affect the
next launch, preserving the running generation. Format-3/4 video parts,
additional source standards, settings overlays, CRT/DDR and audio parts remain
subsequent work.

Hardware evidence applies only to the artifacts it names. The earlier raster
[host seal](validation/2026-10-02-video-parts-seal.md),
[developer Kit 2 diagnostic](validation/2026-10-02-video-parts-kit2-hil.md),
[normal-library diagnostic](validation/2026-10-02-video-library-kit2-hil.md) and
[factory-image record](validation/2026-10-03-video-factory-kit2.md) and the later
[Kit B acceptance record](validation/2026-10-03-video-factory-kitb-de32b3d0.md) cover their
respective builds, including SGM and host preference persistence where stated.
The native [sealed-build and Kit 2 diagnostic](validation/2026-10-03-native-video-parts-kit2.md)
records strict CRAM containment, compiler/Go/Python byte agreement and clean
Direct/Scanlines/relaunch captures for its developer artifacts. Neither record
qualifies a newly selected factory image or a new native/SGM composition.
The [native factory host record](validation/2026-10-03-native-video-factory-build.md)
binds the `3ffe989f` shell, Direct/Scanlines and SGM parts and four independent
composition proofs. The subsequent [factory Kit 2 record](validation/2026-10-03-native-video-factory-kit2.md)
accepts the exact `6ed1dad4` appliance image with those FPGA bytes: normal
catalog Install, six library launches with and without SGM, saved video
preferences, Stop/relaunch and independent HDMI video/audio checks. This
acceptance covers the named open diagnostics, not general cartridge compatibility.

## Atari 520ST integration

The first 16-bit machine is [the `fes.atari-st` core](../sources/misteross/cores/fes-atari-st/README.md).
It now assembles the real 8 MHz 68000, original ST MMU bank aliases, 512 KiB
SDRAM, MFP vectored timers, native HBL/VBL interrupts, keyboard ACIA/IKBD,
YM2149 audio and writable drive-A floppy/DMA. A separate buffer up to
839,680 bytes shares the SDRAM arbiter; upload remains valid while execution
is held or running. `fes.media.atari-st-floppy` 1.0 uses unit 0 and capability
bit 7; the writable extension adds bit 9, relative mouse adds bit 8, and the
required geometry extension adds bit 10. Without that explicit extension the
legacy floppy interface still admits exactly 737,280 bytes.
Durable disk data requires an explicit library binding and uses the complete
image layout in [core persistence](core-persistence.md). The optional expansion connector
is `fes.expansion.atari-st-bus` 1.0 with one registered physical socket.

Host tests cover full CPU diagnostics, peripheral behavior, real SDRAM
commands/refresh/byte masks/contention, coherent dual-clock line caches,
complete CRC disk upload and stock EmuTOS 1.4 RAM/video/timer initialization.
The optional ST raster video socket uses independently sealed Direct/Scanlines
parts, with a built-in Direct fallback. ROM firmware is an
external exact 192 KiB input, and EmuTOS downloads are digest-pinned.
The sealed native shell and independently built probe card pass synthesis,
routing and all three clock targets. Combined ROM/card linking verifies the
complete 192 KiB EmuTOS image through independent Mistral readback, preserves
every card bit and rejects incorrect shell identities or writes outside the
socket. The
[integration record](validation/2026-10-03-atari-st-integration.md) binds the
selected source, timing, RAM placement, ROM map, socket and package evidence.
The later [mouse, disk and video interaction record](validation/2026-10-04-atari-st-interaction.md)
binds the earlier writable shell, independent video archives and GEMDOS guest
diagnostic. The [current bring-up record](validation/2026-10-06-atari-st-bringup.md)
supersedes its memory failure with passing Kit A tests on the corrected shell.
Physical stock GEM/mouse and independent video-part checks also pass. A fresh two-boot
durable disk test verifies every payload byte against the guest model across
normal Stop and cold Play, without restarting the runtime or agent. The later
[automatic-boot record](validation/2026-10-06-atari-st-auto-boot.md) verifies initial
disk upload before CPU release, with AUTO execution on both fresh and saved disks
and zero differing payload bytes. The [startup budget follow-up](validation/2026-10-06-atari-st-startup-budget.md) verifies diskless GEM and fresh/saved AUTO boot on the revised software at the configured 60-second upload timeout. No appliance
hardware acceptance is claimed by that software diagnostic. Atari ST is now
installed separately. The [native image acceptance](validation/2026-10-07-atari-st-native-image.md) binds its historical two-pass image, Direct diskless GEM, Scanlines AUTO/save/cold restore and complete disk-byte comparisons. It also records confirmed rollback and restoration of the populated normal menu.

The [input/audio diagnostic](validation/2026-10-07-atari-st-input-audio.md)
adds guest-visible keyboard/modifier and both joystick-port checks through
normal host input, measured YM tones/noise/envelope and settled HDMI Stop mute
on that exact reused image. Physical USB attachment, held-input Stop/relaunch
and the observed one-sample capture-channel lag remain unqualified. Original
BIG and TCB Cuddly demo disks use ten-sector geometry. The current source
opts into the geometry extension for their original 80×1×10 and 82×2×10
images. All twelve supported shapes have host sector read/write coverage;
loader and border/raster effects require separate qualification. Historical
0.1.0 hardware evidence does not qualify the revised 0.2.0 shell. The
[geometry diagnostic](validation/2026-10-07-atari-st-floppy-geometry.md) now
binds the sealed 0.2.0 shell, diskless GEM, complete original BIG/Cuddly
upload/snapshot comparisons and maximum-size cold restore using built-in
Direct on a private derived image. Neither original demo reaches its effects
with the tested EmuTOS. The geometry diagnostic still records its original
[nextpnr #165](https://github.com/DeanoC/nextpnr/issues/165) routing failure;
that composition defect is resolved. Published egress anchors and the exact
prepared Direct/Scanlines pair pass. Full factory image acceptance and
border/raster compatibility remain open. The subsequent
[BIG loader diagnosis](validation/2026-10-08-atari-st-big-loader.md) corrects
unpopulated-memory acknowledgement below 4 MiB. With original TOS 1.00, BIG
reaches its introductory instructions and main menu in the CPU/chipset model;
the introductory planar framebuffer matches Hatari exactly. A fresh selected package also reaches the menu and B scroller on the kit
with temporary qualified geometry software. Captures show incorrect raster
palette colours; audio is present but fidelity is unqualified. The original
image and populated menu were restored. Demo borders, later sections, accurate
audio and full current-image integration remain open.
The [native palette record](validation/2026-10-08-atari-st-native-palette.md)
binds ordinary low-resolution live RGB capture, complete-frame ownership,
completed simulations, a sealed shell and matching Direct/Scanlines parts.
Its final pixel/system/audio clocks pass at 74.34/53.15/255.62 MHz. A bounded
Kit A test with qualified geometry software shows rainbow bands in the B
scroller through the prepared Direct part. The normal populated menu, original
image and host autostart are restored; the lease is free. Medium/monochrome
retain their indexed renderer. Menu flashing still appears in the capture,
and the model alternates its source raster palette sequence. Original GLUE
latch timing, opened borders, accurate audio and full image integration remain
open; this diagnostic does not establish full BIG compatibility.

The [raster timing follow-up](validation/2026-10-09-atari-st-raster-timing.md)
uses coherent CPU/line/frame cycles, a bottom-stop sample and 24-cycle delayed
Timer B display edges. Color line length samples sync at cycle 54, preserving
frames when BIG restores its bottom pulse after HBL. Constant DE bounds close
the native capture carry paths; whole-I/O equivalence preserves fresh BIG and
stock EmuTOS captures. Full ST and parent checks pass. Diskless EmuTOS reaches
GEM; the original BIG model has sixteen identical logo crops, menu VBL
acknowledgements on line zero and the rainbow B scroller.
The `dbf3fc1c8` package passes final timing at 79.72/52.44/163.93 MHz and seals
Direct/Scanlines parts. Its bounded prepared-Direct hardware probe retains the
rainbow. In twelve seconds, 711 of 717 nonblack logo crops match; six outliers
in two bursts keep the rare palette anomaly open. The original image and
populated free menu are restored, and the private host/credential copy removed.
Full current-image acceptance remains separate; opened-region RGB still crops
to ordinary 320×200, and later demo sections/audio fidelity remain unqualified.

## PC/XT development diagnostic

PC/XT is outside the factory image package set. Its assembled 8088, motherboard,
SDRAM, ISA CGA/MDA and one Direct/Scanlines pipeline have host simulation
coverage, including DMA and refresh. The diagnostic board builder supplies
PLL/reset, SDRAM capture/pads and HDMI, with separate retained attempts under
`sources/misteross/build/diagnostic/fes-pcxt/`. `fes.pcxt` now has an installable
format-2 diagnostic package with a minimal `fes.computer` identity/execution
mailbox and fixed CGA Direct video. The known OSS timing miss remains accepted
for packaging: video 71.18/74.25 MHz and main 90.88/100 MHz. The CGA Direct
diagnostic was rejected by the [Quartus cross-check](https://github.com/DeanoC/fes/pull/836#issuecomment-6261235615),
and [#840](https://github.com/DeanoC/fes/issues/840) remains open. Package creation
does not establish timing closure, SDRAM pad qualification, physical socket
containment or hardware acceptance. BIOS/DOS/game compatibility, keyboard and
other profiles remain open. See the
[PC/XT diagnostic guide](../sources/misteross/cores/fes-pcxt/README.md#assembled-board-diagnostic).

## pc386 machine, phases M1, M2 and M3

pc386 is outside the factory/package set and has no shell or producer. Its AT
core (PICs, 8254, DMA, 8042, RTC/CMOS, ATA) runs the Bochs BIOS, the LGPL
VGABios and FreeDOS 1.4 to a working prompt in host simulation, with the Bochs
oracle as CPU; `make sim-fes-pc386` runs the device, DSP, OPL2, presenter and
port 2 checks. The register-level
VGA, its capture to DDR port 2, the 720p presenter and the display plane on
port 2 show the VGA BIOS text, mode 13h and mode Y pictures at 720p in
simulation, and the VGA trace replay matches Bochs's VGA
(`make sim-fes-pc386-vga`); `make synth-fes-pc386` gives the machine-only
synthesis figures. See the [pc386 README](../sources/misteross/cores/fes-pc386/README.md),
the [M1 record](validation/2026-10-26-pc386-m1-freedos-boot/summary.md) and the
[M2 record](validation/2026-10-26-pc386-m2-vga/summary.md) and the
[M3 audio record](validation/2026-10-26-pc386-m3-audio/summary.md); the
[AT core M10K record](validation/2026-10-27-pc386-at-core-m10k/summary.md) has the
AT core's current synthesis figures. The RTL CPU in the machine, mailbox,
routed result and hardware remain open.

## Not implemented, or not this package

- ZX81 mid-session tape replace and eject use `fes.media.zx81-tape` unit 0
  without a hold-reset reboot. The runtime path and the host
  `live-media` / `change-tape` API are in. The
  [2 Oct HDMI diagnostic](validation/2026-10-02-zx81-session-display.md)
  accepts an older simple-computer shell. It does not accept these
  `fes.computer` bytes. See [FES ZX81](fes-zx81.md) and
  [ZX81 tape media](zx81-tape-media.md). Launch-time ROM splice and
  expansion-cart selection are separate; the expansion bus guide is
  [ZX81 expansion bus](zx81-expansion-bus.md).
- The ZX81 shell includes an in-session launcher plane on required
  `fes.video.session-display` and `fes.memory.hps-ddr`. Opening and closing
  it preserves CPU, RAM and the active core generation. The 2 Oct note does
  not accept these bytes. Physical operator input and expansion/audio
  acceptance remain separate. See [the hardware room](hardware-rooms.md).
- Idle rooms and attract are not the splash bitstream. The splash is board
  firmware. The rooms design is [idle MENU → rooms](idle-menu-rooms.md).
- There is no NES, SNES, or Mega Drive package in the factory set. Old
  acceptance of those systems does not apply to this image.
- Simulation, a sealed package, and a kit session are three different results.
  A passing sim does not produce an RBF. An older sealed package's kit note
  does not accept the bitstream you just built.

## Where to go next

| You want to… | Read |
| --- | --- |
| See which cores match current FPGA choices | [Core alignment](core-alignment.md) |
| Change RTL or seal a core | [misteross cores](../sources/misteross/docs/cores.md) |
| Prove a Cyclone V primitive instead | [misteross OSS place-and-route](../sources/misteross/docs/oss-pnr.md) |
| Prepare one package, then accept it on a leased kit | [Core developer workflow](core-development.md), [package acceptance](package-acceptance.md) |
| Pong settings and best rally | [Core persistence](core-persistence.md) |
| ZX81 machine, expansion, or tape design | [FES ZX81](fes-zx81.md), [expansion bus](zx81-expansion-bus.md), [tape media](zx81-tape-media.md) |
