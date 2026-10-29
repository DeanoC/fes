# DE25 computer transport diagnostic

`fes.fabric.de25-computer` 1.0 describes a standalone Avalon bridge to the
existing `fes.computer` 1.0 mailbox. It introduces no programming profile or
Linux physical address. Its peripheral-relative base is `0x10300`, span 32
bytes. Control, mailbox, HID mapping, tape storage and the machine share the
preserved GHRD 100 MHz clock. Rational enables at 13/200 preserve the 6.5 MHz
ULA and 3.25 MHz CPU; HDMI remains on its separate 74.25 MHz clock.

| Byte offset | Register | Access |
| --- | --- | --- |
| 0 | Identity `0x44325a58` | Read |
| 4 | Version `0x00010000` | Read |
| 8 | Status | Read |
| 12 | Atomic GPO command / held payload | Read/write |
| 16 | GPI response | Read |
| 20 | Sticky fault reasons | Read |

Status bits are READY=1, BUSY=2 and FAULT=4. BUSY compares the held GPO
request toggle with the mailbox ACK; it does not mean the cassette loader
is busy. The computer mailbox powers up holding execution. Its existing
identity, execution, HID keyboard and CRC/chunked tape operations retain their
meaning. This target advertises keyboard, fixed 720p video and read-only ZX81
tape delivery; it does not advertise audio, gamepads, SAVE or session display.

A submission is exactly one aligned, fully byte-enabled 32-bit GPO write,
with a new request toggle, while idle and fault-free. The stored payload
remains unchanged until ACK; a preload with the old toggle is invalid. A
busy write, partial GPO write, reused toggle or invalid/reserved/misaligned
access is rejected with an Avalon error and permanently faults the transport.
Fault reason bits are reset=1, partial write=2, busy write=4, toggle=8 and
address/access=16. Faults have no software clear; reconfiguration is required.
Reads and writes complete with registered responses outside reset, including
faulted reads and rejected writes. Faulted writes cannot change the payload.

GPO and mailbox ACK state survive a later peripheral reset. A first-release
marker and an asynchronously cleared validity register latch such a reset
as a permanent fault, even if the clock was stopped. Fault/reset holds the
CPU and masks tape writes and presentation. It does not independently clear
mailbox transport state. The mailbox may complete an already accepted command
internally after a fault; no CPU or tape authorization is restored by that ACK.
Pixel-clock loss blanks HDMI without resetting the computer or control path.

A client checks fault before and after each exchange, validates identity and
capabilities, submits one full word, checks the held payload and waits for its
matching ACK. A software polling deadline cannot bound a physical bus access;
an uncertain command is never retried automatically. The Python diagnostic
client's regular-file backend is for offline testing. An admitted native MMIO
backend, firmware/device binding and global reset fencing remain future work.

The new keyboard/tape path has no DMA master or DDR dependency. The composed
SOF retains the GHRD's existing HPS, LPDDR4, debug paths and inherited DDR probe.
Warm-reset ACK remains inactive; local containment is not a global bus fence.
Compilation, simulation and generated route checks confer no physical acceptance.
