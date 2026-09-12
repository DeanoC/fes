# ColecoVision Graphics II sprite compiler bring-up

This is the next bounded slice after the [VDP read/status/NMI
validation](2026-09-12-coleco-vdp-io.md). It adds the implemented Graphics II
sprite path to the Coleco-compatible development core and records the paired
Quartus and Yosys/nextpnr/Mistral results. It does not claim full TMS9918 mode
coverage, audio, BIOS services, commercial-cartridge compatibility or release
image acceptance.

## Selection and scope

The integration base selected misteross
`292f57ed61576203123cc749a391f49d348f22eb`. The reviewed component result is
`80c2e498d35a687c43b821f9e3c7b645acc3c4b9`, branch `feat/fes-coleco`, in
`out/dev/fes-coleco/misteross`. The parent integration branch selects that
commit at `sources/misteross`; its other component pins and shared definitions
are unchanged. The parent pin/documentation change is isolated to this
integration branch.

The RTL implements normal 8x8 and 16x16 sprites, magnification,
early-clock positioning, signed/clipped X coordinates, transparent color,
priority, four-visible-sprites-per-line overflow, collision and the first
suppressed SAT index. It walks SAT and pattern data serially and publishes a
completed line bank between raster frames. The raw cartridge limit, open reset
shim, fixed 720p shell and `fes.simple-computer` 1.0 mailbox remain the
existing compatibility boundary.

## Behavioral evidence

The component's open, BIOS-free sprite diagnostic is generated from
`cores/fes-coleco/diagnostic/sprite_io.py`; no commercial ROM, downloaded
cartridge or proprietary BIOS bytes are used. It first configures Graphics I,
clears VRAM, places five 8x8 sprites on one line, verifies collision and the
four-sprite limit through the CPU-visible status port, then configures three
16x16/magnified sprites to exercise early-clock placement and right-edge
clipping. A pass stores `A5`; timeout/failure stores `E1`.

The exact final component checks passed:

- `VERILATOR=build/toolchain/install/bin/verilator make sim-fes-coleco-oss`:
  CPU/status and complete HDMI-frame checks pass for compact/full/compact
  cartridges.
- `VERILATOR=build/toolchain/install/bin/verilator make sim-fes-coleco`:
  both default and `FES_COLECO_OSS` lanes pass. The sprite board reports three
  CPU status samples and exact 720p frames; the board also checks the three
  cartridge loads and 27,680 nonblack active pixels per settled frame.
- `python3 -m unittest discover -s tests`: 668 tests pass, one delegated test
  is skipped.
- `QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus make
  sim-fes-coleco-quartus`, with the relocated Icarus paths documented below:
  the vendor RAM and media probes pass. The RAM probe observes the registered
  read latency; the media probe passes exact 1, 3, 989, 16384 and 989-byte
  copies with immediate RELEASE.

The final source worktree is clean. The final component tests do not force CPU,
VDP, sprite, RAM or raster state; the test cartridge and the independent HDMI
oracle observe the public machine interfaces.

## Exact producer artifacts

Both clean recipes pass on device `5CSEBA6U23I7` from the selected component
commit. The package directories are under the component's ignored build tree.

| Lane | Package ID | Build ID | RBF SHA-256 | RBF bytes |
| --- | --- | --- | --- | ---: |
| OSS | `07f5649722492389571bb25c0431b5c57002d5cbe04b3412e399512a9cc089b2` | `90f677a2b493928030cdac54dcd87537` | `b2552a1577c2c7314d9634bd6ccd8a334931dc87a5c169361b52ab425179389c` | 2,586,750 |
| Quartus | `8973307e966ff0f36d64c32409252745e0eac3af6853d779942b8379b857b9e0` | `d0d19ab5401d36a2718a61d9cf4d403a` | `608c2d948ee6e216b1761b9626ac3ebab6fcd6af3c49cd82aa148d3735edf907` | 2,407,800 |

The OSS package manifest SHA-256 is
`c309c4a1526d98e9d22887deeedae78e38c3294282cba1cd9468400aef6ac8af`; the
Quartus manifest SHA-256 is
`b32c724e0b803b207d0f56f5ef28ed8a5c80789cdbce0cd7733f5b532af18ac3`.

