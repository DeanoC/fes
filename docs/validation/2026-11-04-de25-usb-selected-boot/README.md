# Selected native USB boot path

Base `0003d4876`; implementation `a5c7382c2`. No board was accessed in this
stage. The existing physical App-cycle result remains bound to its older
source and artifacts. This record supplies no physical USB acceptance.

The explicit `usbcheck` native program selects a trusted BL31 USB provider.
The base probe profile still enters with USB contained and no static readiness.
The native link assignment, selected driver/client code, entry receipt and
matched cold profile must agree. Invalid or omitted selection is rejected.
The actual firmware ELF contains strong board setup and USB lifecycle symbols.

Trusted setup binds the accepted boot owner/epoch, complete sealed HPS pin map
and EL3 MMIO/barrier/wait callbacks. Preparation checks clocks/pins with the
controller held. The runtime verifies its cold context/pins and device mapping,
acquires the prepared lease, initializes DWC2, confirms live PHY/VBUS readiness,
polls for five seconds and requests a checked stop. Objects stay rooted after
failure. There is no direct-access fallback or automatic restart. SD and FPGA
data paths remain held throughout this USB-only scope.

All 243 Nano tests and 14 profile tests pass. The new joined C fixture exercises
the actual boot binding, board/platform/service and modeled hardware for normal
cycles, failed preparation, failed PHY confirmation, prepared-startup abort,
failed stop and duplicate setup. USB/DWC2/SD/mapping Go race tests pass, as do
native firmware/RTL integration, producer identity, contract consistency and
test-selection regressions. The full parent and FPGA suites were not rerun.

The selected native SDK, entry, linked BL31, matching loader and final boot
composition build successfully. The FIT is 2,674,576 bytes. All selected native
source bytes match the implementation commit; USB selection agrees across
firmware and loader. Receipts/artifact hashes are in `result.json`; complete
outputs remain under `out/nano-usb-boot-20261104/`. One verifier invocation
timed out fetching its pinned public source. The same verifier passes against
the reviewed local Terasic source and reset-manager blobs; its failed fetch
record is retained. Symbol inspection uses the compiler's recorded library
environment. No compiler or model failure was waived.

The driver currently accepts a direct boot keyboard and rejects a hub. The
keyboard/controller-through-hub setup therefore needs hub enumeration and
split-transaction support before native input qualification. This diagnostic
also grants no simultaneous USB/storage/core permission. Native launcher input,
ordinary core/ROM/media admission, HDMI and native Ethernet remain open before
DE10-style readiness. The next step is hub support and its actual driver/model
failure tests, followed by a bounded hardware input test.
