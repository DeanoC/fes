# KSZ9031 recovery and native package retention

Exact tested source: `551a52903c7b580cb639563a40673ec44b78c282`, based on
`5e19858d9599bf0deb8c689e4aab26de573928ec`, lane:deano. FES owns this
embedded integration. No first-party module or shared wire contract changed.
The earlier [zero-write PHY rejection](../2026-11-01-de25-sd-packages/README.md)
is retained separately; its native candidate was never executed.

The corrected recovery and the Linux-free package-retention diagnostic passed
on TRWU8CBL. This is narrow hardware diagnostic evidence with a synthetic
12-byte payload. It is not FPGA programming or playable-core acceptance.

## What changed

Recovery now enables the actual KSZ9031/Micrel driver and gigabit support.
Build and packaging independently require its linked configuration function
and driver registration. The recovery metadata, identified banner and live
PHY ID must agree before transfer. The pinned vendor driver supplies its own
PHY configuration; no upstream PHY code, clock, link speed or negotiation
deadline was altered. The existing 4,000 ms negotiation limit and one-attempt
stop behavior remain enforced. There is no old-loader fallback.

The previous recovery used the generic PHY path. A positive pre-start status
did not survive that path's first-start reset/configuration before negotiation.
The new image corrects the concrete missing-driver mismatch; the passing test
does not establish endurance or universal network reliability.

## Software and matched artifacts

All 10 selected affected stages passed, including 1,223 parent Python tests
(2 optional checks skipped), firmware/native launch and producer integration,
generated-consumer checks and 1,571 FPGA-tooling tests (75 optional tool checks
skipped). The focused recovery/upload suite passed 22 tests. `make check`
matched 33 generated consumers and 37 fixture copies. All native-command
17-case and wrapper 16-case failure fixtures passed on these actual helpers.
The exact synthetic archive was independently accepted by both the existing
Python package producer and the Go validator used by the native image.

The SDK ELF and entry bytes from source `2113e55c7` were reused after every
selected source digest was checked against the tested tree. BL31, SPL, FIT and
SOF were freshly rebuilt for the changed producer identity. The single-CPU
release lock and all secure ownership gates remain enabled.

| Artifact | SHA-256 / ID |
| --- | --- |
| Native boot ID | `b1f79112ba1db5bc` |
| Native SOF | `eb036d4936d6427f751779f4158448876fa1080b138d8fb54f7d388b4abb7bf4` |
| Native FIT | `a2c4dc4f7c290e1c385d58491eda242f5bf6ea44d68886163aea0bd12b549821` |
| Native ELF | `c5b4ff4db1371f5b514566214de2601127f491b4dd41ac612feea3897e6a1b2f` |
| Entry image | `81f1f9151f4daae0879275178c88c9b9f4e9b75c06c8c17c597366e3f23a594b` |
| Recovery SOF | `07cdad09617584c454fe3d54d3d2b8afd83327e1fa39607cd8ebc634363e7a6b` |
| Recovery FIT | `25ebdf98d253e41f51e4b55b78314ffe6e03d346825af8582268eacef1d25f90` |
| Prepared card image | `42ac2e14fcb224fb8f39c38b0f26e7999b31c800180bd22489619fdc39f4bacf` |
| Synthetic package ID | `164c9e356d32eed24bb5ec59528323c0a325a23a93e2a04c25b78985df22f873` |

## Actual bounded run

The spare 3,980,394,496-byte card was freshly identified by reader, capacity,
unmounted state and the known previous recovery prefix. Its overwritten region
was privately backed up and verified. The new image was written once, flushed
and raw-readback verified. The user confirmed reinsertion before the fresh
preflight and any programming. The original factory card remained safe.

Two fresh recovery boots independently completed PHY startup, DHCP, bounded
Ethernet download, one fixed FAT write, separate readback/hash/byte comparison
and protected boot checks. The first wrote the 6,305,476-byte native FIT to
`FES/UPLOAD.BIN`; the second created the 3,584-byte `FES/PKGTEST.BIN`. The
protected raw prefix, root recovery FIT and recovery manifest were unchanged
after both operations. No raw card write occurred on the board. This exercises
the production transfer core/transport under the encompassing claim; the
standalone CLI claim lifecycle was not exercised. These writes do not prove
atomic publication or power-loss recovery.

The matched native SOF then passed SPL/FIT, secure-provider, manual-loader and
CPU/memory gates. Exactly one `fesboot` was sent. The [complete native trace](native-console.log)
shows ordered EL2 cleanup and EL1 entry, then:

- expected package/payload retained from actual SD/FAT32, generation 3;
- SD contained and retained RAM payload consumed, generation 4;
- explicit resume, old handoff rejected, package reread and matched, generation 6;
- final containment and another retained-payload check, generation 7.

All stages keep owner `268d8362c8566f4f`. Both loader snapshots report the
expected tag, flags and generation. The CPU report keeps one core online.
Native SD writes and FPGA activation are disabled. The package is synthetic
and contains no programmable bitstream or playable core.

Post-checks verified JTAG `4362C0DD`, original 15 MHz cable rate, restored USB
permissions and no UART owner. The final SD receipt is contained. The diagnostic
and spare remain installed with the continuing top-kit claim under the user's
retained-diagnostic policy. Factory restoration was unnecessary.

Next: connect this retained-input boundary to qualified physical runtime
admission and the active-fabric storage/bridge return contract. The cached SD
handoff is not live FPGA authority. The current SD service requires absent
fabric and never-enabled bridges; it does not yet support a real FPGA activation
or concurrent core/SD access. Nano ROM linking, native writes, USB, native
Ethernet, display and real core launch/return remain open. Recovery Ethernet
acceptance does not qualify the native Go network driver.

The complete immutable physical trace is also a host regression fixture. All
21 serial/SD/package observer tests passed after adding it, with its exact raw
SHA-256, expected package identity and final containment required. No hardware
was rerun for that observer regression.

## Retained evidence

`/home/deano/fes-retained/de25-nano-ksz9031-packages-20261101` retains the
exact artifacts, traces, selected SDK sources and producer inputs: 1,769 archive
members were individually hash-verified. Archive SHA-256:
`76d2fb65add965c938a5dec64827e5412f9cdb3e758f6668b2e1e4cb9df65b11`.
The incremental source bundle restores exact commit
`ac418d96a7a608aafc77082dd8325ac0e876b861` and tree
`0dbb18955b07d21265b4af428e27918b3c4eb775`, with explicit prerequisite
`5e19858d9599bf0deb8c689e4aab26de573928ec`. A fresh private bare restore
matched both identities and had no missing reachable objects. Follow the
retained source-retention instructions; the source bundle is not standalone.
Input snapshots correspond to their recorded source and must not be overlaid
onto later checkouts. Private card backups, claim tokens and factory
credentials were excluded. This retention note is subsequent documentation;
`551a52903` remains the exact tested hardware source.
