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

The current schema-7 profile includes `display` and `handoff.display_platform_ready`. Disabled display
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

## Probes and evidence

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
