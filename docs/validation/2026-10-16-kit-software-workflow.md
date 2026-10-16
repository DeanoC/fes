# Retained-base kit software workflow: partial acceptance

[PR #732](https://github.com/DeanoC/fes/pull/732) remains draft. Host validation
and image qualification passed; final hardware acceptance is incomplete. The
[sanitized evidence](2026-10-16-kit-software-workflow/evidence.json) separates
prior hardware observations, failed recovery and pending checks.

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
admission. The selected bundle remains `7d71e90f…` on base `a9b7718c…`.
A bounded twenty-minute read-only monitor saw no boot change in 48 snapshots.
Physical recovery has not been confirmed.

## Remaining integration

1. Physically disconnect and reconnect `.84` power, then verify fresh native idle,
   readiness, free admission and preserved selection, respecting newer intent.
2. Under the kit lease, adopt and confirm the exact qualified image above through
   the supported appliance updater.
3. Repeat failed-candidate recovery and prove the single bounded recovery attempt.
4. Complete HDMI/input, installed-core launch and persistence acceptance.
5. Update this evidence and mark the validated PR ready for the grokbot pipeline.

A front-panel reset or soft reboot does not substitute for the required physical
power cycle. No final-image hardware acceptance is claimed.
