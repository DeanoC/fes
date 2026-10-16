# Retained-base kit software workflow: validation

[PR #732](https://github.com/DeanoC/fes/pull/732) completed host validation,
image qualification, final-image adoption and failed-candidate recovery, followed
by two Pong launch/input-API/Stop cycles, captured HDMI output and installed-core
byte persistence checks. The
[sanitized evidence](2026-10-16-kit-software-workflow/evidence.json) separates
prior hardware observations, failed recovery, artifact-specific results and limits.

## Scope and exact inputs

FES owns this slice: `make kit-software` builds the agent, runtime and both
launchers against a verified retained base image, without OS or FPGA producers.
It publishes an immutable bundle and provenance receipt. Startup verification
precedes the shared sixty-second stop/start/readiness deadline. The supervisor
waits for authenticated native initialization completion before dependents start;
only the exact terminal failure permits fenced diagnostics, and native idle
remains mandatory for acceptance. Explicit root-only acknowledgment and
power-cycle preparation preserve failed-operation audits and newer intent.

Base commit: `011e647417af2a17a985b0ad8710208e9c3f71e1`. Final compiled source:
`9af8dbb2b8d77f622a4414d8969efe5cb5f3cb5e`. This evidence-only addition changes
no compiled inputs. Network APIs, core ABI and base compatibility contracts are
unchanged; physical lifecycle ownership remains in the runtime.

## Host and image validation

The public producer passed thirteen focused tests and independent archive **and
receipt** reproducibility checks. Wrong image hashes and existing outputs were
rejected without overwriting prior artifacts. Platform tests, focused race tests,
ARMv7 builds, consistency, documentation links and independent agent review pass.

The initial aggregate `make test-changed` failed six manifest cases under inherited
umask 0002. Its parent lane subsequently passed all 950 tests, with 38 skips,
under umask 0022. All nine non-FPGA downstream commands and all twenty-two FPGA
commands passed separately, including 1,144 Python tests with eight skips and the
planned RTL simulations. Protected FPGA input identities match final source.
These separate results do not turn the original aggregate invocation into a pass.
All 49 reported CI checks passed at the final code commit.

Two independent 192 MiB image passes, `make verify` structural/package/QEMU smoke
checks, and versioned release creation passed. Image SHA-256:
`32f16cd2eba9befefaef7f6c94ee70f5c830b3c231e859222df7c7aee7b3b993`.
Release version: `0.2.0-kit-software-workflow.20261016`; release identity:
`89151e352f12c985af47faf7355fedc7d6db44cb4c1da59c8ae4fcab4d8fd046`.
QEMU checks packaging and boot prerequisites, not FPGA hardware or agent readiness.

## Prior designated-kit observations

On the authorized `.84` kit, a public bundle containing four changed executable
digests installed behind a held lease and activated after release without reboot.
Merged-main image adoption, queued activation across reboot, cancellation and
accepted-selection persistence across an ordinary reboot passed. Six original
core-library files and selection bytes remained unchanged. These observations
cover the recorded earlier artifacts, not the final supervisor image above.

The deliberately failing candidate did **not** recover on the earlier image.
Native status reported `reboot_required`, `idle_failed/recovery`, with
`ADV7513 I2C device not found`. Verification consuming startup time and dependent
failure interrupting native initialization were independently established hazards;
neither establishes the cause of that observed hardware fault.

The source-`095cf32b` power-preparation helper durably audited and cancelled that
failed request while preserving selection, generation 5, base, boot and process
identities. It left maintenance held; it restarted nothing and released no
admission. A subsequent bounded twenty-minute read-only monitor saw no boot
change in 48 snapshots.

At the user's request, one explicit software reboot then restored readiness,
idle runtime status and free admission for over ten seconds. Boot changed from
`c7505aea…` to `687e01ff…`; base `a9b7718c…`, selected bundle `7d71e90f…` and
generation 5 were preserved. No physical power cycle occurred. The earlier claim
that a soft reboot could not suffice was too strong: this reboot restored the
observed state. The original automatic failed-candidate recovery remains failed.

## Final-image adoption and recovery

The supported appliance updater staged, activated and confirmed the exact
`32f16cd2…` image above. Boot changed to `e9c755b2…`, the image became confirmed
good with `trial: false`, and base identity matched the qualified image hash.
Persistent bundle `7d71e90f…` was reconciled at generation 6; it remained selected
and active with no pending or unreleased operation. Post-adoption readiness,
idle runtime status and free admission remained stable for over ten seconds.
The final base/supervisor is source `9af8dbb2…`; the persistent userspace bundle
retains its recorded source `b463f445…`.

On that final supervisor image, deliberately failing candidate `b69e84c0…`
produced one durable `recovered` receipt. Selected bundle, generation 6, base and
boot remained unchanged, with fifteen seconds of healthy/free state afterward
and no reboot. The observer recorded one entry into recovery and no replay.
Source and host tests establish the bounded recovery policy; these API
observations do not count every process start.

## Pong, HDMI and persistence checks

The first isolated-input attempt refused a runtime metadata pin of `b463f445…`
before any game cycle, and its container was removed cleanly. The corrected pin
is the base runtime artifact revision, `9af8dbb2…`; the actual selected userspace
bundle remains `7d71e90f…`, built from `b463f445…`. The compatible private host is
`beeeb0b4…`. These components do not need identical source commits.

The corrected [input diagnostic](2026-10-16-kit-software-workflow/input-receipt.json)
passed both initial and restart cycles. Each held the exact owned active session
for fifteen seconds, acknowledged two API input events and advanced the input
frame counter by two. Ordinary Stop and identity checks completed; both private
containers exited successfully and were removed. This proves the recorded API
input path and lifecycle, not physical controller delivery or hardware consumption.

A 67-second HDMI recording shows Pong during both active cycles and the launcher
after Stop. The capture was intentionally stopped with SIGINT, returning 255;
`ffprobe` confirmed a complete H.264 MP4. Independently reviewed frames show
[initial Pong](2026-10-16-kit-software-workflow/hdmi-frame-03.png),
[restart Pong](2026-10-16-kit-software-workflow/hdmi-frame-09.png) and the
[restored launcher](2026-10-16-kit-software-workflow/hdmi-frame-12.png).

The [persistence comparison](2026-10-16-kit-software-workflow/core-persistence.json)
found all six original installed-core files unchanged after final adoption,
recovery and both Pong cycles, including the archive, members and selection bytes.
It does not claim Coleco execution. The final observation held ready, idle and
free for over ten seconds on confirmed image `32f16cd2…`, selected bundle
`7d71e90f…`, generation 6, with no pending or unreleased operation.

This completes the workflow's bounded acceptance checks. Physical controller
delivery, hardware input consumption and Coleco execution are not claimed.
The next step is to mark the PR ready for the grokbot review pipeline.
