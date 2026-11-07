# DE25 unit-media integration proof, 2026-10-31

Producer `9bcf1a7a78c647c535db2a4ec1a6d474244ac618` moves unit-media
observation/insert/eject onto the one CoreDriver interface. NativeHardware
consults the selected driver for limits, status and transfer, preserving the
FesGp implementation through overrides. The DE25 driver is separated from the
pure atomic protocol consumer, so existing RTL-proof clients and the maintenance
CLI remain independently linkable. The exact production archive guard and its
missing-member fixture now require de25_core_driver.o.

The DE25 unit uses actual identity-probe MediaInfo limits and state. A retained
snapshot is checked/read before mutation under the operation deadline; live
insert issues no execution/reset command. Ready/empty are recorded only after
confirmed completion. Uncertainty retains loading and refuses cleanup/replay.
The known BRAM contract has no tape-write capability: saveable is false. This
is an explicit unfinished feature, not normal-kit SAVE acceptance.

## Software results

- Focused native tests pass all16 DE25 groups; the standalone maintenance CLI
  builds without the driver dependency. Unit cases cover invalid unit/size,
  retained snapshot contents, expired deadlines before writes, live insertion
  without reset, observed state transitions and sticky uncertainty/no cleanup.
- All eight affected commands pass in507.7seconds: two planner checks, two
  whitespace checks, parent Python, generated consistency, host Go/UI/browser
  and complete runtime functional/incremental/version/exact archive/support
  checks. affected-result.json records the exact commands and results.
- `make check` passes in6.1seconds.
- The locked SDK builds the normal dynamically linked runtime and passes
  programmer filesystem tests and all16 computer groups under cortex-a53 QEMU
  with the SDK sysroot (25.6seconds). sdk-build.json identifies the archive,
  daemon and test binaries. An earlier private wrapper omitted the QEMU sysroot
  for the now-dynamic programmer test and failed before execution; that receipt
  is retained, and the final wrapper supplies the established recipe.

## Actual AArch64 CPU/linkage control

Bottom board TRWU8J8G, boot `15e107d8-0ed1-4e5e-83d8-b27c8a49d853`,
DHCP192.168.10.28, kernel6.12.11-fes-de25-owner, CID
`275048534436344760702d33e0017c00` was reverified over pinned SSH. Kernel
notes and root receipt match the previous programmer trial. CPU-only test
binaries were uploaded into an exclusive temporary directory and removed.

The same current-source16-group test was built both statically and with SDK
dynamic libraries. The first attempt returned127 for both before execution:
the rootfs has no timeout utility. A source-retained alarm(30)/exec wrapper
then bounded each actual test; core dumps were disabled.

The static binary aborts with std::system_error (return134), reproducing the
previous QEMU failure on the CPU. The dynamic binary passes all16 groups
(return0). The error's numeric text differs; its root cause is not diagnosed.
This excludes a QEMU-only explanation for the static candidate. Use the normal
SDK dynamic runtime recipe; the static diagnostic daemon is not an accepted
runtime deployment artifact. The final SDK dynamic unit-test hash is exactly
that of the binary run on the board (retry-payloads.json and sdk-build.json).

These tests use scripted registers and filesystem snapshots, not physical
MMIO. They do not qualify a physical DE25 core driver. No FPGA configuration,
owner ioctl/grant, capture, reboot, card/kernel/QSPI change or production daemon
installation occurred. Temporary test files are verified absent, owner node
remains root:root600 and the boot/root metadata are unchanged. Owned SSH/build
clients are closed; the existing candidate claim remains retained, not released
or baseline-promoted.

## Remaining normal-kit work

The registry/profile table and production AArch64 factory remain closed.
Next add platform-aware package compatibility, registry/startup selection and
safe restart adoption to the existing single lifecycle/factory. DE25 tape SAVE,
managed agent/catalog/MENU and normal ZX81 launch/stop/relaunch/recovery remain
unfinished. The BRAM core does not qualify allocated DMA/DDR/SID/SMMU/firewall,
global reset/drain or physical failed-transfer behavior. Secure-call/shared
mutex bounds and timing issue #1284 remain open. Earlier physical programming
proofs remain tied to their own producer/artifacts.

Complete SDKs, failed controls and full affected logs are independently retained
at `/home/deano/fes/out/artifacts/de25-unit-media/9bcf1a7a7/`.
