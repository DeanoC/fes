# DE25 guard-31 launch attempt: stopped before installation

A fresh bounded TRWU8CBL launch/return/Park procedure stopped during its first
recovery-image programming operation. The native candidate was never installed
or programmed, and the launch test did not run. This is a programmer/transport
failure, **not a new guard-31 runtime result**.

Native selection: source `abb8bfeb0222f9da3826e4cb25ed79263df90cf2`, tested
implementation `379c6aa7d40d1283d80f353e36a7ddce43ae4f1c`, boot
`1a8c1e6016feca1c`, FIT
`1ca80b57e7ff7a6ebbdb089fb41821706df698bbe92fc8f293f0da377a0b572a`.
Host source: `7119ffa96b153149499f67e797a7ae18fe2413db`.
The [off-board repair and matched build](../2026-11-18-de25-final-input-proof/README.md)
remain separate evidence.

## Procedure and stop

One-use procedure SHA-256:
`8829a1f08d547beb0121100d0af26b341e48175b4a45923dcc48cd82ceb51452`.
Fresh forward/rollback recovery nonces, complete source/file admission,
source-bound capture/decoder, five image-selection tests, a 60-second USB
identity window and independent free-handle scans passed before dispatch.
The first image-test invocation omitted its import path; that setup failure
and its corrected passing invocation are retained separately.

The planned order was candidate FIT installation, five configurations/two core
launch-return cycles/final Park, then separately gated exact original-FIT
restoration. Per-role bounds stayed 1,200 / 480 / 1,200 seconds, native capture
450 seconds, total procedure 3,400 seconds. All firmware bounds were unchanged.

The serial-bound forced scans passed before programming at 3 MHz. Quartus Pro
26.3 then reported error 18939, **JTAG chain broken**, and error 18952,
**Detected error when rescan jtag chain**. The same log also contains
"Configuration succeeded at device index 1". Its nonzero exit remains a
failure; neither that success line nor later UART output overrides it.

Forward role: 20.373 seconds; total controller: 20.947 seconds. This was not a
deadline failure. The controller stopped, consumed the procedure and did not
retry, install the candidate, dispatch native programming or start rollback.
There were **zero SD, flash or raw-SD writes**. The original FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
was not modified by this attempt; a new file readback was not performed.

## Observed state and cleanup

The still-open programming capture contains the new nonce's valid cold-reset
witness and SPL progress through DDR initialization and MMC boot. A later
bounded passive capture saw no new bytes; one empty carriage return then
elicited exactly `SOCFPGA_AGILEX5 # `. No recovery command, second programming
operation, reset or physical power cycle followed.

That prompt is a liveness observation, not admission through the failed gate,
SDM/DMA retirement, old-source reuse or a successful launch/return/Park. No
physical reset is requested by this checkpoint. The continuing serial-bound
claim stays held; the kit ledger governs subsequent work.

The [independent stop audit](independent-stop-audit.json) checks the zero-write
receipt, missing native/rollback dispatch, raw reset witness and prompt,
closed owned processes/handles, restored exact USB permissions and original
JTAG 15 MHz / auto-adjust 1. Factory card, QSPI, bottom kit and J8G were untouched.

[Prepared retention](prepared-retention.json) independently verifies both Git
source archives and all 84,525 prepared entries, and references the complete
matched native archive. [Physical retention](physical-retention.json) verifies
all stopped-trial/raw-log evidence under
`/home/deano/fes-retained/de25-guard31-trial-20261118-r1`.

Next: investigate the programmer/rescan failure before preparing a new,
separately admitted bounded launch/return/Park procedure. Preserve this failed
verdict and consumed attempt. Exact-artifact lifecycle qualification, physical
display/input, complete failure coverage and ordinary Kit D25 HIL/restore remain
open. [#1952](https://github.com/DeanoC/fes/pull/1952) stays draft and
`lane:deano`; [#1867](https://github.com/DeanoC/fes/issues/1867) stays open.

A later [fresh server-traced physical trial](../2026-11-18-de25-guard31-physical/README.md)
passes programming and identifies the new runtime rejection as guard 31 reason 7:
the original source deadline expires at 20,576,301 us after full generation-15
DATA. The full cycle remains failed, and the exact original FIT is restored
without a power cycle. This later result does not change this checkpoint's
historical verdict or retroactively identify an older unrecorded rejection.
