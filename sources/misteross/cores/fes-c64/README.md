# FES Commodore 64

`fes.c64` 0.1.0 is a package-only Commodore 64 pathfinder on `fes.computer`
1.0. It follows the Apple II pattern: late-bound firmware, a live removable
disk, and linkable expansion cards. It is not in the factory image. No
Commodore ROM is in this tree. HIP seal and kit acceptance are still open.

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
- VIC-II text: 40×25, color RAM, and the registers the diagnostic writes.
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

## Diagnostic

`diagnostic/firmware.py` assembles the open 16 KiB image. Success stores `$FF`
at `$C000`. A failure stores a stage at `$C000` and `1` at `$C001`:

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
make -C sources/misteross build-fes-c64   # HIP seal; not part of the sim gate
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

`build-fes-c64` authenticates `toolchains/c64.lock` (the shared
`toolchain.lock` commits: Yosys `5391eeb1`, Mistral `8fcc4cb4`, nextpnr
`1656e473`). The socket check admits a compiler-inserted route-through
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
`CFG_ASYNC_READ=1` fails the build. Color RAM stays a combinational logic
read. Before routing, the producer connects the unused write clocks of the
two inferred read-only M10Ks (VIC font and IEC track lookup) to their live
read clocks. It checks their names and disabled write ports so a changed
synthesis shape fails closed.
