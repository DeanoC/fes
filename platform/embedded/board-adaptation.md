# Moving the DE25 base onto board drivers

The next hardware-facing milestone is a Nano backend beside the QEMU backend.
The current VirtIO, xHCI and RAMFB implementations exercise real emulated-device
protocols and remain the regression target for tenfoot, storage and ownership.
Physical-device activation stays disabled until that device's resources and
lifetime rules have been checked on the delivered board.

## Start now

Freeze the board revision, boot firmware/handoff and reference sources. Record
which layer sets up DDR, clocks, pinmux, resets, exception level and security
state. Keep RAM reservations, DMA/cache attributes and interrupt ownership in
one board description, separate from the UI and package linker. Do not copy
QEMU addresses or development-kit linker layouts into a Nano build.

Altera publishes [Agilex 5 bare-metal drivers](https://github.com/altera-fpga/baremetal-drivers/tree/66bdcc347564c5698a8997169bce83a31647d618).
The repository HEAD was checked on 2026-10-29 at the commit linked here. Its
[bring-up instructions](https://github.com/altera-fpga/baremetal-drivers/blob/66bdcc347564c5698a8997169bce83a31647d618/README.md)
separate OCRAM startup from DDR execution after TF-A. That firmware split is
useful input, not a DE25-Nano boot recipe. The published
[driver matrix](https://altera-fpga.github.io/rel-25.1/driver-list_baremetal/)
lists UART, GIC, DMA, SDMMC/ComboPHY, mailbox and XGMAC among the Agilex 5 drivers.
A source listing is not board qualification.

This image uses TamaGo with CGO disabled. Vendor C drivers cannot simply be
imported as Go packages. Audit their register sequences, clock/reset dependencies,
wait bounds and DMA ownership first, then choose a Go adapter or a deliberately
specified firmware interface. Avoid mixing vendor startup with an already
initialized Go runtime. Preserve the existing block/network/input abstractions.
Host register-sequence tests and cross-builds can start before delivery; physical
checks remain open.

## Bring-up order

| Order | Adapter | Useful first acceptance check |
| --- | --- | --- |
| 1 | Firmware handoff, UART, counter/GIC and memory map | Boot a minimal EL1 Go probe; verify reserved RAM, timer progress and handled interrupts before UI/GC load |
| 2 | SDMMC/ComboPHY block device | Read known sectors and mount a prepared FAT32 card; then qualify bounded writes/flush and removal behavior |
| 3 | USB host/DWC2/PHY and Ethernet MAC/PHY | Prove enumeration/input and static-IP packet exchange separately; then verify DMA buffers, IRQs, reset and recovery |
| 4 | FPGA configuration/mailbox and bridge ownership | Program only a qualified Nano artifact under containment; verify identity before input/display release |
| 5 | Physical display/input/session handoff | Run tenfoot Play/Back and failure checks with actual generation-bound memory, cache maintenance and proven quiescence |

SD is a good first peripheral after boot: it immediately exercises the existing
FAT32 and retained-package path. USB host integration needs its own controller,
PHY/power and scheduler audit; the QEMU xHCI path does not validate DWC2. Network
address setup and entropy remain explicit. FPGA configuration belongs last in
this order because a working mailbox alone does not establish containment,
correct bitstream/device identity or safe DMA/display release.

The CPU/video composition probes use Cyclone V synthetic maps and bitstreams.
They verify portable data and lifecycle behavior, not Agilex configuration
encoding or a Nano emulation core. Preserve those evidence boundaries when the
first real driver compiles, and qualify one physical adapter at a time.

## Selectable prototype backends

The native base now selects QEMU or Nano providers at build time. The Nano
prototype supplies UART/counter hooks and read-only PIO against firmware's
selected SDHC card, with an explicit resource profile. USB DWC2, MAC/PHY, FPGA
display and physical activation providers still refuse startup. See the
[board contract](tamago/board/README.md) for build commands, firmware obligations
and the first hardware gate. A Nano cross-build is host evidence only.
