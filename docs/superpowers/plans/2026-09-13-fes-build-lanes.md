# FES build lanes and format-1 retirement

> Execute this plan task by task. The spec is
> docs/superpowers/specs/2026-09-13-fes-build-lanes-design.md.

## Global constraints

- Preserve the dirty parent checkout at /home/deano/fes; work only in the
  isolated worktrees for this plan.
- misteross is a separate prerequisite PR. FES must pin its reviewed commit
  before its integration tests claim the new producer interface.
- Pong, ZX81 and Coleco production nextpnr routes use HIP/GPU and fail closed
  without route-log proof of a live HIP backend.
- Shared-cache selection is explicit --cache-root; ambient
  FES_TOOLCHAIN_* variables must not select a producer lane.
- Preserve HIP compiler identity variables for HIP rows while scrubbing
  caller-owned build overrides; do not apply the old OFF-lane sanitizer to
  HIP producers.
- Keep two distinct cache identities: repository-lock HIP for Pong/ZX81 and
  the specialized Coleco HIP lock.
- Format-1 generic FPGA bundles are not part of the default FES production
  path. Keep idle plus the selected format-2 package, and reject unsupported
  multi-package image selection explicitly.
- Quartus is an oracle/check unless nextpnr lacks system support; never
  auto-fallback from a failed HIP route.
- Use exact package/build evidence. Do not claim hardware acceptance from
  simulation, a green watcher or a Quartus build.
- Do not merge either PR without the user's explicit merge approval.

## Task 1: Standardize the misteross FES HIP producer contract

Owner: fpga-worker in /home/deano/fes/out/dev/fes-hip-format2/misteross.

- [ ] Add explicit --cache-root PATH to Pong, ZX81 and Coleco producer CLIs.
- [ ] Make shared-cache mode depend on that argument, not ambient cache-root
  environment selection; preserve local mode when it is omitted.
- [ ] Add the common explicit HIP lane parameters to Pong and ZX81. Their
  nextpnr commands must contain --router gpu, their records must contain
  the HIP configuration, and route validation must reject CPU-reference
  fallback or missing live HIP evidence.
- [ ] Keep Coleco's specialized lock and existing HIP route proof, but accept
  the same explicit cache-root interface and authenticate the same lane.
- [ ] Ensure every authentication pass during a build uses the same explicit
  cache root and lane, including the final identity recheck.
- [ ] Add focused tests for CLI forwarding, cache-root isolation, cache-key
  separation, route proof and CPU fallback rejection. Run the producer
  focused tests and the repository's relevant check target.
- [ ] Update producer README/build help to document HIP as the standard FES
  nextpnr lane and Quartus as oracle-only where applicable.

Handoff: commit SHA, tests, and the exact producer interface. FES Task 2 must
not assume the upstream change is present until this task is reviewed.

## Task 2: Replace the Pong-only FES resolver with a recipe registry

Owner: integrator in /home/deano/fes/out/dev/fes-hip-format2/fes, after Task 1
is pinned.

- [ ] Add a small immutable recipe descriptor registry for fes.pong, fes.zx81
  and fes.coleco, including producer script/module, lock path, HIP
  router/architectures, explicit cache-root lane and selection filename.
- [ ] Generalize canonical build-record derivation, package inspection,
  package miss/hit reuse and format-2 selection validation over the descriptor.
- [ ] Invoke producers with --cache-root explicitly and preserve HIP identity
  variables in the per-lane child environment. Do not export a shared-cache
  selector as a side effect.
- [ ] Accept the three core IDs in the profile registry. Keep the current
  image single-package contract explicit; reject duplicate/unknown/multiple
  selections with actionable errors until the target-image selector supports
  a package set.
- [ ] Add tests proving each recipe dispatches its producer, uses the correct
  lock/cache lane, emits its core ID, reuses a matching package, builds on a
  miss and rejects a mismatched package.
- [ ] Pin the reviewed Task 1 misteross commit in the FES branch.

## Task 3: Retire format-1 from the FES production path

Owner: feature-worker in the same FES worktree, after Task 2.

- [ ] Remove format-1 bundle construction from the default integration
  profile and build/image/dev actions. Do not run Quartus or generic
  fetch-core/rebuild-core/export-core-bundle for FES production.
- [ ] Keep only idle plus the selected format-2 package in the default image
  path. Remove the default catalog MegaDrive/SNES/NES/Pong bundle inputs and
  make the image scripts operate without a MegaDrive selection.
- [ ] Preserve historical profile compatibility only where it is clearly
  isolated; do not let historical format-1 settings silently affect the
  default integration profile.
- [ ] Update receipts, diagnostics, make verify, development-image reuse and
  target-image shell tests for the package-only path.
- [ ] Document that ZX81/Coleco are resolver-supported but not yet
  multi-package image-selected until the generic target-image selector is
  enabled.

## Task 4: Documentation and matched build-cache evidence

Owner: integrator.

- [ ] Update project/component maps and development docs to describe FES-first
  ownership, the three recipe lanes, HIP standard routing, and Quartus oracle
  boundaries.
- [ ] Add a dated validation record covering host miss/hit, existing Coleco
  HIP hit, Pong/ZX81 HIP toolchain evidence, package miss/hit and parent
  reuse. Record exact branches, pins, commands, hashes and whether Yosys,
  nextpnr, Quartus or QEMU ran.
- [ ] Keep cold HIP builds scoped to isolated worktrees/cache slots and do not
  touch the dirty parent or unrelated worktrees.

## Verification and integration

- [ ] Run focused misteross tests, git diff --check, then request the upstream
  PR review.
- [ ] After pinning the upstream commit, run FES focused bundle/core/image
  tests, make check, and the full available FES test command.
- [ ] Run an independent whole-branch review against the spec and this plan.
- [ ] Push branches and open PRs with exact verification evidence. Leave both
  PRs ready to merge; do not merge them.
