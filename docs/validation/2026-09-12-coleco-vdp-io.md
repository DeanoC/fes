# Coleco buffered VDP reads and VBlank NMI

Scope: the next bounded slice after [controller validation](2026-09-12-coleco-controllers.md).
It adds CPU read-ahead, held-read/status handling and VBlank NMI to the existing
Graphics I machine. Sprites, full palette/modes, audio, proprietary BIOS services
and retail-game compatibility remain outside this change.

## Source and behavior

FES base: `7ab9481f9ee4636e186425992d91be2f0d7e08bf`.
misteross base: `3d81c99eaf42249d782f0f25f8c946327800bd7a`.
Result: `292f57ed61576203123cc749a391f49d348f22eb`, branch `feat/fes-coleco`.
Work remains isolated in `out/dev/fes-coleco/misteross` and
`out/dev/fes-coleco/fes-integration`; root integration inputs and unrelated
work are preserved. FogCast, runtime, mister-packages and wire contracts do
not change.

Read-address setup prefetches the first VRAM byte. Each data read returns the
buffer and requests the next byte, wrapping the 14-bit address. Write-address
setup does not prefetch; data writes also update the buffer. Each held CPU IN
has one side effect and keeps its original return byte until RD/IORQ releases.
Status reads acknowledge VBlank/collision and abandon a half control command;
a simultaneous new frame event wins over acknowledgement and survives a held
read. The read buffer is collected two system edges after launch to accommodate
the existing registered-address RAM paths, not original DRAM access slots.

Pending VBlank gated by register 1 bit 5 drives the Z80's active-low NMI.
Enabling with a pending frame asserts immediately; disabling releases the line
without discarding the flag. Acknowledgement permits another frame interrupt.
The user-approved open shim adds `JP 8066` at `0066` in both HEX and MIF.
Enabling cartridges provide a handler at 8066, initialize a RAM stack,
acknowledge status and return with RETN. This is an open diagnostic cartridge
convention, not a Coleco BIOS implementation.

The behavioral reference is the TI TMS9918A data manual, sections 2.1.3–2.1.6,
and MiSTer ColecoVision revision `5e8713cbc91b7d7abe4806cb87834b16d7348011`
(`rtl/vdp18/vdp18_cpuio.vhd`, `rtl/cv_console.vhd`). No BIOS or commercial
cartridge bytes are copied. The controller ABI, RAM wrappers, raster copies,
clock constraints and compiler tools are unchanged.

## Open diagnostic and focused regressions

`cores/fes-coleco/diagnostic/vdp_io.py` is an original MIT-licensed Z80 emitter.
Its handler saves AF, reads status twice, records errors/count, restores AF and
returns with RETN. The main program verifies sequential/prefetched/wrapped data,
status clearing, interrupt-disabled behavior, enable-with-pending delivery and
two acknowledged NMIs. Only after success does it paint a green one-tile border
with a black interior and HALT. Failure paints an orange interior; timeout is
never accepted. Every final raster lookup location is CPU-initialized.

| Artifact | Bytes | SHA-256 |
| --- | --- | --- |
| `vdp-io.rom` | 3701 | `bb0a2b4a1e47e0bc480195adf28bd4fc637bdb13aa8aea2719e389b1623529c3` |
| `vdp-io-16k.rom` | 16384 | `135dfc75d5b7f06b9e9f9ca70ff784804dc2fc56ba7f39a934f38413624de87f` |

The original graphics and controller diagnostic bytes are unchanged. The new
preview is a 1280×720 PPM, with 27,648 green and 893,952 black pixels, including
the established framebuffer read latency. It is a comparison artifact, never
input to the simulated CPU or raster.

Recorded red/green evidence:

- The original VDP unit failed `read setup did not prefetch first byte`.
  Current default/OSS units pass prefetch, sequential/wrapped reads, held data
  and status reads, write-mode no-prefetch, shared write/read buffer and
  partial-control cancellation. Interrupt checks cover enable/disable with
  pending status, acknowledge, next frame, reset and simultaneous VBlank/read.
