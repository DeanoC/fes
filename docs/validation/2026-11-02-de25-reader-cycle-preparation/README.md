# DE25 secure reader-cycle preparation, 2026-11-02

The owned secure firmware and native adapter now support one bounded
enable/held-address/partial-burst/drain operation for the sealed diagnostic's
on-chip reader. The actual C probe, strong provider, SD lease and configuration
dispatcher pass joined tests against the actual Avalon RTL. No board was
accessed in this step. The new operation still needs a matched native caller,
boot chain and bounded physical test.

Source: `f36ec1dd911a7f3257a0654c5608ad10c72d1bfb`; base: `207e8d619f2cab5bb2f34a7fc90f5c50cea3ca82`.
See [scope and source hashes](evidence.json), the [affected checks](software.json)
and their [exact plan](software-plan.json). The preceding
[physical SD-package configuration pass](../2026-11-02-de25-stored-configuration/README.md)
belongs to source `12a0d0caf487f3984b49b5af6aed629868a41572` and its retained
artifacts; it does not qualify this changed firmware.

## Contract and implementation

Owned bridge contract 1.7 adds operation 10 and reply tag `0x46524431`.
All prior constants and the FPGA endpoint's inputs are unchanged. Native
`HPSContextVerifier.ProbeReader` checks the compiled source/control map,
dedicated reply tag, live build/ABI and the final same-owner closed snapshot
at the source generation plus two. An identity-only response cannot stand in
for the reader result. It supplies no runtime programming policy or trusted
boot-handoff implementation.

The firmware operation shares the identity probe's one attempt. It checks
closed, never-enabled ownership, no active device/configuration, SDM readiness
and two matching idle identity snapshots before the first register write.
The fixed recipe enables reader generation 1, holds an AXI address, disables
with cookie 7, accepts the held address and returns two beats separately.
Neither the held address nor the first beat may acknowledge drain. Completion
requires the exact generation/cookie, zero pending transactions and two final
matching identity/state snapshots before control reclose.

All offsets and values belong to the secure provider; non-secure code supplies
only the expected owner/generation and private channel cookie. Each command
and response beat issues once. Read observations poll at most 128 times per
state. Missing providers reject before consuming an attempt. Failures preserve
the first error, attempt control containment and retire ownership without
publishing successful identity or replaying the operation. Data bridges remain
reset. The responder is on chip; this supplies no DDR, scanout or other-master
drain proof.

## Validation and remaining integration

All twelve affected software groups pass for this implementation, including
generated consistency and shared schemas, parent regressions, the existing
Tenfoot launch/return/failure integration and firmware/RTL checks.
The new direct secure-reader/Avalon fixture passes 169 cases: successful
generation 1 and 4 scopes, failure at every read, before/after every write,
ownership loss, early/wrong ACK, partial burst, fault, bounded timeout and
no replay. A successful probe performs 113 reads and 12 writes. The
[joined strong-provider fixture](joined-reader.log) adds eight cases using
the actual controlled SD service, pinned SHA code, configuration and owned
dispatcher against that same RTL. It includes lost ownership, cache and data
reset scope after a write. Native [adapter checks](native-adapter.log) require
the dedicated opcode/tag, generations 1-to-3 and 4-to-6, and reject partial,
unready, wrong-identity and changed-closure replies without replay.

The initial parent run's only failure was a test-selection assertion: floor
division rejected 632 selected tests out of 1,265, although that is less than
half. The one-line repair compares `2 * selected < total`, preserving the
intended ratio, actual suite selection and existing time bounds. The corrected
parent suite and remaining groups ran again. The unchanged 1,576-test FPGA suite
(75 skips) passed on implementation commit
`fff77d0ce6d70a753ad3350e0ee53be03203d818` and was reused after verifying that
the intervening change touched only that parent-test assertion. See the
[retained initial failure](initial-parent-failure.log). The initial outer
wrapper replaced its inner summary through a filename collision; its complete
terminal log and individual test logs establish the recovered phase outcomes.
The [initial recovered record](initial-software-recovered.json) states that
boundary, including unavailable individual elapsed values. The final runner
uses distinct result filenames.

The native loader still selects its existing identity-only operation. Next wire
the reader operation into that loader with strict saved-trace validation, build
all matched boot components, and run one bounded TRWU8CBL test. The kit remains
parked after the earlier successful SD-to-FPGA diagnostic, with its spare card
and continuing exclusive claim retained. No card swap or factory restoration
is required for these software checks.

Runtime package admission, general active-master containment, repeated loads,
physical launch/return/failure acceptance, media/input delivery and the remaining
USB/network/display path stay open. No physical core-load acceptance is claimed.
Source bundles and complete logs, including initial development failures, are
retained under `/home/deano/fes-retained/de25-reader-cycle-20261102`.
