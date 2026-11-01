# DE25 board-bound appliance updates and base helpers

The existing release store, target update API, storage gate and operator client
now support an explicitly selected DE25. This is software evidence on
`83e1c16b6a4b4cd19b92fe93e42a0902d52e1c3b`, based on published `a0e5a7086`.
No board, FPGA, SD, kernel, installed bundle, QSPI or power state is accessed.
No managed DE25 rootfs, bootstrap or software bundle is produced by this proof.

The shared release codec/store keep DE10-only defaults. Their explicit
`ForBoard` entry points require a separately selected supported board and the
stable kernel; they never choose from the uploaded manifest. Foreign uploads
are rejected before reading their bodies or mutating stored images. Retained
manifests receive the same board/kernel checks after reopening the store.

With a bootstrap factory present, the agent and production storage gate read
the immutable image's `/etc/fes-board` and require the matching ARM/ARM64 target.
Older ARM DE10 images may lack that file. AArch64 requires the exact DE25 file.
Direct-root headless images retain their no-update path. The server reports
its selected board and checks upload headers against it before reading image
bytes. `fes-update --board de25-nano` also checks the image's AArch64 software
base and the authenticated target's reported board before uploading. A legacy
status without a board is accepted only by the DE10 client path.

The existing `build-fes-kit-data` target supports
`FES_KIT_DATA_ARCH=aarch64`, keeps ARMv7 as the default and clears inherited
`GOARM`/`GOARM64` before applying the selected baseline. Both targets build
through that Make recipe; poisoned inherited CPU options and an unknown
architecture are checked. The existing platform builder also builds the
AArch64 supervisor and Bluetooth helper against the changed appliance module.

## Actual validation and preserved failures

The store tests use two separately constructed, fsck-checked 32 MiB ext4
**marker-only fixtures**. They are real filesystem images, not FES system
images or boot acceptance. Native and QEMU Cortex-A53 tests cover staged
images, trial selection, confirmation, durable reopening, factory retention,
cross-board/kernel refusal and rejection of a retained foreign manifest before
image access. API tests check positive uploads and rejection before body reads.
Client tests cover lost activation/confirmation replies and fresh leases on
both board selections.

Seven AArch64 test executables pass 26 test/subtest events. The focused native
continuation passes 232 events across the data gate and target client. The
appliance module's 87 native events from the earlier run are reused only after
its complete source is identical. These are CPU/file/API checks; they exercise
no physical SDM, DMA, watchdog, FPGA or HDMI.

| Attempt | Actual result |
| --- | --- |
| Core `cf4d4e92d`, initial harness | Failed after 0.2 s: the isolated PATH could not find `mke2fs`; no filesystem proof ran. Original script and empty image remain retained. |
| Core `cf4d4e92d`, corrected tool paths | Passed in 24.0 s, including real ext4 native/AArch64 store checks and Python tests. |
| Consumers `079642e0e` | Failed native aggregate after 17.9 s: one new gate test lacked the import alias required by the store package name. The appliance module and other five test packages pass; the storage command has no tests. |
| Consumers `83e1c16b6` | The repaired native checks, all seven AArch64 groups and both Make helper builds pass. The harness then fails after 41.1 s because its copied storage executable lost execute permission. Original build mode is `0775`; copied mode is `0664`. |
| Same source, focused finish | Copies executable modes correctly, reruns the failed argument-refusal check and completes the remaining builds/Python checks in 7.0 s. Passing CPU groups are reused with verified hashes. The earlier failures are not rewritten. |

The final assets are ARMv7/AArch64 storage helpers plus the AArch64 agent,
update client, supervisor and Bluetooth helper. All six actual files pass
their selected static ELF checks. The AArch64 storage and supervisor
argument-refusal cases pass before startup; the agent, update client and
Bluetooth services are not started. Python appliance tests pass 26 cases with
two skips. The raw receipts, source comparisons, logs and binaries remain
available in the retained archive.

The final affected plan on `83e1c16b6` against `a0e5a7086` passes all seven commands in 763.6 seconds. Parent integration runs 1,276 tests with 40 skips; generated consistency passes 40 consumers/42 fixture copies. Host Go and UI checks pass, with 136 successful Go package runs. This is a fresh green software aggregate on the corrected source. The earlier failed attempts remain failed and retained. The planner selects parent/host only; it establishes no FPGA or kit acceptance.

## Retention and remaining acceptance

The independent CPU archive verifies 95 regular files, including modes and all
original failed attempts. Its 41,268,394 bytes have SHA-256
`dcc5def1aa38d1241df3f25e8d126360023ff3b8fd664b37c240321dee0d4fc5`.
Source restoration from the retained bundles passes. The public appliance and
platform trees match the actual final builds. All 374 canonical MENU inputs,
59 combined-compiler inputs and 58 runtime/test inputs remain unchanged, so
the existing FPGA and ZX81 proofs retain their original scope and identity.

DE25 bootstrap/media assembly remains open: the current PID 1 and card recipe
are DE10-specific, while Terasic's card uses a roughly 300 MiB FAT boot
partition and a Linux root partition. Do not use Cyclone V watchdog/reset
register preparation on Agilex. Native update/watchdog recovery, full managed
image startup and the [normal-kit workflow](../../design/de25-linux-platform.md)
still need acceptance. The ABI3 kernel and these userspace artifacts remain
uninstalled. The bottom board's last authenticated observation is the earlier
ABI2 setup; this phase provides no fresh board attestation or baseline promotion.

HDMI output timing, full reset/boot/allocation coordination, disabled-F2H
firmware/native acceptance and an eligible independent identical-RBF rebuild
remain open. There is no additional board-delay data, new timing exception,
lowered clock or invented delay. Future QSPI or boot-configuration deployment
requires separate explicit authorization. Both consolidated PRs stay owned by
`lane:deano`; no PR is merged or handed to Lander.
