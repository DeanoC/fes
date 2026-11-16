# Nano polling USB hub and split-transfer model

Base `0bc0be52d`; implementation `6ac487272`, reviewed capability checks
`fa76d3201`, bounded initialization `aaa45acfc`. This stage accesses no board
and establishes no physical USB acceptance. The prior physical Tenfoot cycle
remains evidence for its own frozen source and artifacts.

The real DWC2 PIO driver enumerates a bounded USB 2 hub tree, assigns device
addresses, powers/debounces/resets ports and selects one unambiguous boot
keyboard. It supports nested high/full-speed hubs and the default single-TT
alternate of multi-TT hubs. High-speed hubs route low/full-speed children through
start/complete splits. Complete splits send no OUT bytes; data stays private
until completion and channel retirement. NAK preserves input toggles; NYET
retries only the completion. Missed translator windows retire the driver.

The driver independently checks root-link state before arming. Hub ancestor
health advances in bounded control steps between keyboard packets. A changed
port, root speed, reset, disconnect or overcurrent retires the source instead
of adopting a replacement. Valid non-keyboard devices remain unconfigured;
unmatched-device errors include discovered USB IDs. Initialization and the
complete traversal share one four-second deadline inside the caller's
five-second context. Frozen-clock poll limits remain in force.

Race tests pass for `dwc2`, `usbcontrol`, `usb` and `input`. The independent
model tracks USB addresses, descriptor/control ordering, hub power/reset/status,
TT ownership, FIFO payloads, frame timing and explicit channel halts. Cases cover
normal/nested hubs, normalized input, source ownership/closure, NAK/NYET, frame
wrap/stall, overall startup expiry, malformed descriptors, multiple keyboards,
missing TT capability and path changes. Native actual-C/Verilated-FPGA integration
and `make check` pass. The broad parent/Nano/FPGA suites were not rerun because
this feature changes the private Go USB driver and its documentation.

The final native SDK, entry, BL31, matching loader and HPS-first composition
build successfully. The FIT is 2,682,768 bytes. The selected native bytes match
`aaa45acfc`; the actual ELF retains the hub/split/health paths. USB-only selection
matches across the entry, BL31 and loader; no SD/FPGA data permission is added.
Complete build outputs and receipts remain under `out/nano-usb-hub-20261104/`,
with hashes in `result.json`. Earlier development builds are retained separately.

Bounds are three hub levels, 15 ports per hub, 31 total ports/devices, one boot
keyboard and at most 64 bytes per packet. Gamepads, HID output, USB 3 hub
protocols, alternate-setting selection and hotplug re-enumeration remain open.
These are implementation limits, not USB or physical power qualification.
Physical split timing, PHY/VBUS, actual hub compatibility and input still need
a bounded test on TRWU8CBL. Combined USB/storage/core ownership, ordinary core
and ROM/media admission, HDMI and native Ethernet remain separate readiness
gates. No DE10-style readiness claim is made here.

Primary references are Linux v6.12
[`hcd.c`](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c),
[`hcd_intr.c`](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd_intr.c),
[`hw.h`](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hw.h),
[`hub.c`](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/core/hub.c) and
[`ch11.h`](https://github.com/torvalds/linux/blob/v6.12/include/uapi/linux/usb/ch11.h).
Four downloaded files are hash-pinned; the header was checked through the
primary web view after its direct download returned HTTP429. These sources
provide protocol/register reference only. No Linux driver or guest Linux is
linked into the native image.