- A fresh Verilator build of exact old `3d81c99` RTL running the new CPU
  diagnostic returns E1, NMI count zero: the read checks reject the old design.
  With the read fix but before NMI wiring, the diagnostic reaches E5 with zero
  NMIs: read/status checks pass but interrupt delivery fails.
- Both current CPU lanes pass compact and full-size cartridges, each through
  a reset-only rerun: result A5, NMI count 2, status 80/00, handler error zero,
  VRAM samples 19/A6/73/42/BD and the expected logical pass picture. Tests use
  public machine inputs/outputs; they never force CPU, NMI, RAM, VRAM or raster state.
- Review tightened the pending-delay loop from 8192 to 6144 iterations so the
  bounded first-NMI window ends before the next natural VBlank. Both CPU lanes
  pass the revised ROM. This closes a diagnostic false-pass opportunity, not
  a functional RTL or compiler failure.
- The focused Python suite passes 20 tests, including output/padding and
  preview checks. Unmodified Quartus vendor RAM/media probes pass immediate
  release at 1/3/989/16384/989 bytes with unchanged model digest
  `e7bc6f0200f8236986c4b255a4ce7937596946bdb646a93551057edc1e08ca69`.

Independent review finds no blocking defect. The final
`OBJCACHE= make sim-fes-coleco VERILATOR=build/toolchain/install/bin/verilator`
passes both complete lanes: 98 exact HDMI frames each (the retained 95 plus
three new VDP pass frames), alongside the dedicated CPU tests. The board uses
GP HOLD/BEGIN/DATA/COMMIT/immediate RELEASE for compact/full-size/compact loads,
checks CPU pass/NMI results and CPU initialization of all final raster lookup
locations, then compares every RGB/DE/HS/VS sample. The full log is
`out/dev/fes-coleco/evidence/vdp-simulation.log`; red logs are retained alongside.

The reviewed component is committed only after these checks and selected in
the integration worktree. Parent `make check` passes with that staged gitlink:
14 generated consumers, 11 fixture copies and four source pins match.
The 20 focused Python tests also run against the clean integration selection.
No other component pin or shared contract changes.

## Fresh FPGA builds

Both recipes complete successfully from the clean selected `292f57e` commit.

| Identity | OSS | Quartus |
| --- | --- | --- |
| Package SHA-256 | `10b23b503c3b5bba2555e66db68614aec44a9c0e6329fc36cde058d8c824b609` | `c9345568dda2098cd90568b2f57d277a2b3f877384df498718017fd06cc34232` |
| Build ID | `564c318605cc27cafd58ae997d631459` | `6351d5c1966b32eba3d20f00de76cd37` |
| RBF SHA-256 | `5f888bf9db0cc3b5b7c396ba5329a4d96db499f9c1f036e5373a3bdcaf666e31` | `6ab5614e9af9755bfc8280e09b3696d3128d085b1b8e23845edd06e9fa298e65` |
| RBF bytes | 2480074 | 2317260 |

OSS reports system 57.600 MHz against 52 MHz and pixel 91.466 MHz against
74.25 MHz, using 134 M10Ks, 2762 MISTRAL_COMBs and 702 FFs. Quartus 17.0.2
reports setup/hold slack +2.492/+0.164 ns and zero TNS across its reported
timing categories. Quartus retains the existing not-fully-constrained notices
and 34 compilation warnings; this is not a claim that every board path is
constrained. Existing clock constraints are unchanged.

Compiler revisions remain Yosys `da6373c0d7565f36036051efc7895fb0d9ac13c3`,
nextpnr `fd862a2c59db7f0406e32831f2e57b3cfe034251` and Mistral
`b28e30a36b5139aaed5a5d361a30b542e6b7c758`. Build logs and full summaries are
retained as `evidence/vdp-{oss,quartus}-build.log` and
`evidence/vdp-{oss,quartus}-build-summary.json`.

## Hardware setup

