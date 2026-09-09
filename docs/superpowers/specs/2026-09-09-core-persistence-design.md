# Persistent settings and progress for described cores

Status: implemented and validated. See the [acceptance record](../../validation/2026-09-09-core-persistence.md) for exact artifacts and limitations.

## Outcome and first slice

Standalone FES Pong remembers a paddle-speed setting and a best-rally record
after Stop, relaunch, reboot and compatible package-version changes. The UI
team receives documented settings/progress APIs; presentation remains theirs.
Raw RBF loading keeps its existing MiSTer default and behavior.

This is small persistent game data, not a save state. A launch starts a fresh
game with restored settings and records, not the previous ball position. SNES
cartridge SRAM already works and retains its existing identity and file format.
The first slice reuses that lifecycle's persistence boundaries and atomic file
operations without migrating existing saves or adding other console transports.

## Existing implementation and approach

The merged package library separates stable `core.id` from content-derived
package ID. Its selection operation already validates compatibility and uses
compare-and-swap. The format-2 manifest already supports required versioned
interfaces; unknown manifest fields are deliberately rejected.

Use those interfaces to declare a bounded persistence transport and a known
data layout. This avoids a container-format revision merely to carry two
values. A new arbitrary schema language would expand every reader and runtime;
host polling of scores would miss events and couple saving to host availability.
Neither is required for this slice. Future layouts can add registered contracts;
this design does not claim that an unfamiliar layout becomes usable without a
runtime implementation.

The existing runtime calls `FlushSave` before Stop and replacement, and its
`SaveFile` retains a directory and publishes complete bytes atomically. Reuse
these boundaries. The current FES GP driver's quiesce asserts gameplay reset,
so quiesce cannot double as a snapshot operation.

## Identity, storage and compatibility

Persistent library launches use a target-local namespace derived from the
validated `core.id`, independent of package hash, semantic version, build ID,
host catalog row and display name. There is one profile per core on a kit in
this slice. Moving to another kit does not silently copy progress.
Core IDs are operator-trusted namespaces, not authenticated publisher identities.

Store a single current record beneath `/media/fat/fogcast/core-data/`, in a
directory named by SHA-256 of the UTF-8 core ID. The record contains its core
identity, layout ID/version, payload and integrity check. Store the identity
inside the validated record as well as using it for the directory name. The
fixed record filename `record.bin` does not include the layout version: incompatible data
must cause a visible rejection rather than look like a missing fresh save.
Package installation, staging cleanup and system-image replacement never
delete this directory. No network request accepts a filesystem path.

Missing data means defaults. Malformed, oversized, truncated, corrupt or
incompatible data means a typed pre-mutation error. Never silently overwrite
it with defaults. Failures before atomic replacement preserve the old record;
use private temporary files, complete writes, file sync, atomic replacement and
directory sync. Failure after replacement may leave the old or complete new
record with uncertain durability, never a partially written record. Report
that ambiguity, retain ownership and reopen/validate on retry. Reuse the
runtime's file-safety mechanisms, but give small core records their own bounded
codec: SNES `SaveFile` currently requires power-of-two 2–128 KiB data and caches
the bytes opened at Prepare. Do not pad a two-word payload into cartridge SRAM
or reuse that cached snapshot across outgoing flush.

Compatibility requires the same core ID and exact supported layout ID, major
and minor for this first implementation. Package version is not a data-format
version. No implicit migration, reset, import, deletion or format downgrade is
provided. A settings update may change only settings, preserving progress.

Selection from a persistence-capable version to a version with a different or
missing persistence contract is rejected, even before the first save, so an
active generation cannot later write data the selected version cannot read.
An older volatile version may be upgraded to the first persistent version.
Two versions with the same contract can be selected in either direction.
Installed unsupported versions may still be inspected without selecting them.

Selection also checks the current target's record compatibility. The response
identifies the target; offline state is unknown, never compatible. Inspection
remains advisory: launch repeats authoritative checks under the target/runtime
lifecycle boundary. A different host or a later write cannot make earlier
inspection an authorization to reinterpret data.
An active persistent generation for the same namespace also constrains the
check, even when it has not published its first record yet. Hold runtime
serialization through outgoing flush, incoming record refresh and activation.

Development-package loading remains explicitly volatile and reports that mode.
It cannot overwrite a library save merely by declaring the same core ID.
Persisting through the development API is outside this first slice.

## Shared ABI and Pong data

Keep container format 2, `fes.simple-game` ABI 1.0, GP transport 1.0 and all
existing base commands. Do not bump the base minor version for this extension.
Add required versioned interfaces `fes.persistence.words` 1.0 for bounded word
transfer and `fes.pong.progress` 1.0 for this payload's meaning. Allocate new
capability bits and commands in mister-packages, generating consumers and wire
fixtures. Older runtimes reject the new required interfaces before programming;
new runtimes continue to accept existing volatile packages.
The generic persistence interface requires exactly one registered layout
interface; missing or multiple layouts are rejected before programming.
Both new interfaces have live identity capability bits: bit 2 for the word
transport and bit 3 for Pong progress, within the existing 16-bit identity word.

