# ADR 0001: Portable target runtime

## Status

**Accepted** — 2026-08-08.

## Context

POC1 and POC2 deliberately kept `Main_MiSTer` as the compatibility runtime and
kept a host-driven sidecar boundary. Those decisions remain valid for their
documented POC scope and accepted evidence. POC6 then demonstrated a real-game
host-to-MiSTer HDMI path with managed lifecycle, but it also exposed the cost
of treating target hardware behavior as an opaque external process: external
FFmpeg, Linux devices, a disposable native presentation hook, multiple
supervisors, SSH development, and operational reconciliation.

POC6 remains accepted only for its documented video/lifecycle scope. It does
not prove controller symmetry, measured physical latency, a production
appliance, a portable runtime, or non-Linux viability. Its retained mechanisms
must not become permanent public contracts.

FogCast needs a path that preserves MiSTer compatibility while separating the
stable host experience from target-native implementation details and enabling
future board/platform work without prematurely committing to bare metal.

## Decision

FogCast will maintain a long-lived GPLv3 `Main_MiSTer` fork and incrementally
extract core-facing hardware compatibility behavior into `libmister-runtime`.
The traditional executable will become a thin compatibility wrapper over the
same library and remains the behavioral comparison and rollback path while a
headless FogCast runtime matures.

The public, versioned FogCast host/target session protocol remains independent
of `Main_MiSTer`, codecs, Linux paths/device nodes, and target process layout.
During migration, the Go `mister-agent` retains authentication, request
admission, cache operations, and supervision, and uses versioned private local
IPC to a native runtime. The native runtime commits authoritative target-local
state and is the sole hardware-mode coordinator. A narrow C ABI provides the
library boundary; its concrete types and lifecycle details require focused
design before implementation.

Linux is first, but platform services isolate Linux-specific mechanisms from
portable runtime code. Overlord is the intended generator and composition
authority for the DE10-Nano/Cyclone V target and later platform choices. It
becomes required for the retained POC6 path only after a narrow Linux slice
reproduces the required build and passes software and HIL gates.

Each target resource has one runtime owner, each reused library has one source
authority, and every launch uses a fresh `(session, generation)` identity.
Changes are staged through the active roadmap and retain a verified rollback
path before target mutation.

## Alternatives considered

### Keep stock `Main_MiSTer` and extend sidecars indefinitely

This preserves short-term compatibility but leaves hardware lifecycle opaque,
keeps Linux/testbed details close to host-facing behavior, and perpetuates
competing supervision and reconciliation boundaries.

### Clean-room rewrite of `Main_MiSTer`

This would require re-establishing FPGA, core, input, video, audio, storage,
and save compatibility. It is a separate research program with unacceptable
risk for the current migration.

### Make a non-Linux or bare-metal target the immediate goal

This would prematurely require FogCast to own USB, networking, filesystems,
codecs, and device support without evidence of material product benefit.

### Make Overlord mandatory immediately

This would risk the accepted POC6 testbed before the required narrow Linux
vertical slice can reproduce the build and demonstrate behavioral equivalence.

## Consequences

- The target becomes an appliance with explicit lifecycle, recovery, and
  resource arbitration rather than an assumption about one process.
- `Main_MiSTer` compatibility is preserved while global behavior moves behind
  `libmister-runtime` in small, reviewable extraction steps.
- The host protocol can survive a Go sidecar, native runtime, or later unified
  target composition without exposing target-private implementation details.
- Linux support remains necessary in the near term, while platform boundaries
  make future portability measurable rather than speculative.
- Existing historical libraries are candidate inputs, not implicit
  dependencies; `ikuy_std_resources` is the curated catalog until an explicit
  promotion decision says otherwise.
- Every cross-repository dependency requires immutable provenance, license
  review, configured-feature record, and artifact hash.
- Migration work carries added test, reproducibility, HIL, and rollback gates;
  it may proceed more slowly than an unbounded refactor but keeps evidence and
  recovery intact.

## Superseded future guidance

For future architecture, this ADR supersedes the forward-looking implication
of POC1/POC2 that FogCast should permanently retain a stock opaque
`Main_MiSTer` plus host-driven sidecars as the architecture. It does **not**
invalidate their historical decisions, POC evidence, or scope.

This ADR also supersedes any future-facing assumption that POC6's external
FFmpeg path, Linux framebuffer/command devices, SSH development, or disposable
presentation hook are destination contracts. They remain retained-testbed
mechanisms until a later accepted gate replaces them.

## Qualification

[ADR 0002](0002-disposable-local-development-target.md) qualifies only the
per-operation authorization, production-hardening, physical-state-preservation,
and rollback prerequisites for one exact privately designated disposable local
development kit. It does not revise this ADR's runtime, protocol, ownership,
portability, provenance, reproducibility, HIL, or compatibility decision, and
it does not apply to production or any other target.

## Links

- [Canonical architecture](../ARCHITECTURE.md)
- [Active migration roadmap](../ROADMAP.md)
- [ADR 0002: Disposable local development target](0002-disposable-local-development-target.md)
- [Portable target runtime design](../superpowers/specs/2026-08-08-fogcast-portable-target-runtime-design.md)
- [POC6 accepted results](../POC6-RESULTS.md)
- [POC6 retained-testbed guide](../POC6-DEVELOPMENT.md)