The OSS route passes with no unrouted nets. It reaches 55.1633 MHz on
`clk_sys` against the requested 52 MHz and 98.6485 MHz on `pixel_clk` against
74.25 MHz. The final resource record uses 154/553 Mistral M10Ks, 3,255/83,820
MISTRAL_COMB cells, 866/167,640 FFs and zero forbidden DSP use. The exact
tool identities are Yosys
`da6373c0d7565f36036051efc7895fb0d9ac13c3`, nextpnr-mistral
`fd862a2c59db7f0406e32831f2e57b3cfe034251` and Mistral
`b28e30a36b5139aaed5a5d361a30b542e6b7c758`.

Quartus Prime Lite `17.0.2 Build 602` completes analysis, fitting, assembly and
TimeQuest with zero errors and 38 warnings. The worst positive slacks are
setup 3.476 ns, hold 0.146 ns, recovery 12.294 ns, removal 1.105 ns and
minimum pulse width 0.961 ns; each reported TNS is zero. Quartus reports
2,717 logic cells, 124 RAM segments, two PLLs and one DSP element. The
Quartus executable identity is SHA-256
`71035cc10a244002d1b07bef128c7cd429f34ccd69b417c0a80f47c639d8ab93`.
These reports do not constrain or accept every external board path.

## Compiler and RAM workarounds for the downstream agent

These are the concrete portability accommodations to preserve while the
Yosys/nextpnr/Mistral backend evolves. They are not new mailbox or wire
contracts.

| Pressure point | Current accommodation and reason |
| --- | --- |
| Sprite evaluator size | Do not restore the procedural 16x2 render loop. It synthesized to roughly 42K mapped combinational cells and did not produce a useful route. The accepted path advances one source pixel per system clock with registered column/repeat counters and a read/write phase pair. |
| Sprite line storage | Each alternating 256-entry line bank is one packed 4-bit M10K word: two pixel bits, occupied metadata and visible metadata. This reduces the measured mapped fabric to about 3.1K system-clock combinational cells while retaining priority, collision, transparency and fifth-sprite semantics. |
| M10K access schedule | Port A performs a registered read followed by a write on the next phase; port B supplies the registered raster read. The renderer consumes `q_a` one phase after the read, so it does not depend on a same-edge RAM read-during-write result. |
| Bank clear | Clear one 256-word line bank sequentially. Avoid an unrolled reset/start loop or bulk `initial` RAM initialization; those expand into `$meminit`/logic and threaten the Mistral memory budget. |
| Publication race | Keep the pending-Y/bank/status publication interlock. A new build must not clear the bank whose completed contents are still pending publication. The diagnostic lets one frame drain after setup so the reset-time empty-SAT evaluation cannot be mistaken for the configured line. |
| VDP read topology | Keep four coherent VRAM copies: one CPU copy and three raster/walker copies, with CPU writes broadcast. A single memory with multiple combinational raster reads fails Mistral mapping and expands badly in Quartus. |
| Quartus sprite RAM mode | Quartus 17.0.2 rejects `read_during_write_mode_port_a = "OLD_DATA"` for the packed bidirectional `altsyncram` shape. The wrapper uses `NEW_DATA_NO_NBE_READ`; this is safe because the registered renderer consumes `q_a` on the following phase and never uses a same-edge result. Do not “fix” this back to `OLD_DATA` without reproducing against the installed vendor compiler. |
| Route recipe | The reproducible final recipe is device `5CSEBA6U23I7`, nextpnr `--seed 5 --router router1`, no `--tmg-ripup`, with a 74.25 MHz pixel request. Seed 3 and seed 4 failed timing on the final packed netlist; timing-driven rip-up was slower/regressed the route. Placement is build-ID-sensitive, so keep the seed tied to the exact recipe/toolchain and revalidate any backend change. |
| Frontend | Use the Verilog TV80/T80pa path with `TV80_REFRESH=1`; do not reintroduce the VHDL CPU frontend into the OSS recipe. |
| Quartus vendor probes | Use Icarus for the unmodified Intel `altera_mf.v`: Verilator 5.051 rejects its `i_good_to_write_a2`/`i_good_to_write_b2` feedback constructs. The tested Intel model SHA-256 is `e7bc6f0200f8236986c4b255a4ce7937596946bdb646a93551057edc1e08ca69`. The model is neither patched nor copied into the repository. |
| Relocated tools | For the installed tool locations, set `IVERILOG` and `VVP` to `build/toolchain/icarus/install/usr/bin/{iverilog,vvp}`, `IVERILOG_BASE` to `build/toolchain/icarus/install/usr/lib/x86_64-linux-gnu/ivl`, and `QUARTUS_ROOTDIR` to `/home/deano/intelFPGA_lite/17.0/quartus`. |

