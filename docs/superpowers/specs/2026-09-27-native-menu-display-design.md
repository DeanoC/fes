# Native HDMI menu display design

Status: proposed; hardware and implementation are not approved by this document.
Base: FES 221d0251. The controller fix b4c39848 is a separate branch.

## Intent and scope

Provide the kit's HDMI output for the existing tenfoot library and guided core
setup. Preserve the mesh direction: library sources and executors are separate
identities, the kit can eventually host, and no display contract assumes one
fixed remote host. Cached browsing does not authorize offline installation or
launching. Physical display transport must not own library decisions.

This spec covers a minimal full-color pixel transport and menu/game handoff.
It does not add graphics acceleration, game overlays, new core features, or
redesign mesh discovery. Initial work proves display feasibility; later UI and
image integration depend on that proof.

## Existing evidence and gaps

The splash drives fixed 1280x720p60 HDMI but has no framebuffer interface.
The FC2D graphics backend is a software recording stub, not FPGA hardware.
Experiment 911_hps_ddr proves a halfword transfer through FPGA-to-HPS SDRAM
port 2 (64-bit data on port 3), including a dated physical kit result recorded
in misteross/docs/oss-experiments.md. It does not prove scanout bandwidth,
Linux-owned buffer allocation, coherency, or safe buffer destruction. Its
hard-coded scratch-address probe is not a production memory allocation method.

The image kernel is 5.15.1-MiSTer. There is no selected DMA buffer allocator
for this path. Runtime programming already contains the SDR ports and bridges;
a menu needs an explicit activation path, not an implicit change to contained
raw-RBF loading.

## Approaches

Recommended: HPS DDR pixel buffers. Reuse the software renderer and transfer
only buffer addresses/control through GP. A bounded kernel allocation path and
streaming reader are new work, but color and artwork do not require a second UI.

Alternative: GP-uploaded on-chip pixel buffers. Avoids shared Linux memory,
but full frames require many acknowledged 16-bit exchanges and consume scarce
FPGA RAM. No measured transfer rate currently establishes menu responsiveness.

Alternative: tile/text FPGA renderer. Reduces bandwidth, but introduces a
second drawing vocabulary, font/image limits, and substantial UI coupling.

## Proposed component ownership

- mister-packages defines the menu display identity, pixel layout, GP controls,
  capability limits, completion semantics and generated constants.
- misteross builds a separate menu display firmware: fixed video timing,
  DDR read engine, line FIFO, frame-boundary buffer switching and GP controls.
- libmister-runtime owns the Linux DMA allocation driver and userspace adapter,
  firmware admission, display generation, presentation and physical teardown.
- FogCast supplies pixels and retains browsing state. A kit shell uses shared
  UI/rendering services; it does not grant the agent or UI MMIO access.
- FES selects artifacts and builds the kernel module/DT integration and image.

Do not add an import from kitlauncher to tenfoot to sidestep existing package
boundaries. Resolve reuse through a common rendering/application service or a
separate tenfoot kit executable. Choose that wiring in the subsequent UI plan,
after display feasibility is established.

## Pixels and transport

Initial scanout target: 1280x720 at the existing 720p60 timing. Two buffers,
each 1280x720x4 = 3,686,400 bytes, total 7,372,800 bytes excluding allocation
alignment. Pixel memory is little-endian XRGB8888: B,G,R,unused bytes. Convert
from the software renderer's RGBA bytes at the presenter boundary.

Active pixel reads alone require 221,184,000 bytes/sec at 60Hz. This is a
calculated requirement, not demonstrated throughput. DDR bursts and FIFO
prefetch must tolerate contention and blanking without stretching HDMI timing.
An underflow outputs black for the affected pixels and increments a readable
counter. Continuous underflow is a failed feasibility result.

