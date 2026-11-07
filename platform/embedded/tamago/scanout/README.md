# Boot-owned scanout copy

This is the common CPU presenter used by QEMU RAMFB and the Nano's optional
firmware-supplied framebuffer. It copies private Go RGBA pixels to a separate
pinned XRGB8888 window: bytes B, G, R, 0xff in little-endian memory. Source and
destination strides may differ; padding remains untouched. Every operation is
bounded to four rows and yields between chunks on the cooperative runtime.
Calls serialize, so two CPU frames cannot interleave their writes.

The memory provider must copy synchronously into dedicated Normal noncacheable
memory outside Go's heap, then order the stores in `Publish`. A successful
`Present` means CPU copy/publication completed. It does **not** mean the FPGA
finished reading, a vblank occurred, the picture is tear-free, or an AXI/DMA
master drained. The fixed reader may scan during the copy. There is no release,
revoke or framebuffer-reuse API; its reservation lasts for the entire boot.

A write or publication failure poisons the surface, retains the complete window
and refuses subsequent accesses. Invalid frame arguments reject before writing
and do not poison an otherwise usable presenter. Tenfoot reports a presenter
error before finishing any outstanding model frame grant; model Close then
refuses to steal it. Tests exercise partial writes, publication failure and that
held-frame behavior. They do not establish a physical scanout drain.

## Nano firmware contract

The current schema-8 profile includes `display` and `handoff.display_platform_ready`. Disabled display
is `null` with readiness false. Enabling it requires exactly these fields:

- `base`, `size`: a dedicated, 2 MiB-aligned, **4 MiB** HPS DDR reservation,
  disjoint from Go RAM, all firmware reservations and Ethernet DMA. It must fit
  the mapper's existing L1 section. DE10's `0x30000000` window is rejected.
- `width: 1280`, `height: 720`, `stride: 5120`, `format: "xrgb8888"`.
- `lifetime: "boot"`: firmware keeps the FPGA image, reader address/geometry,
  DDR reachability and ownership fixed for the whole boot. Physical programming
  and reassignment must remain unavailable while it reads this reservation.
- `firmware_artifact_sha256`: the nonzero lowercase digest of the corresponding
  firmware artifact, recorded as **declared provenance**. This adapter cannot
  prove live FPGA identity or authenticate that declaration.

Firmware must already provide a compatible FPGA reader, HDMI timing/PHY setup,
initial valid pixels, CPU write permission and read reachability to exactly that
reservation. The native adapter maps it noncacheable, permits one open attempt,
and performs CPU copies. It programs no FPGA or display registers, advertises
no new FES GP capability, and never treats a CPU barrier as a physical drain.
Its DMA window stays pinned even after application exit or an uncertain error.
A wrong firmware/profile can still cause a bus fault or unsafe FPGA access;
profile validation cannot establish the board's actual electrical/AXI behavior.

The existing DE10 menu package is **not** admitted on Nano by this adapter:
its shared ABI fixes a DE10 DDR layout. No compatible Nano scanout bitstream,
boot image or HDMI hardware acceptance is supplied by this change. The optional
provider describes what matching boot firmware must hand over before the first
Nano UI can be exercised. Physical configuration/identity/containment and safe
core/session display transitions remain separate, unfinished work.

## Owned menu video prototype

The separate FPGA [`fes_embedded_menu_scanout`](../../../../sources/misteross/prototypes/embedded_control/fes_embedded_menu_scanout.sv)
now reuses the real FES menu video/reader to produce a 720p RGB/sync stream from
two XRGB slots. `python3 sources/misteross/prototypes/embedded_control/verify_menu.py`
checks actual Tenfoot frames through the owned endpoint and Nano 256-bit DDR
boundary, with full-frame acknowledgement, local quiescence and failure paths.
It requires committed inputs; `--allow-dirty` labels working-tree simulations.

This source needs a separately admitted **8 MiB** double-buffer arena, a native
control binding and board pixel-clock/HDMI integration. The existing boot copier
and 4 MiB CRC-window firmware do not provide those contracts. Local Stopped is
not firmware master drain or permission to release the arena. No current Nano
boot or core package selects this prototype; physical display remains open.

## Probes and evidence