The existing functional accommodations remain required as well: registered
media-read priming/delayed write/final flush, explicit reset-ROM HEX/MIF
formats, four-copy VDP reads, registered framebuffer inference, the accepted
HPS I²C placement and the supported clock/pin constraint subset. A backend
change should remove a listed workaround only with a new fit, timing, vendor
probe and full simulation result.

## Parent integration evidence

After selecting the component gitlink, the parent consistency check passed:

```text
consistency: package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match
```

The incremental parent command
`QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus make dev` also passed.
It rebuilt the four cores selected by the current native-development profile,
then produced the structurally verified diagnostic image
`out/native-integration-dev/development/linux.img` with SHA-256
`78b19823fb4b48d0c594fb20aa072c46138ca0d94ef773a693f3820fdf118ccd`.
Its development receipt is
`out/native-integration-dev/development/development.json`, SHA-256
`e75b81e8fe1f2f98f2cb30dc7a17f02fb7bdc7c2731928e2b251ae1f9ee1e4dc`.
Because the current profile still selects Mega Drive, Pong, SNES and NES, this
image is parent integration evidence only; it does not claim that Coleco is
already included in the native image.

## Hardware classification and next step

The exact package inspections pass for both final artifacts, but physical
sprite acceptance is blocked by the deployed target image rather than by an
observed compiler or lease failure. `core-inspect` reports the expected
package/build identity for both the OSS and Quartus packages. Exact `.fcore`
`core-load` requests for both lanes return `MISTER_UNAVAILABLE` from the
designated FogCast target-agent path, so neither package reached media delivery
or HDMI capture. An older Coleco package and an existing FES Pong format-2
package return the same error on this target.

A bounded raw-RBF diagnostic through the designated `scripts/kit.py` lease
held the lease but returned the documented `development probe timed out`
result for the custom OSS RBF; Stop and release returned the kit to idle/free.
The same lease/program/Stop path with the known-good native Pong RBF entered
`active`, reported `held`, then returned idle on Stop and free on release.
That is lifecycle/programming diagnostic evidence, not Coleco acceptance.

The target health endpoint is ready, but it currently reports runtime commit
`a729acc593ec772fa5ecd5f802e2dee9758bd4dc` and image SHA-256
`d1733d3fdcc97c4669f07576c3f449ba72f05e6ed77fbcf3b0a029830d98d760`; this
integration selects libmister-runtime `2629c6e1a896663b3e06688462624c3fac67ba67`.
No direct JTAG, Main FIFO or runtime-socket programming was used, and no
exact-artifact HDMI capture is claimed. Hardware classification is therefore:
known-good Pong physical lifecycle path passes; exact Coleco sprite hardware
acceptance is pending target image/runtime alignment and is not accepted.

The next integration step is:

1. Through the authorized appliance/target-image path, provision and verify
   the designated kit with a target image containing the selected parent
   runtime `2629c6e1a896663b3e06688462624c3fac67ba67` and its FES-GP support.
   Preserve the existing kit lease rules and verify the actual boot/image
   identity after reboot.
2. Through the designated FogCast `scripts/kit.py`/target-agent path, load each
   exact package above, run the open sprite cartridge, capture the settled HDMI
   frame, exercise Stop and confirm idle, then release the lease. Record the
   package/build IDs, target identity, lease evidence, capture hashes and any
   compiler-lane discrepancy here. A successful load or Stop alone is
   lifecycle diagnostic evidence; sprite/video acceptance requires the exact
   expected CPU-generated frame from both artifacts. Do not use direct JTAG,
   Main FIFO or runtime-socket programming.

The parent pin and this record do not select Coleco for the production image;
they hand the reviewed development package to the existing integration path.
