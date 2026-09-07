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
- [ ] Select clean checkpoint inputs in isolated FES; run consistency and host build.
- [ ] Prove watchdog arming and root switching with the unchanged kernel.
- [ ] Implement/test immutable release storage and consumed-trial selection.
- [ ] Implement/test stable bootstrap and independent watchdog guard.
- [ ] Implement/test target endpoints and coordinator update admission.
- [ ] Implement/test host upload, activation, discovery and confirmation.
- [ ] Assemble reproducible bootstrap/release media with external manifests.
- [ ] Run independent review, complete checks and physical fault acceptance.

Each implementation task receives a concrete file/interface checklist before
code changes. Boot-dependent implementation waits for the primitive feasibility
result; release-baseline work proceeds independently. No release or hardware
success is claimed by these unchecked tasks.
