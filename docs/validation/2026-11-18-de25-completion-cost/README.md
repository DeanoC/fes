# Native completion rejection and fresh-read hashing cost

**The fresh TRWU8CBL serial diagnostic completes both core launch/returns and
final USB Stop/Park under the unchanged 25-second source-operation limit.**
The earlier diagnostic identifies the masked failure as a native deadline at
25.018012418 seconds, after firmware completion. Both one-use procedures retain
their separate verdicts and independently restore the exact original FIT without
power cycling. This is experimental CBL evidence; ordinary Kit D25 HIL,
physical display/input and whole-system qualification remain open.

## Original cause and fail-closed completion

The [preceding trial](../2026-11-18-de25-usb-pre-park-drain/README.md) printed
`configuration already completed` after generation15 MENU continuation.
`ConfigurationTransfer.Cancel` refused to cancel an already completed transfer,
masking its owner's original clock rejection.

Both native configuration services now share a clock observer at their existing
pre/post-Advance boundaries. It preserves the original deadline, backwards-clock
or context error, reports phase and historical completion, and accounts for
phase cost without another clock or hardware read. The first clock sample stays
at the original operation start. Expiry at exactly 25 seconds still rejects.
A private rejection method permanently latches the failure even when hardware
has already completed. Historical Done does not authorize activation, rearm,
source reuse or another Advance; public Cancel semantics remain unchanged.

Diagnostic source `a64487ead3f197668bf85bd1f33ec4def1778e1c`, procedure
`7b6eff1c7da921e3bf8b17b16db3ebfca74f9889c0d8b1585d78cace9afa8c85`,
reproduces the physical stop with its true reason:

```text
configuration deadline: elapsed=25.018012418s step=128.234483ms
limit=25s phase=retire->complete completed=true
```

Observed phase costs are start 7.761379638s, write 3.748492410s, completion
7.666229968s, done 5.708272998s and retire 128.234483ms: 25.012609497s total,
with 5.402921ms between sampled phase intervals. The operation is 18.012418ms
over its bound. Firmware generation15 proof, retirement and continuation all
succeed; no firmware source-clock error is reported. Native ownership remains
retained and generation16 MENU control never opens. This diagnostic remains
**failed**, with the original restored. Its timings do not establish the exact
duration or cause of the older source646 trial.

[Diagnostic boundary, raw-record audit, terminal result and retention](diagnostic-evidence.json).

## Cost repair with the same checks

The actual configuration constructor supplies `region.Bytes` as both staging
region and source. Direct hashing of that source therefore runs against the
uncached mapping. The existing private 32KiB streaming scratch helper now
supports SHA384 as well as SHA256. Each whole-image and sealed-chunk invocation
copies every byte afresh before the standard hash implementation reads it.
The separate source and region SHA256 checks also both remain fresh, including
when they alias. No digest, result or ownership observation is cached.

Lengths, expected SHA256/SHA384 digests, chunk coverage, comparisons, barriers,
clock origin, sample count and all ownership predicates are unchanged. Secure
firmware and production USB/DWC2 sources are unchanged in this repair. The
source-operation limit stays 25 seconds, preparation 4 seconds, command 100ms,
retirement 4 seconds, release 1 second, and packet 500ms; all other phase and
host bounds are unchanged. There is no isolated physical hash benchmark or
measured successful native-operation margin: a successful service establishes
that its existing checks passed, not a new per-operation timing report.

## Exact source, software and artifacts

Implementation, native artifacts and host decoders use
`838ac5b48c307315eb98bb9f819be22647553f35`; host runner remains
`7119ffa96b153149499f67e797a7ae18fe2413db`. The later delivery commit changes
only documentation/evidence, reconstructed independently from retained source
plus its docs patch. Neither prior failures nor artifacts are relabelled.

Focused tests exercise before/exact/late deadline, backwards clock and
post-completion cancellation through actual program and window services,
including blocked control opening, replay and reuse. Hash tests compare SHA384
on unaligned inputs, tails, 32KiB boundaries and a 1MiB source, then independently
reject altered whole SHA256, whole SHA384, first/tail chunks and fresh source
mutations. The complete nativefabric race suite passes in 231.298s. All eight
affected commands pass in 1019.119s: 2,904 parent tests, 21 skips, inventory and
consumer checks, real native firmware/RTL/launcher software integration.

