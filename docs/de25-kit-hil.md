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

Use the sealed DE25 packages from the exact PR head.

### Which packages a head seals

Powerboat's HILQ seals DE25 work from the recipes registered in
`config/core-recipes.toml` (`board = "de25-nano"`, Quartus Pro), never from a
list of producer names. `scripts/powerboat/de25_hilq.py` gives a D25 PR the
selection `de25:auto`; a `core:de25-<name>` label selects `fes.<name>` instead,
and `core:de25-all` selects every registered recipe. On fes-build-3090,
`scripts/powerboat/de25-seal-auto.sh` checks out the exact head and runs that
head's `scripts/de25_seal_plan.py`, which:

- asks each registered DE25 producer for its selection-only build record at
  the head and at the merge base with `origin/main`, without compiling;
- for `de25:auto`, builds each recipe and model whose design identity or
  toolchain selection moved, or that the merge base does not have. When no
  package input moved (a runtime, software or image change), it builds MENU so
  the kit still runs an exact-head package;
- refuses any name that is not a registered DE25 Quartus Pro recipe or model.

A producer with models (Spectrum `48`/`128`/`plus3`/`pentagon`/`native48`,
Atari ST `520st`, C64 `pal`/`nativepal`) offers them as
its `--model` choices. Each model is its own package with its own BUILD_ID, so
each is sealed and run on the kit. All models of one recipe share one source
closure, so a source change cannot be attributed to a single model: the
identities decide, and every model whose identity moved is built. A label or
override can name one model as `fes.spectrum@128`.

Each producer is judged by its `build-summary.json` (exact head revision,
package candidate, timing admitted, planned model) and one exported `.fcore`,
not by its exit code. `core-build.result` holds one line per admitted package,
`DONE <fcore> <sha256> <slug>`, and any failure makes the whole seal `FAIL`.
`scripts/powerboat/de25-seal-remote.sh` copies every package back to
Powerboat and re-verifies each digest.

### Run

`scripts/powerboat/hil-de25-core.sh` verifies every package's digest and
manifest before it claims J8G, then installs the exact-head software once and
smokes each package in turn under that one claim. The run passes only if every
package passes and the restore verifies. One package keeps the `out/` layout;
several packages each write `out/<slug>/`. The lander-watch and HILQ unit caps
grow by 2400 s for each package after the first.

Automatic exact-head HIL
exercises MENU through the normal volatile `fogcast core-load` path and other
packages through the normal install/launch lifecycle, compatibility checks, and
supported input path. The locked idle MENU is captured before and after as
restore evidence. The D25 smoke reads named ROM state only when the package
descriptor declares a ROM, checks post-stop idleness through the paired
session API, and fails closed if its generated API/ROM/idle probes do not
compile and match the D25 host. The MENU rejection was an explicit runtime
policy in `NativeHardware::LoadCoreInternal`: MENU was restricted to idle
configuration. The deployed agent endpoint and format-2 package reader were
present. The DE25 runtime now admits MENU for volatile development `core-load`;
catalog launches continue to reject MENU entries. Both paths compare the
serial-bound target state before and after. For
a full image or boot-path change, build the exact DE25 image and run the
corresponding boot checks as well; a package run does not qualify an image
change.

For non-MENU packages the D25 smoke passes both saved core frames (`10-core`
and `11-core`) to the exact head's `scripts/de25_boot_screen.py` and writes its
output to `boot-screen.json`; a recognised boot screen is a `boot_screen` check,
and a core the utility does not know (exit 3) adds no check. To check a capture
by hand:

```sh
python3 scripts/de25_boot_screen.py --core fes.atari-st \
  /path/to/10-core.png /path/to/11-core.png > /path/to/boot-screen.json
```

The check recognises a GEM desktop from its layout, so EmuTOS and TOS both
pass and OCR is not needed. The automated smoke also keeps its
changed-frame `core_video` check.

The approved HDMI node is `/dev/v4l/by-id/fes-ms2109-hil`. Save before, active,
input-response and restored frames. Review the frames to verify the screen
belongs to the claimed board and shows the expected UI or core. Do not open the
raw MS2109 device or `/dev/video0`/`/dev/video1`.

For the volatile development `fogcast core-load` smoke of `fes.menu`, the
active MENU plane is expected to be in its validated safe-black initial state:
`core-load` does not launch the kit's local tenfoot presenter. Verify the exact
package is active as `fpga_development`, then verify Stop returns to idle and
the baseline Welcome/MENU screen is visible again. Interactive menu
presentation is a separate check of the configured idle MENU and its local
presenter; a black frame during the volatile load does not establish that UI
acceptance.

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
  node. The runner uses bounded `ffmpeg` and `tesseract` attempts and saves
  the frames and OCR output in `out/restore-menu.*`. It rejects a room error.

