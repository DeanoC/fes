# FES ColecoVision Bring-up Design

## Status and bounded scope

This change adds the next standalone FES FPGA bring-up in `misteross`: a
reduced ColecoVision-compatible console slice with matching Verilator,
Quartus, and OSS nextpnr/Mistral build lanes. The reference target is the
official MiSTer ColecoVision Quartus core, but this work does not copy its
framework or claim complete retail-game compatibility.

The implementation stays in `misteross` and does not change FogCast,
libmister-runtime, or mister-packages. The existing `fes.simple-computer` 1.0
mailbox is reused for reset, active-low input rows, fixed 720p video, and one
bounded media blob. A later integration task may define a console-specific
ABI and native ColecoVision runtime profile; this bring-up must not invent
that shared contract locally.

The first slice is deliberately limited to:

- a Z80-compatible TV80 CPU at a fractional-enable approximation of the
  ColecoVision clock, driven from the existing 52 MHz system domain;
- the first 16 KiB of a raw cartridge blob, mirrored over the cartridge window;
- an open 8 KiB reset shim that jumps to cartridge address `0x8000`, avoiding a
  proprietary BIOS dependency while keeping the ColecoVision address map;
- 1 KiB mirrored CPU RAM;
- a TMS9918-style VDP subset with 16 KiB VRAM, CPU data/control ports,
  register-based name/pattern/color tables, Graphics I tile rendering, and a
  bounded VBlank status path;
- two controller matrix views derived from the existing 40 active-low
  `fes.simple-computer` keyboard bits; and
- the established 720p60 HDMI shell, HPS GP block, ADV7513 I²C block, and
  build-record/package format.

Audio, ColecoVision BIOS calls, expansion hardware, bank-switching schemes,
full VDP modes, exact cycle timing, and native FogCast/runtime selection are
out of scope. The media limit and reset shim are explicit first-slice limits,
not silent compatibility claims.

## Candidate approaches considered

1. **Port the complete upstream MiSTer core.** This provides the highest
   compatibility, but the upstream tree includes VHDL and MiSTer framework
   dependencies that do not fit the pinned Yosys/nextpnr OSS lane. It would
   also require a new shared media/controller ABI and a much larger review.

2. **Build a Verilog-only reduced ColecoVision slice around the existing TV80
   and FES shell.** This is the selected approach. It exercises the next
   meaningful hardware boundary above ZX81—console cartridge mapping, a VDP,
   VRAM, and controllers—while keeping the OSS and Quartus sources
   in one language and reusing already proven shell patterns.

3. **Use the existing Pong ABI with a built-in demonstration ROM.** This would
   make the build small, but would remove host media delivery and make the
   result a demo rather than a useful emulator bring-up. It is rejected.

## Architecture

### FES mailbox and top-level shell

`cores/fes-coleco/rtl/top.v` follows FES ZX81's two-clock shell:

- `sys_pll.v` produces 52 MHz from the 50 MHz DE10-Nano oscillator;
- `pixel_pll.v` produces the fixed 74.25 MHz HDMI clock;
- the existing `fes_computer_gp` implementation is copied into the package so
  its inputs are pinned and independently reviewable;
- Quartus uses its native open-drain I²C tri-state path, while the OSS lane
  uses the proven `MISTRAL_IO` pads and the fixed HPS I²C BEL;
- the top-level parameter carries the deterministic 128-bit build identity;
  and
- no build command programs a board.

The media handshake remains the existing one-request-at-a-time blob protocol.
During reset, the machine consumes the committed media bytes through the
mailbox address/data interface and writes them into the cartridge RAM. The
host must release reset only after media commit, exactly as with the ZX81
bring-up. In the OSS branch the mailbox RAM read is registered, so the machine
primes the next address, consumes the previous result at a separate write
address, flushes the final byte on an extra edge, and re-arms when
`media_ready` drops or reset rises. The default/Quartus branch retains the
unregistered read timing.

The eight 5-bit active-low keyboard rows are interpreted as a controller
matrix by the core. The current first slice exposes rows 0 and 1 as the two
five-bit controller groups; rows 2–7 remain neutral/reserved. The mapping is
documented next to the RTL and is intentionally an adapter over the existing
computer ABI, not a new public ABI.

### CPU and memory map

The Coleco machine reuses the Verilog TV80 sources already proven in the FES
ZX81 lane. The machine presents the following first-slice map:

| Address or port | Function |
| --- | --- |
| `0x0000–0x1fff` | open reset shim ROM; reset vector jumps to `0x8000` |
| `0x6000–0x63ff` | 1 KiB writeable RAM |
| `0x6400–0x7fff` | mirrored RAM |
| `0x8000–0xffff` | 16 KiB cartridge blob, mirrored every `0x4000` |
| I/O `0xbe` | VDP data port |
| I/O `0xbf` | VDP control/status port |
| I/O `0xfc` | controller 1 read |
| I/O `0xff` | controller 2 read |

The CPU clock enable uses an accumulator in the 52 MHz domain to approximate
3.579545 MHz without introducing a third PLL output. CPU bus signals are
sampled only on the enable phases required by TV80; the VDP and cartridge
memories remain synchronous. The approximation is sufficient for this
bring-up and is called out in the manifest/architecture notes rather than
presented as cycle-perfect timing.