The persistence transport provides atomic snapshot/freeze, indexed read,
staged restore with explicit commit, and resume without gameplay reset.
Transfers are bounded to at most 256 little-endian 16-bit words. Partial restore
never becomes live state; partial snapshot never becomes a persisted record.
Only verified capability/layout combinations authorize these commands. Runtime
identity checks must match the declared capabilities and exact build identity
before any restore or gameplay command. FPGA reset starts with safe defaults.

Pong's first payload is exactly two words:

| Word | Meaning | Validation/default |
| --- | --- | --- |
| 0 | Paddle-speed enum | 0 slow (2 pixels/frame), 1 normal (4), 2 fast (6); default 1 |
| 1 | Best rally | Player paddle returns in a single rally, saturating at 65535; default 0 |

Paddle movement saturates at y=0 and y=208 for every speed; changing the old
four-pixel step must not retain a boundary guard that allows six-pixel overshoot.

Current rally starts at zero for a new serve, increments on each player-paddle
return, and ends when either side scores. Best rally updates as the rally grows,
including an unfinished rally when the user exits. It is a shared record across
the three paddle speeds for this first version. The displayed 0–9 game scores
and their existing wrap behavior remain separate. Do not infer records by
sampling those scores. This needs no FPGA menu or launcher redesign.

Add explicit collision/point event hooks or equivalent state outputs to the
shared Pong game module. Preserve the original MiSTer wrapper's default
behavior and port wiring. The standalone shell owns persistence registers;
gameplay reset must not accidentally clear restored settings or the record.
Use saturating, explicitly sized arithmetic and deterministic simulations at
the actual 74.25 MHz pixel-clock configuration.

### Frozen wire and record contracts

Generate the following additions from mister-packages before component workers
implement consumers. Existing opcodes 1–3 retain their meaning.

| Opcode | Index/argument | Result |
| --- | --- | --- |
| 4 data control | index 0, argument 0 | Freeze gameplay without reset and latch a complete snapshot before ACK |
| 4 data control | index 0, argument 1 | Begin fresh restore staging while gameplay reset is held |
| 4 data control | index 0, argument 2 | Commit all staged words atomically while gameplay reset is held |
| 4 data control | index 0, argument 3 | Resume frozen gameplay without changing game or persistent data |
| 5 data read | word index, argument 0 | Read a frozen snapshot word |
| 6 data write | word index, argument is value | Write a staged restore word |
| 7 data info | index 0–3, argument 0 | Word count, layout tag, layout major, layout minor respectively |

Pong reports count 2, layout tag 1, major 1, minor 0. Its tag-to-ID mapping is
part of the generated contract. Control/write success returns zero. Invalid
opcode/index/argument uses existing error values; add error 4 for invalid state.
Unknown values never change state. Freeze requires released gameplay or an
already frozen game. Freeze is idempotent while frozen and keeps
the original snapshot. Begin clears the staging-completeness bitmap; commit
requires both words and valid values. Read requires frozen state, write requires
open staging, and resume requires frozen state. Incomplete commit cannot modify
live registers. A successful commit closes staging and remains reset-held until
the ordinary gameplay release. Volatile launches may release defaults without
restoring. Snapshot ACK means the latched data is already stable, not merely
that a latch request was received.

The runtime retains the existing poisoned-exchange behavior after an ambiguous
timeout. It does not blindly retry commit/resume or issue further wire commands
when it cannot establish their completion. Failure then follows the explicit
recovery path with any complete host snapshot retained.

The canonical disk record is the following concatenation, with no padding or
trailing bytes: ASCII magic `FESDATA1` (8 bytes), SHA-256 of core ID (32 bytes),
layout-ID byte length (u16), layout major (u16), layout minor (u16), payload word
count (u16), ASCII layout ID, payload words (u16 each), and SHA-256 of all prior
record bytes (32 bytes). All integers are little-endian. IDs obey the existing
package ID grammar and are at most 96 bytes; payload count is 1–256. Maximum
record size is therefore 688 bytes. This layout uses the exact Pong values
above and rejects any other count/version. The record revision is lowercase
SHA-256 of the entire canonical record; absence is the string `absent`.
JSON uses `"revision":"absent"` for that case and the digest string otherwise.
Shared fixtures cover exact bytes, digests, default construction and rejection.

## Launch, exit and failures

1. Admit exact package bytes and validate its persistence contract, target data
   and storage access before retiring current input or programming anything.
2. If replacing a session, flush its persistent data first. Failure prevents
   replacement and retains honest ownership. Do not load a stale copy read
   before this flush: refresh the incoming record after a successful outgoing
   flush, particularly when both generations have the same core ID.
3. Program, verify ABI/build identity, restore the complete payload while
   gameplay is held, then release gameplay and enable input. Failed initial
   activation and identity mismatch never publish default or partial saves.
