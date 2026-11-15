# Bounded configuration START boundary trial

Matched image `8bc1ed9a9478e26d`, compiled from
`0ffcfe9bb9a350e8d8bbd8aba50ebb16e8c8c3bc`, adds only three constant UART
boundaries to the existing START observer. Phase 12 precedes the original
remapper call, phase 13 follows it, phase 14 follows response clearing, and the
original phase 5 remains the CANCEL return. Exact source reversal and current
compiled calls are verified. All memory descriptors, layout, protected producer
inputs, mailbox driver/header, actual device calls, arguments, guards and
deadlines remain unchanged. Prior `6c9e1aaecb129336` and every operation remain
consumed/retired after its successful secure FPGA_CONFIG_COMP reply and later
startup deadline. The exact stalled call remains unproven.

Use only the continuing exclusive claim on top TRWU8CBL, spare card installed,
factory card safe and restoration waived by the user. Bottom TRWU8J8G remains
excluded. No physical input action is requested. Require current admitted
inputs, committed operator sources, more than 30 GiB free, stable unique USB,
free UART and a verified prepared archive. Take the shared Quartus lock as
deano before sudo. Retain qualified fixed 3 MHz/auto-adjust 0 and both historical
programmer failures. Each recovery/native operation requires a fresh durable
one-use reservation and scan; no consumed image or grant may be replayed.

Require all 13 network uploads and fresh SD readbacks before native programming,
then complete original boot proof before the full seven-configuration,
three-launch/four-menu/tape cycle. Preserve native 450 s, outer 800 s, host 890 s,
initial Ready 100 s and tape 1 s bounds. Stop on the first failure with only the
original two-second passive tail. Independently verify original
15 MHz/auto-adjust 1, exact USB permissions, our private server gone and UART
free. Retain raw logs, consumed records, terminal archive and source bundle,
and append the actual result to the owned kit log. Boundaries locate progress;
they grant no hardware authority or ordinary appliance acceptance.

The attempt is terminal and retired. Fixed 3 MHz/auto-adjust 0 recovery/native
programming and all 13 uploads/fresh SD reads passed. MMU/private-DDR proof,
normal vector/decode restoration, matched loader and EL1/SD/FAT startup passed.
The existing FPGA_CONFIG_COMP command again returned 0 after doorbell 1 in two
polls. START phases 0 through 4 returned 0, followed by phase 12. Phase 13 and
all subsequent checkpoints were absent. The unchanged initial Ready 100-second
bound expired, followed by the original two-second passive tail. This narrows
the stall to the remapper-call interval; the exact instruction, actual callee
entry, runtime exception or unreturned bus transaction remain unproven.
No FPGA data WRITE, menu Ready, launch, tape or normal return was observed.

The original remapper contains one System Manager read and two conditional
writes, with no loop. Inspection of the actual linked tables confirms their
existing Device/NS/XN translation. A transaction-security mismatch is therefore
a candidate cause. The manual describes target firewall rejection for
incompatible security attributes and includes the System Manager among
controlled targets; its live firewall setting was not sampled. See
[firewall behavior](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/firewall-and-security?contentId=mGRpDpvDnBWJrlTo1TUEyw)
and [target controls](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/target-firewall-and-security?contentId=q7gA4rvcjP8ItYsnRqfrxg).
This supports an inference, not a confirmed remapper root cause.

Independent cleanup passed: original 15 MHz/auto-adjust 1, exact USB permissions,
our private server gone and UART free. Both operations are consumed, with no
replay. Historical programmer failures remain two; no new programmer failure.
The host is detached; a terminal firmware return or CPU park was not observed.
Next prepare and test a source-bound secure mapping for the original remapper
accesses, retaining physical addresses before MMU activation, original values,
ownership checks and all deadlines. Preserve every other peripheral mapping.
