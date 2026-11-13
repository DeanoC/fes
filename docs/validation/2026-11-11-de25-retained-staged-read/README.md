# Read-only retained records and existing staged packages

Against `39c83e79a`, the source-controlled recovery group verifier accepts an
explicit tuple of up to 32 independently expected installed-record byte strings.
It checks their content-derived filenames before the fixed ten- or fourteen-role
staged-source set, within one new UploadSession. It rejects duplicate expected
records, oversized or mutable buffers and invalid tuple shapes before commands.
Each record also passes the existing installed-record envelope check before its
own commands. Every read must retain the same session token, protected-card and
scratch-map checks. The session closes on success or failure; no old upload ledger
is adopted, and no network transfer, SD write or activation is performed.

The group receipt distinguishes verified retained bytes from complete native
catalog admission. It does not prove a fresh physical recovery boot, native CID
admission, an exhaustive installed-file inventory or persistence. A failed staged
read leaves the overall group failed even if earlier retained records matched.
The later native operator must independently verify its initial title list and
admit every package before any launch.

The focused installer suites pass 51 tests. Affected checks pass, including
1,956 parent tests with three skips, planner checks, whitespace and generated
consumer consistency. New tests cover retained-record ordering/session binding,
missing or corrupt records, invalid expectations, scope/session substitution and
partial reads. Real sealed/staged bytes pass in three separate controlled cases:
the existing prior ZX81 record alone; refusal when future trial records are
requested from a prior-only model card; and hypothetical post-install discovery
with all three independently expected records supplied. Predicted new records
are not reported as present on the physical card.

The private prototype at `out/nano-existing-staging-install-20261111/trial`
replaces the earlier upload phase with this read-only recovery check, before the
same explicit two-core installation/launch/return cycle. The actual read operator
runs with injected consoles and temporary receipts in its model tests. Both
record and staged-file corruption fail without network or writes; the native
receipt gate rejects cross-session, source, boot-receipt and scope substitutions.
UART exchange and two-core receipt models pass. Import resolution and AST checks
verify recovery boot, source readback and native phases in that order. No uploader
module or write phase is present. Two initial structure-check harness assumptions
were corrected and their failed logs retained.

All 2,131 selected SDK hashes, 26 native composition inputs and 28 recovery inputs
still verify. The existing fc2cec132701a0d8 image can be reused; this host-reader
change does not rebuild or qualify it. The new operator remains a prototype with
no live preflight, programming grant or hardware execution. Its fresh complete
file/source admission, connection/UART/claim checks and physical attempt remain
open. The previous USB startup failure stays terminal and retained. No board
operation was performed while awaiting the user's hub/upstream check, and no USB
timeout or diagnostic requirement was changed to bypass that failure.
