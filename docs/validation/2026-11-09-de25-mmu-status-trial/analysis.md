# Physical before/after observations

Image `58a43a2afe6818d7` passed recovery, thirteen verified uploads/fresh SD
reads, native programming, the cold provider, fifteen MMU steps and the raw
activation record. It then reported the same asynchronous SError as the
preceding image: ESR `be000011`, SPSR `2cc`, SCR `238`, SCTLR `cd183f`,
TCR `80803520`, root `80046000` and MAIR `4400ff`.

Both 27-row inventories completed, including the guarded current RAS bank.
Every before/fault value is identical. DSU and OCRAM detection and interrupt
controls read zero; status and location registers also read zero. ECC manager
configuration is `83090`, initialization status `1`, pending status zero and
decoder status zero. RAS reports two records, current selector zero, features
`a9a6`, control zero, status/MISC0 zero and unchanged selector zero. The second
bank was deliberately not selected or inspected. No status was acknowledged.

These clear records do not diagnose the fault: the sampled detection controls
are disabled. Initialization status `1` also does not independently prove every
memory location or cache transaction healthy. A targeted diagnostic needs to
enable reviewed error recording with a checked baseline and restore its control
on success and terminal capture, while leaving sticky status untouched.

The new interrupted SP_EL0 is `51e40`, inside the fixed `50000..58000` stack
reservation; the handler SP_EL3 is `58000`. This removes the previous stack-bank
ambiguity and does not establish stack integrity. ELR `8001ce38` maps to
`tf_log+24`, again `STR X7,[SP,#152]`; delivery remains asynchronous, so this
instruction and FAR `5c780` are not proven causes. The matched disassembly is
retained alongside the report.

There is no returned SCTLR callback, private memory probe, restored normal
vector, native launcher or tape transfer. The raw-console replay exactly
reproduces the saved decoder, including both complete inventories and a terminal
closed admission state. No outgoing native command followed the failure.

Independent cleanup confirms original USB permissions, recorded 15M readback,
termination of the owned private server and free serial port. Both programming
operations are consumed and this image is retired. The top kit remains parked
with its spare card under the continuing exclusive claim and factory-restore
waiver; the bottom kit was untouched. Core-loading readiness remains open.
