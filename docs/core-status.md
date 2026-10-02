# Core status

This is the current described-core matrix. It is taken from
`config/core-recipes.toml`, `profiles/native-integration-dev.toml`, and the
misteross producers that emit format-2 manifests, except ZX81, SMS, SG-1000 and
Apple II format-3 manifests with sealed ROM maps. A dated note under
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
`fes.menu`, `fes.pong`, `fes.zx81`, `fes.coleco`, `fes.ramtest` in that order. Menu (`fes.menu`) supplies
the idle display and is not a library core. Factory standing records image inclusion;
playability and hardware acceptance depend on the exact-artifact kit evidence below.

The image selector supports nine IDs: `fes.menu`, `fes.pong`, `fes.zx81`,
`fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.c64`, `fes.spectrum`, `fes.ramtest`. SMS, SG-1000
and Spectrum remain sealed package-only recipes because adding them exceeds the
128 MiB rootfs limit. C64 is registered but has no current timing-passing HIP seal.
The factory packages, SMS, SG-1000, Apple II, Spectrum and Catch seal with
HIP/nextpnr. Quartus is an oracle where the recipe says so. It is not the product
path and not a fallback.

## Matrix

| Package | Version | Standing | ABI | Required interfaces | Optional | What it implements |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.ramtest` | 1.1.0 | Factory | `fes.application` 1.0 | `fes.gamepad` 1.0, `fes.video.fixed-720p60` 1.0, `fes.memory.hps-ddr` 1.0 | — | ROM-less RAM testing utility for the SDRAM addon and all three HPS DDR ports. Ships the OSS 100 MHz variant using `toolchains/ramtest.lock`; 130 MHz timing closure remains open (#264). No exact-image hardware acceptance is claimed. |
| `fes.pong` | 1.1.0 | Factory | `fes.simple-game` 1.0 | `fes.gamepad` 1.0, `fes.video.fixed-720p60` 1.0, `fes.persistence.words` 1.0, `fes.pong.progress` 1.0 | — | ROM-less Pong. Paddle speed and best rally persist. Lock `toolchain.lock`. |
| `fes.zx81` | 1.5.0 | Factory | `fes.simple-computer` 1.0 | `fes.keyboard` 1.0, `fes.video.fixed-720p60` 1.0, `fes.media.blob` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.memory.hps-ddr` 1.0, `fes.video.session-display` 1.0 | `fes.expansion.zx81-bus` 2.0 | Sinclair ZX81. 1 KiB RAM, 40-key matrix, live `.p` blob, sealed ROM link and in-session HDMI controls. The [frozen image diagnostic](validation/2026-10-02-zx81-session-display.md) records visible cassette swap/eject with preserved BASIC state. Physical operator input and expansion/audio acceptance of this shell remain separate. Bus 2.0 retains CPU clock/reset, exact-average 3.25 MHz CPU and 1.625 MHz AY. Historical shell/cart evidence does not qualify 1.5.0. Lock `toolchains/zx81-expansion.lock`. |
| `fes.coleco` | 1.2.0 | Factory | `fes.application` 1.0 | `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.gamepad.ports` 1.0, `fes.keypad.ports` 1.0, `fes.video.fixed-720p60` 1.0, `fes.media.blob` 1.0, `fes.media.blob-stream` 1.0 | `fes.firmware.blob` 1.0, `fes.expansion.coleco-bus` 2.0 | Reduced ColecoVision with a vacant or linked SGM socket. Blob 1–16 KiB keeps the mirrored map. Stream admits 1–32 KiB at `0x8000–0xffff`. Two gamepads, two 12-key keypads, SN76489, optional 8 KiB firmware overlay. Open `JP 0x8000` shim when no firmware is bound. Not a retail-complete core. Lock `toolchains/coleco-sgm.lock`. |
| `fes.sms` | 1.4.0 | Package-only | `fes.simple-computer` 1.0 | `fes.keyboard` 1.0, `fes.video.fixed-720p60` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | Sega Master System slice. Do not use `fes.mastersystem`. Exact 32 KiB `cartridge-rom` is linked through a sealed ROM map before download; pad shorter fixed-map images with `0xff`. 8 KiB RAM at `0xc000`, Mode 4 VDP, SN76489 on `0x7E`/`0x7F`, shared PCM-to-I2S into the ADV7513. No host audio-stream mailbox or Sega mapper. Lock `toolchains/fes-sms.lock`. |
| `fes.catch` | 1.0.0 | Package-only | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.gamepad` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | ROM-less paddle game on the shared application shell. No BIOS, cartridge, or factory-image entry. Lock `toolchain.lock`. |
| `fes.sg1000` | 1.2.0 | Package-only | `fes.simple-computer` 1.0 | `fes.keyboard` 1.0, `fes.video.fixed-720p60` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | Sega SG-1000 slice. Exact 16 KiB `cartridge-rom` linked before download at `0x0000`; pad shorter fixed-map images with `0xff`. 1 KiB RAM at `0xc000`, joysticks on `0xdc`/`0xdd`, SN76489 PSG at `0x40–0x7f` through HDMI I2S. No startup media blob. Lock `toolchains/registered-memory.lock`. |
| `fes.c64` | 0.1.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.c64-disk` 1.0 | `fes.expansion.c64-bus` 1.0 | Commodore 64 pathfinder: 6510 at 1.022727 MHz, 64 KiB RAM, VIC-II text, reduced SID, CIA keyboard and joystick. Exact 16 KiB `c64-firmware` (BASIC window then KERNAL window) linked before download. Read-only 174,848-byte D64 on media unit 0. Two cartridge sockets (ROM window and I/O window) on one shared port. Open diagnostic only; no Commodore ROM is shipped. Simulation passes. Lock `toolchains/c64.lock`. |
| `fes.apple2` | 0.1.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.apple2-floppy` 1.0 | `fes.expansion.apple2-bus` 1.0 | Apple II pathfinder: 6502 at 1.0205 MHz, 64 KiB RAM with language card, text/lores/hires, speaker. Exact 16 KiB `apple2-firmware` ($C000–$FFFF window) linked before download. Built-in Disk II in slot 6 reads a live-swappable 143,360-byte DOS-order image; no writes. Physical card sockets 2, 4, 5 and 7 take independently built cards composed by the target's Go linker. Open diagnostic firmware and probe card only; no Apple ROM is shipped. [Sealed build](validation/2026-09-26-apple2-pathfinder-seal.md) and a [kit hardware diagnostic](validation/2026-09-27-apple2-kit-diagnostic.md) with linked firmware, four cards and live disk swap, [re-checked](validation/2026-09-27-apple2-cart-clock-recheck.md) after the general cart-merge fix; no image acceptance. Lock `toolchains/apple2.lock`. |
| `fes.spectrum` | 0.1.0 | Package-only | `fes.computer` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.keyboard.hid` 1.0, `fes.gamepad.ports` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.media.spectrum-tape` 1.0 | `fes.expansion.spectrum-bus` 1.0 | ZX Spectrum 48K pathfinder: Z80 at 3.5 MHz average, 48 KiB RAM, border/beeper/keyboard on port `$FE`, built-in Kempston on `$1F`. Exact 16 KiB `spectrum-firmware` (`$0000–$3FFF`) linked before download. Unit 0 plays a 1..65,536-byte `.tap` while the machine runs; no MIC write-back. Sockets 1–4 take independently built cards on the edge connector. Open diagnostic firmware and probe card only; no Sinclair ROM is shipped. [Sealed shell](validation/2026-09-28-spectrum-pathfinder-seal.md) from `ccafc7e9` (seed 2 met 52.224 / 74.25 / 12.288 MHz). A [kit ROM link](validation/2026-09-28-spectrum-basic-kit.md) of the 16 KiB Sinclair 48K ROM showed the RAM test and the 1982 copyright line. No card seal, keyboard check, or tape check. Lock `toolchains/spectrum.lock`. |
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

## Not implemented, or not this package

- ZX81 mid-session tape replace and eject use the same `fes.media.blob`
  mailbox without a hold-reset reboot. The runtime path and the host
  `live-media` / `change-tape` API are in. The kit's visible picker has a
  [dated HDMI diagnostic](validation/2026-10-02-zx81-session-display.md).
  See [FES ZX81](fes-zx81.md) and
  [ZX81 tape media](zx81-tape-media.md). Launch-time ROM splice and
  expansion-cart selection are separate; the expansion bus guide is
  [ZX81 expansion bus](zx81-expansion-bus.md).
- ZX81 source includes an in-session launcher plane with observed
  `fes.video.session-display` and HPS DDR capabilities. Opening/closing it
  preserves CPU, RAM and the active core generation; kit tenfoot enables live
  tape routes only on that capable path. The sealed 1.5.0 package, verified
  image and designated-kit observations are recorded in the
  [session-display validation](validation/2026-10-02-zx81-session-display.md).
  Physical operator input and expansion/audio acceptance remain separate.
  See [the hardware room](hardware-rooms.md).
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
| Change RTL or seal a core | [misteross cores](../sources/misteross/docs/cores.md) |
| Prove a Cyclone V primitive instead | [misteross OSS place-and-route](../sources/misteross/docs/oss-pnr.md) |
| Prepare one package, then accept it on a leased kit | [Core developer workflow](core-development.md), [package acceptance](package-acceptance.md) |
| Pong settings and best rally | [Core persistence](core-persistence.md) |
| ZX81 machine, expansion, or tape design | [FES ZX81](fes-zx81.md), [expansion bus](zx81-expansion-bus.md), [tape media](zx81-tape-media.md) |
