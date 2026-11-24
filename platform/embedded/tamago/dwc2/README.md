# DWC2 polling USB host (DE25-Nano)

`dwc2` implements a PIO USB 2 host driver for DE25-Nano USB0 (`0x10b00000`)
with no DMA buffers or Linux dependencies, defaulting to polling mode with
optional opt-in GICv3 SPI interrupt wakeups (`EnableIRQ` / `Ready()`).
QEMU retains its separate xHCI backend in [usb](../usb/README.md); both share
the USB descriptor, boot-report and HID gamepad decoders.

## Hardware contract and topology limits

- **Firmware obligations:** Firmware must release USB0 resets (PER0 bits 3 and
  11), disable DMA, configure the 60 MHz ULPI PHY, pinmux, VBUS supply and
  Type-C host role, and wrap host access through
  [usbcontrol](../usbcontrol/README.md).
- **Supported controller profile:** DWC2 2.x/3.x host-capable core with dynamic
  FIFOs (>= 256 words). The driver resets the core, partitions/flushes FIFOs,
  forces host mode and enumerates one boot keyboard (or, via `OpenGamepad`, one
  non-boot HID gamepad) directly or through a USB 2 hub tree.
- **Hub and split limits:** Up to 3 hub levels, 15 ports/hub, 31 total ports
  and 31 addressed devices; high-speed hubs use start/complete-split
  transactions for low/full-speed children. Multi-TT hubs use alternate 0.
- **HID boot keyboard setup:** Issues `SET_ADDRESS`, descriptor reads,
  `SET_CONFIGURATION`, `SET_PROTOCOL` and `SET_IDLE` (100 ms, report ID 0) so
  idle keyboards still supply current snapshots for neutral-report fences.
- **Limits:** Single polling owner, one initialization attempt per boot, no
  hotplug re-enumeration, no USB 3 hub protocols, no HID output reports.

## Polling, pause/resume and failure ownership

- **Bounded PIO transfers:** Every transfer step verifies polling PIO mode,
  masked interrupts and disabled descriptor DMA. Reports remain private until
  actual IN-data RX, expected PID, transfer completion and channel retirement.
  Direct control IN handles raw NAK as a continuation token; direct periodic IN
  queues one request per service, retires on paired RX/IRQ toggle events and
  rearms after the advertised interval.
- **Pause across core loads:** `PausePolling` drains any pending packet or
  hub-health step within 500 ms / 4,096 steps (canceling an observed halted
  periodic toggle attempt cleanly), checks intact link and idle channels, and
  blocks all MMIO while paused. `ResumePolling` clears local pause without
  re-enumerating the device.
- **Optional GICv3 SPI wakeups:** `EnableIRQ(g, de25nano.IRQUSB0)` routes
  Agilex 5 USB0 SPI 125 into a level-safe handler that immediately masks
  `GINTMSK=0` and wakes `Ready()` while leaving `GINTSTS`, RX FIFO status and
  `HCINT` intact for `PollKeyboard` / `PollGamepad`. `PausePolling` suspends
  the SPI and clears `GINTMSK`, `HAINTMSK` and `GAHBCFG.GLBLINTRMSK` before
  entering the zero-MMIO paused state; `fail` and `DisableIRQ` permanently
  mask and unregister the SPI. DMA and descriptor DMA remain forbidden in both
  polling and IRQ modes.
- **Fail-closed retirement:** Disconnect, STALL, unexpected RX/PID, frame
  overrun, unexpected DMA enable or expired bounds permanently retire the host
  (retaining a read-only register snapshot and 8-command history for diagnosis).

## Multi-device PIO bus host (`MultiHost`)

`OpenMulti` implements the controller-independent `usb.BusHost` interface on
the same DE25-Nano PIO hardware contract (no DMA):

- **16-channel scheduler:** Discovers channel count (`HWCFG2`) and FIFO RAM
  depth (`HWCFG3`) at open time; reserves CH0 for synchronous Control IN/OUT
  transfers (`ControlTransfer`) and allocates CH1..15 to concurrent Interrupt
  IN/OUT and Bulk IN/OUT pipes (`OpenPipe`, up to 512 B HS Bulk MPS) with inline
  `[512]byte` RX/TX buffers and zero steady-state heap allocations.
- **Dynamic FIFOs & Multi-TT hubs:** Scales RX/NPTX/PTX FIFO partitions from
  `HWCFG3` (`512/256/256` words on Agilex 5 >= 1024-word RAM), enables Multi-TT
  (`SET_INTERFACE alt=1`) on protocol-2 HS hubs, and widens split SSPLIT
  scheduling across microframes `0..5`.
- **Zero-device boot & dynamic hotplug:** `OpenMulti` succeeds even with no
  device plugged into the root port, monitors root port connect/disconnect and
  hub Interrupt-IN status-change bitmaps inside `Service()`, emits
  `DeviceAttached` / `DeviceDetached` events, and isolates per-pipe STALL/CRC
  faults without tearing down sibling devices or the host controller.
- **GICv3 IRQ & pause/resume:** `EnableIRQ` / `EnableIRQGuarded` enables
  `HAINTMSK` across all active channels plus `GINTMSK.SOF` when waiting for
  periodic or split-transaction slots; `PausePolling` / `ResumePolling` cleanly
  halts and drains all channels for FPGA reconfiguration and `usbcontrol.BusHost`
  closure.

## Diagnostics and references

- `run.py keyboard --board qemu` exercises the shared normalized keyboard path
  under QEMU; `usbcheck` targets Nano bring-up (`--usb-input gamepad` selects
  `OpenGamepad`).
- Register and protocol behavior were cross-checked against Linux v6.12
  `drivers/usb/dwc2` (`hw.h`, `core.c`, `hcd.c`, `hcd_intr.c`) and `hub.c`,
  TinyUSB's slave IN toggle handler, and USB HID 1.11. Neither Linux nor
  TinyUSB code is linked into the native image.
