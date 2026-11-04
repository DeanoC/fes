# DWC2 direct boot keyboard prototype

The Nano provider now opens USB0 (`0x10b00000`) through this polling PIO host.
It uses no DMA buffers, GIC or Linux service. The existing shared USB descriptor,
boot-report and normalized input decoders remain the application boundary.
QEMU retains its separate xHCI driver.

The prototype admits DWC2 2.x/3.x host-capable cores with dynamic FIFOs, at
least 256 FIFO words and an already configured 60 MHz ULPI PHY. Firmware must
release all channels, disable DMA, establish SoC clocks/reset/pinmux and prepare
the board's VBUS source and Type-C role. The driver validates those controller
capabilities, resets the core, partitions/flushes FIFOs, forces host mode, powers
and resets the root port, then enumerates one directly attached low/full-speed
boot keyboard. PHY and VBUS electrical operation remain untested on Nano.

Control transfers perform SET_ADDRESS, descriptor discovery, SET_CONFIGURATION,
SET_PROTOCOL and SET_IDLE. The host uses bounded, single-packet transactions
with endpoint data toggles and explicit channel retirement. Interrupt polling
never waits for device input: a fixed amount of FIFO/status work advances a
pending transfer, NAK retains the data toggle, and the endpoint interval gates
new attempts. All waits have time and poll limits, including frozen-clock cases.
Reports remain private until both transfer completion and channel retirement.

Disconnect, STALL, unexpected RX status/channel/PID, overflow, missing or short
reports, hardware errors and ambiguous timeout retire the host. It attempts a
bounded channel halt and refuses further hardware access through that instance.
The board wrapper permits one initialization attempt per boot. Hotplug recovery,
hubs, split transactions, high-speed endpoints, gamepads and HID output reports
are not implemented. The API has one polling owner; concurrent calls are not
supported.

`cmd/keyboard` is a serial-only bring-up image: initialize the selected backend,
print `FES_KEYBOARD_READY`, then print normalized key events. It needs neither
storage nor a working FPGA display. `run.py keyboard --board qemu` exercises
the same normalized path through an actual QEMU USB keypress. A Nano cross-build
requires the firmware profile's USB readiness and 60 MHz clock declaration.

Registers and transaction behavior were checked against the primary Linux
[v6.12 DWC2 sources](https://github.com/torvalds/linux/tree/v6.12/drivers/usb/dwc2):
`hw.h`, `core.c`, `hcd.c` and `hcd_intr.c`. The hardware-definition header is
Copyright 2004-2013 Synopsys, under GPL-2.0-or-later OR BSD-3-Clause. The Go code
is an independent exploratory implementation; neither a Linux driver nor a
vendor BSP is linked into the native image. The validation receipt records
reference file hashes. Register/device model tests prove software behavior,
not PHY timing, electrical safety or physical USB compatibility.

The controlled Nano entry point wraps this host in `usbcontrol.Source`. `Open`
returns a partial host on initialization or enumeration failure so its owner can
retain it through lease cleanup. Every transfer step rechecks polling PIO mode,
interrupt masking and descriptor-DMA disable. If DMA unexpectedly becomes
enabled, the host retires without rewriting or halting an uncertain channel.

The explicit Nano `usbcheck` build selects the source-bound secure provider and
`OpenKeyboardWithControl`, rather than asserting readiness at entry. It verifies
the cold context/pins before acquiring USB, confirms live readiness after driver
startup and stops after a bounded polling interval. This does not enable USB
for storage/Tenfoot profiles or implement the hub/split-transaction path.
