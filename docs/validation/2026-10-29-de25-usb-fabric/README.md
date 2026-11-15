# USB input, cooperative rendering and fabric lifecycle model

Exploratory follow-on to `84acd2ded397ea2b43ddcac03f780f0605e18679`.
All device evidence is QEMU-only. No DE25 board, shared kit or physical FPGA was
used. The production runtime and its package/shared definitions are unchanged.

## USB tenfoot integration

[The combined receipt](usb-tenfoot-result.json) records a native AArch64 image
booting on QEMU with an xHCI controller and USB boot keyboard. The controller
performs slot/address setup, real USB descriptor requests, configuration and
boot-protocol selection, then interrupt-IN transfers. QMP sends keys to the
emulated USB device; the runner does not translate them to tenfoot commands.

The first boot enumerates VID `0627`, PID `0001`, interface 0, endpoint `81`,
eight-byte reports. Tenfoot fetches the real private FogCast catalog, receives
Right then `l` from USB, moves focus and saves Shelf to FAT32. A second cold QEMU
boot enumerates the keyboard again and restores Shelf from that same disk.
[First](usb-boot-1.log) and [second](usb-boot-2.log) console logs, a
[USB packet capture](usb-boot-1.pcap), and actual QMP screenshots
([Shelf](usb-shelf.png), [restored](usb-restored.png)) are retained.

A [separate ring-wrap diagnostic](usb-wrap-result.json) sends 300 presses and
300 releases, observes 1,200 interrupt reports, wraps both event and transfer
rings, then disconnects the keyboard and observes the sticky failure. Its
[console](usb-wrap-console.log) and retained USB capture bind the result.

The restricted host supports one directly attached USB2 boot keyboard. Hubs,
gamepads, mice, USB storage, reconnect and a general HID report interpreter are
not implemented. Disconnect errors are sticky and retain DMA ownership until
reboot. Nano DWC3 host-mode, PHY, clocks, power, reset and interrupt routing still
require a board port. The runner retains an explicit `--input uart` diagnostic
mode; absent USB never silently selects that mode.

The QEMU `highmem=off` device tree confirms PCI ECAM at `0x3f000000` and a low
PCI memory window beginning at `0x10000000`. `OpenUSBPCI` admits only the expected
QEMU xHCI device/class at PCI 0:1.0 and assigns its BAR while decoding and bus
mastering are disabled. These are emulator addresses, not DE25 constants.

## Cooperative renderer

[The paired renderer/network receipt](renderer-network-receipt.json) retains
frozen baseline and candidate source/ELF identities. The baseline stalls or
loses connections when the actual 720p software renderer and ramfb copy run
alongside TCP transfers. The candidate yields every four raster/scanout rows
and splits large copies into 32 KiB chunks. All 30 exact 192 KiB responses pass
with the same five-second client timeout; the pixel hash is identical to the
baseline. The Linux helper compiles away, preserving existing pixels and copy
behavior. This establishes this workload, not a general real-time guarantee.

## Fabric model

The [separate native image](fabric-result.json) runs the same eleven lifecycle
invariant checks as the host suite. Its [console](fabric-console.log) records
admission retention, save-before-replacement, single-use commands, identity
before release, abandoned/failed frame ownership, uncertain-save retention and
bounded recovery. A further host regression retains the exact issued command
buffer after an unknown program result.

This is an executable software specification. `CompleteModel` supplies simulated
observations, not physical completion proof. No FES package or real bitstream is
admitted, no GPIO/MMIO FPGA protocol is used, and no hardware is configured.
The controlled clock does not establish physical timing. Input neutralization,
real saves, AXI drain, SDM configuration and hardware identity remain separate
production-runtime and board work. Tenfoot Play remains unavailable.

The [repository checks](repository-checks.json) pass, including 1,065 parent
regressions, generated consumers, FogCast Go and browser suites, and dead-code
analysis on Linux amd64/ARM. Focused race checks and an independent scoped
USB/PCI/input/model review also pass. The explicit UART diagnostic mode was
retested across the same two-boot preference flow.

## Reproduction

Use [the native build/run guide](../../../platform/embedded/tamago/README.md)
for the USB two-boot test. Use `run.py fabric` for the separate lifecycle model.
The module/cache/compiler stay pinned; no new dependency or production toolchain
pin is introduced. The [USB component guide](../../../platform/embedded/tamago/usb/README.md)
and [model guide](../../../platform/embedded/tamago/fabricmodel/README.md)
state their full limits.
