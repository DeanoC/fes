# Matched Linux-free boot candidate

Candidate `bec0efa961847c4d` completes the SPL/U-Boot/FIT/HPS-first SOF
composition for the [owned MAC dispatcher firmware](../2026-11-10-de25-mac-dispatch/README.md).
It is assembled from source `3382abbb37c022b4043ba33dd4a7919111b7c633` with no implementation changes in this
step. The reused native SDK, entry and BL31 receipts and all recorded source
hashes match the current tree. The native binary itself remains unchanged.

The new SPL and FIT are built from the clean pinned Terasic U-Boot revision
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`. The builder checks the BL31 ELF/RAM/MMU proof, matching
cold handoff, embedded entry/BL31/FDT contents and hashes, load nonoverlap and
sealed SPL/FIT pairing. The independent matched-chain check verifies all
component/resource identities, current input hashes, selected flags and the
unchanged boot and menu FPGA payloads.

PFG composition runs as `deano` under `flock -w60 -E75` on
`/tmp/lander-quartus-3090.lock`. It replaces the SPL in the sealed HPS-first
project, independently inspects the embedded SPL, checks the device/order and
split HPS I/O hashes, and compares every split core byte to the sealed reference.
All 2273 native inputs are checked before and after conversion.
SPL, BL31, entry, profile and FIT metadata agree across the finished chain.
The existing firmware MAC provider remains unbound and Ethernet disabled.
SD remains read-only and preferences volatile.

| Component | SHA-256 |
| --- | --- |
| Native SDK ELF | `bcdc2e9195253904259d22969a0252f3e090bca667015c6cbb34034a70940a31` |
| Entry image | `1d5756babcd2a092c7391b6c3df0c65189c128b4544b80fcfecfb075fc7149c3` |
| BL31 binary | `09c64d6f0d45396968ac687771990e6f2c91d7ca22dd7360374f4ba8878ec4c4` |
| SPL hex | `5bc89cbdf63f94dfadea038515c9eb7bcc2feaeb78ddf4dbf4b3d81ed63f520e` |
| FIT | `049a935deb3922acb75afe2f7cc603eb6dff2e0f27b21a39765cca61968ecbf7` |
| HPS-first SOF | `e60226203439deee1a12030f49eb6287e8ad13933290ea6e1d95deacc5423867` |

FIT size is 8,342,184 bytes under the unchanged 8,388,608-byte bound,
leaving 46,424 bytes. The limit and memory exclusions are preserved;
further image growth must account for that remaining space. All three bounded
jobs (SPL/FIT build, PFG composition and matched verification) pass. Existing
software evidence remains source-valid; no unchanged regression suite was rerun.

Commands, component receipts, hashes, logs, `publication.json` and
`validation.json` are under `out/nano-mac-boot-chain-20261110/`. The complete
candidate files are in its `native-composition/` directory. The SDK is retained
from `out/nano-mac-lease-final-20261110/native-sdk/`; entry/BL31 are retained
from `out/nano-mac-dispatch-20261110/`. No file from an older image was replaced.

This is an off-board diagnostic candidate, not physical boot, programming or
ordinary core-loading acceptance. No kit, card, JTAG/UART or remote builder was
touched. Old-output cleanup remains paused. Hardware testing waits for operator
availability; a fresh bounded trial bundle and current lease/connection checks
must precede use. Physical keyboard/HDMI launch/return, a qualified native MAC
supplier/drain policy, writable SD, broader packages and full DE10-equivalent
appliance behavior remain open. The overall goal is incomplete.