The existing leased updater confirms diagnostic appliance
`77dccfd1f6ff2ea013ff186e31d6030bd5cdd566d1bc22985d05652fa3a2dd3e`
on boot `f34a7446-52f3-455c-8a3a-dea2aeeb63e9`, trial false, raw idle ready.
This reuses the existing dynamic-runtime diagnostic derivative, not a new cold
reproducible appliance. FogCast remains
`c761cff0d9e7878d90eb3dee24ba96010acb46de`, runtime
`2629c6e1a896663b3e06688462624c3fac67ba67`. The temporary headless host uses
the prior private input-enabled configuration copy; original configuration and
normal host are preserved. The updater and host use the target agent's existing
lease and runtime lifecycle path. There is no direct GP/runtime-socket/JTAG
programming or block-device operation.

`evidence/run_vdp_hil.py` loads exact package IDs, uploads the open ROM, captures
uncompressed YUYV HDMI and compares the result with the pass PPM. It checks
compact/full-size/compact reloads within one package generation and requires
Stop to return idle. The stable-color checker excludes two pixels around
expected horizontal transitions for YUYV chroma, without alignment or scaling;
this is color/geometry diagnostic evidence, not bit-exact RGB or gameplay.

## Exact-artifact hardware results

The previous OSS package
`9cae548470224b343eb390358e063c74a7ac73da1f8167d1c9bf46f37f1a1c4c`
is a negative control: the new compact ROM produces the expected orange
failure interior (168,960 orange pixels), not a timeout or pass frame.
Its PNG SHA-256 is
`52b8a73c80af8f857b39d6e6efd516492f2e28e99a08abd8b69cebab23695799`.

Both fresh packages above pass compact/full-size/compact-reload tests. All six
captures have 100% stable whole-frame and logical-region color agreement.
All three corresponding PNG pairs are byte-identical across compiler lanes;
each SHA-256 is
`b9a820eaad8c6fa4f22c4781e4204eed54a9f744c70f39c0b4a22c76d063564a`.
The pass frame is produced by the CPU only after its VRAM read/status/NMI
checks succeed; it is not a host-supplied image. Every session returns idle
through Stop. Evidence is in `evidence/vdp-{baseline-oss,quartus,oss}/` and
the corresponding `vdp-*-hil.log` files.

This is exact-artifact hardware diagnostic acceptance on the named diagnostic
appliance, not a new cold reproducible image release or full Coleco gameplay
acceptance. Reset-only cartridge reruns and simultaneous status/frame races
are covered by simulation, not claimed as separate hardware operations.

## Compiler handoff

The new read-ahead schedule accommodates the already documented registered RAM
latency. No new Yosys, nextpnr or Mistral patch, option or constraint is required
by this RTL implementation. Held-read snapshots, one-time address advancement,
status acknowledgement, NMI gating and the open vector are functional emulation
changes, not toolchain defects. Existing memory inference, replicated raster
reads, initialization and I²C/constraint accommodations remain in the core
guide's workaround table.

## Restoration and integration handoff

The leased rollback confirms original image
`d1733d3fdcc97c4669f07576c3f449ba72f05e6ed77fbcf3b0a029830d98d760`
on boot `b32f6a47-3f93-4b12-b8a1-02c3d289c9cc`, raw idle ready, trial false,
pending empty and corrupt false. The kit lease is free; the normal host is
ready on that same boot with original runtime
`a729acc593ec772fa5ecd5f802e2dee9758bd4dc`. Only the temporary test host is
terminated. Configuration, normal host, diagnostic/recovery images, factory,
kernel and bootstrap are preserved. Evidence is `vdp-rollback.log`,
`vdp-final-lease.json` and `vdp-restored-host.json` under the task evidence directory.

Final parent `make check` passes. The integration branch selects only
misteross `292f57e` and adds this record/index link; no other component pin or
shared contract changes. No push or main-branch merge is performed. The
component base/result above, exact artifacts and bounded acceptance are the
handoff for this completed VDP read/status/NMI slice.

The next implementation slice is sprite rendering and its status behavior,
with original CPU-driven diagnostics and paired simulation/build/hardware
checks. It is not included in this result; full palette, remaining VDP modes,
audio and broader cartridge compatibility remain subsequent work.
