# DE25 comparison-cost physical trial

The comparison repair passes the previously failed generation-15 preparation
and arm on TRWU8CBL, within the unchanged four-second guard. The next MENU's
full 1,708,032-byte configuration is acknowledged and reports configured.
The complete repeated cycle remains **FAILED**: final input proof rejects at
source-clock guard 31, before retirement is entered. ZX80 and final native
Park are not reached. Exact original-FIT restoration passes through a fresh
recovery session without power cycling.

This is a bounded Linux-free serial diagnostic for
[#1867](https://github.com/DeanoC/fes/issues/1867), not ordinary Kit D25 HIL or
physical display/input acceptance. No implementation or deadline changes
were made during this trial.

## Selection and observed boundaries

[The trial record](trial.json) binds consumed one-use procedure
`0c6e561983246cfd2630e260ae48ac1f623f9749df68a4b406f78a6be10c914b` to native source
`251cce26a39b56ab759d19f054ed03436296b153`, tested implementation
`d422c32b30e9cf560e81caa07273d8e47942d44e` and host
`7119ffa96b153149499f67e797a7ae18fe2413db`. Boot ID is `84123df97253f2bd`;
FIT SHA-256 is `d28f64df7fa900e1696688968e140c74879346c2985eeb0c1134cf74e036f25b`;
full SOF SHA-256 is `ecc2b484e5a499b23363a2f50440b62a729e8c6bd5b3e24ac569d955756b6705`. Both core payloads
are unchanged from the [matched comparison build](../2026-11-18-de25-warm-preparation-comparison/README.md).

| Boundary | Result |
| --- | --- |
| Fresh recovery, candidate FIT and all protected readbacks | PASS, 297.795 s |
| Initial MENU, first release 4→10 | PASS; release 936,413 us |
| Warm ZX81, 917-byte tape and running CPU fetch | PASS; fetch delta 12,088 |
| ZX81 stop, held/SD return and release 10→15 | PASS; release 923,244 us |
| Next MENU preparation and arm at generation 15 | PASS; no setup-failure marker; successful rearm receipt |
| Next MENU DATA, acknowledgements and configured status | PASS, all 1,708,032 bytes; zero pending/outstanding |
| Final input proof | FAIL, status 4, proof 0, source failed, guard 31 |
| Generation-15 retirement/continuation, ZX80, final native Park | NOT REACHED |
| Separate fresh recovery and exact original-FIT restoration | PASS, 298.789 s |
| Transport cleanup and independent final handle scan | PASS |

The [bounded runner](bounded-run.json) did not hit its host cap. Native
execution used 192.227 s within its 480 s role / 450 s capture bounds. Both
FIT transactions stayed within 1,200 s; the complete procedure used 789.724 s
within 3,400 s. The failed native diagnostic was not retried.

## Final input proof failure

[The snapshot](native-failure.json) records `configured=1 failed=0
first_error=0 service_error=0 pending=0 outstanding=0`, with complete DATA
accounting, but `status=4 proof=0 source_failed=1 scope_reason=31`.
The transfer service's last accepted elapsed receipt is **19,626,142 us**.
It is not the exact rejected clock sample or rejection duration.

[Source analysis](timing-analysis.json) maps guard 31 to `sdm_source_time`:
prior source failure, clock not started, null output, probe context/frequency
failure, a backward sample or the enclosing 20-second source-transfer bound.
The near-budget receipt makes the enclosing deadline a lead; the trace does
not identify its exact rejected member. It does not justify increasing a
deadline. No generation-15 retirement result or continuation is emitted:
`fes_nano_sdm_retire_source` is called only after final proof succeeds.

`FES_NANO_INPUT_PARK retained=1 resume=0 activated=0` is the terminal firmware
failure park. It is **not** the frontend's successful final Park/quiescence
receipt requested by this test. No returned-MENU activation or old-source
reuse is accepted after this failure.

Preparation's successful guarded path is now physical evidence for this
artifact, but no separate preparation duration was logged. The earlier 3.38 s
and 84.885% load-reduction results remain synthetic/model evidence; they are
not reclassified as hardware latency measurements.

## Restoration and retention

[Independent wire replay](independent-audit.json) verifies all 17 before/after
readbacks per transaction, 16 preserved files, 824+825 accepted command
receipts, distinct reset/session/capture identities, 19 NS SMMU reset words
per recovery and exactly two FAT writes: candidate then exact original FIT.
Original FIT SHA-256 is
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`. Fresh recovery authorizes only the
selected restoration; it does not prove SDM reset, old DMA retirement or
ordinary native resume.

[Final handle checks](final-handles.json) find no open top-kit UART/adapter
handles. Private servers/programmers are independently absent, with original
15M/auto1 JTAG settings and exact USB permissions restored. The board is
parked at its verified recovery prompt on the spare card, original FIT
restored, continuing claim `7a124fae52bf7062` held. The kit ledger governs
current state. No original-native baseline boot, physical power cycle,
power-loss durability, QSPI/raw-SD/factory-card/bottom-kit/J8G action is claimed.

[Prepared retention](retained-prepared.json) verifies the complete current
host/native Git sources, 84,524 prepared entries and the prior complete matched
boot archive. [Early native retention](retained-native.json) independently
verifies 54 entries, saved before restoration completed. [Physical retention](retained-physical.json)
verifies 262 entries, archive SHA-256
`116124ac2f613ca916f08d30e8f37c521dacb4c61e3a51d4fa645bdfff950179`. All are under
`/home/deano/fes-retained/de25-comparison-trial-20261118-r1`. ROMs and raw
binary/console evidence remain private.

Next: distinguish guard 31's rejected member and reproduce the final input
proof through actual suppliers off-board, preserving ownership and finite
bounds. Exact-head review, the full physical repeated lifecycle/failure
coverage, vendor guarantees and ordinary Kit D25 HIL/restore remain open.
No HIL waiver is recorded. [#1952](https://github.com/DeanoC/fes/pull/1952)
stays draft, `lane:deano`; #1867 remains open.
