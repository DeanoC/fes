# Original boot FIT restored without power cycling

TRWU8CBL's spare card now contains the exact saved original experimental boot
FIT in `FES/UPLOAD.BIN`. One FAT file write replaced the candidate; all 16
preserved files passed before/after checks. A separately bounded software
recovery boot then read the original FIT from SD successfully in 62.01 s.
Neither operation requested or performed a physical power cycle. The board
is parked at the recovery prompt with our claim held.

The initial restoration transaction remains **failed**. Its original FIT
transfer, write/readback, all 17 prewrite file checks and all 16 postwrite
preserved-file checks passed. The 1,200 s host bound then expired on `version`
at the start of the final redundant boot-file check. The raw wire contains
the `version` response, but its post-command ownership check expired before
the response could enter the accepted ledger; no final FIT load/hash ran. The session and transport closed cleanly. The unused second role of
that consumed procedure was not dispatched, and no write was retried.

A new read-only procedure required that exact retained failure, the verified
original write/readback, all 16 preserved-file results, closed handles and
an unused build nonce. It performed a fresh volatile JTAG recovery and read
back the restored FIT and protected recovery files. It issued zero SD writes.
This confirms the restored data without recasting the first controller as a
passing run or retrying the failed transaction.

| Phase | Result | Evidence |
| --- | --- | --- |
| Fresh reset and prewrite admission | Passed | Nonce-bound SPL entry, EL3 CPU/master/bridge containment, 19 NS SMMU reset-register reads |
| Original FIT transfer and write | Passed | Full TFTP return trace, original SHA-256, exactly one fixed FAT write and readback |
| Preservation | Passed | 17 prewrite files and 16 postwrite preserved files |
| Final redundant read in first transaction | Failed | Host deadline after version response; final FIT load/hash never ran; failed verdict retained |
| New software reset and original FIT readback | Passed | Different unused nonce, fresh loader/session, protected-file checks, no writes |
| Cleanup | Passed | Original JTAG settings and USB permissions restored; both owned server/programmer processes absent; UART/adapter handles independently closed |

The restored FIT is 8,379,072 bytes, SHA-256
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`.
The replaced candidate was
`df0cabe09eb4dbc6453e55766f5e1dfecd3166750e165b554c81d3435ef15520`.
The recovery FIT remains
`3722e24c551a794dd970e665cc4932b96cb62aefa353f39079285458d67d48e5`;
its protected prefix and manifest were verified throughout.

Tested code: `4c4738b3dfe8d80d5881a3a980cfc507dd276748`.
[Independent audit](hardware-audit.json) binds both raw boot windows, full
wire hashes, the original failed procedure and the separate read-only plan.
The committed [restoration boot](restore-boot.log) and
[verification boot](verify-boot.log) show the distinct nonces and cold/JTAG
entry observations. The full command traces, receipts and frozen operators
are retained in the archive referenced below.

The updated recovery gate addresses the first review's admission findings:
a drained, attempt-bound `ResetCapture` plus shared persistent nonce registry
rejects buffered/preprogram witnesses and reused artifacts; prefix/reference
image digests are independent constructor inputs; relevant ELF PROGBITS and
file-backed LOAD bytes must match the decoded SPL image. The two physical
boots used distinct freshly built SPLs composed under the shared Pro lock.
The normal/default profile does not select the diagnostic.

[Software checks](software-checks.json) passed: all 11 affected scopes,
2,893 parent tests (21 skips), native/firmware/RTL integration and 2,380 FPGA
tests (90 skips), plus 83 focused tests covering replay, substituted card
identity, malformed loaded sections, uncertain writes and terminal failures.
An owned software job was paused briefly for CPU contention and resumed;
no job or phase deadline was extended.

The remaining host cost is repeated source hashing and live JTAG-parameter
queries during each SD console command. It caused the first transaction's
deadline and should be reduced through explicit phase ownership before
claiming the full restoration controller's hardware completion. Increasing
a failed trial's deadline or reclassifying that failure is not acceptance.

This is file restoration and fresh software-reset readback. It does not
qualify a native launch/return cycle, whole-card replacement, power-loss
durability, SDM reset or normal Kit D25 HIL. QSPI, the factory card, bottom kit
and J8G were untouched. [Issue #1867](https://github.com/DeanoC/fes/issues/1867)
remains open for the complete secure-system investigation.

The [retention receipt](retention.json) records the exact tested-source and
artifact archives under `/home/deano/fes-retained/de25-reset-restoration-20261117-r1`.
All 86,571 artifact members and 17,114 source Git blobs were independently
read back and verified, including the stopped transaction and separate final
read-only procedure. Its copied preflight labels the final check as unsent;
the raw wire clarifies that `version` returned but was not admitted after the
deadline. Neither description grants a passing verdict to that transaction.
