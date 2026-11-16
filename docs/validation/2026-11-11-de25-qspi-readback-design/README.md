# Read-only factory-flash identification and backup path audit

This is an off-board interface audit after the passing phase1-only hardware test.
No flash command or live read was issued. It provides no programming approval,
backup, hardware acceptance or rollback guarantee.

The installed Quartus Pro 26.3 help confirms `--cmd_code` and `--cmd_param` for
sending SDM mailbox commands through JTAG. Its help snapshots and hashes are in
`out/nano-qspi-readback-20261111/` and `path-audit.json`. The `R` programmer
operation is erase. `--file_system=read` reads a named flash filesystem entry;
it is not a complete raw-flash backup.

The official [Agilex 5 command table](https://docs.altera.com/r/docs/813773/26.1/device-configuration-user-guide-agilextm-5-fpgas-and-socs/operation-commands)
describes exclusive open, explicit chip selection, register/data reads and close.
It limits data reads to 1024 words and warns that a partial read may report OK.
[AN936](https://docs.altera.com/r/docs/683313/current/an-936-executing-sdm-commands-via-jtag-interface/execute-command)
demonstrates JTAG SDM read commands and response-header/data framing. The exact
installed CLI syntax is independently confirmed by local help.

The proposed operator will allow only open, select chip zero, read JEDEC ID or
status, bounded aligned data reads and close. It must reject every write/erase,
generic device opcode, direct-controller handover, RSU/reconfiguration and flash
programming operation. Stock recovery `sf probe` remains excluded because its
compiled driver enables unlock on probe and can enable quad mode.

Before a live run, implement and test strict command/response framing, ownership,
one-use admission, terminal uncertainty and bounded close-only cleanup. Establish
whether separate CLI invocations retain the same exclusive mailbox client;
otherwise use a documented persistent transport. Independently identify the
part/capacity, verify byte order, then obtain two matching complete array reads
including unused ranges. Preserve status separately and do not imply coverage of
unread OTP/security areas. A successful status header alone cannot verify a full
read. A source-bound archive and concrete usable rollback image are required
before requesting approval for a reviewed flash change.

These readback gates are still open. The passing phase1-only volatile test remains
separate from autonomous QSPI power-on boot and broader appliance acceptance.
