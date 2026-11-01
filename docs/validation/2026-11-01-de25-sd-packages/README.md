# Native package retention and recovery PHY rejection

Base: `4e111b90ed873be193f4386df46ed53579332d35`. Implementation and exact
hardware-attempt source: `2113e55c7cfbe219035c435fd93dbe1bcb41023c`
(`codex/de25-embedded-os`, lane:deano). This is FES-owned embedded integration;
no first-party module or shared wire contract changes.

## Implemented and checked

The new [package store](../../../platform/embedded/tamago/packagestore/README.md)
retains canonical packages and supported ROM/part inputs from read-only FAT32
through the existing validator/linker. A contained handoff is published only
after the owned SD session stops successfully. Resume invalidates old handoffs
and selections; ownership/device failures retain the lease without retry.
The Nano-only `sdpackages` image embeds an independently selected package ID
and requires retain/contain/resume/reread/final-contain. It performs no SD write
or FPGA activation. A separate fixed upload purpose stages `FES/PKGTEST.BIN`.

Host checks passed: all 11 selected affected stages, including 1,220 parent
Python tests, native SD/FAT/package tests with the race detector, host/UI and
runtime checks, generated contracts, and 1,571 FPGA-tooling tests. `make check`
matched 33 generated consumers and 37 fixture copies. Tests cover immutable
retention after source changes, real read-only FAT32 with an unchanged disk
hash, format-3/4 ROM linking against the existing independent Mistral output
oracle, concurrent retention/containment, stale handoffs, malformed readers,
cancellation and stop/resume/ownership failures. Nano formats 3/4 remain
unsupported; those linker tests retain their DE10 target.

The actual native-command fragment passed 17 injected trace cases. The actual
trial wrapper passed 16 failure/cleanup cases. These are host evidence. The
model compatibility policy remains model-only; cached SD handoffs grant no
physical FPGA permission.

## Matched candidate

| Artifact | Identity |
| --- | --- |
| Boot build ID | `eccde91bb9b455e9` |
| Native SOF SHA-256 | `b8ac5bd6117fc0ff1659a8b7e211b44f9ef1185aaec32bdfb718c4a7a0c3c8cf` |
| Native FIT SHA-256 | `5824dc98804f0848b62ce6c048cbde93ec706d65b72367a2029446c3e4f7fd75` |
| Native ELF SHA-256 | `c5b4ff4db1371f5b514566214de2601127f491b4dd41ac612feea3897e6a1b2f` |
| Entry image SHA-256 | `81f1f9151f4daae0879275178c88c9b9f4e9b75c06c8c17c597366e3f23a594b` |
| Test package ID | `164c9e356d32eed24bb5ec59528323c0a325a23a93e2a04c25b78985df22f873` |
| Test archive SHA-256 | `0181b38b59e8a2fd0399a299c95f325dff7ad55cb623f480721b93a6b1ca1212` |

The 3,584-byte test archive contains a **12-byte synthetic payload**, not an
FPGA bitstream or playable core. It was independently constructed and checked
by the existing package producer. Its expected ID is compiled into the probe.
All cross-builds, strong SD callbacks, FIT/SPL/entry seals, SOF composition and
regular-file packaging passed. No native acceptance is inherited from those
results or from the previous smaller SD probe.

## Bounded TRWU8CBL attempt

The top kit remained exclusively claimed, with the original factory card safe
and the spare in the board. A fresh source/artifact/UART/network preflight
passed. The wrapper temporarily selected 6 MHz on that cable, booted the
known recovery image and verified the 3.7 GiB card, partition, protected prefix,
root recovery FIT and recovery manifest.

The [serial trace](recovery-upload-console.log) then reports positive PHY
status `796d` before the first Ethernet start. Inside `dhcp`, the loader
reconfigured the PHY and timed out during auto-negotiation. The command
returned failure and the wrapper stopped: **no download, no SD write, no new
native programming and no `fesboot` command**. No phase was replayed.

One bounded [read-only MDIO observation](phy-console.log) after the stop
reported ID `00221622`, BMSR `796d`, and gigabit status `3c00`. The pinned
U-Boot `include/micrel.h` maps this ID to KSZ9031 under mask `00fffff0`. Its
current recovery configuration has `PHY_MICREL` disabled and a 4,000 ms
negotiation deadline. The XGMAC first-start path calls `phy_connect` (soft
reset), `phy_config`, then `phy_startup`; the pre-start status sample therefore
does not prove the post-configuration link. The missing vendor driver is a
concrete mismatch to correct. Its correction has not yet been tested here.

Read-only post-checks confirmed JTAG `4362C0DD`, original 15 MHz, restored USB
permissions and no UART owner. The board remains at the recovery prompt with
the spare unchanged and the continuing claim held. Factory restoration was
not needed under the user's retained-diagnostic policy.

Next: build recovery with the matching KSZ9031 driver, preserving the current
negotiation bound and stop-on-failure behavior; qualify that recovery, then
stage the matched native FIT and test package in separate fresh recovery
boots before the bounded native retention run. Writable native storage,
active-fabric return, USB, native network/display, FPGA configuration and real
core launch/return remain open.
