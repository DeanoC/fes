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
