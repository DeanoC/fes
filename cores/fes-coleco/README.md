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
The host holds execution reset while uploading and commits media before
releasing it. GP commit acknowledges publication of the mailbox blob; the
machine then copies it into cartridge RAM. CPU and VDP reset remain asserted
until `media_ready && media_loaded`, even if the host releases immediately
after the commit ACK. The OSS final registered write completes before
`media_loaded` permits execution. No host delay or new mailbox operation is
required. A release without committed media also keeps CPU and VDP in reset.

The machine adapter qualifies each held CPU `OUT` cycle into one VDP write
strobe. TV80 holds IORQ/WR low across multiple CPU enables; passing every enable
through used to duplicate control bytes and VRAM writes. Read sampling retains
its existing window; this diagnostic does not establish buffered VRAM-read or
complete TMS9918 compatibility.

## Open Graphics I diagnostic

From the misteross root:

```sh
make coleco-diagnostic
# Or generate only the exact compact cartridge and a reference image:
python3 cores/fes-coleco/diagnostic/generate.py \
  --output build/diagnostics/fes-coleco/graphics-i.rom \
  --preview build/diagnostics/fes-coleco/graphics-i.ppm
```

`diagnostic/generate.py` is the annotated Z80 instruction source/emitter.
Python's standard library is sufficient; no assembler, downloaded ROM,
commercial cartridge or proprietary BIOS is used. Its original code, generated
cartridge and reference image are covered by `diagnostic/LICENSE` (MIT).
The raw cartridge is **989 bytes**, entered at **0x8000** by the open reset
shim's `JP 0x8000`. Its SHA-256 is recorded by the generation command. There is
no header or container. The entry/code/data stay entirely within the portable
1..16384-byte media aperture. `--pad-to 16384` produces an equivalent full-size
image, with unused trailing FF bytes; `make coleco-diagnostic` generates this
as `graphics-i-16k.rom` too. Upload the `.rom` bytes through the FogCast media
path for a loaded `fes.coleco` package; the PPM is a host comparison artifact.

The CPU disables interrupts, initializes SP without reading or using stack RAM,
clears all 16 KiB VRAM through port BE, sets all eight VDP registers through BF,
then fills the name table at 0000, three patterns at 0800 and colors at 2000.
It also writes a sprite-list terminator at 1B00, enables Graphics I display, and
halts with a static picture. All VRAM consulted for the final image is written
by the program; CPU RAM power-up values are irrelevant. The VDP currently
ignores display-enable masking, so transient startup contents can be visible
while initialization runs. Compare the settled image, allowing about a second
after execution release for this diagnostic.

Expected active HDMI image (`graphics-i.ppm`, 1280×720):

- Black surroundings; centered 512×384 picture at x=384..895, y=168..551.
- Solid green border, one 8×8 logical tile thick (nominally 16 HDMI pixels).
- Inside, 30 columns × 22 rows of alternating green/orange 12×12 HDMI squares
  with black gaps. The upper-left interior square is green.
- Green is RGB `00ff40`, orange `ff4000`, black `000000`: 75,168 green,
  47,520 orange, 798,912 black active pixels; 122,688 nonblack in total.

The current registered framebuffer read shifts the contents one HDMI pixel
right inside the fixed image window. The preview and board oracle include
that existing latency/clipping (left border 17 pixels, right border 15 pixels).
The VDP uses one color byte per tile name and a reduced two-color foreground
mapping, not the complete TMS9918 Graphics I palette. This cartridge diagnoses
this slice, not a stock BIOS cartridge format or retail compatibility. It does
not test audio, controller input, sprites, interrupts or BIOS services.

