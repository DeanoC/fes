# Controlled native Ethernet startup and matched boot chain

Base is `73c493f3712e825b1dc859450eb8866e042e7fe0`; implementation and build source is
`db5fc2d66b1426e79e63a77577499d1139eda33e`. FES owns this native base integration change.
All changes are local; no push, PR, kit, card, JTAG/UART session or remote builder
is used. Physical testing remains deferred until the user returns. Cleanup of
old outputs remains paused.

The previous Nano network opener initialized XGMAC directly and its CPU transport
did not route the private MAC firmware service. `xgmac.OpenControlled` now uses
one private client for Query, stopped-controller Acquire, real descriptor/PHY
initialization, Confirm and exclusive driver-control binding. It exposes packet
and lifecycle access only after that sequence. A failed non-nil driver retains
its lease and stays retired without further register access after the uncertain
boundary; there is no automatic Stop, replacement or retry. Firmware keeps its
reservation, and the DMA arena remains pinned until reboot.

`de25nano.OpenNetworkWithControl` accepts the owner from independently accepted
boot admission, admits one startup attempt and returns the IP interface plus
the controller for `ProgramService.AttachNetwork`. It retains partial resources.
The generic Nano opener has no accepted owner and refuses startup. QEMU keeps
its VirtIO path. The actual CPU transport now routes the fixed MAC call through
the compiled profile selection and exact cookie/operation/reserved-argument
checks; it preserves the full owner/generation and firmware reply. Firmware
still establishes ownership, exclusion and independent drain.

The real HTTP/ARP/IPv4/TCP test now uses the same controlled startup function,
actual XGMAC descriptors, native MAC client and owned C dispatcher. It completes
three pause/resume cycles with explicitly modeled physical grant/drain callbacks.
The four affected Go packages pass race-enabled tests. Nine startup failure
cases cover unavailable or mismatched ownership, rejected/lost acquisition,
PHY mismatch, rejected/lost/cancelled confirmation and a changed DMA window.
They check that failed startup returns no lifecycle controls, does not perform
uncertain cleanup or later driver access, and cannot adopt a used firmware lease.
A separately cancelled startup performs no firmware or device access. Transport
negatives exercise disabled profiles, foreign calls, unknown/wide operations,
wrong/wide cookies and reserved arguments without invoking firmware.

All seven affected commands pass: planners, committed/working whitespace,
1846 parent Python tests, generated-consumer consistency and native runtime/
firmware/FPGA integration. Shared definitions, external dependencies and FPGA
source are unchanged. The new ARM64 SDK compiles with the actual Nano board
adapter and records 2125 input hashes. The network hook is compiled but
not selected by the current native startup. It still requires a qualified
secure provider and explicit wiring from accepted boot admission.

A fresh matched entry/BL31/SPL/FIT/HPS-first SOF chain is built for native boot ID
`a02d808ea11eb54f`. All current component/source hashes agree. PFG runs as `deano`
under `flock -w60 -E75 /tmp/lander-quartus-3090.lock`; composition checks
26 inputs and retains the byte-identical sealed FPGA core payload.
The existing RAM/MMU exclusions and 8,388,608-byte FIT limit remain intact.
FIT size is 8,342,184 bytes, leaving 46,424 bytes. The linked ARM64 MAC setup
is still the weak unavailable return; the independent binary check verifies its
exact instruction bytes and ELF hash. Native Ethernet remains disabled, SD
read-only and preferences volatile.

An initial offline entry-build recipe mistakenly selected the previous ELF with
the new SDK receipt. The builder rejected it before creating output. The failed
recipe/result/log are retained; only the payload path was corrected, and no
memory, identity or source check was relaxed. The corrected build and every
subsequent chain step pass. This is not a hardware attempt or a hardware retry.

All commands, receipts, failed-attempt evidence, source hashes and final
`publication.json`/`validation.json` are under `out/nano-mac-startup-20261110/`.
The [previous offline trial bundle](../2026-11-10-de25-mac-keyboard-trial/README.md)
remains preserved for source `73c493f3712e825b1dc859450eb8866e042e7fe0` and its earlier image
`bec0efa961847c4d`; it must not be replayed against this new image. A fresh
operator bundle and current lease/connection/stability checks must precede any
physical test. Neither a new operator record nor live preflight is created here.

This is source, model and linked-artifact evidence. It does not establish native
boot or keyboard/HDMI return on this image, physical Ethernet, DMA/interconnect
containment or ordinary core-load acceptance. A qualified MAC/clock/PHY/SMMU
supplier, independent drain, active-core coexistence, writable SD, broader
packages and the full DE10-equivalent appliance remain open. The goal is incomplete.
