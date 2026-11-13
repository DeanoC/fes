# QEMU USB boot keyboard prototype

This package contains a small polling xHCI host adapter and controller-independent
USB descriptor / HID boot-report decoding. It enumerates QEMU's `usb-kbd` through
real USB control transfers, selects configuration and boot protocol, then receives
interrupt-IN reports. QMP keyboard injection feeds the emulated USB device; it is
not translated directly into application commands by the runner.

The QEMU board owns PCI discovery, BAR assignment and DMA memory setup. The
adapter accepts that mapped controller through `OpenXHCI(base)`. Call
`PollKeyboard` regularly and pass the returned eight-byte reports through
`input.NewUSBKeyboard`. The normalized input adapter tracks press/release edges
and maps US keyboard navigation and printable keys; it does not implement held
key repeat or international layouts.

The bounded controller profile supports one directly attached USB 2 boot
keyboard, one configuration, 32-byte contexts, 4 KiB pages and no scratchpad
buffers. It reserves 64 KiB of the board's existing noncacheable DMA arena.
Control commands have deadlines and yield to the cooperative runtime; interrupt
polls consume at most 16 events. Transfer and event rings implement cycle-bit
wrap. No interrupts, hub routing, device replacement, suspend, bulk transfers,
USB storage, mouse, gamepad transport or general report-descriptor interpreter are
implemented. Unsupported descriptors and controller capabilities fail explicitly.
Short control transfers fail; this profile requests the exact descriptor size.

All DMA allocations remain owned until reboot, including on error. Disconnect
and controller errors are sticky; the caller must stop using the keyboard and
restart the prototype. There is no close/reuse path which could free memory
while the controller can still access it.

This is **not a DE25-Nano USB controller port**. No DWC2 host-mode selection,
PHY, reset, clock, power, interrupt, cache-coherency or board routing behavior is
inferred from QEMU. A board port must establish those resources before adapting
the xHCI transport, and must check the controller capabilities against this
restricted profile.

`ParseGamepad` is a separate controller-independent HID 1.11 parser used by
the Nano DWC2 gamepad transport. It supports one Joystick/Game Pad application,
absolute variable X/Y/Z/Rx/Ry/Rz axes, up to 32 buttons, four/eight-position
hats, report IDs, signed fields and constant padding. It bounds descriptors to
4096 bytes, input reports to 64 bytes and nesting/report-ID counts. Invalid
reports preserve the complete prior state, including fields belonging to other
report IDs. Unsupported arrays, relative controls and ambiguous layouts fail.
Tests use synthetic descriptors; the connected 081f:e401 schema is not captured.

`input.NewUSBGamepad` adapts that state to menu press edges using an explicit
confirm/back button mapping and an axis deadzone. Its state snapshot is available
for a future core-specific route. It does not fabricate keyboard reports, and
neither this parser nor its model tests establish physical input acceptance.

References used for implementation:

- [Intel xHCI specification, revision 1.22](https://www.intel.com/content/dam/www/public/us/en/documents/technical-specifications/extensible-host-controler-interface-usb-xhci.pdf): sections 4–6, register / context / TRB ownership and control transfers.
- [USB-IF HID 1.11](https://www.usb.org/sites/default/files/documents/hid1_11.pdf): boot protocol and keyboard reports, appendices B and C.
- [QEMU USB documentation](https://www.qemu.org/docs/master/system/devices/usb): xHCI and virtual keyboard configuration and USB packet capture.
- [QEMU 10.0.2 xHCI implementation](https://github.com/qemu/qemu/blob/v10.0.2/hw/usb/hcd-xhci.c) and [HID device descriptors](https://github.com/qemu/qemu/blob/v10.0.2/hw/usb/dev-hid.c): emulated capability and device profiles.
