# FogCast active migration roadmap

## Status and authority

**Status:** Active migration sequence, designed on 2026-08-08; no stage below
is claimed complete by this document.

This roadmap translates the approved [portable target runtime design](superpowers/specs/2026-08-08-fogcast-portable-target-runtime-design.md)
into gates. It is subordinate to accepted evidence,
[ADR 0001](adr/0001-portable-target-runtime.md), and the disposable-target
qualification in [ADR 0002](adr/0002-disposable-local-development-target.md).
The current architecture is [ARCHITECTURE.md](ARCHITECTURE.md). Historical POC
roadmaps describe their completed or scoped POC work; they are not this active
migration plan.

## Status vocabulary

Every work item must use evidence-appropriate status rather than inferred
progress:

| Status | Meaning |
| --- | --- |
| Designed | Approved direction or gate exists; no implementation or evidence is implied. |
| Software-tested | The stated automated tests passed for identified source and artifact inputs; no reproducibility or physical claim is implied. |
| Reproducible | Independent clean builds from pinned inputs met the stated reproducibility comparison; no physical behavior is implied. |
| HIL-observed | The stated behavior was physically observed on identified hardware and artifacts; scope is limited to the recorded observation. |
| Accepted | The gates explicitly required for the stated scope were reviewed and accepted. For a migration stage, this includes its required software, reproducibility, and HIL gates; it never imports gates that its historical evidence did not require. |

POC6 is **Accepted** only for its documented host-to-MiSTer HDMI video,
authenticated session ownership, and managed lifecycle scope. See
[POC6 results](POC6-RESULTS.md). Its controller-symmetry and physical-latency
gates remain unaccepted. Its historical acceptance does not assert fresh-
checkout reproducibility or completion of the stricter migration-stage gates.

## Cross-stage rollback gate

Before **any Stage A or later mutation of a non-exempt target**, create and
review both tracked rollback controls:

- `build/poc6-rollback.lock.toml`, identifying by immutable commit or hash the
  POC6 image, kernel, Main binary, agent, decode bridge, presentation hook,
  supervisor configuration, cores, and non-secret configuration inputs; and
- `docs/runbooks/poc6-rollback.md`, covering authorized retrieval, hash
  verification, bounded restoration, health checks, and required physical
  acceptance observations.

Large or private artifacts remain in the authorized artifact store; the tracked
lock contains only safe identifiers and hashes. Until both controls exist and
are reviewed, mutation of a non-exempt target is blocked. The current retained-testbed
operating and reconciliation procedure is [POC6 development guide](POC6-DEVELOPMENT.md),
not a substitute for this prerequisite.

The exact privately designated disposable local development kit is exempt only
as defined by [ADR 0002](adr/0002-disposable-local-development-target.md). Its
reports mark this gate **Not required per ADR 0002**. Before a destructive
action, resolve the designation from operator-controlled private configuration
and verify the exact target identity; type, hostname, IP address, and discovery
do not establish the exemption. Other and production targets remain
non-exempt.

## Stage A — describe and reproduce the current target

**Status:** Designed.

### Current Stage A0 checkpoint (2026-08-09)

The local Main fork, preliminary first-build captures, software-only
two-capture comparator, strict six-policy schema, lock-bound promotion
validator, and Git-backed source-set/fork-delta candidate observer are
implemented and tested. A local candidate command now derives compile/link,
ELF/dependency, generated-input, and intermediate-path documents from the
reviewed verbose build receipt; all six policy candidates are emitted as
canonical JSON, but remain explicitly non-promotable. The final Stage A gate
remains open. The current evidence is therefore **Software-tested /
local-only**, not Reproducible, HIL-observed, or Accepted. The generator now
also emits a candidate `materials.json` catalog bound to the reviewed receipt,
build-log digest, toolchain archive hash/root, and extracted-tree digest. The
fresh adapter-backed capture also fixes the build job count at one and records
the shim identity. Two fresh adapter-backed captures compare with different
build-log digests and byte-identical final artifacts, while remaining
explicitly local-only.
remaining work is to replace that candidate with an immutable material/license
catalog (including reviewed build-log identity), complete durable
container/toolchain provenance (a local OCI manifest/config digest is now
recorded, but not published), and promote two independent clean builds, then
the
narrow Overlord DE10-Nano/Cyclone V and HIL comparison slice.

Use Overlord to add the minimum DE10-Nano and Cyclone V resources and generate
the memory map, register definitions, toolchain configuration, and software
dependency closure needed for the current Linux Main build. Build an
upstream-style `Main_MiSTer` artifact without intentional behavior changes.

