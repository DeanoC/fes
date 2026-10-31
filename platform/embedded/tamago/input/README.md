# Interactive serial input

`UART.Poll(now)` reads at most 64 available bytes from the selected console
without waiting for input: QEMU PL011 or the Nano firmware-selected UART. It
returns normalized key names and printable ASCII text. The embedding UI decides whether text is a
shortcut or goes to an active text field. Key names can be passed to
`tenfoot.CommandFromKey`.

Arrow/home keys use normal terminal escape sequences. A standalone Escape is
released after a 30 ms ambiguity interval, checked without blocking. Return,
Tab, Shift-Tab and Backspace are supported. This first adapter has no key-up,
key-repeat, non-ASCII or modifier-state protocol.

Use a QEMU stdio/PTY or Unix-socket serial backend to supply actual host
keystrokes. Automated checks can send the same bytes through the child
process's stdin. This is live input into the guest, but it is **not** a USB
host controller or a HID enumeration implementation. The PL011 address
`0x09000000` is specific to QEMU `virt`.

The optional `USBKeyboard` adapter consumes real eight-byte boot reports from
`usb.XHCI` (or another `USBReports` implementation). It produces the same key
names and text as terminal input, on press edges only. Releases are tracked to
recognize the next press; held-key repeat and non-US layouts are not implemented.
Shift modifies text and Shift-Tab. Caps Lock, Ctrl/Alt shortcuts, LEDs and gamepad
reports are outside this first adapter. See [the USB transport](../usb/README.md)
for its enumeration and controller limits.