4. Ordinary Stop or replacement stops input, freezes and captures the complete
   payload, persists it, then permits reset/reprogramming. The controller's
   Select+Start exit goes through this same path.

A save error is not a successful Stop. Preserve the existing runtime pattern:
if gameplay can safely resume, restore the same generation and input and
invalidate its stale captured snapshot before the next attempt. If resume is
unsafe, retain the available snapshot and ownership in an explicit recovery
state; do not report idle or force-reset to hide a write failure. A retry must
not reread already reset hardware. Transport ambiguity cannot be repaired by
pretending an incomplete payload is complete.
Safe resume requires the driver's explicit persistence-resume command to
succeed before input is reopened; reopening input alone cannot unfreeze Pong.
Clear the retained snapshot only after both resume and input restoration
succeed. Keep the generation attribution required for immediate input-fault
callbacks while restoration is in progress.

Startup, generic fault cleanup and failed launch do not save. Successful Stop
is the durability boundary. Sudden power loss may lose progress since the last
successful boundary, but must not replace the prior complete record with a
partial write. Periodic checkpointing and host backup/synchronization are later
features, not implicit promises of this milestone.

## APIs and component responsibilities

The runtime owns record validation, restore, capture and durable publication.
FogCast's target adapter derives trusted storage locations and passes them
through its local protocol, just as for cartridge saves. Target coordination
serializes settings changes, inspections and lifecycle transitions; host code
does not write target files or implement physical register operations.

Library launch and development load currently share the same custom-load path.
Add explicit persistent-library context through host, target and local runtime
requests. The host derives it from a resolved library entry, not a raw-load
caller flag; the target rederives core ID and layout from admitted package
bytes. Direct development requests remain volatile. Never infer persistence
from the runtime's current `development` execution label or a display game ID.

Provide host `GET` and `PUT /api/v1/library/core-entries/{game_id}/settings` and
`GET /api/v1/library/core-entries/{game_id}/progress`, backed by authenticated
target operations and the runtime's bounded data interface. The host resolves
the selected immutable package, verifies returned identities, and reports the
target ID, core ID, layout/version, persistence mode and durable record revision.
Revision is a digest of the complete canonical record; an absent record has an
explicit absent revision. GET returns durable data, not an unlabelled live score.
For inactive data access, the host sends the exact selected archive and expected
package ID through bounded private staging and read-only native admission.
There is no target catalog and previous launch staging may have been removed;
core/layout strings supplied by a host are not a substitute for admission.
Always clean up that staging through its existing ownership mechanism, and
report cleanup failure without claiming the operation fully succeeded.

Settings PUT accepts the expected selected package ID and record revision plus
the typed paddle-speed enum. It rejects stale selection/revision, invalid values
and updates while a session for that namespace is active or recovery is pending.
It atomically preserves the current progress and applies to the next launch.
The target/runtime compare and write under the same serialization boundary;
a host-only comparison is insufficient. Neither GET nor PUT activates a core.
Writes respect the existing kit lease and never override another owner's
active or failed-cleanup session. Read-only inspection does not claim hardware.
Expose equivalent CLI commands for operator and acceptance use. Document
unsupported, offline, busy, incompatible, corrupt-data and write-failure results
without disclosing server paths. No new settings UI belongs in this change.

Component work proceeds after the shared contract is fixed:

- mister-packages: interface IDs, versions, wire constants and common fixtures.
- misteross: Pong setting/record events, mailbox snapshot/restore, simulation,
  unchanged-MiSTer behavior and reproducible package export.
- libmister-runtime: driver data operations, durable codec, admission, lifecycle
  and retry ownership; retain existing SNES transport and raw `.srm` format.
- FogCast: library persistence context, target/local protocol, selection checks,
  settings/progress API and CLI, error and ownership propagation.
- FES: selected commits, consistency checks, incremental image and acceptance
  evidence. Component agents use separate worktrees with integration review.

## Acceptance

Software checks cover shared positive/negative wire fixtures, unsupported
interfaces, partial transfers, invalid settings, record corruption, symlink and
write failures, stale settings CAS, same-core replacement freshness, failed-save
ownership/retry and unchanged volatile/MiSTer/SNES behavior. RTL simulations
cover rally events, saturation, reset/restore/freeze and all paddle speeds.
Exercise the synthesized design sufficiently to catch simulation/synthesis
disagreement before occupying the kit.

On the designated leased kit, set a visibly distinct paddle speed, play Pong,
Stop, inspect the record, relaunch, reboot and confirm restoration. Select a
second package with the same layout and then return to the first; data survives.
Reject an incompatible-layout and a persistence-removing selection without
changing selection, data or active input. Induce a controlled storage failure,
verify the old complete record and ownership, repair it and retry successfully.
Confirm return to a usable menu and a SNES save regression check.

Use incremental component/parent builds while developing. Reuse unchanged
Linux/compiler and console-RBF caches. Run final selected-source consistency,
focused/full component validation, reproducible image checks and exact-artifact
kit acceptance once the combination is stable. Record diagnostic and final
image evidence separately; no test of this feature has yet been performed.