**Gate:** after the applicable rollback prerequisite, produce two independent clean builds
from the same pinned source, toolchain, environment, locale, timezone, and
`SOURCE_DATE_EPOCH` in separate output trees. The resulting new-build outputs
must be byte-identical. Any unavoidable nondeterminism is documented by
artifact, byte range or section, cause, normalization rule, and verifier; an
unexplained whole-binary hash difference fails the gate.

Compare the generated register maps, memory topology, compiler settings,
dependency closure, and image manifests to reviewed locked known-good values;
unexpected additions, removals, address changes, privilege changes, or other
locked-comparison differences fail the gate. Separately, compare the generated
upstream-style `Main_MiSTer` artifact and FPGA-launch behavior with a
provenance-recorded known-good artifact and behavior comparator on the dedicated
hardware. For a non-exempt target, also retain a verified physical rollback
path.
This old-artifact comparison is not the new-build reproducibility claim:
permitted binary differences caused by historical build metadata or other
identified causes require an explicit normalization before the comparison is
accepted.

For the ADR 0002 kit, there is no requirement to preserve or restore its prior
physical state. That does not waive comparison: an equivalence claim still
requires a valid known-good artifact and behavior comparator with recorded
provenance. Without one, the work may advance only at the evidence status its
actual checks support and cannot claim behavior equivalence or acceptance.

## Stage B — extract `libmister-runtime`

**Status:** Designed.

Introduce create/start/tick/load/status/stop/destroy lifecycle behind an owned
runtime context. Incrementally move global initialization into that context,
retain the traditional `Main_MiSTer` executable as a wrapper over the same
library, and add host-side platform fakes with deterministic lifecycle tests.

**Gate:** traditional and headless wrappers pass the same repeated launch,
core-transition, input, video, audio, save, shutdown, and failure-recovery
gates on the same target. The minimum behavior-preservation gate is ten
consecutive normal launch/stop cycles across the two previously accepted FPGA
core families, plus observed startup rollback, runtime death, agent death,
reboot reconciliation, and rollback paths. Traditional and headless wrappers
must use the same content, controllers, display/audio fixture, configuration,
and observation checklist. No unexplained orphan, lease, device-owner, cache,
or save difference is accepted.

## Stage C — compose the FogCast target runtime

**Status:** Designed.

Add versioned private local IPC between the compatibility Go agent and native
runtime. Move authoritative core state and reconciliation into the native
runtime while preserving the public FogCast target protocol and existing host
semantics. Generate a fresh per-launch identity and enforce conditioned stop
across the local boundary.

**Gate:** the current host can use either the retained adapter or native runtime
without API-visible semantic differences, and the rollback path is verified.

## Stage D — internalize media and input

**Status:** Designed.

Replace the external FFmpeg process with a linked decoder backend, introduce
explicit native presentation arbitration, complete host-emulator input symmetry
with physical state-change evidence, and automate orphan detection/recovery
after ungraceful process death.

**Gate:** the supported appliance no longer requires the POC6 presentation
hook, external decoder command, or manual bridge reconciliation. This is a
future gate; it does not revise the accepted POC6 evidence.

## Stage E — evaluate non-Linux portability

**Status:** Designed; non-Linux is not scheduled.

Only after the Linux boundary is proven, implement either a second platform
backend or a constrained bare-metal spike. Measure missing kernel services,
driver cost, boot time, memory, latency, and maintainability. Retain Linux
unless evidence shows a material product benefit worth owning USB, networking,
filesystems, codecs, and device support directly.

**Gate:** a focused, evidence-backed portability decision determines whether a
non-Linux target advances; no date, platform commitment, or acceptance is
implied.

## Evidence and reporting requirements

Each stage fixes its test matrix and HIL run counts before execution. Reports
identify exact host, target image, Main fork, Overlord, resource catalog,
runtime, core, configuration commits or hashes, commands run, machine-observed
counters, operator observations, and inferences. Software tests, reproducible
builds, HIL observations, and acceptance decisions remain separately labeled.

Migration may not broaden POC6 claims. Physical evidence remains required for
FPGA load, recognizable HDMI output, audio, input press/release and disconnect
cleanup, saves, normal and failed lifecycle recovery, and compatibility/stock
rollback. See [ARCHITECTURE.md](ARCHITECTURE.md) for the governing invariants
and [POC6 results](POC6-RESULTS.md) for the accepted baseline.
