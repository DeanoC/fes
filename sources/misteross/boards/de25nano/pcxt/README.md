# DE25 standard PC/XT

This lane brings the FES PC/XT DOS machine (`fes.pcxt-dos`) to the DE25-Nano
behind the shell the [standard ZX Spectrum](../spectrum/README.md) and
[Atari ST](../atari-st/README.md) use: the managed board recipe's atomic
computer transport and allocated display, launch-time firmware upload and the
board SDRAM. Its producer is `../producers/build_pcxt_package.py`, selected
by the `de25-nano` `fes.pcxt-dos` recipe. Nothing here programs a kit.

## The DE10 machine

The machine is the DE10 package's `pcxt_system` with its DOS profile, and
every file under `cores/fes-pcxt` is compiled as it is: the MCL86 8088, the
motherboard (8259A, 8253, 8255, 8237A DMA subset, XT keyboard, speaker), the
ISA CGA and MDA cards, the XT floppy adapter with sector writes, the HID
keyboard frontend, `pcxt_memory` and the CAS-3 capture pipeline.
`../shared-rtl/fes_pcxt_core.v` replaces `fes_pcxt_top`'s shell.

The clocks are the DE10 board's. One PLL on CLOCK1_50 (`machine_pll.ip`,
x 30) makes the machine's 100 MHz for the CPU, motherboard and SDRAM, and
beside it the three SDRAM clocks `fes_pcxt_top` takes from its memory PLL:
the forwarded clock's source at +1.25 ns, the CAS-3 read-word handoff and the
DQ capture clock. The handoff is at +9.25 ns, this PLL's nearest phase to the
DE10's +9.375 ns. The capture clock is at +4.0 ns where the DE10's is at
phase 0: this board's pads return the word later (at phase 0 the first
compile missed setup by 1.0 ns with 12 ns of hold). It samples in the same
read window, before the handoff, so the controller's cycle counts are the
DE10's. The fifth output is the audio master clock,
12.295082 MHz (/122, +564 ppm); on the DE10 too it is unrelated to the
machine clock, and the speaker's 48 kHz PCM is held across the crossing. The
board's machine builder takes the clocks from this spec (`MACHINE_HZ`,
`PLL_IP`, `PLL_PARAMETERS`, `SDRAM_CLOCKS`, `PLL_COMMENT`); the SDRAM clocks
reach the component on its SDRAM conduit.

| DE10 | DE25 |
| --- | --- |
| GLaBIOS's 64 KiB window and the CGA font in the bitstream; `pcxt_boot` copies the window into SDRAM at F0000 once after a cold start | Both uploaded at launch through [`fes.fabric.firmware-upload`](../../../../mister-packages/docs/firmware-upload.md) as one 69,632-byte image: the window, then the font. `fes_pcxt_firmware` drives the same `pcxt_system` firmware port `pcxt_boot` drives, byte by byte, and fills a writable copy of the font with the DE10 font's two combinational read ports |
| MiSTer addon SDRAM, masks shared with A11/A12, Cyclone V DDR clock cell, `altiobuf` pads | Board SDRAM rank 0 with its own mask pins; the second chip stays deselected. `../atari-st/derive.py` swaps the forwarded-clock cell for the inverted clock; the board top owns the DQ tri-state |
| `coleco_video_dpram` as a Cyclone V `altsyncram` under the `QUARTUS` macro | `derive.py` keeps its inferred memory, in M20K: the same ports and one-clock reads |
| HPS GP registers, `fes_computer_mailbox_pcxt` | The atomic computer transport, `fes_gp_crossing`, and the same mailbox profile with the session display and the shell's gamepad ports (the XT reads none) |
| 720p word straight to HDMI | The direct video part's word into the session display |

Conventional RAM, the firmware window and the floppy keep their DE10
halfword offsets: the 1 MiB guest map from 0 and the 360 KiB image at
halfword 80000. The 8088 control ROM's byte lanes and facts header are
generated from the vendored `mcl86_ucode.mem` exactly as the DE10 builds
generate them (`derive.control_rom`).

A new image is a new power-on. `pcxt_system` admits one sequential window
after each cold reset, so a pair at byte 0 after an earlier image's bytes
restarts the machine's memory (SDRAM initialization, the BIOS warm-boot word
and the floppy) before it is stored.

`pcxt-clocks.sdc` names every path between the machine, pixel and audio
clocks: the video mailbox's two toggles and its hold level, and the published
bank it holds. The SDRAM pins are constrained against the ISSI IS42/45VM16320G
data sheet at CAS latency 3, with the capture registers two capture edges
after the chip launches a word, as the DE10 controller's read window takes it.

## Run

```sh
make test   # derivation contracts; no simulator or Quartus
make sim    # the component through its real computer slave; sim.py --dos boots FreeDOS
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/new/private/path
```

`sim` drives the Avalon computer slave against a logical SDRAM rank at CAS
latency 3 on the chip's own clock edge, which checks the controller's command
protocol, recovery times and refresh interval. It checks identity, the 1.1
capability words, that execution cannot be released without firmware, an
abandoned image followed by a complete one, the window in SDRAM, GLaBIOS's
POST banner in CGA text memory with an empty drive, the FreeDOS floppy
committed while the machine runs, and that a hold keeps the firmware. With
`--dos` the BIOS boots the floppy to `A:\>`. The HPS, scanout memory master,
PLL, SDRAM pad timing and HDMI are not modeled.

## Open

The producer compiles a design that meets every reported timing check and
passes the input and audio reviews, and refuses to package it on three
unreviewed Design Assistant findings
([record](../../../docs/validation/2026-11-22-de25-pcxt-compile/README.md)):
CDC-50003 and CDC-50102 on the video mailbox's two-bit bank code, and
LNT-30010 on the DE10 machine reset, which clears the vendored 8253
asynchronously and reaches 248 clock enables. The remaining tier 1 work is on DeanoC/fes#2068 and #2042: the
signoff gate, the repeat compile and a package, the board SDRAM at 100 MHz on
hardware (and the mobile SDRAM's extended mode register, which the DE10
controller does not program), a kit run to `A:\>`, the game corpus, library
launches with an initial disk (FogCast's initial-disk path is still the DE10
format 2's), and bundled GLaBIOS/font firmware.
