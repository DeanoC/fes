# ADR 0002: Disposable local development target

## Status

**Accepted** — 2026-08-08.

## Context

FogCast needs rapid iteration on a local MiSTer Pi while the portable target
runtime is developed. The operator has designated one exact private local kit
as disposable project development hardware. Requiring per-operation
confirmation, production hardening, preservation of its current physical state,
or a POC6 restoration path for this kit would protect state the operator has
explicitly chosen not to preserve.

The designation is private. Hardware type, hostname, IP address, and discovery
are mutable or ambiguous and cannot prove that a target is the authorized kit.
The POC6 hashes are historical evidence for accepted POC6 artifacts, not
evidence of any device's current image, software, configuration, credentials,
or physical state.

## Decision

The operator-designated local MiSTer Pi is a disposable FogCast project
development target. It has standing operator authorization, without
per-operation confirmation, for:

- project access and deployment;
- reboot;
- software, image, configuration, and credential replacement; and
- wipe and rebuild.

Production hardening, preservation of prior physical state, and the tracked
POC6 rollback lock/runbook are not prerequisites for those actions on this
exact kit. Reports record the rollback gate as **Not required per ADR 0002**.

Before any destructive action, the actor must resolve the designation through
operator-controlled private configuration and verify the exact target identity.
If designation or identity verification is missing, ambiguous, or mismatched,
the action stops. No secret value or private target identifier is recorded in
this ADR, source control, generated manifests, command arguments, or reports.

## Scope and retained requirements

This decision applies only to the privately designated exact kit. It cannot be
inferred or transferred based on target type, hostname, IP address, discovery,
similar configuration, or prior use. Other development targets and all
production targets retain the existing per-operation authorization, rollback,
recovery, and security rules.

The waiver does not relax:

- secret handling;
- lifecycle and resource ownership, bounded teardown, or reconciliation;
- reproducibility and provenance;
- HIL classification or the separation of machine observations, operator
  observations, and inferences;
- artifact hashes and fresh hashes for rebuilt or replaced artifacts; or
- compatibility and behavior-equivalence comparison.

A behavior-equivalence claim still requires a valid known-good comparator with
recorded provenance. The kit's previous physical state need not be preserved
and cannot be assumed to supply that comparator.

This decision does not enable unattended production updates. Automated target
updates remain disabled until the accepted update design defines signed
manifests, an immutable trust root, key lifecycle, downgrade policy,
verification before installation, atomic or A/B activation, power-loss
recovery, and automatic rollback.

## Consequences

- Authorized project work can wipe and rebuild the kit without preserving its
  prior state or first constructing a POC6 rollback package.
- Loss of unrecorded local state on the kit is accepted; required source,
  provenance, artifact hashes, and evidence must live in their designated
  durable locations.
- Destructive automation must fail closed unless private designation and exact
  identity verification both succeed.
- Evidence from work on the kit is classified by the checks actually run; the
  standing authorization itself proves no software, reproducibility, HIL, or
  acceptance status.
- Production and every other target remain governed by ADR 0001 and the normal
  authorization, rollback, recovery, and security gates.

## Links

- [Canonical architecture](../ARCHITECTURE.md)
- [Active migration roadmap](../ROADMAP.md)
- [ADR 0001: Portable target runtime](0001-portable-target-runtime.md)
- [Portable target runtime design](../superpowers/specs/2026-08-08-fogcast-portable-target-runtime-design.md)
- [POC6 accepted results](../POC6-RESULTS.md)
