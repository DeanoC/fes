# DE25 held-lock cold reset entry

The source-derived recovery gap in the [matched timing candidate](../2026-11-14-de25-publication-matched/README.md)
is a recursive lock: failed source retirement parks inside the owned dispatcher,
while ordinary PSCI reset acquires that same lock during invalidation.

The platform now separates held-lock invalidation from ordinary invalidation.
The ordinary path still acquires and releases the lock. The new trusted cold
entry consumes one per-boot attempt, revokes ordinary boot/bridge authority,
checks cached owner/epoch/bridge-generation binding, then calls a separately
supplied recovery-scope observation. That observation must independently prove
the live CPU, held lock, source reservation and master containment after
revocation. An expired retirement guard or ordinary boot `owns()` cannot supply
this proof. There is no NS request for this entry.

Changed identities, lost ownership, absent or nonboolean scope results, revived
ordinary authority and reentrant requests reject without a reset request. Every
outcome keeps the lock and reservation; repeated requests cannot post again.
The fixed provider in the generated PSCI unit preserves the vendor cold-path
cache sequence and calls `mailbox_reset_cold()` directly. It cannot select RSU
or warm reset. A returning mailbox call is an uncertain failure, never reset
completion, source release, memory reuse or permission to resume dispatch.

The sanitizer fixture executes the actual controller/invalidation functions
and generated provider. Its 27 cases run with cache maintenance selected and
unselected (54 executions), including ordinary invalidation, ownership drift,
reentrancy, a preexisting boot error, source retention and returning transport.
Cache instructions, the lock and mailbox primitive are injected; neither
physical cache effects nor SDM reset are qualified. Nine cold-boot tests, two
owned-service tests, two MAC-dispatch tests, four call-trace tests and the
repository consistency check also pass.

This is an integration entry, not an installed recovery policy. Existing source
profiles do not invoke it automatically. The completed-source terminal supplier,
an explicitly selected diagnostic, loader redelivery and fresh post-reset
device/epoch admission remain to be connected and qualified. No hardware, card,
QSPI, factory-card or bottom-kit operation is performed for this change.

Source `17b4675ed20a070a121f7fee44f81f2d4c90c63f` also passes a fresh
normal SDK, CPU-entry, BL31, SPL/U-Boot, HPS-first composition and complete
native file-admission build in frozen `de25-held-reset-proof`. The chain is
`e0a5d81c60828b7077261b4fdccb35267fa8e3a84de08eee87404f055671c04c`, boot tag
`944bdea49a7f42d8`, FIT
`a298840f42c297259ac5b08996e43b2176babeb1218b39edd9f9399be5d6f13a`
(8,542,928 bytes). Core payloads remain unchanged. Composition holds the shared
Quartus Pro lock and performs no synthesis, fitting or programming.
[matched.json](matched.json) binds the full producer/file-admission checks.

The actual installed controller/provider bodies match the new sources, and
both AArch64 objects contain their reset entry. The normal linker discards
these uncalled functions: this image has no installed recovery policy. Its
NOBITS end remains `0x5ff40`, with 192 bytes spare and four unchanged 8 KiB
stacks. This does not establish the footprint or whole-EL3 stack usage of a
future image that actually selects the recovery entry.

A separate [vendor-function model](vendor-model.json) repeats the 54 case
executions with the actual pinned `mailbox_reset_cold()` body. It verifies the
fixed job-15, command-`0x47`, zero-argument nonurgent request after revocation,
including a returning failed transport. The primitive is independently checked
against the freshly built vendor export. Mailbox transport, cache instructions
and the lock remain injected; physical reset and post-reset admission remain
unqualified.

The complete matched outputs, source bundle, models and completed check logs
are retained at
`/home/deano/fes-retained/de25-held-reset-20261114-r1/matched.tar.gz`, SHA-256
`e4b8e5a57fa7edf0a210ccc2587db60d2e917d9a0b217745452287e8689f178a`.
All 46,732 archive entries are independently read back for bytes, hashes, modes
and link targets. [retention-proof.json](retention-proof.json) records that
verification and the actual object/layout checks. TRWU8CBL's last verified
state remains the original native baseline at final Park; this checkpoint
performs no hardware operations and does not qualify repeated loading.
