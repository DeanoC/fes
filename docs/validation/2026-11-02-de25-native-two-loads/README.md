# Two native fixed-package loads with fresh FPGA stop ownership

Implementation and compiled native producer:
`169d54c4899beb265b2d3136188ca45216b1ed64`, based on
`55c70d46eaa22fddc329b1bce1ce3f695c3012e0`. The sealed FPGA producer remains
`c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`.

The saved physical trace independently verifies two configurations of the same
retained sealed package, each followed by actual Z80 fetch activity, held
master-stop and owned return. **The original host controller remains failed:**
its terminal check raised a host-side `KeyError` after both cycles completed.
This is a bounded hardware diagnostic, not ordinary runtime admission or a
passing end-to-end controller run. No hardware retry was performed.

## Change and software checks

The secure configuration provider now requires a completion callback after
whole-image verification and successful final firmware status. The matched
reader-plus-computer provider creates a fresh master-stop activation only for
that positively completed configuration. Warm activation additionally requires
the preceding successful reader/probe stop, complete identity closure, exact
owner and generation, closed bridges and no active lease. Failed authority is
never cleared into a reusable state. Active-DDR warm activation still rejects.

The opt-in `--computer-control --computer-reload` native diagnostic performs
exactly two loads using the retained package and reusable boot-pinned arena.
Each load keeps its original 20-second deadline. Entry, secure firmware, SPL,
FIT and the chain identity bind that selection. The decoder requires two
explicit frames, fresh activation/readiness/stop observations, exact generation
lineage, bounded continuous timing and eight fetch samples per cycle. The
second cycle inherits only the verified preceding return; it cannot replay a
cold boot or an earlier stop receipt.

All ten [selected software groups](software-result.json) passed. The exact
1,307 parent IDs ran in three upfront intact-module partitions capped at 300
seconds each, with a 900-second outer cap; two checks were skipped. FPGA software
ran 1,577 tests with 75 skips within its 840-second cap. Native firmware/FPGA,
producer/identity and firmware/RTL integrations and generated consistency also
passed. [Coverage](coverage-summary.json) records the counts and boundaries.

The strong C provider fixture uses the actual reader-plus-computer profile,
pinned vendor SHA and owned dispatcher. It covers two successful configurations
and fresh stops, failed configuration/opening/stop/readiness cases, stale scope
and unsupported observation rejection. A paired control uses the same current
fixture: the preceding provider completes two configurations but fails the
second opening after only one master stop; the new provider completes both
openings and both stops. Actual captured Z80/mailbox RTL and the native control
driver also passed their host model. These are software/model checks.

A fresh native SDK, entry, BL31, SPL and FIT were built and composed with the
unchanged sealed FPGA core. [Build evidence](build-summary.json) records exact
hashes and actual linked startup, relocation and contained-console checks.
Only local Powerboat Quartus PFG packaging ran; no FPGA compilation ran on
Powerboat or the 3090.

## Bounded physical observation

Exclusive top kit **TRWU8CBL**, 2026-11-02 20:33:26–20:37:02 UTC, native boot
`1d8d771dc74c712c`. One qualified recovery boot and one owned Ethernet upload
session wrote and independently read back the diagnostic FIT and package, then
freshly reread and hashed the FIT after the package write. Writes were confined
to the two diagnostic files; the root recovery boot files were checked intact.
The original factory card remained safe outside the board.

The matched native boot passed CPU/reset, loaded-BL31, EL3 and EL1 checks. Native
SD package retention took 58.763209 seconds within the unchanged 100-second
gate. The same package was retained once and programmed twice; no second SD
read or native SD write occurred. Both loads used package
`b2353f540cc81b9896db86867248880aceffce8c81314042b02317f76f947f37`, build
`dda236646bb868e63c72bd0e777f3dd6`, and the unchanged 1,646,592-byte core payload
with SHA-256
`e9255d3dc149e7db17f012c3032f8c1da245d913ded3947a2342034eb1927ac0`.

| Observation | First load | Second load |
| --- | --- | --- |
| Configuration interval | 13.273118 s | 13.274045 s |
| New activation generation | 4, previous 0, rearm 0 | 6, previous 4, rearm 1 |
| Fresh master-stop generation | 5 | 7 |
| Stop status / reset | 0 / `0xd` | 0 / `0xd` |
| Reader reads / probe polls | 88 / 2 | 88 / 2 |
| Owned held return generation | 6 | 8 |
| Initial / initial-held fetches | 0 / 0 | 0 / 0 |
| Run fetches | 38,419 | 38,413 |
| Return / return-held fetches | 259,270 / 259,270 | 259,262 / 259,262 |
| Relaunch fetches | 297,682 | 297,676 |
| Final / final-held fetches | 325,996 / 325,996 | 325,992 / 325,992 |

Held fetch addresses were also stable; both newly configured instances began
at fetch count and address zero. The unchanged prompt 50-second, observer
45-second and per-transfer 20-second bounds remained in force. No data bridge
release or active FPGA DDR request was made.

[Raw UART](native-console.log) contains 16,014 bytes, SHA-256
`d68d5b7448b024a832bc2f262e1c7dcacd4e7c64f81eb5393931050ebebf647e`.
The source-bound strict decoder independently redecoded those exact bytes with
both cycles verified, zero errors and `last_checkpoint=reload_complete`.
[Hardware observations](hardware-observation.json) retain the complete decoded
evidence and exact source identities.

## Host controller failure and independent audit

The frozen private controller's final helper expected
`fabric_activation.complete` and `fabric_activation.data_held`. The decoder's
normalized activation contains only owner, generation, previous and rearm;
its raw parser already requires the literal `complete=1 data_held=1`. The
controller therefore raised `KeyError: 'complete'` after the second END marker.
Its earlier abstract fixtures invented those normalized fields and missed the
schema mismatch. The [original controller receipt](controller-result.json)
stays failed, together with the original helper and traceback in retained data.

An independently corrected offline checker consumes the actual normalized
schema without changing the decoder, raw bytes, completion rules or board.
The [saved-trace regression](saved-trace-regression.json) reproduces the
original error, accepts the exact saved trace, and rejects 25 damaged copies,
including removed/repeated cycle/activation/stop/return rows, altered activation
completion/data-held constants and a late panic. This establishes the recorded
physical observations independently; it does not rewrite the failed experiment
receipt into a pass. Use the corrected schema for future controller work.

[Cleanup verification](hardware-postcheck.json) passed: the original 15 MHz JTAG
rate was independently read back before stopping the owned private service,
exact USB permissions were restored and UART was free. The continuing exclusive
claim, spare card and completed diagnostic remain parked under the user's
retention policy. No factory restore was requested.

## Retention and remaining work

[Source retention](source-retention.json) records the complete tested-source
bundle with no prerequisites, an independent bare restore without shared
objects, and full integrity checking. The source and validation archive are
retained at `/home/deano/fes-retained/de25-native-activation-20261102/`.
Raw builds, checks, baseline control, original controller failure and independent
audit remain under `out/nano-activation-20261102/`.

Next is normal native runtime admission and loading compatible packages through
the real platform contract. These two loads deliberately use the same compiled
sealed package. Arbitrary package switching, Tenfoot physical launch/return,
native USB/video/network acceptance and active DDR remain open. The full
DE10-equivalent core-load goal remains active. No PR push, handoff or merge
occurred; ownership remains `lane:deano`.

Every future Quartus job on `fes-build-3090` must acquire and hold the existing
shared exclusive lock `/tmp/lander-quartus-3090.lock` for its entire duration.
Never unlink, replace or substitute that lock.
