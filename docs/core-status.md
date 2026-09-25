# Core status

This is the current described-core matrix. It is taken from
`config/core-recipes.toml`, `profiles/native-integration-dev.toml`, and the
misteross producers that emit format-2 manifests, except ZX81, SMS and SG-1000's
format-3 manifests with sealed ROM maps. A dated note under
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

The factory order is `fes.pong`, `fes.zx81`, `fes.coleco`. All of those, plus
`fes.sms`, `fes.sg1000` and `fes.catch`, seal with HIP/nextpnr. Quartus is an oracle where
the recipe says so. It is not the product path and not a fallback.

## Matrix

| Package | Version | Standing | ABI | Required interfaces | Optional | What it implements |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.pong` | 1.1.0 | Factory | `fes.simple-game` 1.0 | `fes.gamepad` 1.0, `fes.video.fixed-720p60` 1.0, `fes.persistence.words` 1.0, `fes.pong.progress` 1.0 | — | ROM-less Pong. Paddle speed and best rally persist. Lock `toolchain.lock`. |
| `fes.zx81` | 1.2.0 | Factory | `fes.simple-computer` 1.0 | `fes.keyboard` 1.0, `fes.video.fixed-720p60` 1.0, `fes.media.blob` 1.0 | `fes.expansion.zx81-bus` 1.0 | 1 KiB RAM, 40-key matrix, `.p` blob (1–16 KiB) at launch or mid-session, vacant expansion socket. BASIC is spliced onto the sealed bitstream at launch; it is not hashed into the package. Lock `toolchains/zx81-expansion.lock`. |
| `fes.coleco` | 1.2.0 | Factory | `fes.application` 1.0 | `fes.audio.pcm-s16-stereo-48k` 1.0, `fes.gamepad.ports` 1.0, `fes.keypad.ports` 1.0, `fes.video.fixed-720p60` 1.0, `fes.media.blob` 1.0, `fes.media.blob-stream` 1.0 | `fes.firmware.blob` 1.0, `fes.expansion.coleco-bus` 2.0 | Reduced ColecoVision with a vacant or linked SGM socket. Blob 1–16 KiB keeps the mirrored map. Stream admits 1–32 KiB at `0x8000–0xffff`. Two gamepads, two 12-key keypads, SN76489, optional 8 KiB firmware overlay. Open `JP 0x8000` shim when no firmware is bound. Not a retail-complete core. Lock `toolchains/coleco-sgm.lock`. |
| `fes.sms` | 1.3.0 | Package-only | `fes.simple-computer` 1.0 | `fes.keyboard` 1.0, `fes.video.fixed-720p60` 1.0 | — | Master System slice. Do not use `fes.mastersystem`. Exact 32 KiB `cartridge-rom` is linked through a sealed ROM map before download; pad shorter fixed-map images with `0xff`. 8 KiB RAM at `0xc000`, Mode 4 VDP, SN76489 on `0x7E`/`0x7F`, FPGA I2S into the ADV7513. No `fes.audio` mailbox or Sega mapper. Same registered-memory lock as SG-1000. |
| `fes.catch` | 1.0.0 | Package-only | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.gamepad` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | ROM-less paddle game on the shared application shell. No BIOS, cartridge, or factory-image entry. Lock `toolchain.lock`. |
| `fes.sg1000` | 1.1.0 | Package-only | `fes.simple-computer` 1.0 | `fes.keyboard` 1.0, `fes.video.fixed-720p60` 1.0 | — | SG-1000 slice. Exact 16 KiB `cartridge-rom` linked before download at `0x0000`; pad shorter fixed-map images with `0xff`. 1 KiB RAM at `0xc000`, joysticks on `0xdc`/`0xdd`. No PSG or startup media blob. Same registered-memory lock as SMS. |
| `fes.demo` | 1.0.0 | Reference | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0 | — | Autonomous video. Not registered. |
| `fes.demo-media` | 1.0.0 | Reference | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.gamepad` 1.0, `fes.media.blob` 1.0 | — | Palette-from-blob reference. Not registered. |
| `fes.demo-audio` | 1.0.0 | Reference | `fes.application` 1.0 | `fes.video.fixed-720p60` 1.0, `fes.gamepad` 1.0, `fes.audio.pcm-s16-stereo-48k` 1.0 | — | Stereo tone reference. Not registered. Catch is the registered game on this shell. |
| splash / idle | sealed file | Board firmware | none | none | — | `sources/misteross/sealed/fes-splash.rbf`, pinned by image policy for splash and Stop-idle. 720p60 HDMI. No MiSTer user-io: nothing answers Probe `0x0014` or HPS framebuffer `0x002f`. |

`fes.media.blob` 1.0 is 1–16384 bytes. `fes.media.blob-stream` 1.0 is a
different interface and admits 1–32768 bytes on the cores that require it.
Host storage can hold larger files. Storage size is not cartridge capacity.
See [media capacity](core-media-evolution.md).

The Coleco v2 shell and independently linked SGM expansion have
[exact-artifact kit diagnostic acceptance](validation/2026-09-25-coleco-sgm-v2-hil.md)
for the named artifacts in that record. Factory-image acceptance of a newly
built package remains a separate check.

## Not implemented, or not this package

- ZX81 mid-session tape replace and eject use the same `fes.media.blob`
  mailbox without a hold-reset reboot. The runtime path and the host
  `live-media` / `change-tape` API are in. Sofa and kit checks are not part
  of that landing. See [FES ZX81](fes-zx81.md) and
  [ZX81 tape media](zx81-tape-media.md). Launch-time ROM splice and
  expansion-cart selection are separate; the expansion bus guide is
  [ZX81 expansion bus](zx81-expansion-bus.md).
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
