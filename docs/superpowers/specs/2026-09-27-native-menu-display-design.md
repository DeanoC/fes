# Native HDMI menu display design

Status: design approved by the user on 2026-09-27; depends on the in-progress
HPS DDR implementation and its qualification. Implementation plan review and
exact-artifact hardware acceptance remain separate.
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

The active `kepler/altera-h2p-ddr-ram-support` worktree defines
`fes.memory.hps-ddr` 1.0, shared `fes_hps_ddr`/guard RTL and a reserved core
window at `0x30000000–0x3fffffff`. Its runtime companion is
`kepler/hps-ddr-runtime`. These are dependencies, not menu-owned files to
reimplement. Current branch evidence includes b6e2d9f0 and 0eb54627; changes
and kit testing remain in progress, so no acceptance is inferred.

The boot splash must carry the shared port layout before U-Boot applies the
SDR configuration. Runtime layout mirrors alone cannot establish the latched
boot configuration. The window avoids the kernel framebuffer at 0x22000000.
Use the dependency's final shared constants, boot requirements and admission
rules when it lands; never change the live SDR static configuration from Linux.

## Approaches

Recommended: HPS DDR pixel buffers. Reuse the software renderer and transfer
only buffer addresses/control through GP. A bounded reserved-window presenter and
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
- libmister-runtime owns the reserved-window mapping and userspace adapter,
  firmware admission, display generation, presentation and physical teardown.
- FogCast supplies pixels and retains browsing state. A kit shell uses shared
  UI/rendering services; it does not grant the agent or UI MMIO access.
- FES selects artifacts and assembles the image, including the DDR dependency
  boot splash. No independent menu kernel allocator is proposed.

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

Reserve two 4 MiB slots at offsets 0 and 0x00400000 within the dependency's
core-owned DDR window while menu firmware is active. Each scanout reads only
3,686,400 bytes of its slot. Derive physical addresses from shared window
constants, validate all arithmetic/ranges and use the shared port's word
address convention. Other cores may reuse the window after menu teardown;
these are not persistent allocations or concurrently owned game buffers.

Runtime alone maps this reserved region using a noncached Linux mapping with
verified ARM write ordering. Do not allocate ordinary Linux pages and hand
their addresses to the FPGA. Confirm the final dependency's reservation and
boot requirements before mapping; absence or disagreement rejects activation.
ARM pixel writes must be visible before GP submission; qualify this with
alternating frame patterns and address/stride markers on the kit. Never use
experiment 911's old scratch addresses or the kernel's framebuffer region.

Runtime controls buffer descriptors. The UI does not submit physical addresses.
A bounded local presentation operation carries exact-size pixels through a
runtime-owned shared-memory staging descriptor passed over the existing Unix
socket with SCM_RIGHTS. The runtime copies a complete staged frame to the
available reserved DDR buffer before submission. JSON carries control metadata only.
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
new requests, drains outstanding reads and then acknowledges. Runtime prevents reuse of the reserved region until quiesce or verified
physical bridge containment; a UI disconnect must never permit writes racing
with scanout.

## Lifecycle

Menu activation is a runtime-owned idle firmware operation with exact artifact
validation and live identity checking. Use the dependency's described
`fes.application` / `fes.memory.hps-ddr` admission path and shared guards; the
menu is not published as a playable library entry. GP identity/layout checks
precede port release, and buffer configuration precedes scanout enablement.
Do not add an alternate bridge-release path for menu firmware.
Only the menu generation can present. Gameplay and contained diagnostic loads
reject menu submissions.

Before a game load, revoke presentation, quiesce reads, contain bridges, then
reuse the existing serialized programming path. Late frames cannot reach the
new core. Stop restores configured menu firmware, establishes a new display
generation and requests a fresh complete frame; FogCast retains selection and
search independently. Do not reuse an old DMA address or generation.

Mapping, identity or scanout failure keeps the existing bounded runtime
failure/recovery behavior. Retain the boot splash as fallback and boot artifact.
Do not create another recovery coordinator or silently mark a failed menu as
working. If bus quiescence is ambiguous, prevent region reuse until containment
is verified; inability to establish containment follows existing reboot-required
behavior. No menu process owns kernel allocations or independent bridge controls.

## Staged delivery and validation

1. Adopt the merged and qualified DDR dependency. The other agent owns RAM
   support/testing, shared guards and bridge admission. Menu work adds bounded
   read-only scanout simulation using that interface: burst backpressure,
   range bounds, drain, alternating buffers and ordered ARM writes. Do not
   duplicate its RAM test or alter its in-progress worktree.
2. Fixed HDMI diagnostic scanout with color bars, sequence markers and readable
   underflow count. Seal through the current OSS lane on GPU 0. Quartus may
   check timing as an oracle; report toolchain defects separately.
3. Runtime identity, local presentation and lifecycle integration. Exercise
   malformed frames, stale generations, pending submission, client disconnect,
   game replacement, Stop and bounded failure recovery.
4. Shared UI kit shell and mesh-aware source/executor selection using existing
   services. Reuse guided setup and avoid implementing a second core catalog.
5. FES artifact selection and image/media integration only after diagnostics
   succeed. The dependency's boot port-layout requirements require provisioned media
   and boot identity checks where the existing card lacks that layout.

Physical diagnostics require the designated kit lease. Show tearing-free
alternating patterns, no underflows during a ten-minute normal-load test,
controller navigation, and repeat menu/game/Stop cycles. Measure presentation
latency and CPU/memory load before setting supported responsiveness claims.
No simulation or earlier image receipt counts as exact-artifact acceptance.

If dependency qualification, reserved-window mapping, routing or sustained scanout fails, stop
at that proof and report evidence. Revisit resolution/transport explicitly;
do not silently replace the design or claim menu integration is complete.
