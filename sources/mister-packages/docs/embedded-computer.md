# DE25 computer Avalon adapter

[`fes.fabric.embedded-computer`](../packages/fabric/fes_fabric_embedded_computer.yaml)
describes the 32-byte, peripheral-relative Avalon CSR adapter around the
existing `fes.computer` GP transport. Its original map comes from the selected
board producer at FES revision `79d616e85e19d65f0d645b67a85ffe7e142dcdfc`.
The current overlay extends its register version to 1.1 with independent
read-only fetch observations. The FES native
[computer driver](../../../platform/embedded/tamago/computer/README.md)
consumes generated Go constants alongside the shared GP ABI constants.

Identity/version, status, GPO, GPI and sticky fault registers occupy offsets
0, 4, 8, 12, 16 and 20. Version 1.1 adds a 32-bit fetch count at 24 and its
sampled 16-bit last-fetch address at 28. Reading the count captures the matching
address for a later address read. The counter observes rising CPU bus clock
edges with M1, MREQ and RD active, wraps modulo 2^32 and initializes only on
FPGA configuration. Hold, GP traffic and transport reset/fault state do not
clear or advance it. It measures fetch bus activity, not retired instructions,
correct emulation or video. Both registers reject writes. An undefined access
faults the adapter. GPO writes are atomic full-width submissions and
must toggle bit 31 exactly once; a partial write, busy write or repeated toggle
faults. Avalon completion acknowledges submission, and the separate GP ACK
acknowledges command execution. GP errors still require consumer handling.

The first reset release arms the transport epoch. A later hardware reset
permanently poisons that epoch until FPGA reconfiguration. Execution Hold is a
mailbox command and does not reset the transport. Status, GPO/GPI parity and
sticky causes must agree before a command is accepted.

The diagnostic ZX81 tape capacity is 16375 bytes, distinct from the general
ABI maximum. Its internal RAM introduces no additional HPS DDR master.
The peripheral offset is not a CPU physical address. This definition confers
no bridge ownership, boot proof, hardware acceptance or runtime admission.
