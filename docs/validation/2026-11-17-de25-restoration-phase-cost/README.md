# Host restoration cost and source-release diagnostic, 17 November 2026

Host repair [#1953](https://github.com/DeanoC/fes/pull/1953) follows the merged
[recovery work in #1938](https://github.com/DeanoC/fes/pull/1938).
[#1867](https://github.com/DeanoC/fes/issues/1867) remains open. Native release diagnostics come from
[#1952](https://github.com/DeanoC/fes/pull/1952).

Host phase-ownership implementation: `488f02b222dad8c4e951bce4d0066924159e1b54`,
base `e6e9320cf6f85d13899f4f33e733a507e42d13d7`. Native implementation:
`b700d743c7e0799cf2a78ed2fce710b23a59f5ae`, documentation/source head
`3f103b19992c43812dfe83801944a5c720fdf217`. The physical candidate-FIT and
original-FIT restoration transactions below identify host source
`7119ffa96b153149499f67e797a7ae18fe2413db` in their records.

That physical source is distinct from the assigned PR review head at the start
of this fix, `c5fbc2bf28139427f16dcfcd249fbea074cec95c`. Commits after `7119`
added path-component incarnation monitoring in `pinned_files.py`
(`9bccd8bbb788cbe36f739d4c3560a199ec649d6c`, with the follow-up
`9d147b41b276f61057b28a9738598b0e647948a8`) and final ownership-audit failure
handling for restoration receipts (`c5fbc2bf28139427f16dcfcd249fbea074cec95c`).
The recorded physical transactions do not exercise those later changes and are
not exact-review-head hardware acceptance. On the assigned head, the focused
host-only pinned-file and restoration suites pass (5 and 7 tests respectively);
no new physical run was performed.

## Host result

The [phase ownership contract](../../design/de25-restoration-phase-ownership.md)
separates full programming audits from live serial ownership. No deadline was
extended. All seven selected affected scopes pass, including 2,900 parent
tests (21 skips), plus the overlapping 90 focused recovery/upload/ownership
checks. The full restoration model retains over 1,000 serial observations with
nine programming observations. Live mutation/incarnation checks on the retained
215-file input set averaged 12.6 ms, with zero programming subprocesses.

The physical candidate FIT transaction passed in **303.116 seconds**, and exact
original-FIT restoration passed in **297.497 seconds**, within the unchanged
1,200-second bound per role. Each performed all 17 before/after readbacks of the
boot FIT and 16 preserved files. Independent replay matched 825 and 824 command
receipts, respectively, plus seven reset commands/19 NS SMMU words each. Each
role used 33 JTAG parameter commands at programming/cleanup boundaries, with
1,653/1,651 live serial observations and 5,401/5,388 live file checks.

Exactly two FAT writes occurred: candidate FIT
`bdd2093de0732df419dbdd3b3bbd74151b3da3eb62bc2c15765bd41ef31e074c`
(8,571,584 bytes), then original experimental FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
(8,379,072 bytes), both to `FES/UPLOAD.BIN`. The operator's inherited write-note
label says “originalFIT” for both; the selected direction, exact hashes, byte
counts, receipts and raw wire distinguish them. No QSPI, raw SD, factory card,
bottom kit/J8G or old native baseline activation occurred.

The previous 1,200-second controller failure remains failed. These are new
one-use operations and new nonces, not a reclassification or replay.

## Native result: failed, now narrowed to guard 34

The admitted matched native boot ID is `7876283624f9c3d4`. The old file-admission
cache correctly rejected intervening documentation changes inside its source
closure. A fresh full `checked_native` validation and independently reloaded
snapshot admitted the unchanged binary chain plus the final frozen operator:
SHA-256 `54dd99932449b59acf966577f7c273cf22c1a83472f01ab16f3606a7ff6bdf69`.
An in-progress earlier preparation was discarded when the operator changed;
only the final fresh admission preceded programming.

Native boot/MENU, initial source release `4 → 10`, warm ZX81 configuration,
retirement/continuation, 917-byte tape delivery and CPU-fetch delta 12,086 pass.
The first source release records 936,525 microseconds. Return `10 → 15` rejects:

```
stage=2 clock_error=0 scope_reason=34
start_us=154790512 last_us=154790831 observed_us=154790831
bad_address=0 bad_value=0 retained=1 reuse_authorized=0
```

Stage 2 is the first full-table admission phase. Its recorded rejection is the
completed-return ownership predicate: `fabric_configuration_generation` must
match the source binding and `sdm_release_returned(binding)` must hold. There
is no recorded release-clock error or register/table mismatch. The exact failed
subpredicate inside this composite guard is **not established**.

319 microseconds is the difference between the last accepted clock sample and
the start. Reporting adds no fresh counter read, so this is **not a measured
failure duration**. The recorded guard failure does not justify extending the
one-second release bound. The old historical trace still lacks these fields;
this is a fresh reproduction at the same generation boundary.

The native phase remains FAILED and was not retried. Its source was retained
and reuse denied. The separately selected fresh-nonce cold recovery then passed
its own boot/master/reset/card gates and restored the original FIT. No physical
power cycle was needed. This does not prove SDM reset or old-DMA retirement.

## Retention and final state

The [independent physical audit](physical-audit.json) matches raw wire to every
accepted command, verifies both fresh reset/capture identities and sessions,
the exact two writes, all preservation checks, original JTAG 15 MHz/auto-adjust
1, USB permissions, absent owned server/programmer incarnations and closed
UART/adapter handles. Whole procedure elapsed 773.145 seconds.

This trial left TRWU8CBL at its verified recovery prompt with the original
experimental FIT staged on the spare card. Its ownership bearer is redacted
from the current tracked evidence. Earlier Git history retains the old value.
On 21 November 2026 the claim owner rotated the bearer in the live TRWU8CBL
ledger. The production `Claim.owns` check accepted the old bearer before the
rotation and rejected it afterward. A separate check accepted the replacement,
preserving the reservation. The rotation changed only the host ledger and
issued no board commands.

This trial established FIT preservation and restoration. Native full-cycle
Park, physical display and input, normal Kit D25 HIL, and power-loss durability
were outside its acceptance scope.

Retention root:
`/home/deano/fes-retained/de25-restoration-phase-cost-20261117-r1`.
[Prepared receipt](prepared-receipt.json) independently verifies 17,123 source
Git blobs and 82,952 prepared artifact members. [Physical receipt](physical-receipt.json)
independently verifies the actual wire/results archive. The unchanged complete
native chain remains in the separate, previously verified native archive linked
by the prepared receipt. [Failed native trace](native-failure.json) and its
[archive receipt](native-failure-receipt.json) preserve the failure separately.

Next: reproduce guard 34 using actual activation/CPU-stop/return callbacks,
rather than the current model's injected completion state; identify the failing
member and implement a justified repair before another native trial. Linux
defaults and all integrity, ownership, seal and finite-failure gates remain.
