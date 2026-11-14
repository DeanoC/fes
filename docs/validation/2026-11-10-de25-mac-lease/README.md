# Private MAC ownership engine and native driver confirmation

The firmware prototype now has a fixed-device/fixed-window MAC lease engine,
with an independent generation and required trusted scope, grant, confirmation
and independent drain callbacks. A private binding retains the original boot
owner/epoch, selected MAC/PHY and pinned DMA window. It supplies no non-secure
initialization, caller-selected address, automatic readiness or physical supplier.
The [shared contract](../../../sources/mister-packages/docs/embedded-mac.md)
owns the generated C/Go wire constants. The new channel is not routed into the
physical Nano SiP dispatcher or selected by a boot profile.

A reservation precedes the first potentially releasing callback. Only complete
post-callback scope permits a new generation or containment reply. Missing or
failed grant/confirmation/drain, stale/foreign requests, lost/corrupt replies and
exhausted generation space cannot publish success. Uncertain mutations retain
reservation and the last known receipt, retire without retry or register cleanup,
and keep the original memory pinned. Acquire reserves generation capacity for
confirmation and stop. Queries and already-contained stops preserve generation.
Bridge, SD and USB generations are not reused or advanced.

The native client shares its private state across copied handles. It accepts
only this boot's contained generation zero. After initial acquire, driver setup
and confirmation, BindPlatform transfers one lease into the XGMAC driver and
prevents raw aliases from issuing calls. Restart obtains a new grant, initializes
the same descriptors and verifies the PHY, then confirms firmware readiness
before packet traffic reopens. Failure/cancellation retires without later MMIO.
The common network controller rejects a platform confirmation hook that has not
been consumed by the driver.

Portable C tests cover ownership, fixed-binding checks, missing qualifications
and callbacks, generation wrap prevention, retained reservations, configuration
busy/replay rejection and failure without further callbacks. Go wire tests execute
the actual C engine with modeled boot/grant/drain and damaged/lost responses.
The actual descriptor/Lneto/gVisor HTTP test now joins the C engine, native lease
client, XGMAC adapter and common traffic controller across three restart cycles;
no host TCP socket or fake HTTP transport substitutes for the packet path.
Separate driver tests check confirmation ordering, PHY failure, failed/lost
confirmation, cancellation and no further device/memory access after retirement.

The official [system manager programming model](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/system-manager-configurable-registers?contentId=eq6KypQuEbC__pP3bDdeHw)
requires static EMAC interface fields to be set under reset; AXI-disable is not
used here as a dynamic drain shortcut. The [CCU support description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/axi-and-ace-support?contentId=mazFf~_f_zPCjSIF1gegZg)
does not support barriers or ACE5-Lite low-power signaling. No inference from
these controls qualifies interconnect drain. A physical source-bound MAC/clock/
PHY/SMMU supplier, independent fence, live-core coexistence and complete secure
boot/dispatcher composition remain required before native Ethernet can be enabled.

Base is `7d622df69955b472ed6ae75fe36f5fce75394a1b`; final tested source is
`9eda8554c48b3a3400ce00557d28dc452af707d1`. All fourteen affected checks pass, including parent
Python regressions, SD/PIO, native runtime/producer, host/runtime/contracts,
firmware/RTL integration and FPGA Python tests. Focused race/C tests and the
committed generated-consumer check pass (54 generated consumers, 37 fixtures).
The full Nano ARM64 launcher cross-build passes with all 2122 compiler
input hashes verified. ELF SHA-256: `bcdc2e9195253904259d22969a0252f3e090bca667015c6cbb34034a70940a31`.
Ethernet stays disabled, SD read-only and preferences volatile. No matched
entry/BL31/FIT/SOF was rebuilt or deployed.

The initial broader parent run found one coverage fixture still expecting 52
generated files. Both its exact count and validated-source inventory now include
the new C/Go contract. A separate fault injection proved confirmation could
change the reported DMA window while restart published ready; the final driver
checks the original window again before reopening traffic. The negative test
fails on the first source and passes with the repair; its logs are retained.
The first make check stopped because the package source was still
uncommitted, as required by the existing source gate; its log is retained.
Original attempts and fault injections remain in `out/nano-mac-lease-20261110/`;
final commands, source snapshots, logs and `validation.json` are under
`out/nano-mac-lease-final-20261110/`.
No kit, card, JTAG/UART, remote builder or cleanup operation was used. Hardware
remains paused until operator availability. Physical keyboard/HDMI, writable SD,
broader packages and DE10-equivalent appliance acceptance remain incomplete.
