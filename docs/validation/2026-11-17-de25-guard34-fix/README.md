# DE25 guard 34: never-enabled computer reader

The native ZX81 return completed its master-stop and held bridge close, then
rejected source release at generation 15. The stop engine accepts a reader
that was never enabled: every mutable generation, request, cookie, ACK,
status and pending field is zero. The source-release predicate instead
unconditionally required a nonzero drained generation/cookie. The original
joined fixture fabricated that receipt and concealed the mismatch.

The repair keeps the fully drained path and accepts the exact never-enabled
zero tuple only when no DISABLE was issued. Complete production activation,
identity, stop/probe, held-return/data containment, SD grant/return/stop,
source-generation, table and single-use checks remain mandatory. There are no
new release writes or hardware/counter callbacks and no deadline, layout, ABI,
FPGA payload or production Linux default change. This remains an opt-in
experiment tracked by [#1867](https://github.com/DeanoC/fes/issues/1867).

## Source and before/after control

- Test-only reproduction: `95c8e9aef93d6a5c1e8890051ac37731bb3a167e`.
- Production repair: `0af48924aac246e0ea94630671f619233f404d9d`; additional
  stale SD/activation cases are in the subsequent test commit.
- Final tested implementation, normally merged with #1938:
  `d3aefb4b95c7678eaa49e31a523810fa226903b2`.
- Comparison base: `e2b8130b5a18b652095a8d5e23124fd0a994e9c0` (main's #1938 merge).

[The preserved before-fix record](before-fix.json) binds the sanitized C binary,
request, supplier hash and source. Production activation/control, identity,
master-stop/probe, bridge close and SD return reproduce the same 10→15 guard
34 rejection as the later physical diagnostic: master-stop succeeds with 88
reader reads, one reader poll, no DISABLE and two probe polls, followed by
stage 2, `clock_error=0 scope_reason=34 retained=1 reuse_authorized=0`.
This is a successful reproduction of a failure, not a passing runtime cycle.
The physical snapshot reports the composite guard, not each of its members;
the production-callback control isolates this concrete mismatched member on
the legitimate never-enabled path. Further physical conditions and acceptance
of the repair remain unmeasured.

The original physical trial is preserved separately in the
[#1953 evidence](https://github.com/DeanoC/fes/blob/codex/de25-restoration-phase-cost/docs/validation/2026-11-17-de25-restoration-phase-cost/README.md).
It remains FAILED. Its last accepted sample is 319 µs after the start, not a
measured rejection duration. The demonstrated guard mismatch does not justify
increasing a deadline or retroactively changing that verdict.

## Validation

[Software results](software.json) record the exact commands, log hashes,
scopes and counts. The regression uses the native ProgramService, retained
arena, ROM handling, independent tape publication and MENU/computer return
with production C activation/stop/return callbacks.

- Four valid scenarios each perform four launch/return cycles and Park. They
  cover the never-enabled reader, long simulated gameplay, an active reader
  requiring real DISABLE/ACK, and an already-drained reader. ROMs alternate
  8/4 KiB and tape sizes are 917/0/1/16,375 bytes.
- Twenty-seven rejected returns retain all 8 MiB of source/ROM/header memory,
  require reboot and prevent another release. Twenty new tuple, stop/probe,
  containment, SD and activation faults reject through guard 34 with no clock
  error. [An independent log audit](callback-log-audit.json) verifies all cases.
- All 38 sanitized source/retirement tests pass. The full affected run covers
  the final merged implementation: all 11 scopes pass, with 2,895 parent tests
  (21 skips), native/RTL/Tenfoot/producer integration and 2,512 FPGA software
  tests (90 skips). Individual scope verdicts are recorded in `software.json`.
  Test counts overlap and must not be added.

Physical MMIO/CPU observations, source hashing/sealed inputs, ROM device work
and tape firmware completion remain injected. The combined C transfer joins
configuration boundaries rather than every Go SMC. Configuration packages are
synthetic 128-byte inputs. These tests do not measure physical warm release
cost, prove DMA retirement guarantees, test final physical USB shutdown or
qualify display/input. Other component failure/cancellation tests do not close
every combined lifecycle boundary in #1867.

The separately recorded focused 38-test run used implementation `0af48924`;
the final merged parent run also executes those tests with the final rejection
extensions.

The supplemental [ARM link preview](link-preview.json), read together with its
[source correction](link-preview-source-correction.json), retains 128 bytes of
OCRAM margin (`__BSS_END__=0x5ff80`). Its copied report has a stale historical
source label; the correction identifies the actual base and selected source
hashes. This preview uses an old generated profile, is unadmitted, and must
never be programmed. The complete freshly rebuilt chain has separate evidence
below; the preview grants it no acceptance.

## Matched boot chain and retention

[The fresh chain record](matched-boot-chain.json) binds SDK, CPU entry, BL31,
SPL/U-Boot, full-SOF composition and checked native file admission to the tested
implementation. Composition uses the shared Quartus Pro lock; both core
payloads are reused unchanged. This is source/artifact admission only. It does
not grant trusted runtime handoff, old-source reuse or physical acceptance.
Native boot ID `6bd8f8bff436443a`; FIT 8,571,584 bytes, SHA-256
`fc0321235d58d5a1356a23e600fe34da35043afd518ab2d2be30fb1bf8a8a738`;
full SOF SHA-256
`21ef1d47cc2989d7b0f6b3e52d7230d94f9a386e17b6d38a4d4210d2768164c4`.
The [independent rebuilt-BL31 check](matched-layout.json) retains the same
128-byte OCRAM margin.

No kit, programming transport, card, factory image, QSPI or raw SD access was
performed in this repair run.

[Source archives](retained-source.json) independently verify every Git blob in
the before-fix and final tested source snapshots. [Evidence retention](retained-evidence.json)
records independently checked software/control and complete matched-chain
archives under `/home/deano/fes-retained/de25-guard34-fix-20261117-r1`.
They include the failing control binary/request/raw logs and the new artifacts,
recipes, private projects and admission receipts. ROM content stays private.
The initial interrupted affected run remains superseded, not passed.

## Open gates

The new artifact is unprogrammed. The next step is a fresh bounded physical
`MENU → ZX81+tape → MENU → ZX80 → MENU → Park` and declared-failure recovery
trial, rechecking exact artifacts and the current kit ledger first. Newly
reached warm source validation still needs a physical cost measurement within
its unchanged bound. Serial diagnostics, physical display/input and ordinary
exact-head Kit D25 HIL/restore are separate gates. No HIL waiver is recorded.
The two earlier clean Codex reviews cover the diagnostic milestone; they do not
cover this behavior repair. The PR remains draft and `lane:deano`; #1867 stays
open for the remaining design, combined-failure and physical requirements.