`make sim-fes-coleco` generates both cartridges and runs the default and
`FES_COLECO_OSS` lanes with the production `TV80_REFRESH=1` setting.
Each board simulation uploads 989 → 16384 → 989 bytes
through HOLD/BEGIN/DATA/COMMIT/RELEASE, including the odd tail and full aperture,
with **no wait before RELEASE**. It asserts CPU/VDP reset through the copy,
compares every loaded byte including the OSS final flush, observes one VDP
write per CPU OUT, and requires the CPU to write every VRAM address before
HALT. Randomized initial RAM exercises independence from power-up contents.
After raster settling, every RGB/DE/HS/VS sample of a complete 1650×750 HDMI
frame is checked against an independent C++ oracle for each load. Only the
simulated GP, clocks and I²C boundaries are driven; the test does not preload
the cartridge, VDP or framebuffer or force CPU execution state.

These are host-only behavioral simulations with controllable digital clocks,
not PLL/timing checks, FogCast/runtime integration, or hardware acceptance.
This step changes `coleco_machine.sv` (reset gating and VDP write strobes), so
the earlier FPGA packages below cannot establish this diagnostic's result.
The selected revision also changes BUILD_ID: the integrator must rebuild and
seal the final artifact, then perform the separately owned upload/capture test.
No full FPGA build or hardware operation is part of this diagnostic change.

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

The clean integration build of implementation revision
`b60e1aaccc5ec0c6f93d654c5cb2e6caf9f3e873` synthesized 85
`MISTRAL_M10K_TDP` and 48 `MISTRAL_M10K` cells, completed with no unrouted nets,
and reached 59.62 MHz on `clk_sys` and 90.88 MHz on `pixel_clk`. Its sealed RBF
was 2,484,053 bytes with SHA-256
`370e478fcb1706845bc39eb71a3ee3caeb9dedfed1855126f49579efbe0389e8` in
package `3b1b9dbcf2a30e8b389ee2aaf11dc0d9facfd3c4b9461b4c9f301afa6244f368`.

A clean Quartus Prime Lite 17.0.2 compile of that same implementation revision
completed analysis, fitting, assembly, and the required TimeQuest checks. It
used 2,168 logic cells and 100 RAM segments; its sealed RBF was 2,294,532 bytes
with SHA-256
`b0b315e758e8cb7ae0171ad4be501acfad3ea7c600fe473bfea479cb9a7dc973` in
package `6e863542effce1b60ff45f8665edfead60297f5bb31c15782c3de8bbb667114e`.

The earlier raw OSS RBF (`c6a060fa...a83eca9`) was loaded only as a development
probe and timed out because it has no MiSTer identity. The exact clean OSS
format-2 package above then loaded through the native FogCast path; the target
reported the package, ABI, build ID, and required interfaces, and the host-owned
stop returned it to idle. The HDMI sample was black, so this is exact-artifact
load/stop diagnostic evidence, not Coleco functional or video acceptance.

## Bring-up fixes and compiler workarounds

### Functional TV80/media fixes

Before the write-strobe guard, the real CPU's `OUT (BF),A` sequence for
register 1 (`C0` data, then `81` selector) left register 1 as `81`, not `C0`.
The default simulation also showed register 2=`82` instead of `00` and
register 4=`84` instead of `01`. Both lanes returned black at HDMI (384,168),
where the cartridge requires green. The board regression explicitly rejected
a second `bus_write` strobe while the same CPU IORQ/WR transaction remained
asserted: `VDP consumed the same CPU OUT transaction twice`. The fix adds
`vdp_write_seen` in the machine adapter, re-armed when IORQ or WR deasserts,
and suppresses further write strobes within that transaction. Direct VDP tests
had driven one strobe per write and did not expose the TV80 integration bug.

The immediate-release regression separately failed with
`CPU/VDP escaped reset before cartridge copy completed` before internal reset
gating was added. A commit ACK publishes media; it does not finish the copy.
The new test removes the old `size + 32` delay before RELEASE and observes
reset until the actual copy completes, then validates every copied byte and
CPU-generated pixel over 989 → 16384 → 989-byte loads in both lanes.
These two fixes change functional RTL. They are **not** Yosys/nextpnr/Quartus
workarounds and require fresh final FPGA builds. No new compiler issue was
investigated in this host-simulation-only step.

### OSS/Yosys/nextpnr workarounds

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
