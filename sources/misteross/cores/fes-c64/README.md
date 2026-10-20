# FES Commodore 64

`fes.c64` 0.1.0 is a package-only Commodore 64 pathfinder on `fes.computer`
1.0. It follows the Apple II pattern: late-bound firmware, a live removable
disk, and linkable expansion cards. It is not in the factory image. No
Commodore ROM is in this tree. The shared-pin seal is recorded: CUDA package
`1d599353eaef5502…` (build `6f865fddebe62184…`) on `toolchain.lock`, seed 5,
final signoff system 54.29/52.224, pixel 75.10/74.25, audio 229.67/12.288.
A CUDA seal is valid on its own (Deano, 2026-10-13); HIP only adds GPUs.
Kit acceptance of those bytes is still open. The keyboard transposition in
[#750](https://github.com/DeanoC/fes/issues/750) is unrelated.

The core lane is [docs/cores.md](../../docs/cores.md). The mailbox contract is
[home-computer I/O](../../../mister-packages/docs/computer-io.md).

## Machine

- 6510 is the shared NMOS 6502 plus the processor port at `$00`/`$01`. Phi2 is
  a fractional 1.022727 MHz enable of the 52.224 MHz system clock.
- 64 KiB RAM. Standard banking, plus 8 KiB, 16 KiB and Ultimax cartridge maps
  from `/EXROM` and `/GAME`.
- 16 KiB linked firmware. Bytes 0..8191 are the BASIC window `$A000–$BFFF`.
  Bytes 8192..16383 are the KERNAL window `$E000–$FFFF`. The OSS package leaves
  sixteen 1024×10 M10K lanes (column 5, rows 32–47) blank. The character
  generator is in the core, not in the firmware image.
- VIC-II text: 40×25 and the registers the diagnostic writes. Color RAM is
  one synchronous dual-port M10K, 1024×4 at `$D800–$DBFF`. The 6510 can use
  `$DBE8–$DBFF` as scratch. The VIC scan returns zero outside the 1000-cell
  matrix.
  Raster timing is not locked to HDMI. No sprites, bitmap or badlines.
- Reduced SID: three voices, pulse, saw, triangle and noise, a crude envelope
  and volume. No filter.
- CIA1: keyboard matrix, joystick and IRQ. CIA2: IEC, the VIC bank and NMI.
  Both CIAs have timer A and timer B with live counter reads, stopped high-byte
  loading, force-load strobes, continuous and one-shot operation. Timer B can
  count Phi2 or timer A underflows. CNT is held high without external edges,
  so CNT edge modes do not count and the gated timer A mode can count.
  TOD, serial shift and PB6/PB7 timer outputs remain unimplemented; internal
  6526 pipeline delays are not modelled. Port 2 (CIA1 port A) is controller port 0.
- HDMI 720p60. The 320×200 text picture is scaled 4× horizontally and 3×
  vertically. Audio is the shared 48 kHz I2S serializer.

## Disk

`fes.media.c64-disk` 1.0 is media unit 0 (the same unit number an Apple II
floppy uses; a core declares one of them). The image is an exact 174,848-byte
35-track D64. The built-in device 8 speaks CLK/DATA, LSB first, with EOI on
the last byte, well enough for LISTEN, OPEN, a filename, UNLISTEN, TALK and
SECOND. Writes are ignored. `$` returns a four-byte synthetic program.

## Cartridge sockets

One shared cartridge port is exposed as two physical sockets so multi-slot
composition is exercised. Socket 1 is the ROM window (`ROML`/`ROMH`, `/EXROM`,
`/GAME`). Socket 2 is the I/O window (`IO1` `$DE00`, `IO2` `$DF00`). The
layout is `fes.c64-bus.sockets/1`. The rectangles reuse the Apple II slot 2
and slot 4 rows. `scripts/c64_slots.py` generates
`rtl/c64_slot_sockets.v`.

`expansions/probe.v` is the open card. Mode 0 answers `FES1` at `$8000`. Mode
1 is scratch at `$DE00` and id `$C6` at `$DE01`. The machine simulation links
both. A sealed shell leaves the sockets vacant; `scripts/build_c64_slot_card.py`
builds one card into one socket of a frozen shell.
The producer selects probe mode 0 for socket 1 and mode 1 for socket 2;
it authenticates both the legacy PLL output aliases and current physical
C6/C7 pin layout, preserving an already-correct physical layout.
`--gpu-router` and `--toolchain-root` select the shell's GPU lane. HIP is
the default. CUDA sets `CUDA_DEVICE_ORDER=PCI_BUS_ID`. gpu-run leases any
free GPU; the producer does not set `GPU_LEASE_IDS` or a device visibility
mask. The route must prove that backend, and it must match the backend
recorded on the frozen shell.
Each synthesized response is lowered to an independent identity ALUT2 before
the frozen merge: constants and aliased output signals must not be lost by the
pinned cart merger. Clock-pin checks, final analogue setup/hold on all required
clocks and exact CRAM containment are separate admission checks.

## Diagnostic

`diagnostic/firmware.py` assembles the open 16 KiB image. Success stores `$FF`
at `$C000` and paints the word PASS, in white, across three rows in the middle
of the character screen. The border and the screen background are black.
A green bar is the 720p letterbox and is not the pass mark. A failure stores
a stage at `$C000` and `1` at `$C001`:

Stages 7 and 8 with `$C001 = 0` mean waiting, not failure. Mount the
synthetic D64 and confirm media unit 0 is ready before pressing joystick Up,
then keyboard A. This gates LOAD on operator readiness rather than a fixed
sleep: C64 media is currently mounted after CPU release. The SID pulse uses
50% duty cycle so audio acceptance can require an AC tone rather than DC.
`--without-cartridges` skips only stages 5 and 6 for a vacant sealed shell; it
is not expansion acceptance.
The simulation covers both initially-ready input/media and delayed mount,
joystick and keyboard delivery, then captures the next full frame and requires
PASS in white, a black border, a black background and no stage digit.

C64's D64/IEC path remains read-only. Atari ST's initial-disk-before-release
and writable-disk save/restore path is a useful ordering reference, but its
sector commit and game/base-media persistence contract is not implemented by
C64 and is not implied by a successful C64 LOAD.

| Stage | Check |
| --- | --- |
| 1 | RAM |
| 2 | BASIC signature |
| 3 | VIC border |
| 4 | character generator |
| 5 | cartridge ROM |
| 6 | cartridge I/O |
| 7 | joystick |
| 8 | keyboard |
| 9 | D64 `BOOT` bytes `01 08 11 22 33 44` at `$0800` |
| 10 | CIA1 timer B one-shot, IRQ vector and interrupt acknowledgement |
| 11 | CIA2 timer A one-shot, NMI vector and acknowledgement while `SEI` is set |

```sh
make -C sources/misteross sim-fes-c64
make -C sources/misteross sim-fes-c64-cia # directed timer/register checks
make -C sources/misteross build-fes-c64   # default HIP lane; not part of the sim gate
```

The CIA register behavior follows the MOS/Commodore
[6526 data sheet](https://www.retrodocs.fr/wp-content/uploads/pdf/MOS-6526.pdf),
pages 5–7: separate latches and counters, load conditions, clock selection,
one-shot stopping, ICR acknowledgement and the read-zero force-load bit.
The CIA2 NMI connection follows the Commodore
[Programmer's Reference Guide](https://www.zimmers.net/anonftp/pub/cbm/c64/manuals/c64-programmers-reference-guide.txt),
pages 348–349. `sim-fes-c64` includes the directed CIA regression and the
firmware's CPU interrupt checks. These are host simulations, with no sealed
artifact or kit acceptance implied.

`build-fes-c64` authenticates `cores/fes-c64/toolchain.lock` (Yosys `3a5accbd`,
Mistral `8fcc4cb4`, nextpnr `9128800f`). The build record uses the audited
static closure in `scripts/source_closures.json`. The seal also requires both
achieved system-PLL outputs within 1 Hz of 52,224,000 Hz and 12,288,000 Hz.
The [C64 build contract](../../docs/architecture.md#fes-commodore-64) states
the packed M/N/K coefficients that fix those frequencies before the placer
seed runs. `--gpu-router CUDA` with
`--toolchain-root` is a separate package; slot cards take those same flags.
The 6 Oct kit diagnostic used
nextpnr `0c5ed400` and does not accept this pin. Supporting evidence from a
HIP build of [#749](https://github.com/DeanoC/fes/pull/749) head
`8b713d5d5716f47bfbc58582957e77ade4878565` on this lock, on Powerboat's
RX 7900 XTX: package
`22ecf46827b3f57b55f7b5e5187167c7177bf29dc931870721aba629639a9dbf`, build
`4c90ff27c072d8fbfa81366f2bd7d9a1`, timing system 54.51/52.22, pixel
82.79/74.25, audio 215.33/12.29. The
[kit B HIL](https://github.com/DeanoC/fes/pull/749#issuecomment-6229821313)
showed BASIC READY and clean video. The recorded seal is the CUDA package
above.

The socket check admits a compiler-inserted route-through
buffer only at a pinned boundary flip-flop's paired combinational half, with
the exact physical pin map and a dedicated connection to that flip-flop;
clock-coverage outputs stay unused and every other shell cell inside a socket
is rejected.
The sixteen firmware lanes are synchronous 1024x10 M10Ks on the system
clock, with reads enabled and active-low writes disabled. The bank selector
is registered with the M10K address; a combinational lane mux and the final
data register keep the two-cycle latency the 6510 samples at cycle 16 of
phi2. The ROM map requires `CFG_ASYNC_READ=0`. After synthesis, and again
on the routed netlist, any `MISTRAL_M10K` or `MISTRAL_M10K_TDP` with
`CFG_ASYNC_READ=1` fails the build. Color RAM uses one synchronous dual-port
M10K: the system-clock port returns CPU reads before the cycle-16 sample and
the pixel-clock port follows the same two-stage scan pipeline as main RAM.
Before routing, the producer connects the unused write clocks of the
two inferred read-only M10Ks (VIC font and IEC track lookup) to their live
read clocks. It checks their names and disabled write ports so a changed
synthesis shape fails closed.
