# Interactive serial input

`UART.Poll(now)` reads at most 64 available bytes from the existing QEMU
PL011 console without waiting for input. It returns normalized key names
and printable ASCII text. The embedding UI decides whether text is a
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
