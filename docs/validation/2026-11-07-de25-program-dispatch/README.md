# Joined firmware menu/computer dispatcher, 2026-11-07

Implementation `e64836112d4e2b1ecaad736c29703d465ccf7401` adds a default-off composition of the actual
menu and computer session engines in the owned SiP entry. The secure
configuration-completion callback verifies captured role/ABI/resources, exact
owner/generation and byte count, healthy inactive identity, prior session
phase and live closed bridges. It changes active role/identity only before
the configuration engine publishes successful completion. Descriptor capture
keeps the prior role and return policy. Invalid activation permanently retires
the dispatcher; raw diagnostic probes cannot bypass selected sessions.

The fixture runs the actual SiP dispatcher, descriptor decoder, configuration
engine, both session engines and bridge operations. Four five-load sequences
cover alternating roles in both starting orders and repeated loads of each
role. Additional cases cover failed completion, open, ROM and master-stop,
activation replay, sixteen invalid activation states and six incomplete
compile selections. Expected role-specific held-data versus drained-window
closures and selected identity updates are checked. Platform ownership/digest,
SD-return, reader and ROM I/O are injected; this is software evidence.

All 12 selected checks passed. Parent tests: 1641 with
3 skips. Separate misteross suite: 1595 with
79 skips; these counts overlap and are not additive. The
selected checks include native SD, real native C/RTL/Go/Tenfoot integration,
producer identity, DE25 control RTL and firmware identity/drain integration.
The first parent run had one failure in the unchanged entry-memory test,
with a SEGV inside the host ASan allocator. The focused rerun and identical
full parent recheck passed. The initial failure log, rerun and matching base
source hashes are retained; the exact cause is unconfirmed and no memory
guard or test was changed. The exact inputs, outcomes and limitations are
in [result.json](result.json).

The exporter installs and hashes the new include. Current board builds do not
enable mixed roles; the strong cold provider does not call this activation
hook yet. Its per-role identity/master-stop/ROM/SD-return policy must be
connected before enabling a source-matched combined native/entry/SPL/firmware
selection with the full8MiB memory exclusion. Actual Tenfoot menu/core/menu
presentation and combined physical acceptance remain open. No board or card
was touched, and this does not qualify timing or full appliance behavior.