Only then release the ledger and locks. Failed restore checks keep the ledger
claimed and trigger the documented recovery path; do not report HIL PASS.

## Main baseline promotion and recovery

DE25 main promotion is recorded in `current-restore-de25.json` independently
from the Kit A/B promotion. The D25 promotion runner serializes on
`hilq-state/de25-baseline-promote.lock`; it builds from fetched main with the
DE25 Quartus Pro 26.3 MENU and ZX81 producers, runs both exact packages on J8G,
and publishes only `current-restore-de25.json` after both smokes and the final
restore verifier pass. It never edits the Kit A/B baseline.

Before each exact-head package smoke, the Powerboat D25 runner also builds and
installs a managed DE25 kit-software bundle from that same full FES revision.
It checks that the active `mister-runtime` executable digest equals the
bundle manifest, then reactivates the recorded baseline bundle before running
the restore verifier. Bundle installation and activation use the ordinary
managed software-release client; no image or boot configuration changes.

The independent D25 timer stays enabled only after the first promotion passes
cleanly. A failed promotion leaves the timer disabled and the prior D25
baseline unchanged.

### J8G serial-checked recovery

The only recovery console in this procedure is
`/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8J8G-if02-port0` (J8G, observed
as `/dev/ttyUSB2`). Start with a receive-only probe using
`python3 scripts/de25_serial_recovery.py --probe --serial TRWU8J8G --output
<evidence>/serial-probe.json`. The tool refuses any other serial identity or
resolved port. It records Linux boot output and whether a login prompt exists;
the production image removes the serial getty, so recovery uses the enabled
kernel SysRq console path rather than an assumed login.

When network restore fails, keep the D25 claim and both locks. Before using a
serial reboot, verify that Stop completed and J8G returned to MENU. An HPS
reset can leave an older HPS-window bitstream running and stall the next boot
([#2245](https://github.com/DeanoC/fes/issues/2245)). If Stop fails or the
fabric state is unknown, use the receive-only probe and arrange the authorized
J8G power cycle.

With MENU verified, capture the last authenticated target and boot identity,
then run `python3 scripts/de25_serial_recovery.py --reboot --menu-confirmed
--serial TRWU8J8G --output <evidence>/serial-reboot.json`. This sends a serial
BREAK followed by SysRq `b` and records the boot output. Wait for the same J8G target ID to return on
the network, capture fresh post-reboot state, and create a recovery receipt
with the before/after boot IDs and observed Linux boot marker:

```sh
EVIDENCE=/absolute/path/to/run/evidence
python3 scripts/de25_serial_recovery.py \
  --receipt-from "$EVIDENCE/serial-reboot.json" --serial TRWU8J8G \
  --before-boot-id "$(jq -r '.boot_id' "$EVIDENCE/baseline-before.json")" \
  --after-boot-id "$(jq -r '.boot_id' "$EVIDENCE/baseline-after.json")" \
  --target-id "$(jq -r '.target_id' "$EVIDENCE/baseline-before.json")" \
  --output "$EVIDENCE/serial-recovery-receipt.json"

python3 scripts/de25_restore.py --baseline ~/fes-hil/baselines/current-restore-de25.json \
  --before "$EVIDENCE/baseline-before.json" --after "$EVIDENCE/baseline-after.json" \
  --serial-recovery "$EVIDENCE/serial-recovery-receipt.json"
```

The verifier permits the boot ID change only with that serial receipt, which
must bind the observed before and after boot IDs to the same target. If the
recorded baseline boot ID was already stale, the verifier reconstructs its
native-state bytes by substituting that recorded boot ID into the captured
before and after states. Both reconstructed hashes must match the recorded
full baseline hash. Each capture must contain exactly one matching boot ID and
its own correct native-state hash; changing any other byte fails verification.
Image, bundle, target ID, runtime idle and free lease must still match.

If J8G does not return after the serial reboot, recover it from the retained
LAN managed image through the appliance recovery/update path for J8G only, with
the image digest checked against the serial-bound recovery record. Never select
a device node by enumeration; positively identify J8G's SD device before an
SD reflash. After image recovery, restore or activate the named baseline bundle
as image compatibility requires, boot and identify J8G, and rerun
`scripts/de25_restore.py` before FREE. Reimage/reflash is a destructive kit
action and needs Deano's explicit OK first (see `AGENTS.md`), even though J8G
is a disposable dev kit. Never touch TRWU8CBL or its ttyUSB0/ttyUSB1 ports.

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
