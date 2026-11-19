# DE25 guard 34 physical trial

The guard 34 repair clears the previously rejected secure release on
TRWU8CBL: ZX81 source 10→15 releases in **918,403 µs**, within the unchanged
one-second bound. The complete repeated cycle remains **FAILED**. Preparation
of the next MENU configuration stops at its enclosing four-second guard;
ZX80 and final Park are not reached. The exact original experimental FIT is
restored through a separate fresh recovery boot, without power cycling.

This is a bounded Linux-free hardware diagnostic for
[#1867](https://github.com/DeanoC/fes/issues/1867), not ordinary Kit D25 HIL or
physical display/input acceptance. No firmware implementation or bound changed
in this trial.

## Exact selection and results

[The trial record](trial.json) binds one-use procedure
`cce0db6fcccc37e5a6b6bfc4e9b2f2dc2510a72cfe2e4ae904dedde00832434d`
to native source `1f0b34b8afed977142f987c15240e07189e7df5e` (tested
implementation `d3aefb4b95c7678eaa49e31a523810fa226903b2`) and host source
`7119ffa96b153149499f67e797a7ae18fe2413db`. Native boot ID is
`6bd8f8bff436443a`; FIT SHA-256
`fc0321235d58d5a1356a23e600fe34da35043afd518ab2d2be30fb1bf8a8a738`;
full SOF SHA-256
`21ef1d47cc2989d7b0f6b3e52d7230d94f9a386e17b6d38a4d4210d2768164c4`.
Both FPGA core payloads are unchanged from the
[matched build](../2026-11-17-de25-guard34-fix/README.md).

| Boundary | Result |
| --- | --- |
| Fresh nonce-bound recovery and candidate FIT transaction | PASS, 308.531 s; all 17 before/after file readbacks |
| Initial MENU and first source release 4→10 | PASS; release 931,504 µs |
| Warm ZX81, 917-byte initial tape and running CPU fetch | PASS; fetch delta 12,036; tape 1.790 s within six seconds |
| ZX81 master-stop, held return, SD return and release 10→15 | PASS; secure release 918,403 µs, reuse authorized |
| Next MENU preparation at generation 15 | FAIL, stage 2 / `-110`; not prepared or armed, source retained and grant denied |
| ZX80 and final Park | NOT REACHED |
| Fresh independent recovery and exact original FIT restoration | PASS, 300.473 s; all 17 before/after file readbacks |
| Transport and independent final handle checks | PASS; original JTAG 15M/auto1 and USB permissions restored |

The [bounded runner](bounded-run.json) returns failure for the incomplete
cycle. It did not hit its host cap. Native execution used 178.651 s within its
480 s role / 450 s capture bounds. Both FIT roles stayed within 1,200 s each;
the whole procedure used 788.662 s within its 3,400 s transaction bound. The
failed native procedure is consumed and was not retried.

## New preparation failure

[The retained snapshot](native-failure.json) records `warm=1 stage=2
status=-110 generation=15 preparing=1 prepared=0 arming=0 armed=0`, with
`retained=1 grant=0`. The previous 10→15 source-release failure marker is
absent; its success receipt is present. The next MENU source never became
armed for configuration DATA.

The last accepted preparation counter is **3,997,499 µs** after its start.
The exact rejected counter was not logged, so this is not an exact failure
duration. The command queue completed **83,076 µs** after publication, within
its separate 100,000 µs bound; its failure/error fields are zero. The last
recorded I/O callback and ownership-scope reason are also zero. The enclosing
preparation guard stops during post-command layout revalidation; the last
read is offset `0x90`, value `0x94008003`. The snapshot does not distinguish
which repeated layout pass was active.

[Source analysis](timing-analysis.json) separates this four-second preparation
cost from command completion, source release and host timeouts. No deadline
increase is made or implied. The next software work is to reproduce this
second warm preparation through the real suppliers and reduce its validation
cost while preserving ownership and finite failure behavior. A later trial
needs fresh artifact/source admission and a new procedure.

## Restoration and retention

[Independent wire replay](independent-audit.json) verifies 824 forward and 825
rollback command receipts, unique reset/session/capture identities, 19 NS
SMMU reset words per recovery, all preserved files and exactly two FAT writes:
candidate then exact original FIT. Original FIT SHA-256 is
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`.
The fresh loader/master/reset gates authorize restoration only; they do not
prove SDM reset or permit old-source reuse or ordinary native resume.

[Final handle verification](final-handles.json) finds no open top-kit UART or
adapter handles. Each private programmer/server is independently absent.
The board is parked at its verified recovery prompt on the spare card, with
the original FIT restored and claim `7a124fae52bf7062` held. Current operations
still depend on the kit ledger. No original-native baseline reboot, power-loss
durability, raw SD/QSPI/factory-card/bottom-kit/J8G action or physical reset is
claimed.

[Prepared retention](retained-prepared.json) independently verifies all 17,273
host and 17,284 native Git blobs, 84,516 prepared archive entries and the
existing complete matched boot archive. [Physical retention](retained-physical.json)
verifies all 261 entries of the raw trial/archive, SHA-256
`95278c6791c703ffe9031e8013d38330a25bc3036ff1aa8a953288c66c99b7d4`.
The native failure was retained separately before restoration finished: 54
entries, archive SHA-256
`99bfad4e99c444f89464532dd7d4bdcd764f055982fc1df5bf7724b69baa9dbf`.
All are under `/home/deano/fes-retained/de25-guard34-trial-20261117-r1`.
ROMs and full raw receipts remain private; tracked records contain hashes and
bounded summaries.

[#1952](https://github.com/DeanoC/fes/pull/1952) stays draft and `lane:deano`;
[#1867](https://github.com/DeanoC/fes/issues/1867) remains open. The new preparation
repair, complete repeated lifecycle/Park, declared recovery coverage,
exact-head review and ordinary Kit D25 HIL/restore remain open gates. There is
no HIL waiver.
