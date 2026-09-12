# FES ColecoVision first slice

This directory contains the next FES emulator bring-up after Pong and ZX81.
It is a reduced ColecoVision-compatible console slice that uses the existing
`fes.simple-computer` 1.0 mailbox and the DE10-Nano fixed 720p shell. The
reference system is the [MiSTer ColecoVision core](https://github.com/MiSTer-devel/ColecoVision_MiSTer),
but this package does not copy the MiSTer framework and does not claim full
retail-game compatibility.

## Implemented first slice

- Verilog TV80 Z80-compatible CPU, clock-enabled from the 52 MHz FES system
  domain.
- Raw 1–16 KiB mailbox media blob, mirrored through `0x8000–0xffff`.
- Open 8 KiB reset shim at `0x0000–0x1fff`; its vector is `JP 0x8000`, so no
  proprietary ColecoVision BIOS is embedded.
- 1 KiB CPU RAM at `0x6000–0x63ff`, mirrored through `0x7fff`.
- TMS9918-style VDP ports `0xbe` (data) and `0xbf` (control/status), 16 KiB
  VRAM, register-based Graphics I name/pattern/color tables, tile pixels, and
  VBlank status.
- Controller reads at `0xfc` and `0xff`, adapted from keyboard rows 0 and 1
  as active-low five-bit groups.
- Centered 512×384 logical image in the established 1650×750 HDMI timing.

Audio, BIOS services, expansion hardware, bank switching, full VDP modes,
cycle-perfect clocking, sprite evaluation, and native FogCast/runtime
selection remain outside this first slice. The raw media limit and reset shim
are deliberate compatibility boundaries.

## Memory and host interfaces

| Address or port | Function |
| --- | --- |
| `0x0000–0x1fff` | open reset ROM; jumps to `0x8000` |
| `0x6000–0x7fff` | mirrored 1 KiB CPU RAM |
| `0x8000–0xffff` | mirrored 16 KiB cartridge aperture |
| I/O `0xbe` | VDP data |
| I/O `0xbf` | VDP control/status |
| I/O `0xfc` | controller 1 |
| I/O `0xff` | controller 2 |

The mailbox, keyboard rows, media handshake, build identity, and fixed-video
interfaces are byte-for-byte the existing `fes.simple-computer` boundary.
The machine consumes media during reset; the host must commit media before
releasing execution reset.

## Commands

```sh
make sim-fes-coleco VERILATOR=/absolute/path/to/verilator
make build-fes-coleco-quartus
make build-fes-coleco
```

The Quartus recipe requires authenticated Quartus Prime Lite 17.0.2. The OSS
recipe authenticates the repository-local Yosys, nextpnr-mistral, and Mistral
tools, routes `5CSEBA6U23I7`, and seals a format-2 package only after the
timing/resource checks pass. Neither command programs hardware.

The raw OSS lane has been exercised against the current source: Yosys
synthesized 85 `MISTRAL_M10K_TDP` and 48 `MISTRAL_M10K` cells, nextpnr
completed with no unrouted nets, and timing reached 59.82 MHz on `clk_sys` and
89.48 MHz on `pixel_clk`; the generated RBF was 2,481,055 bytes with SHA-256
`c6a060fa117be2bf9769c3b9be65b9f5d034c4dece2cf1327a8033db6a83eca9`. A sealed
package still requires a clean selected commit because the recipe intentionally
rejects dirty source trees.

A manual Quartus Prime Lite 17.0.2 compile of the same dirty source also
completed analysis, fitting, assembly, and the required TimeQuest checks. It
used 2,170 logic cells and 100 RAM segments; the diagnostic RBF was 2,296,512
bytes with SHA-256
`5efb4f431b99c103f08dbf42289624c75a658f22bdcc306d5c1ca469808d3fed`. This is
host-side diagnostic evidence, not a sealed artifact or hardware acceptance.

## OSS/Yosys/nextpnr workarounds

These are the concrete portability accommodations to hand to the
Yosys/nextpnr/Mistral owner:

| Boundary | Workaround in this bring-up |
| --- | --- |
| Verilog/VHDL frontend | OSS uses only the Verilog TV80 files and `T80pa`, with `TV80_REFRESH=1`; it does not depend on the VHDL T80 path. |
| Inferred machine RAM | Cartridge, CPU RAM, and reset ROM use `coleco_dpram`; OSS selects registered `ram_style="m10k_tdp"` ports, while simulation and Quartus retain asynchronous/unregistered reads. |
| Registered media bridge | OSS mailbox RAM returns `media_q` one clock after `media_addr`; the machine primes the request, delays the cartridge write address, flushes the final byte, and re-arms when `media_ready` drops or reset rises. |
| VDP multi-read VRAM | A single inferred VRAM with one CPU port and three combinational raster reads fails Mistral memory mapping and also leaves Quartus with an oversized direct-memory implementation. Both compiler paths use three coherent `coleco_dpram` copies, broadcast CPU writes, and pipeline name → pattern/color reads by two clocks. |
| Quartus framebuffer inference | The original 49,152-entry async-read framebuffer expanded to 241,553 combinational nodes, exceeding the Cyclone V limit of 83,820. `coleco_video_dpram` makes the system write/pixel read boundary explicit with an independent-clock, registered-read `altsyncram` in Quartus and an M10K-shaped wrapper in OSS. |
| Quartus VDP inference | After the framebuffer fix, a direct VDP VRAM array still produced 186,906 combinational nodes and could not fit. The registered three-copy VDP path is therefore selected for `QUARTUS` as well as `FES_COLECO_OSS`; this is a Quartus resource-inference workaround, not a mailbox-contract change. |
| Bulk initialization | Clearing 16 KiB VRAM, 16 KiB cartridge, or the 49,152-entry framebuffer in an `initial` loop expands into thousands of `$meminit` cells and can exhaust the synthesis memory budget. The bring-up leaves those RAMs uninitialized and initializes only scalar state. |
| Reset image format | OSS/Yosys consumes the tracked byte-per-line `coleco_reset_rom.hex`; Quartus `altsyncram` consumes the tracked range-form `coleco_reset_rom.mif`. The Quartus recipe copies and pins both files. |
| PLLs | The two `altera_pll` wrappers are retained. OSS models them through the existing Mistral cells; the CPU frequency approximation is a clock-enable divider, not a fabric-generated clock. |
| HDMI I²C | Quartus uses tri-state assignments; OSS uses `MISTRAL_IO` open-drain pads and places the HPS I²C primitive at BEL `cyclonev_hps_interface_peripheral_i2c.52.60.0`. |
| Constraints | OSS uses only the accepted `constraints-oss.qsf` and `clocks-oss.sdc` subset: pin assignments plus a 50 MHz input `create_clock`; nextpnr derives the PLL clocks. |
| Route pressure | The reproducible passing settings are device `5CSEBA6U23I7`, seed 7, `router1`, `--tmg-ripup`, and a 74.25 MHz request. |
| Conditional simulation | `make sim-fes-coleco-oss` compiles GP, VDP, machine, and top-level tests with `FES_COLECO_OSS`; `make sim-fes-coleco` includes that target before the default lane. |

The initial failures and fixes are intentionally preserved in the source and
architecture notes so toolchain changes can remove a workaround instead of
silently retaining it. The exact current-source OSS RBF was loaded through the
FogCast target-agent kit lease on the designated disposable kit. The target
agent's development probe timed out; the core was stopped and the lease was
released cleanly. No HDMI capture or functional/acceptance result was claimed.
