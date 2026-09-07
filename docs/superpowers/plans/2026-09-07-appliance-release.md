# FES appliance release implementation plan

**Goal:** Deliver a versioned native system that can update and automatically
return to a known-good image when a trial fails.

**Architecture:** Stable loop-mounted bootstrap, immutable system images,
watchdog-bounded trial, authenticated lease-owned update operations.

**Spec:** ../specs/2026-09-07-appliance-release-design.md

## Sequence

- [x] Inspect current boot/kernel/watchdog paths and preserve other worktrees.
- [x] Obtain explicit choice of automatic fallback for pre-init failure.
- [x] Restore the missing runtime media-timeout lock and stale test fixtures.
- [x] Reproduce and clear the existing discovery vet warning; run focused tests.
- [x] Select clean checkpoint inputs in isolated FES; run consistency and host build.
- [x] Prove watchdog reset and return with the unchanged kernel (root switching passed).
- [x] Implement/test immutable release storage and consumed-trial selection.
- [x] Implement/test stable bootstrap and independent watchdog guard.
- [x] Implement/test target endpoints and coordinator update admission.
- [x] Implement/test host upload, activation, discovery and confirmation.
- [ ] Assemble reproducible bootstrap/release media with external manifests.
- [ ] Run independent review, complete checks and physical fault acceptance.

Each implementation task receives a concrete file/interface checklist before
code changes. Boot-dependent implementation waits for the primitive feasibility
result; release-baseline work proceeds independently. No release or hardware
success is claimed by these unchecked tasks.

## Integration progress

FogCast checkpoint `276eeb66b1fe3f501f0757a8290de1d37c1e1798` includes the
runtime timeout pin, bootstrap/store/HTTP/operator client, source guides and tests.
FES checkpoint `1c18e79` selects it. Full Go tests, affected race tests, vet,
static ARM builds and parent consistency pass. The stable ext4-to-ext4 pivot,
PID 1 execution, read-only root and retained guard namespace passed a real isolated
container test. The initial cold two-pass rootfs build and structural/QEMU
packaging checks pass with locked kernel and reused validated FPGA bundles. Both
passes produced `5ca866def7d1f2652c734b43064ae972599974bfada49a33a025cdb08981ee5d`.

Independent review fixed visible-versus-durable confirmation, rejected-trial
cleanup, truthful known-good/previous state after factory fallback, cancellation,
startup lease reconciliation, and source-bound artifact snapshot/provenance.
Appliance card assembly needs a 1 GiB data partition to hold factory, good,
previous and a candidate; the original 256 MiB layout is insufficient. Pinned
SPL bytes locate the A2 partition via MBR, supporting relocation after the larger
FAT partition while retaining locked bootloader/kernel bytes.

Two diagnostic guard runs on the designated idle kit acknowledged hardware
arming but did not return ready within 120 seconds. The first used an accelerated
2-second device timeout; the second used the production 30-second timeout and
5-second heartbeat, with only the trial deadline shortened to 3 seconds. The
operator confirmed a manual power-cycle after the first test. The existing
rootfs and boot files were not changed. The production-timeout result means
the short-timeout hypothesis alone does not explain recovery failure. Preserve
both failed records; neither establishes automatic fallback acceptance.

The 1 GiB appliance geometry and recipe-change checks pass 3 real pinned-container
appliance tests and 8 host tests. All 31 original-media container tests pass;
side-by-side assembly with the original function produced identical original-
layout bytes. This validates host assembly, not physical boot of the new layout.

The controlled warm-boot test isolated retained-OCRAM boot as the recovery issue.
FogCast `980be19710a5e1ab3d5f98ab74d01eb51509844d` adds reviewed ARM DE10-nano
reset preparation before opening the watchdog: completed-preloader marker and
retained-RAM disable, with ordered readbacks. Its first physical guard diagnostic
returned to a new idle boot in 64.23 seconds and retained preloader index 0.
After the other development task released its lease, five consecutive watchdog
recoveries passed with an uninterrupted boot-ID chain and preloader index 0.
The final boot stayed ready for 180 seconds under a fresh lease. A separate
confirmation-close diagnostic also kept the same boot ready for 180 seconds.
The operator reported no additional manual power-cycle during an intervening
boot. Post-release reboots remain unattributed; identified idle cleanup code does
not request reboot. These results prove the controlled watchdog primitives,
not the complete new bootstrap/update path. The final source-selected cold build
is running against the reset fix.
