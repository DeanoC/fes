# DE25 repeated-source firmware prototype

The real selected firmware service and SMMU supplier now support a new source
after consuming the previous completed transfer's source release. Sanitized
software integration passes twelve successive MENU/computer generations, with
changing lengths and digests, actual vendor mailbox/DATA functions, retirement,
continuation, CONFIG_STATUS and stopped-SD return checks. Forty rejection cases
pass, including changes during table writes and an unexpected mailbox reply.

This is software evidence. Exported profiles still set
`FES_NANO_SDM_SOURCE_RELEASE=0`. There is no newly admitted boot profile, linked
BL31, whole-stack qualification, matched boot image or hardware trial. The
[earlier format-14 hardware result](../2026-11-14-de25-retained-secure-reader-physical/README.md)
remains first-MENU evidence for its recorded artifact. It does not establish
repeatable loads through this new path. Ordinary Linux defaults are unchanged.

## Ownership and transitions

The service admits another prepare only from its actual completed and released
record. It pins that record across the platform preparation and new ownership
callback, requires the same owner/epoch and a newer generation, then installs a
fresh transfer clock and ledger. Reentrant preparation and all errors stay
terminal. The last consumed success is retained privately for diagnostics; it
is never used to authorize a new source or a retry.

The platform requires the actual release for the exact rearmed configuration,
native return provenance, stopped devices, idle vendor descriptors and credits,
empty mailbox/parser/FIFOs and the previous private controller/retirement
engines. MENU and computer returns retain their distinct generation advances
and window/held-data closure predicates. Two private slots preserve the real
previous engine address during the next preparation. Only an older successful
slot can be reused; a failed controller is never cleared.

Warm preparation checks the disabled SMMU and unchanged empty queue cursor,
reads all old table words, writes and verifies the next source's canonical
tables, runs all six invalidations from the owned cursor including wrap, and
enables the next read-only SID10 map. It keeps Non-secure ABORT set, preserves
queue bases and consumer history, and clears no queue, event or error. Fresh
Secure-bank observations continue to require the sealed reader's bypass state.
Retirement then uses the actual new setup cursor and its proved initial abort
state. Cold preparation keeps its existing zero-origin recipe and deadlines.

## Validation and limits

- The complete focused run passes 36 tests with no skips. After the final
  diagnostic-history retention change, the actual warm service, release
  supplier and profile tests pass again: eight tests with no skips.
- Portable models pass 64 warm setup/retire/continue generations, 256
  retirement generations and 384 command generations, with wraparound and
  mutation/rejection checks. The selected fault-only cold API also passes.
- The joined warm-service model passes twelve generations and forty negative
  cases. It executes the real selected service/supplier, vendor mailbox/DATA,
  configuration engine, return predicates and SD lifecycle. Source hashing,
  descriptor capture and completed fabric/activation state are injected; this
  is not a real package, ROM, HDMI, keyboard or complete native-boot test.
- Nine real AArch64 vendor objects compile with the release/rearm supplier
  manually enabled, including the platform, SiP service, transfer service,
  setup, command and retirement units. Their source hashes, object hashes,
  symbols and individual stack records are retained. The transfer-prepare
  frame is 896 bytes. Individual records do not qualify the whole EL3 stack.

The stale-response fault initially reproduced a gap during unpublished table
replacement. Rechecking mailbox idleness in the warm guard fixes it; the saved
failure remains separate from the passing successor. An initial focused command
also named two nonexistent test modules; its valid cases passed, but that
invocation remains failed. The corrected complete run above passes. The first
expanded vendor compile exposed a stale generated-header copy in its private
test tree; the fresh successful copy uses the current generated headers.

The preceding supplier commit `47a9938e38981d290a3de50c160696072465cb71`
now separately passes all eleven selected affected phases. That broader result
does not cover this successor. [Software evidence](software.json) binds this
prototype's selected bytes and terminal checks.

Next is a separately selected, source-bound repeated-load boot contract and
its SDK/entry/auditor integration, followed by a complete matched build and
fresh file admission. Only then can a new bounded TRWU8CBL trial test actual
MENU → core → MENU loading and rollback. Source overwrite guards remain in
place. General appliance acceptance and the earlier warm USB uncertainty remain
open; PR #1791 stays `lane:deano`.
