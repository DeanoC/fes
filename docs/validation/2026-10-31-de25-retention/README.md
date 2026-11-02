# DE25 programming retention, matched image and physical ZX81 proof

Kernel/image producer `29e0eca19228a7b3027e5d7729202618a6bf79a9`, vendor kernel
`d7d192a9ddd955fe206aa350afed8f82e368a74c`, bottom board `TRWU8J8G`. This extends
[the manager write repair](../2026-10-31-de25-manager-write/README.md) and the
[earlier physical owner-kernel proof](../2026-10-31-de25-owner-zx81/README.md).
Normal FES runtime admission remains closed.

## Kernel changes and source-function proof

The sole active platform patch now gates both bounded service-claim polling and
manager failure retention under `CONFIG_FES_DE25_DMA_PLATFORM`. It replaces the
older standalone claim patch/manifest; ordinary kernels retain vendor behavior.
The service loop honors stop, sleeps while BUSY and bounds inactivity without
reporting buffers returned on error. The manager marks uncertainty before a
request and retains it on failure. A fresh caller cannot issue another request,
reset locks or resume write/complete. Only confirmed completion, all buffers
returned and a joined worker without a final error clear that state.

Tracing the actual FPGA framework also found that its scatter/gather path calls
write repeatedly for one image. The data deadline now belongs to the manager
and persists across fragments. Completion polling accounts for command time
and spurious notifications within its existing five-second phase budget.

All 37 reported actual-function cases pass: nine claim, fourteen write and
fourteen lifecycle scenarios. Each runner authenticates the entire original
file, patch and result before compiling the changed C functions against
injected clock, service and callback helpers. Explicit profile-disabled positive
controls pass. The original service hangs on BUSY; the original write loop
hangs on permanent ENOMEM, each under an owned one-second control timeout.
The original init permits retry after failure and reproduces the dedicated
retry assertion. Lifecycle tests also inject errors/lock state during thread
join. The existing unused vendor callback parameters use the kernel's compiler
policy in this host harness; other warnings remain fatal. These source tests
are not kernel scheduling, secure-firmware or physical failed-transfer evidence.

Receipts and C inputs: [claim](claim/checks.json), [write](write/checks.json),
[retention](retention/checks.json). The initial lifecycle harness compilation
exposed those pre-existing unused parameters; the final command preserves the
original callback signatures rather than editing the tested functions.

## Kernel and matched image

The full AArch64 kernel runner passes in 221.6 seconds (215.9 in its pinned
container), including built-in supplier/owner and compiled-DTB checks. Image
SHA256 is `a4604214a70855ec824ffbf70c45abbd82983dcbefd68bb9f51de7b7089ab478`.
All six affected commands pass at this producer in 280.3 seconds.

The matching image assembles twice identically in a 30.1-second runner and
passes independent structural verification. Its SHA256 is
`c1bc63c5cdc420584c320dc01328fd53b8fe70762a0e851e0ebee3a84ccb56ab`.
The DTB/configuration and root startup/release/device rules retain their previous
bytes. Only the kernel Image and root kernel manifest need the live update.
The first host verification could not find debugfs on PATH; adding the installed
system tools path allowed the independent verifier to pass. Kernel receipt and
image structure are retained in [kernel.json](kernel.json),
[image.json](image.json) and [image-verify.json](image-verify.json).

## Physical delivery, reprogramming and reboot

Pinned SSH preflight confirms boot `9191e320-2d97-41a2-8e28-735ab8695f7c`, the
known card CID and root-only owner device, with the previous temporary core
files absent. A verified file-level maintenance update preserves the exact
previous Image and manifest in FAT `fes-owner-before-retention`. The full
original pretrial SD backup remains retained separately. No reader card, QSPI,
JTAG or new FPGA build was used. The first installer had a syntax error and
stopped before mutations; the original and corrected script/receipts are
retained. Local and target shell syntax checks precede the corrected install.

