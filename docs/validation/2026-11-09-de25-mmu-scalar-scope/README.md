# Scalar live MMU observation

The last physical image `bd103b1a199bc2be` reached Tenfoot Ready, submitted
four menu frames, drained/closed that menu and configured ZX81. Its first
launch failed in initial tape transfer with LIMIT(-6), at512/917 chunk progress.
That image and both operations remain terminal, consumed and retired.

This change removes the full callback-table construction/copies from every
live MMU scope check. Scope now takes its observation context and callback
as scalar arguments; the cold enable operations table remains unchanged.
All predicates, fresh observation calls and first-failure latching stay.
The MMU object remains in its existing secure NC OCRAM storage, and the
original one-second tape budget, all ownership/CPU/USB/register guard checks,
retry/reset behavior and other producer inputs remain.

The complete linked wrapper is eight instructions/32 bytes, with no stack
frame, memcpy or callback-table copies. All six production tables and their
linked layout are byte-identical to the preceding image, and the mutable
120-byte MMU state remains at0x5c780. Exact source reversal proves that the
scope body changes only its observation parameter spelling and call, while
cold enable and every other engine body stay unchanged.

Fifty focused MMU tests and four actual cold-platform tests pass, including
fresh observation, callback/CPU/owner failures and permanent failure states.
The AArch64 BL31 and matched bootloader build pass; the FIT remains8387240
bytes within the original8MiB upload limit. Prospective native image ID is
`38c6b2fe551dcd88`. The selected native integration checks and final composition pass; a
fresh bounded hardware trial remains. This is off-board evidence only; the
previous physical result does not qualify the new image or ordinary core loads.

Builds use an isolated checkout on chip-30x0-ai-dev. A private mount view
provides the original canonical paths without symbolic links, retaining the
native path-admission rule. Exact pinned board/overlay Git objects were
copied from Powerboat and independently retained; the shared system received
only the missing libssl-dev package at its already-installed runtime version,
with zero package upgrades/removals. Failed environment preparations remain
recorded. The earlier host-only private-state prototype is unprogrammed and
retired; it is not part of this selected source change.

Powerboat cleanup is paused at the user's request. Its archive passed complete
file/mode readback before any deletion; a later permission error stopped the
partial deletion. The verified archive, pause record and logs are retained,
and no cleanup restarts are authorized without a user resume.

The additional full FPGA campaign on the remote host stopped at its300-second
cap with unrelated optional tool failures. Focused replay accounts for all ten
observed failures: stock Yosys0.33 rejects existing HPS atoms or C64 function
syntax; Verilator5.020 treats existing width/case warnings as fatal. The three
Verilator cases pass unchanged on Powerboat5.032. No FPGA/shared-definition
source changed: exact2553-file membership/hashes, the214-module catalog and
original complete prefix/suffix receipts are rechecked for scoped reuse.
Current native/FW/identity/RTL integrations0..8 passed separately. The remote
full campaign remains failed and is not presented as a green FPGA regression
run or a new all-core synthesis qualification.
