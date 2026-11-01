# DE25 allocated-buffer scanout software evidence — 2026-10-28

The pinned AArch64 test kernel built from FES
`44de76eea08b679da18cbb7e6ac3c03c40b96c54` booted under QEMU and passed all
16 `fes-de25-dma` KUnit cases, with zero failed or skipped cases. Both the
raw kernel log and parsed run log contain no warning, oops, panic or KASAN
markers. This is kernel software evidence using a real KUnit DMA device and
scripted registers. DE25 production binding still returns `EOPNOTSUPP`.

[The audited receipt](kernel-evidence.json) retains the original producer
receipt, artifact/source hashes, case results and independent checks. The
retained local output is `out/de25-kunit-44de76eea0/`. The driver, generated
kernel contract and runner at FPGA source commit
`cd9e7cf8960f69463424b4918e948aadc4f763d7` match the tested kernel producer's
bytes. The kernel receipt does not establish FPGA compilation or timing.

## Ownership contract

[DMA ABI version 2](../../../image/kernel/de25-dma/README.md) exposes geometry,
a full 64-bit allocation generation, increasing 32-bit frame sequences and
slot state. It accepts no CPU physical or DMA address from userspace. The
kernel allocates and clears one coherent 8 MiB region, constructs all register
writes from its DMA handle, orders publication, and maps only the proven eager
`VM_IO | VM_PFNMAP` form. Production contains no physical MMIO accessor.

The allocation's exposure reference and module pin span the engine lifetime.
They remain held across repeated frame submissions and full-frame completions.
One frame may be pending. Acceptance and selection for fetch do not release
the previous front slot. An ordered frozen snapshot must match the complete
generation/grant and the pending accepted/current/displayed sequence and slot,
with Pending clear and no frame fault, before the previous slot becomes the
back slot. An old completion or contradictory tuple cannot release it.

A client must draw only into the reported back slot and finish writing before
PUBLISH. The single writable mapping covers both slots; the interface does not
enforce per-slot PTE write protection. Version 1 requests have no compatibility
path. Generation and sequence exhaustion cannot wrap into a reused operation.

REVOKE denies new publications/mappings, synchronizes past in-flight mmap
linkage, and removes client PTEs before issuing STOP. Retirement requires the
old full generation, immutable grant and original external epoch, actual
Drained state and zero outstanding traffic. Only after that proof may CLEAR
invalidate the old grant and prepare a new lease. Frame completion, timeout,
client close, an unreadable epoch, or reset's empty generation cannot stand in
for drain. Failed/uncertain operations retain storage and module/device/inode
references. An unknown configured engine is refused, never cleared by a new
constructor to claim ownership.

The shared [scanout register contract](../../../sources/mister-packages/docs/de25-scanout.md)
and private kernel accessors require bounded errors if the pixel-clock register
endpoint becomes unavailable. Hardware accessors, translation and firewall
admission remain absent.

## What was checked

The original eight cases retain actual coherent allocation/file operations,
UAPI copying, strict VMA/GUP behavior, post-revoke faults, revoke-before-stop
ordering, failed-start/drain retention, unreadable epoch behavior and a
controlled mmap-callback/VMA-link race. Eight added cases exercise:

- Acceptance versus full-frame completion and repeated alternating slots.
- Ordered kernel-owned descriptor writes before the first SUBMIT.
- Stale high generation bits and reset's inability to prove old drain.
- Contradictory current/displayed tuples and frame errors retaining ownership.
- Bounded snapshot serial handshakes, serial wrap and unstable snapshot refusal.
- Nonwrapping generation/sequence counters and POLL identity rejection before effects.

The descriptor/lifetime case also checks that a second constructor refuses a
configured engine with `EBUSY` without issuing CLEAR. The actual mapped-UAPI
case exercises INFO, PUBLISH and bidirectional POLL, rather than only calling a
separate state model.

The evidence audit rehashed all nine files named by the producer receipt,
checked all ten selected driver/config/generated files and the runner against
the exact Git commit and current selected bytes, verified both object ELF
headers/compiler comments, and checked the SDK/vendor Image hashes against the
lock. Nine relevant extracted Linux files match the pinned Git objects; this
was a bounded source-path audit, not an independent rehash of the complete
Linux extraction tree. No tests were rerun for this evidence audit.

