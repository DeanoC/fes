# DE25 native programming stop

One fresh trial selected source `8394acfa52b1cbd898010e33d8e70aa380dd632d`,
format 18 and matched FIT
`3a70eb273cbdd3d2f01882f03e8b12c64ef2851d5ba56b4a4d7136e21d259a0c`.
Procedure `8ec8372b6f829ea56a11c48119b0933e48c023911f9c915372d383787581d239`
consumed its single attempt. Recovery boot, temporary spare-card FIT write/readback
and all seventeen protected-file checks passed.

The native HPS-first programmer then failed after one second with
`JTAG chain broken` / `Detected error when rescan jtag chain`. Its selected
phase-1 image SHA-256 was
`5dec3d316891248e29f2a536a6782c9cf38b1422b5949bbbcf92077cffb88c20`.
The immediately preceding ownership-bound vendor scan had passed at fixed
3 MHz with automatic adjustment disabled. A successful scan does not establish
subsequent programming success. No native UART bytes or native SDM DATA were
observed; partial configuration remains uncertain. The outer run stopped after
316.599 seconds without a timeout. This attempt did not exercise the new
firmware's retirement budgets, terminal reset supplier or repeated core loads.

[failure.json](failure.json) records the stopped result. All ten recorded
process identities were independently confirmed stopped, the UART had no open
handles, original USB identity/permissions matched, and JTAG parameters were
restored to automatic adjustment / 15 MHz. The retained failure archive is
`/home/deano/fes-retained/de25-terminal-trial-failed-physical-20261114-r2/physical-trial.tar.gz`,
SHA-256 `55aa7ee8831ffd9b406f75af70b70f6301b828a7098627de08818a2cd301d2eb`.
All 143 files were read back for bytes, hashes and modes. The initial retention
script lacked `fuser`; its replacement uses a root read-only `/proc` FD scan,
and preserves that initial failure. [failure-retention.json](failure-retention.json)
records the archive, without granting retry or recovery authority.

Before this attempt, 66 operator models and eight real root admission controls
passed, including role/digest rejection. Four host telemetry tests cover
partial, foreign, duplicate and reordered terminal records. Observation of a
reset attempt/return or later SPL banner grants no reset completion, source
reuse, ordinary resume or fresh boot. Seven restoration scope tests and both
baseline preflights pass. See [completed-checks.json](completed-checks.json).

After the fresh human-confirmed top-only power cycle, restoration-only
procedure `d9583648655d04ce54392b53b506503ea8a40aaa7cbcdd5f2564c9aa942bd36d`
passed after 530.725 seconds without a timeout. The exact original FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`,
all seventeen protected-file before/after readbacks, fresh sealed original boot,
all 51 ZX81/ZX80 launcher/launch/return exchanges and final Park generation 5
passed. Independent saved-wire/program replay confirms those receipts. All eight
owned process records are stopped, root FD scan finds the UART free, original
USB identity/inode/permissions match, and JTAG parameters are restored. See
[restoration.json](restoration.json).

The restoration archive is
`/home/deano/fes-retained/de25-terminal-programming-baseline-restored-physical-20261114-r1/restoration.tar.gz`,
SHA-256 `3f0e034bdf67fb3d23fe00fb03fe473dd804266c7e3dddfe7c580ac85e828b84`.
Its [independent readback proof](restoration-retention.json) preserves the actual
wire, process and programming records and references the immutable prepared and
failure archives. The failed diagnostic was not retried.

TRWU8CBL's last verified state is the original native baseline at final Park,
with the spare card installed, all our hardware handles closed and the continuing
top-kit claim held. The [saved programming comparison](programming-comparison.json)
shows that the restored baseline programmed successfully using the same fixed
3 MHz policy, vendor libraries and scan probe as the failed attempt. The
images differ; this comparison does not identify the programming failure's root
cause or qualify the new firmware. Next isolate the programming failure before
another firmware trial; do not infer a deadline problem from this stop.

QSPI, the factory card and the bottom kit remain untouched. General repeated
loading, software cold-reset recovery and DE10-equivalent readiness remain open.
