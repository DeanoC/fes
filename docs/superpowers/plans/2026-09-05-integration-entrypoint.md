# FES integration entry point implementation plan

> Agentic workers execute bounded tasks with disjoint file ownership; the root
> integrates and verifies the result in this session.

**Goal:** Make FES a usable shared entry point with a reconciled native profile.
**Architecture:** Pinned Git components, profile-selected historical revisions,
existing child recipes, Python standard library orchestration and Go emitter.
**Spec:** ../specs/2026-09-05-integration-entrypoint-design.md

## Constraints

- Preserve both existing profile source combinations.
- No new runtime coordinator or packaging framework; no Main production pin.
- Keep source checkouts clean; edit components in separate worktrees.
- Do not publish changes or claim hardware acceptance from software tests.

## Tasks

- [x] Audit merged consumers and record exact package/runtime/FogCast pins.
- [x] Add regression coverage for historical revision selection and mismatched
  runtime locks; implement profile selection in scripts/inputs.py and build.py.
- [x] Add matching/mismatching recipe tests, then validate bundle recipe hashes
  on fresh builds, reuse and explicit verification.
- [x] Pin mister-packages and add scripts/consistency.py with real temporary
  regeneration and source-lock comparison; cover drift rejection in tests.
- [x] Add native-integration-dev using the merged child's source-bundle API,
  publish its selection record, and keep legacy overlay only for old sources.
- [x] Add root AGENTS.md, development handoff guide, README entry points and
  lightweight CI. Reconcile stale ownership and capability statements.
- [x] Run parent tests and consistency, build host/image, verify packaging and
  reuse; independently review the final changes and record evidence/limits.