The pinned kernel is Terasic Linux
`d7d192a9ddd955fe206aa350afed8f82e368a74c` (6.12.11), compiled using Buildroot
GCC 9.4.0. The builder is
`sha256:ea200655b92ab1211bc8b326c6e799815f1d648b2d5e5a6c982f597cb2b9650d`;
the exact SDK hash and QEMU version are in the JSON record. The separate QEMU
virt test config enables the driver/KUnit and 32 MiB CMA. It is not the shipping
BSP kernel configuration or evidence of module loadability on that kernel.

## Preserved failed candidate

`e719af5eff41e85fd32c036a4a878fda28085ff6` failed during kernel compilation,
before QEMU or any KUnit cases ran. Private identifiers `current` and `barrier`
collided with kernel macros. Its original log remains at
`out/de25-kunit-e719af5eff/kunit-run.log`; its hash is retained in the JSON.
The successful `44de76eea0` commit renamed them to `current_sequence` and
`io_fence`, without warning suppression. The failed candidate has no passing
receipt and was not relabelled as successful.

## Affected software coverage

[The software record](software-checks.json) covers all 20 planned commands.
The local `test_changed.py` run completed its first 15 commands through the
full C64 simulation. Parent tests ran 1,130 cases: 1,126 passed and four were
skipped. FPGA producer tests ran 1,428 cases: 1,393 passed and 35 were skipped.
Host/UI, runtime, contracts and native video/socket/session checks also passed.

The remaining Coleco, MENU, PC386, Spectrum and ZX81 scenarios passed in
[CI run 40754921557](https://github.com/DeanoC/fes/actions/runs/40754921557).
The checked-out merge commit `39c44d8193006883d6af00b6f05f37377d1dd7f2`
has exactly the Git tree of branch commit `c23d59ac94`; later changes are
documentation only. The local duplicate tail was stopped after this identity
and the successful jobs were verified. Its partial log is retained and is
never labelled a complete local runner pass. CI's own C64 job was still running
at observation, so aggregate CI success is not claimed; local C64 passed.

The original local plan selected `cd9e7cf896` with four build jobs. Ten audited
software groups have identical Git objects through board producer `6da029aa49`,
and all eleven selected kernel/runner files match the actual `44de76eea0`
kernel producer. Later board and transport-helper edits have their separate
focused, generated-simulation and Quartus evidence. The raw local log, exact
command plan, CI job identities and limited source comparison are retained in
the JSON. `make check` passes with 28 generated consumers and 37 fixture copies.

The affected runner excludes core P&R, cold image/platform packaging, required
browser integration and hardware acceptance. The separate DE25 diagnostic
Quartus result does not remove those limits or admit production binding.

## Remaining integration gates

The direct endpoint bench separately passed 12 native Verilator groups,
including four full pixel-checked frames, delayed/same-cycle first responses,
burst bounds, stalled STOP/drain, response errors, underflow and a recovered
clock-fault pulse remaining sticky. This checks the selected endpoint/reader/
video source; it is separate from generated interconnect, Quartus and physical
evidence. FPGA compilation/STA and generated two-clock-path evidence are
recorded separately by the integrating producer. The
[final FPGA record](../../../sources/misteross/docs/validation/2026-10-28-de25-scanout/README.md)
binds producer `6da029aa49`, its compiled SOF and 74 nonnegative timing rows.
The new CDC finding is cleared without waiver; five design-rule classes and
physical qualification remain open.

The [generated transport record](../../../sources/misteross/docs/validation/2026-10-28-de25-scanout/transport/README.md)
retains seven passing two-clock cases, five complete pixel-checked frames and
the exact source comparison to FPGA producer `6da029aa49`. Its generated FIFO
logic uses explicitly modeled M20K storage; primitive equivalence and physical
CDC remain unproven. This is separate from final Quartus timing evidence.

No hardware was accessed. The actual device/SID and IOMMU domain, bus-to-DDR
translation, physical firewall, boot/firmware pairing, bounded MMIO access,
clock/reset ownership and device detach lifetime remain qualification gates.
No DMA binding or runtime backend/profile was added, and no image was activated.
The shared reader/video/display-plane edits also affect existing DE10 MENU
consumers: the strict HIL plan is `FULL_IMAGE` / `core:menu`, so existing-consumer
package and HIL acceptance remain open. The standalone diagnostic lane does not
remove that integration requirement. Physical DE25 kernel boot, DDR visibility,
HDMI and interactive MENU acceptance also remain open; kernel tests and RTL
simulations do not satisfy those gates.