The complete matched SDK/entry/BL31/U-Boot/SPL/full-SOF build and fresh native
file admission pass in 604.158s. Boot `2b421de70c22043c`; FIT 8,575,696 bytes,
SHA256 `65270cad5b0b9221814da1080a74ff02b8146dea16410368a746c9901b033005`;
full SOF `7473b27f3196f3fd63a70b92629faafa00080b1bfa1bcd6ba4b947476c8469cd`.
Core payloads are unchanged. [Software results, policy audit, exact stages and
matched artifact identities](software-evidence.json).

## Fresh physical cycle and restoration

Procedure `0c15f2277264e6610268dca15db95dffaea5a53dae094b58b3c7c33be8b7a3db`
uses unused forward/rollback reset nonces, exact file admission, source-bound
capture, stable USB preflight and independent prepared-source retention.
All five configurations at source generations4/10/15/21/26 complete proof,
retirement and continuation. Four retained-source releases pass. ZX81 runs
with its 917-byte initial tape and fetch proof; ZX80 also runs and returns.
The previous generation15 deadline boundary now passes and MENU control opens.

At final shutdown, the same-owner Ready input pause precedes MENU bridge
closure. Reports/polls remain 752/115144 through inactive USB Stop, then
`FES_NATIVE_TENFOOT_EXIT state=parked generation=5` is recorded. No panic or
source-clock/release failure occurs. This also supplies the previously missing
physical serial coverage of the pre-Park USB drain ordering repair.

Candidate/native/original roles complete in 318.254/273.471/317.750s; the
controller completes in 910.736s, within its unchanged 3400s bound. Independent
restoration replay verifies all 17 before/after file readbacks per recovery,
16 preserved files, 824/823 command receipts, 19 NS reset words per recovery
and cleanup. Exactly two FAT
writes occur: candidate then original FIT. All three programmer/server traces
are retained and checked. Exact original USB permissions and JTAG 15MHz/auto1
are restored; all owned hardware handles/processes are closed.

The outer host status wrapper terminates with143 while the owned controller
continues. Its missing completion receipt is preserved as an interruption;
it is not labelled a successful wrapper run. An observer follows the same
PID/start identity and original absolute 3450s cap. The frozen controller and
1200/480/1200s role limits remain active, no role or procedure is retried, and
independent raw-wire replay establishes the actual cycle and restoration result.
See [physical result, timings, replay and retention](physical-evidence.json).

Recovery packaging first encounters the preferred 3090 shared Pro lock; that
unused failed preparation is retained and performs no kit action. Only our fresh
nonce-bound recovery packaging then uses Powerboat's free shared Pro slot.
Matching Pro26.3 wrapper/binary hashes and the local payload verifier remain
required. No other job, shared lock or tool cap is disturbed.

Original FIT `d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
is restored/read back. Final verified state is the recovery prompt, parked,
spare installed and continuing claim held. No physical power cycle, QSPI,
raw SD, factory/bottom/J8G action or old-native baseline boot occurs.

Raw binaries, ROMs, wire and traces remain outside Git in
`/home/deano/fes-retained/de25-completion-{cause,hash}-{source,trial}-20261118-r1`.
The JSON receipts identify each archive and source independently. The repaired
physical archive verifies 296 entries with SHA256
`2d7e808601f2b0254fb58923b74dcd851aa3904b99ca09ab3da392931a477450`;
the failed diagnostic archive verifies 286 entries with SHA256
`7a33a336d843a746153a44c5204a6f04477e756405ddc408a22a951f2fd0348b`. PR #1952 stays
draft/`lane:deano`; [#1867](https://github.com/DeanoC/fes/issues/1867) stays open.
Next work is repeated/failure-path coverage, exact-head review and ordinary
J8G HIL/restore. Physical display/input and vendor reset/old-DMA guarantees are
not established by these serial proofs; no merge-gate waiver or Lander handoff
is implied.
