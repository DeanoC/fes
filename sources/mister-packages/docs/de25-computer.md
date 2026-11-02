# DE25 computer Avalon adapter

[`fes.fabric.de25-computer`](../packages/fabric/fes_fabric_de25_computer.yaml)
describes the 32-byte, peripheral-relative Avalon CSR adapter around the
existing `fes.computer` GP transport. Its definition is imported unchanged
from the selected board producer at FES revision
`79d616e85e19d65f0d645b67a85ffe7e142dcdfc`. The FES native
[computer driver](../../../platform/embedded/tamago/computer/README.md)
consumes generated Go constants alongside the shared GP ABI constants.

Identity/version, status, GPO, GPI and sticky fault registers occupy offsets
0, 4, 8, 12, 16 and 20. The reserved addresses are not safe probes: an undefined
access faults the adapter. GPO writes are atomic full-width submissions and
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