The kernel driver allocates two contiguous DMA-coherent buffers with a 32-bit
DMA mask and exposes only those allocations to the runtime. No /dev/mem
framebuffer, guessed physical address, or userspace virtual address is used.
The proof must establish the DMA-to-f2sdram address interpretation on this SoC.
Mapping and write ordering use the kernel DMA API; GP submission follows a
completed pixel copy and the required memory ordering barrier.

Runtime controls buffer descriptors. The UI does not submit physical addresses.
A bounded local presentation operation carries exact-size pixels through a
runtime-owned shared-memory staging descriptor passed over the existing Unix
socket with SCM_RIGHTS. The runtime copies a complete staged frame to the
available DMA buffer before submission. JSON carries control metadata only.
Validate descriptor size, generation and byte count before copying. Allow only
one pending submission; stale or busy submissions return an explicit result.

GP controls identify firmware/build, configure the two buffers while disabled,
enable scanout, request a buffer switch with a sequence, report the displayed
sequence and underflow count, and quiesce. Exact opcodes and generated layouts
must be assigned without collision in the shared contract change. Descriptor
updates are unavailable during enabled scanout. The FPGA reads only the two
configured ranges and bounded bursts; no arbitrary memory read/write command
is exposed by the production firmware.

Switch buffers only at a frame boundary. A displayed-sequence acknowledgment
means the previous buffer is no longer read and can be reused. Quiesce stops
new requests, drains outstanding reads and then acknowledges. Runtime retains
allocations until quiesce or verified physical bridge containment; close of a
UI connection must never free memory still reachable by the FPGA.

## Lifecycle

Menu activation is a runtime-owned idle firmware operation with exact artifact
validation and live identity checking. It is not a launchable game package and
must not enter the library. GP identity is verified while DDR stays contained;
buffers are configured before enabling the required SDR port and scanout.
Only the menu generation can present. Gameplay and contained diagnostic loads
reject menu submissions.

Before a game load, revoke presentation, quiesce reads, contain bridges, then
reuse the existing serialized programming path. Late frames cannot reach the
new core. Stop restores configured menu firmware, establishes a new display
generation and requests a fresh complete frame; FogCast retains selection and
search independently. Do not reuse an old DMA address or generation.

Allocation, identity or scanout failure keeps the existing bounded runtime
failure/recovery behavior. Retain the boot splash as fallback and boot artifact.
Do not create another recovery coordinator or silently mark a failed menu as
working. If bus quiescence is ambiguous, retain allocations until containment
is verified; inability to establish containment follows existing reboot-required
behavior. Kernel removal/unbind is refused while DMA remains active.

## Staged delivery and validation

1. DMA allocation/addressing and bounded DDR-reader diagnostic. Verify read
   bursts, backpressure, range bounds, command drain, alternating buffers and
   coherent ARM writes. Extend models beyond experiment 911's single beat.
2. Fixed HDMI diagnostic scanout with color bars, sequence markers and readable
   underflow count. Seal through the current OSS lane on GPU 0. Quartus may
   check timing as an oracle; report toolchain defects separately.
3. Runtime identity, local presentation and lifecycle integration. Exercise
   malformed frames, stale generations, pending submission, client disconnect,
   game replacement, Stop and bounded failure recovery.
4. Shared UI kit shell and mesh-aware source/executor selection using existing
   services. Reuse guided setup and avoid implementing a second core catalog.
5. FES artifact selection and image/media integration only after diagnostics
   succeed. Kernel/DT changes require provisioned media and boot identity checks.

Physical diagnostics require the designated kit lease. Show tearing-free
alternating patterns, no underflows during a ten-minute normal-load test,
controller navigation, and repeat menu/game/Stop cycles. Measure presentation
latency and CPU/memory load before setting supported responsiveness claims.
No simulation or earlier image receipt counts as exact-artifact acceptance.

If DDR addressing, kernel allocation, routing or sustained scanout fails, stop
at that proof and report evidence. Revisit resolution/transport explicitly;
do not silently replace the design or claim menu integration is complete.
