# Bounded read-only SDM flash identification operator

This prepares one TRWU8CBL diagnostic after archived phase1-only startup and
final Park. It is not a hardware result, backup or flash-programming approval.
CPU Park is prior evidence, not a fresh CPU observation in this attempt.

Fresh sixty-second parent/top USB stability, clean committed source, frozen
helper/input/tool hashes, current claim, free UART, exact USB node, private
single-kit server, fixed 3 MHz clock, mandatory vendor-first listing and a fresh
forced scan precede the fixed SDM sequence. The attempt is consumed durably
before starting the service. Failed admission cannot be retried with this fuse.

This separate experiment tests the documented Micron Quad I/O AFh read hypothesis
without changing mode. The prior 9Fh-zero attempt remains failed and archived.
There is no fallback to 9Fh or any other opcode. The ID is validated immediately,
before any further register read. Only CONFIG_STATUS, QSPI_OPEN, chip-zero SELECT, ID register 0xaf and status
register 0x05 reads, CLOSE and final CONFIG_STATUS are allowed. No array read,
image programming, flash/SD write, generic device opcode, controller handover,
reset or boot is part of the procedure. Failure retires the sequence; only one
close-only cleanup may follow while the same owned transport remains valid.
An uncertain close cannot be retried or reported as restoration. Bounds are
command 30 seconds, experiment 300 and host 330.

Injected tests pass for restricted arguments, response status/length, malformed,
duplicate/partial/wrong echoes, uncertainty at every command, close failures,
owner loss and one-use retirement. The first live CONFIG_STATUS must satisfy
the strict grammar before QSPI_OPEN. Models establish no hardware wire layout
or byte order. Identity is retained as raw word and candidate little-endian
bytes; part geometry and complete backup remain unqualified even on success.

The [manufacturer reference](https://onlinedocs.microchip.com/oxy/GUID-5474CCCD-F385-4AD7-9759-539BB1019357-en-US-10/GUID-4F13BD82-4678-46E3-A8DB-759A4A056317.html)
describes AFh for Micron Quad I/O identification; it does not prove this board
uses that protocol or part. The installed CLI documents its SDM options. The official
[Agilex 5 command table](https://docs.altera.com/r/docs/813773/26.1/device-configuration-user-guide-agilextm-5-fpgas-and-socs/operation-commands)
specifies exclusive open/select/read/close, and the
[JTAG example](https://docs.altera.com/r/docs/683313/current/an-936-executing-sdm-commands-via-jtag-interface/execute-command)
shows response framing. Ready/SEU masks match the vendor firmware header.
Offline inspection found an installed Tcl programmer package, but its metadata
says no executable supports it, so the live operator uses the documented CLI.
Exclusive mailbox identity across separate CLI invocations is an observed
question; every step must report complete SDM success.

Cleanup restores 15 MHz/automatic adjustment and exact USB permissions and stops
only the owned private service. UART is never opened. Factory card/flash and
the other kit are preserved. `operator-source.json` seals the selected inputs.