`NewWindowReader` controls the separate CPU-started window diagnostic through
platform-supplied relative register I/O. The platform has already admitted and
enabled its exact reader generation and keeps the entire reservation pinned.
Opening checks the live build/ABI, generation and compiled geometry. Each
`ReadFrame(ctx, expectedCRC)` issues START once with that generation and a fresh
cookie. It reads a latched byte-count/IEEE CRC32 result, then checks ownership
again before returning it. The caller keeps its published bytes unchanged for
the operation; the expected CRC covers visible row bytes in memory order.

Operations use at most 64 polls and a caller-selected deadline up to one second,
with bounded monotonic-clock and yield hooks. Cancellation after submission,
bus/protocol/CRC failure, lost ownership and uncertain command completion retire
the instance. Later calls perform no I/O. The platform retains the reservation
and performs the separate endpoint and all-master drain. A `FrameRead` observes
delivery into the FPGA diagnostic CRC sink; it grants no buffer reuse, vblank
or HDMI authority. `Surface.Present` keeps its existing CPU-copy semantics.

`Drain(ctx, cookie)` issues DISABLE once and waits for that exact 64-bit token,
reader generation and zero pending traffic. It uses the same time/poll bounds
and retires the instance on success or uncertainty. Drain cookies must increase
across generations. The platform may enable a newer generation under its own
exclusive ownership after publishing new pixels. The returned `ReaderDrain`
covers only this endpoint; all other masters, bridge closure and the pinned
allocation remain the platform's responsibility. The native
[`WindowSession`](../nativefabric/README.md) combines these operations for
controlled updates within one admitted arena.

Failed open/frame operations can return `ReaderFailure`, retaining their stage,
attempted read/write counts, START attempt and last complete latched sample.
`errors.Is` still identifies the cause. The ordinary result stays empty on every
failure. Inspecting this cached diagnostic performs no device or clock access;
its counters and CRC grant no completion, closure or memory reuse authority.

The Nano source producer exposes this mode as `hps-ddr-window`, with a distinct
diagnostic profile and a fixed 4 MiB bus window. The ordinary fixed-reader boot
constructor continues to reject it. Explicit `fpgaload --window-memory` binds
the complete reservation to the native entry and cold firmware, publishes one
actual tenfoot frame, and keeps FPGA data access held. Its separate stop contract
uses the window maintenance register and both master-drain helpers. The
[matched single-frame physical result](../../../../docs/validation/2026-11-04-de25-fullwidth-window/README.md)
qualifies its recorded DDR read/drain/closure. Repeated native presentation,
HDMI and ordinary Nano display admission remain open.

The [owned window-source model](../../../../sources/misteross/docs/architecture.md#embedded-control-endpoint)
now passes complete 1280×720 tenfoot frames through this CPU copy adapter and
the actual FPGA read/drain endpoint. Run
`python3 sources/misteross/prototypes/embedded_control/verify_window.py` from
the FES root with committed inputs, or add `--allow-dirty` for a labelled
working-tree model. It checks packed and padded/page-edge windows, every pixel,
frame boundaries, backpressure, failures and generation-bound restart after
real response drain. It produces model receipts and rendered frame files under
`out/`. The same check also runs this native driver against the actual board
Avalon wrapper, with two complete rendered frames, coherent snapshots, CRC
corruption, stale/busy requests, uncertain writes, cancellation and ownership
loss. Both the model and Go tests run through the existing producer integration
lane. These remain host results; physical DDR/HDMI and Nano display binding are
unfinished.

`run.py display --board qemu` boots a display-only native program, captures its
actual RAMFB color bars, sends serial `n`, and captures inverted bars. Every
RGB pixel in both QMP captures must match the expected pattern. There is no
storage, networking, USB or FPGA programming dependency in this probe.
The same `cmd/display` cross-builds for Nano and uses its profile-owned window;
serial `n` advances its second pattern when hardware is available.

Host tests cover channel order, nonzero-origin subimages, padded strides,
window bounds, bounded chunks, source independence, concurrent calls, invalid
geometry and retained failure ownership. QEMU proves the shared copy path and
its own fw_cfg/RAMFB configuration only. Neither test proves a Nano image is
loaded, has access to HPS DDR, generates HDMI or supports vblank/drain/replacement.