The cartridge and VRAM arrays use explicit `coleco_dpram` shapes and
`ramstyle = "M10K"` annotations. OSS selects registered true-dual-port reads;
the default/Quartus branches retain the unregistered read behavior needed by
the existing shell and Quartus `altsyncram` configuration. No mixed-width
memory shape is required by the first slice.

### VDP and video

`coleco_vdp.sv` owns CPU-facing VDP ports, VRAM, registers, tile state, and a
256×192 logical raster. The selected Graphics I path uses the standard name,
pattern, and color table register bases and exposes VBlank status. Sprite
evaluation, collision, and overflow status are deferred from this first slice;
the VDP does not implement every TMS9918 mode.

`coleco_video_720p.v` captures the logical raster into an M10K-backed frame
buffer in the system domain and reads it in the 74.25 MHz pixel domain. It
centers a 512×384 2× nearest-neighbour image inside the existing 1650×750
timing, emits fixed 720p sync/de signals, and uses a stable Coleco palette.
This keeps video-domain timing independent from CPU/VDP clock-enable jitter.

## Build lanes and artifacts

The component adds:

- `make sim-fes-coleco`: default and `FES_COLECO_OSS` GP mailbox, machine/VDP,
  video, and board-level Verilator tests;
- `make build-fes-coleco-quartus`: an authenticated Quartus 17.0.2 project
  with generated QSF, SDC, input hashes, timing evidence, RBF, and manifest;
- `make build-fes-coleco`: Yosys `synth_intel_alm -nolutram -nodsp`, pinned
  nextpnr-mistral placement/routing, RBF, timing/utilization evidence, and a
  sealed format-2 package; and
- `cores/fes-coleco/README.md` plus the canonical
  `docs/architecture.md` section describing the present slice and its
  limitations.

The OSS recipe must require the HPS GP and I²C cells, two PLL cells, and at
least one M10K while rejecting MLAB, DSP, and oscillator resources. It uses the
same device, I²C BEL, router settings, seed, and 50→52/74.25 MHz clock
evidence checks as FES ZX81 unless the build proves a narrowly scoped change
is necessary.

## Toolchain workarounds to record and hand off

These are implementation constraints to validate, not claims that the
upstream tools are wrong. Each one must be recorded in the build summary or
the architecture note with the observed failure/evidence if it is needed:

1. **Verilog-only TV80 path:** the OSS recipe must select `tv80_core.v`,
   `tv80_alu.v`, `tv80_mcode.v`, `tv80_reg.v`, and `t80pa.v` with
   `-DTV80_REFRESH=1`; it must not pull in the ZX81 VHDL T80 path.
2. **M10K inference shape:** use registered synchronous ports and explicit
   `ramstyle` attributes for cartridge, VRAM, and capture storage. If locked
   Yosys fails to infer one of these arrays, the first fallback is a small
   explicit `MISTRAL_M10K` wrapper matching an existing experiment, not a
   change to the machine map.
3. **PLL model split:** retain the existing `altera_pll` wrappers and
   `QUARTUS`/OSS conditional source behavior. Do not add a fabric clock
   divider in place of the two required PLL resources; the CPU approximation
   is a clock enable only.
4. **Open-drain I²C:** keep the Quartus tri-state and OSS `MISTRAL_IO` forms
   separate, with the HPS I²C site pinned to `52.60.0`. A route failure at
   this site is a nextpnr/Mistral issue to hand off, not a reason to delete
   HDMI I²C from the core.
5. **SDC/QSF parser limits:** keep constraints in the already accepted
   `clocks-oss.sdc`/`constraints-oss.qsf` subset. If nextpnr rejects a
   semantically unnecessary Quartus assignment, remove only that assignment
   from the OSS copy and record the exact diagnostic.
6. **SystemVerilog memory initialization:** provide a checked `.hex` path for
   Verilator/OSS and a Quartus-compatible copied ROM input in the generated
   project. If `readmemh` plus inferred M10K is rejected, use the existing
   MIF/parameter split from ZX81 and record the minimal source-level
   conditional.
7. **Routing pressure:** start with the ZX81 `router1`, seed 7, and
   `--tmg-ripup` settings. If the added VDP/framebuffer does not route, record
   the first failing resource/timing class and the smallest reproducible
   reduced design for the Yosys/nextpnr/Mistral owner.

The first six items are expected portability accommodations; item 7 is a
measurement-driven escalation only if the actual route requires it.

## Verification and acceptance

Tests are written before the corresponding RTL and must fail for the missing
behavior before implementation:

1. GP tests prove identity, reset/release, controller-row writes, media begin,
   byte-pair/tail transfer, commit, and invalid-state handling using the
   existing exchange style.
2. Machine tests boot a synthetic 16 KiB cartridge, verify the reset shim,
   RAM mirror, cartridge mirror, controller reads, VDP register writes, VRAM
   writes, and status reads.
3. VDP/video tests verify deterministic tile pixels, VBlank status,
   frame-buffer capture, 720p timing, and frame tick.
4. Board simulation verifies HPS GP/I²C shell connectivity and HDMI outputs.
5. The OSS build validates synthesis cells, complete routing, 52 MHz and
   74.25 MHz timing, forbidden-resource absence, RBF bounds, manifest hashes,
   and clean-tree provenance. The Quartus lane performs the corresponding
   project/timing/RBF sealing when Quartus 17.0.2 is available.

Hardware programming is not part of this change. Any later physical test
must use the designated FogCast kit lease and exact-artifact acceptance path.