Software reboot reaches `3678ada3-13a3-42a7-9266-ddfbca4e50b1`, DHCP
`192.168.10.78` and pinned-key SSH. Initial DHCP attempts obtained no lease;
background retry succeeds without a service restart or network-policy change.
UART inspection and `/sys/kernel/notes` identify the actual running compiled
Image: note digest `be78380775fe921a08e9b5f1ab312df073338290fe0ebe06777a21b075462b9b`.
The notes are extracted from Image using the linked System.map ranges. Generic
first-boot collection and offline verification pass.

Two real Linux configfs/SDM loads of the known BRAM ZX81 core pass in this same
boot. The RBF remains SHA256
`7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a`, FPGA producer
`79d616e85e19d65f0d645b67a85ffe7e142dcdfc`, build ID
`06ee047f143317df0fd1d98717b2b991`. The unchanged static maintenance client is
from `d2012e3ac6df0d66a8525bd2558735f7ec0a565b`; its payload hashes are checked
before programming. All twenty native client operations pass, explicitly
reporting physical CPU MMIO and denying production runtime admission.

After each load, reset, tape upload, ROM LOAD and RUN visibly produce BASIC 7.
The second successful load is a physical check that normal completion leaves
the manager usable for another configuration. Owner preconfiguration syscall
checks pass before/after loads and cleanup; no scanout grant is admitted.

| Observation | Capture |
| --- | --- |
| First confirmed LOAD token | [load-token.png](board/zx81/load-token.png) |
| BASIC 7 after first load | [basic-seven.png](board/zx81/basic-seven.png) |
| ROM K after second FPGA load | [second-rom.png](board/zx81/second-rom.png) |
| BASIC 7 after second load | [second-seven.png](board/zx81/second-seven.png) |

Early first-cycle key batches arrived before the ROM was ready. Captures retain
the initial blank/K and empty-program RUN 0/0 observations. Observing the LOAD
and loaded screen before RUN produces 7; the second cycle follows those visible
states. Delivery ACK is not proof of ROM readiness. This is not a guarantee
that a fixed input delay suffices. Only the shared `fes-ms2109-hil` fan-out was
opened; other viewers/producer were preserved.

The core was held; the owned overlay, configfs mount, firmware and temporary
client/media/update trees were removed. A further software reboot reaches
`15e107d8-0ed1-4e5e-83d8-b27c8a49d853`, DHCP `192.168.10.28`, the same pinned
host key and kernel-note digest. Generic collection/offline verification and
actual owner open/ioctl/capability checks pass again; the node remains
root:root mode 600. All owned UART/SSH/capture clients are terminal and closed.
The board stays powered on under the existing candidate claim, not released as
restored or baseline-promoted. Warm reset does not attest erased FPGA SRAM.

## Acceptance boundary and retention

All four OF overlay-property lifetime warnings are retained. The libfdt verifier
build retains its existing unused-parameter warning. No clean-warning claim,
warning waiver or timing qualification is made. The live SD is the verified
file-level update, not a whole-card match to the offline image digest.

No physical failed transfer was injected. Polling return and worker join do not
prove SDM abort/DMA drain; a synchronous secure call or shared-service mutex can
still block. Failure fencing, uncertain-transfer reboot, physical SID/SMMU/
firewall/DDR/DMA/reset acceptance and timing signoff remain open. The production
factory still rejects DE25/AArch64. Managed kernel-backed programming/control,
agent/catalog/MENU and normal ZX81 launch/stop/relaunch remain required.

[results.json](results.json) indexes client/boot identities and operation scope.
Complete kernel, image and operational evidence is independently retained at
`/home/deano/fes/out/artifacts/de25-retention/29e0eca19`, with
[artifact-inventory.json](artifact-inventory.json). Authentication helpers and
binary payloads are excluded from the public record. This diagnostic does not
establish DE10-equivalent normal-kit acceptance.
