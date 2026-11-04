# DWC2 polling keyboard and hub prototype

The Nano provider now opens USB0 (`0x10b00000`) through this polling PIO host.
It uses no DMA buffers, GIC or Linux service. The existing shared USB descriptor,
boot-report and normalized input decoders remain the application boundary.
QEMU retains its separate xHCI driver.

The prototype admits DWC2 2.x/3.x host-capable cores with dynamic FIFOs, at
least 256 FIFO words and an already configured 60 MHz ULPI PHY. Firmware must
release all channels, disable DMA, establish SoC clocks/reset/pinmux and prepare
the board's VBUS source and Type-C role. The driver validates those controller
capabilities, resets the core, partitions/flushes FIFOs, forces host mode, powers
and resets the root port, then enumerates one boot keyboard, directly or through a bounded USB 2 hub
tree. High-speed hubs use transaction translators for low/full-speed children. PHY and VBUS electrical operation remain untested on Nano.

Control transfers perform SET_ADDRESS, descriptor discovery, SET_CONFIGURATION,
SET_PROTOCOL and SET_IDLE. The host uses bounded, single-packet transactions
with endpoint data toggles and explicit channel retirement. Interrupt polling
never waits for device input: a fixed amount of FIFO/status work advances a
pending transfer, NAK retains the data toggle, and the endpoint interval gates
new attempts. All waits have time and poll limits, including frozen-clock cases. Initialization
and the complete hub traversal share a four-second deadline, inside the native
caller’s five-second startup context; per-request limits cannot extend it.
Reports remain private until both transfer completion and channel retirement.
Only IN-data receive entries supply payload byte count and PID. Completion and
channel-halted entries may retain those fields from a preceding packet; the
driver consumes no additional FIFO payload for them. HCINT completion, a real
IN-data entry and channel retirement remain required before reporting input.

Disconnect, STALL, unexpected RX status/channel/PID, overflow, missing or short
reports, hardware errors and ambiguous timeout retire the host. It attempts a
bounded channel halt and refuses further hardware access through that instance.
Invalid receive entries retain a non-destructive GRXSTSR sample, the one popped
GRXSTSP value and the surrounding interrupt/channel/port/reset registers. Status
zero remains an error. This snapshot diagnoses the first physical Nano failure;
it does not establish its cause or make a failed channel safe to reuse.
The board wrapper permits one initialization attempt per boot. Hotplug recovery,
gamepads, HID output reports, USB 3 hub protocols and hotplug re-enumeration
are not implemented. Hub paths are checked and retired on change. The API has one polling owner; concurrent calls are not
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
for storage/Tenfoot profiles or establish physical hub/input qualification.

Hub discovery admits at most three hub levels, 15 ports per hub, 31 total ports
and 31 addressed devices. It powers ports, requires stable connection, checks
reset completion, validates descriptors and rejects multiple boot keyboards.
Valid non-keyboard devices remain unconfigured. Multi-TT hubs use alternate
zero; no alternate setting is switched. The selected keyboard and translator
route stay private until the complete topology is admitted.

Low/full-speed transfers behind a high-speed hub use start/complete splits
with explicit hub/port routing, ACK/NYET/NAK handling and channel retirement.
OUT payload bytes are sent only with start-split. A complete-split never repeats
them. Transactions start early in a USB frame; missed completion windows retire
the host. High-speed frame intervals use 125 microseconds; keyboard intervals
use the speed-specific USB encoding. Split progress gets at most 2 ms/4096
steps per packet poll. Periodic hub-path health checks advance through bounded
control steps; one public poll spends at most that budget plus one final packet
step. No control wait blocks until user input. Disconnect, reset or overcurrent
on an ancestor path retires the host, without adopting a replacement device.

The model independently implements device addresses, hub power/reset/status,
transaction-translator ownership, split timing, FIFO contents and channel
halts. Tests cover nested high/full-speed hubs, keyboard selection, normalized
input, frame wrap, NAK/NYET, bad descriptors, stalled frames, ambiguous devices,
ancestor changes and firmware-owned source closure. These are model results;
physical split scheduling, hub/keyboard compatibility and Type-C power still
require a bounded Nano test. Linux v6.12 `hcd.c`, `hcd_intr.c`, `hw.h` and
`usb/core/hub.c` are the primary protocol references. Their version/hashes are
retained with validation. No Linux driver is linked into the image.
