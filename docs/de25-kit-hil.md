# DE25 Kit D25 development and HIL

Kit D25 is the managed DE25-Nano kit with serial `TRWU8J8G` and kit id
`de25-j8g`. The top board, `TRWU8CBL`, is a separate kit and is not available
while its ledger says recovery hold. Never select it as a fallback.

## Select and claim

Select D25 for DE25 PRs when their changed artifacts target
`core:de25-menu`, `core:de25-zx81`, `core:de25-ALL`, DE25 board/runtime paths,
or the DE25 image. DE10 PRs continue to use Kit A or Kit B. A kit mismatch
cannot satisfy the HIL gate. The repo HIL planner and Powerboat HILQ both route
these changes to D25 only. HILQ launches the exact-head runner through
lander-watch with an explicit cap and PR/head identity. A/B dispatch continues
to use the existing queue and runner paths.

Before accessing D25, acquire both
`~/kit-status/de25-TRWU8J8G-STATUS.lock` and
`~/kit-status/de25-TRWU8J8G-STATUS.md.lock` with exclusive `flock`, inspect the
last ledger line, and append a `KIT D25 IN USE` entry containing owner, PR,
head, scope and evidence path. Keep both locks for the whole run. On completion
append `KIT D25 FREE (baseline verified)` only after all restore checks pass.
The pre-existing `.UART.lock` and `.reader.lock` are for those separate
interfaces and do not replace the kit claim.

## Run and capture

Use the sealed DE25 package from the exact PR head. The MENU display package is
loaded with FogCast's volatile `core-load` development path, checked while
active, captured, then stopped back to the locked idle display. Playable
packages such as ZX81 use the normal install/launch lifecycle, compatibility
checks, and supported input path. Both paths compare the serial-bound target
state before and after. For a full image or boot-path change, build the exact
DE25 image and run the corresponding boot checks as well; a package run does
not qualify an image change.

The approved HDMI node is `/dev/v4l/by-id/fes-ms2109-hil`. Save before, active,
input-response and restored frames. Review the frames to verify the screen
belongs to the claimed board and shows the expected UI or core. Do not open the
raw MS2109 device or `/dev/video0`/`/dev/video1`.

## Restore baseline

The DE25 baseline is separate from the two-kit DE10 baseline. Its machine
record is `~/fes-hil/baselines/current-restore-de25.json`; it names the
managed image, selected bundle, target id, boot identity, and last passing
restore evidence. A run records a fresh pre-state, then verifies after Stop:

- The target id and boot identity match the claim.
- The managed image and active bundle match the baseline record.
- The target is ready, runtime idle and lease free.
- The root, kernel, boot files, persistent data mount and protected state
  match the pre-run capture or the named baseline diff.
- The restored FES Welcome/MENU screen is visible on the approved capture
  node.

Only then release the ledger and locks. Failed restore checks keep the ledger
claimed and trigger the documented recovery path; do not report HIL PASS.

## Main baseline promotion and recovery

DE25 main promotion is recorded in `current-restore-de25.json` independently
from the Kit A/B promotion. The D25 promotion runner serializes on
`hilq-state/de25-baseline-promote.lock`; it builds from fetched main with the
DE25 Quartus Pro 26.3 MENU and ZX81 producers, runs both exact packages on J8G,
and publishes only `current-restore-de25.json` after both smokes and the final
restore verifier pass. It never edits the Kit A/B baseline.

Serial-console recovery remains a separate owner task. The J8G console path is
owned by `root:dialout` and the current automation account cannot open it; do
not attempt console recovery, card writes or reimaging until Deano has restored
access and authorized that recovery workflow. Keep the retained J8G recovery
materials intact. Never use J8G recovery authorization on TRWU8CBL.

## Add the second DE25 board later

On TRWU8CBL, first wait for Deano's physical reset and explicit release of the
recovery hold. Then independently verify serial, MAC, SSH host key, running
image, active bundle and baseline; make a retained card backup; add its own
serial ledger, locks, restore record, capture mapping and selection id; run the
full claim/deploy/launch/input/capture/restore flow; and add it to promotion
only after a passing restore. Do not reuse J8G's address, host key or baseline.

## Evidence boundary

The 2026-11-16 infrastructure proof imported the already-qualified ZX81 package
`e873bc2e20097c6c3250b087cf9c522581ee4a0e4140ba2dcf1d1220e45aad0b` on J8G,
checked compatibility, launched it, sent F12 through Linux uinput, captured
the DE25 media selector, stopped to idle, and verified the pre/post restore
diff. This proves the kit path; it does not qualify any later PR artifact.
Relevant DE25 PRs still need exact-head HIL and their normal Quartus, timing,
seal and image gates. DE25 remains Quartus-only; nextpnr, Yosys and OSS checks
are not DE25 production gates.
