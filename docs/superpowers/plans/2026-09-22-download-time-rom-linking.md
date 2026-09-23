# Download-time ROM and expansion linking

Status: implemented as an uncommitted diff; format-3 ZX81 target launch and
expansion linking have temporary exact-kit diagnostic evidence. Current-main
producer and full lifecycle validation are recorded in the 2026-09-23 checkpoint
below. Image/release acceptance and other-core conversion remain outstanding.
Initial plan base: `4dede116` (PR #112, following package-only cleanup PR #111).
Current integration base: `d1743b0c` (after the upstream stop work merged).
Scope owner: FES integration; implementation spans the four tracked modules.

## Outcome

Make separately supplied ROMs and independently built expansion assets the normal
inputs to a described core launch. Assemble one complete FPGA bitstream on the
kit before FPGA programming, without Python, synthesis, placement, routing, or
mistral-cv at launch. Preserve sealed core packages, library identity, explicit
persistence, cancellation, and the existing runtime hardware lifecycle.

“Download-time” means prepare and validate before quiescing the active core,
then program the complete admitted RBF through the runtime. It does not mean
partial FPGA reconfiguration or editing an unchecked stream during programming.

## Findings from current code

- `sources/misteross/expansion/rbf.go` already supplies a pure-Go Cyclone V
  decompressor, frame validation, checksums, compression and fixed ZX81 socket
  overlay. `asset.go` binds a cart to the exact shell package/build/payload.
  This is already consumed by FogCast through a local Go module replacement.
- `sources/FogCast/corepackage/composition.go` independently recomposes expansion
  bundles at admission and compares the result. Adoption also recomposes.
  Runtime `src/native/core_composition.cpp` checks the companion identities and
  retains the programming artifact. Keep this division of responsibility.
- `cores/fes-zx81/rtl/zx81_rom_link.v` supplies eight blank, placed 1024x10
  M10Ks at column 5, rows 73–80. They hold 8 KiB of machine ROM separately from
  the expansion socket. The hardware layout is specific to this produced shell.
- `scripts/link_static_rbf.py` packs four byte-containing 10-bit lanes into a
  40-bit word, applies the nextpnr permutation and inversion, and uses
  mistral-cv decompile/compile/decompile to replace and verify RAM settings.
  The expensive text/toolchain round trip is as relevant as Python itself.
- `sources/FogCast/fogcast/zx81_rom.go` runs that Python path on the HOST using
  `[zx81_machine_rom]` paths. It special-cases `fes.zx81`, composes a selected
  expansion first and initializes ROM last.
- `corepackage/rominit.go` transports the original package, supplied programmed
  RBF, image hash and optional composition. Its comment explicitly says the
  target does not recompute the initialized RBF. Hash checking proves the
  supplied bytes match their receipt, not that they change only ROM INIT bits.
- `internal/misterruntime/runtime.go` stages that companion and dispatches
  initialized core/library/composed operations. Runtime hardware retains and
  hashes the supplied file. The final rehash currently follows quiescing:
  failure classification and recovery ordering need examination during migration.
- Expansion APIs are intentionally fixed to the ZX81 bus, one slot and one
  shell-specific cart. `compositionShell` requires simple-computer 1.0 and the
  optional ZX81 expansion interface. This is not yet a generic bus framework.
- Coleco currently has firmware-write ports and media delivery; SMS and SG-1000
  retain their media paths. A blank-ROM package cannot silently inherit those
  older packages' boot sequence or hardware qualification.

The architecture document reports expansion staging/adoption around 7.9/7.4 s
on an ARM kit after frame-oriented optimization, versus about 60 s previously.
Those are historical measurements, not benchmarks of this ROM prototype or a
performance promise for this plan.

## Approach choice

Recommended: extend the existing Go module with a small generic bitstream-linking
layer and ROM INIT operations. Keep its existing expansion API as the current
consumer while extracting shared internals only where necessary. The target
agent calls the library in-process; an optional diagnostic CLI can call the same
code. No extra daemon or compiler database on the kit.

Alternatives:

1. Cross-compile mistral-cv and wrap it: useful as an interim diagnostic/oracle,
   but retains device-database and text round trips and does not provide the
   compact bounded launch path wanted here.
2. Implement a C++ linker inside the runtime: possible, but duplicates the
   existing Go codec and moves content composition into hardware lifecycle.
   Reconsider only if ARM benchmarks establish a problem Go cannot reasonably
   solve, not on an assumed language performance difference.

## Proposed contracts

### Producer-owned link descriptions

mister-packages owns the versioned schema and conformance cases. misteross emits
an exact-artifact link description during package production, using the pinned
Mistral database/compiler to resolve physical RAM bit locations.

A description binds device, encoding version, exact base RBF digest and length,
and named ROM regions. Each region declares role (machine firmware/cartridge),
logical size, supported layout, byte/bit ordering, explicit padding/mirroring,
allowed INIT destinations and expected blank bits. Encode regular mappings
compactly; bound the expanded operation count before allocating. Start with the
actual 1024x10 layout, not an arbitrary patch language.

Reject duplicate destinations, overflow, unsupported encodings, out-of-range
coordinates, non-INIT destinations and inconsistent blank values. Preserve RAM
port configuration, clocks, routing, identity and all undeclared payload bits.
Generated mapping must be independently checked against Mistral; BEL names alone
are insufficient to locate encoded INIT bits.

The description must be sealed into package identity. Current format-2 readers
accept a closed manifest plus `core.rbf`; do not tack on an ignored third file.
Recommended first contract: a new explicitly supported package format with
manifest-digested `link-map` member. The map binds the RBF hash, and package
identity includes the map, avoiding a circular package-ID-in-map dependency.
Finalize serialization and aggregate limits with shared reader fixtures before
writing producers. Do not silently broaden the old closed format.

Machine ROM maps belong to the shell; ROM maps within a routed cartridge belong
to that exact expansion asset. Expansion regions and shell ROM INIT locations
must be disjoint. A selected cart's ROM patches may only affect its declared
INIT locations, applied after the validated cart overlay. No arbitrary uploaded
coordinate map is accepted independently of its owning artifact.

### ROM selection and programmed identity

FogCast resolves named ROM requirements using its existing firmware/media stores,
imports canonical binary bytes, and binds selections by digest and size. Replace
absolute script/image/tool paths with those selections. Missing required ROMs
block readiness and launch before hardware mutation. Importers may normalize
known container/hex formats explicitly; the linker accepts binary bytes and does
not guess from names or trim input silently.

Keep package ID unchanged when selecting another ROM. Define a versioned launch
composition record containing base package ID, link-map identity, ordered
expansion identities, named ROM digests/sizes, linker contract version and final
RBF digest/size. Expose it in active runtime/agent status so restart and lost-reply
reconciliation distinguish two ROM selections on the same package.

Retain existing core-scoped save semantics: ROM/expansion selection must not
silently create save namespaces or change data-layout compatibility. Validate
persistence separately; development loads remain volatile.

## Kit execution

1. Receive bounded immutable package/expansion/ROM inputs and canonical request.
2. Validate package/map identities, requirements, supported contracts, bounds and
   selected ROM contents. Freeze the complete selection for this launch.
3. Decode/validate base frames and optional cart once; apply permitted expansion
   changes; patch mapped ROM INIT bits directly in the frame representation.
4. Regenerate affected EDCRC/frame and outer CRC fields correctly, preserve all
   non-owned bits/header fields, and emit deterministic canonical compressed RBF.
   Compressed bytes cannot be patched by fixed offsets.
5. Validate the final result, retain it in existing private staging with its
   composition record, and dispatch through the runtime's admission boundary.
6. Runtime retains and checks the exact artifact FD, owns save/quiesce/program/
   identify/restore/start, and reports full active identity. Any failure after
   quiesce must accurately report mutation/recovery; it is not a preflight error.

Cancellation must cover reads, linking loops, publication and ownership handoff.
No failed link or missing ROM stops the current game. Agent restart/adoption and
ambiguous responses use the same validated identity and never replay programming
blindly. Retain companions until stop or proven-safe retirement.

Initial implementation can retain the existing transfer mechanism but must
recompute on target. Follow with input-only transfer so the host need not ship
both intermediate and final RBFs. Host previews may use the identical library;
they do not replace target validation. Reuse existing content storage for any
later linked-output cache; key it by the full recipe identity and revalidate it.
No new cache service is needed for the first slice.

## Implementation sequence and acceptance gates

### 1. Qualify a native ROM linker on the exact ZX81 layout

Files: `sources/misteross/expansion/rbf.go`, new focused ROM-map/INIT code and
tests alongside it; `scripts/link_static_rbf.py`, `tests/test_link_static_rbf.py`
remain the development oracle; use the pinned Mistral source for map extraction.

Generate maps for the eight existing machine ROM cells. Produce synthetic zero,
all-one, walking-bit, address-ramp and random fixtures; compare Go output with
Python/Mistral and independently read back logical ROM bytes. Assert unchanged
non-INIT CRAM/header and valid checksums for compressed and uncompressed input.
Include malformed/truncated frames, overlapping maps and cancellation. A valid
zero ROM that matches blank contents must not be rejected as a no-op.

Measure ARMv7 cold/warm link wall time, peak RSS, allocations, temporary disk,
output size and total staging/adoption time, with and without expansion. Add
small and maximum-supported ROM cases. Set a measured latency/memory budget at
this gate; no fabricated speed target or generic Go-versus-C++ claim.

### 2. Seal maps and advertise support

Files: `sources/mister-packages/schema/`, fixture generator/readers;
`sources/misteross/scripts/export_core_package.py`, `build_fes_zx81_oss.py`;
FogCast `corepackage` and runtime package readers; FES consumer fixture mapping.

Add the explicit package/map version and regeneration coverage across all readers.
New readers still understand existing packages during transition; old readers
reject new packages clearly. Advertise link/initialization support before host
selection. Missing or incompatible linker/map support never falls back to raw RBF
or a Python command. Producer publishes no package without verified maps.

### 3. Use target-side linking for ZX81 library launch

Files: FogCast `fogcast/zx81_rom.go`, configuration, firmware/media selection,
`corepackage/rominit.go`, `corepackage/composition.go`, target staging and
`internal/misterruntime/{runtime.go,protocol_v2.go}`; runtime daemon protocol,
`core_composition.cpp`, `native/hardware.cpp`, active-state serializers/tests.

Replace host command execution with declared ROM inputs and one target link.
Add the new composition record to runtime status and reconciliation. Preserve
library versus development persistence binding. Validate cancellation before
admission, during link and after publication; failed admission preserves the
active generation; lost reply/restart preserves the correct programmed identity.
Test expansion plus ROM together, change ROM only, change expansion only, and
reject a map or cart belonging to a different shell.

### 4. Integrate and qualify ZX81 on the designated kit

Files: FES image input/build checks, `scripts/affected.py`, acceptance tooling,
current operator/component architecture guides.

Cross-build the CGO-free Go code for Linux ARMv7, verify the image needs neither
Python nor mistral-cv, run focused host/runtime/shared tests then parent checks.
Use incremental image assembly for development and cold build/verify for final
integration. Under the existing kit lease, exercise real BASIC boot, keyboard,
tape loading, selected expansion, stop/relaunch, cancellation and agent restart.
Record exact shell/cart/map/ROM/programmed hashes and image identity. API success
alone does not prove ROM execution, display, audio or expansion behavior.

### 5. Migrate other cores in explicit slices

| Core/content | First migration | Retirement condition |
| --- | --- | --- |
| ZX81 machine ROM | Declared 8 KiB initialization | Native link and full lifecycle acceptance |
| ZX81 expansion ROM/RAM/peripherals | Preserve actual bus; add asset-owned INIT maps where needed | Each independently produced asset qualified |
| Coleco BIOS | Blank declared BIOS cells and selected firmware | Boot and firmware/cartridge combinations accepted |
| Coleco fixed cartridges | Declare init capacity and mirroring; map cart ROM | Existing title/media behavior preserved and accepted |
| SMS / SG-1000 | Fixed-map ROM first, per-machine bus contract | Capacity/banking/reset/controller/audio cases accepted |
| Pong / ROM-free cores | Plain sealed package | No unnecessary ROM requirement |
| Larger/banked future cartridges | Expansion implements mapper; declared backing-memory transport | Separate implementation and measured acceptance |

Use the ZX81 architecture pattern, not its literal 44/20-bit bus or physical
rectangle, as the standard. Validate bus timing, WAIT/reset, memory ownership,
interrupts and audio for each family; share contracts only where semantics match.
Start with one qualified slot per machine. Multiple slots require resource and
routing-conflict rules and are a later extension, not an implicit promise.

CRAM initialization can only populate memories present in FPGA configuration.
External SDRAM and large cartridge images still need an explicit pre-start data
transport. Preserve the common composition/ROM selection model while declaring
that transport; never claim arbitrary ROMs fit M10Ks. Tape/disk and mutable media
are not automatically ROM patches and retain their own semantics.

### 6. Remove superseded launch paths

After each migrated core passes its acceptance gate, update installed package
selection and remove its redundant firmware/blob-ROM loader and special-case
configuration. Keep required tape/disk/SDRAM delivery. Remove PythonMachineROM
and the unchecked host-produced initialized bundle from production after ZX81
cutover; Python may remain a developer oracle. Give old selections an explicit
unsupported/migration result once retired; no hidden fallback. Preserve imported
ROMs and saves. Publish a per-package support table with exit conditions so the
transition does not become permanent compatibility machinery.

## Validation commands and handoff

Focused commands include `go test ./...` in `sources/misteross/expansion`,
`go test ./corepackage ./internal/misterruntime ./fogcast` in FogCast, the runtime
`make test`, shared fixture checks and ZX81 ROM/bus simulations from the current
misteross Makefile. Run `make check-generated` and affected parent checks after
contract changes. Extend CI selection for every new test file.

This examination ran no build, benchmark or hardware test. No new performance or
hardware support is claimed. This document is an uncommitted proposed plan based
on `4dede116`; next implementation milestone is map extraction plus native Go
INIT equivalence, followed by shared package-contract and launch integration.


## Execution checkpoint — 2026-09-22

Uncommitted implementation on base `4dede116`, branch `plan/rom-linking-transition`:
producer map extraction, pure-Go frame patching, cancellation checks, host/ARMv7
diagnostic CLI, and five independent synthetic Mistral golden cases. The current
architecture documents the implemented subset and reproduction commands.
No shared package/wire contract or live launch behavior has changed. No hardware
was programmed. Milestone 1 still requires actual ARM latency/peak-memory and
routed ZX81 acceptance; later package, launch and per-core milestones remain.

Validation: Go module tests, race detector, vet, FogCast corepackage tests,
28 parent planner/changed-test tests, and 10 focused Python tests (one optional
external compiler test skipped) passed. The external oracle generator was also
run explicitly for all five patterns. Linux ARMv7 static cross-build succeeded.
Independent review found a FIFO cancellation issue in the diagnostic, fixed
with a regular-file check and regression test; final review has no material
findings. Host eight-block benchmark: 99.6 ms/link, 18.0 MB allocated/link;
these are not target measurements.

The broad misteross Python suite ran 861 tests: 18 failures, 85 errors, one
optional skip. Failure classes include absent local toolchain executables in
`test_compare_builds`/`test_oracle_boundary`, and the existing closed-policy
rejection of `synth_only` in `test_oracle_boundary`. An untouched `4dede116`
archive reproduced those classes (857 tests, 19 failures, 86 errors); its two
additional failures were `test_create_build_record_names_quartus_and_both_clocks`
and `test_generated_directories_are_ignored`, because an archive has no `.git`.
The full suite is not green. Parent `make check` refuses the uncommitted module
checkout by design, so parent image validation is not claimed.

Next integration step: seal producer maps into the new package contract, then
use the target agent's existing composition/admission path to recompute ROM
links before the runtime hardware transition. Keep this diagnostic map format
explicitly separate from the eventual shared package contract.


## Package-contract checkpoint — 2026-09-22

Ruling: format 3 initially declares one required ROM per artifact with an ID,
firmware/cartridge role and exact source size. This matches the current ZX81
machine map and avoids introducing an unused general patch language. Multiple
ROM requirements within one artifact will need an explicit contract extension;
this does not claim completion of all-core migration.

Implemented the closed three-member package contract, separate identity domain,
shared JSON schemas and 46 conformance cases. Python export is explicitly opt-in;
production producers remain format 2. Go imports, stores, stages, inspects and
adopts all sealed members. Python/Go readers validate map syntax, bounds and
bindings; C++ retains and rechecks the map bytes and identity. C++ deliberately
refuses format-3 activation, including prototype initialized loads, before any
hardware/input transition. The protocol-2 descriptor extension carries ROM
metadata on inspections without advertising launch support.

Validation passed: full FogCast Go suite; expansion Go suite/race detector;
Go vet for package/runtime adapter; 40 focused Python package/export/map tests
(one optional external-oracle skip); full mister-packages test target using the
existing jsonschema-equipped Python environment; full runtime host suite with
an isolated build directory; 46 parent generation/consistency/planner tests;
`generate.py --check` (12 generated files, 24 fixture copies); ARMv7 static
agent and diagnostic cross-builds. Independent review found the Go runtime
response shape still rejected `rom`; fixed with a red/green regression and a
frozen fixture emitted by the actual C++ serializer. Focused re-review passes.
Earlier broad misteross Python baseline failures remain outside this change.

Scope includes all four modules and parent fixture routing. Base remains
`4dede116`; all results are uncommitted. No physical FPGA or image deployment
was performed, and no format-3 hardware acceptance is claimed. Parent image
validation still requires committed source selection.

Next: add target-side ROM input/selection and deterministic linking with
programmed identity, cancellation and adoption. Only then remove the explicit
runtime activation gate and enable format-3 production ZX81 exports. Keep the
remaining per-core conversion and kit acceptance gates separate.

## Target launch checkpoint — 2026-09-22

Implemented the next software slice on the same uncommitted base `4dede116`:

- Named, package-bound ROM selection in the host catalog and GET/PUT library API,
  readiness projections, and source-only transport (`rom-link.json`, sealed
  `package.tar`, `rom.bin`, optional `expansion.tar`).
- Target-side Go linking with optional expansion composition before ROM patching,
  map/socket overlap rejection, private retained artifacts, cancellation cleanup,
  and restart adoption that relinks inputs before accepting their identity.
- Protocol-2 capability `rom_linking: 1` and the three explicit ROM load operations.
  The runtime validates the named/map/source/programmed identity and retained
  artifact before retiring input or quiescing hardware. Ordinary/initialized
  format-3 loads remain invalid. Status and lost-reply/restart reconciliation
  distinguish ROMs sharing a package ID.
- Launch upload limits include the source envelope; inspection/import limits remain
  sealed-package limits. Host launch confirms the target's selected source identity.

Independent review identified final-admission cancellation and reset-held cartridge
mailboxes. The launch path now rechecks all contexts immediately before input
replacement, and linked cartridge contracts reject endpoints that require a later
media commit, including the firmware mailbox whose commit retains reset.
Firmware ROMs retain independent media handling.

This does not convert production FPGA producers or retire their format-2 prototype.
Next integration step: emit and seal the actual ZX81 ROM map in its production
export, validate its expansion bus/ROM simulations, then measure and accept the
exact linked artifact on the designated leased kit. Per-core conversion remains
explicit; tape/disk/SDRAM media must not be treated as CRAM patches by implication.

Validation for this checkpoint: full FogCast `go test ./...`; 298 browser tests;
focused package, runtime, agent, target-client and HTTP race suites; expansion
race suite; affected Go vet checks; ARMv7 static target-agent cross-build;
runtime `all run-tests archive-audit` with `/tmp/fes-rom-launch-runtime-build`;
46 parent tests and generated-consumer/24-fixture consistency checks. Final
cartridge/firmware guard regressions pass in both Go and C++. The C++ full
`make test` wrapper still assumes relative/default build paths in separate
incremental/active-tree scripts, so the isolated host binary suite is the evidence
reported here. No FPGA programming, deployment, kit timing, or exact-artifact
hardware acceptance was performed. Parent image assembly awaits committed source
selection. All changes remain uncommitted in `out/dev/rom-linking-plan/fes`.

## Production ZX81 diagnostic checkpoint — 2026-09-22

The production ZX81 exporter now emits a format-3 blank-ROM package and sealed
map. The FES cache, image selector and core-dev reconstruction preserve the map.
The production package and expansion cart built from development-only snapshots;
ZX81 ROM, bus, machine and video simulations passed. The Go linker produced the
same programmed RBF as the independent Mistral oracle. On the designated MiSTer
Pi, the ARMv7 linker matched that oracle in five runs (3.20–3.26 seconds), and
a plain 1 KiB ZX81 library launch reported the exact package, ROM/map/source and
programmed identities, accepted keyboard input and displayed BASIC over HDMI.
This is a temporary exact-kit diagnostic, not image or release acceptance.

The first expansion launch returned target `INVALID_ARCHIVE` with idle status;
the host surfaced a generic recovery message. Exact production bytes pass both
local Go source-envelope staging (including target-linked expanded oracle digest)
and the C++ package/composition admission checks. The target failure's precise
admission substep has not yet been captured. A second kit diagnostic stopped
before launch because the service supervisor left duplicate agent processes,
which blocked temporary-mount rollback. After an interrupted recovery and a new
kit boot, read-only checks showed the original executable hashes, one runtime
and agent, no temporary mounts, and ready/idle/free state. No expansion hardware
acceptance is claimed.

The user identified an in-progress PR improving stops and asked to wait for it
before stop changes. Resume target diagnostics after that PR is integrated:
capture the raw target admission error and runtime log on an expansion-only
launch under the kit lease, then verify expanded programmed identity, HDMI,
keyboard, Stop, relaunch and selection persistence. Keep all stop/lifecycle
changes out of this ROM worktree until the upstream PR is available.

## Main integration checkpoint — 2026-09-23

The upstream stop work is in FES main `d1743b0c`. The ROM changes were ported
to a separate integration worktree and remain uncommitted. The expansion
admission failure was the agent's 10-second observation budget, which started
before target-side composition. A separate 60-second package-load budget now
covers measured 33–40-second kit launches while raw-RBF diagnostics retain the
10-second budget. The full FogCast Go suite, 298 browser tests, runtime host
suite, focused parent tests, and generated-consumer checks passed on this base.

The current-main ZX81 producer sealed a fresh format-3 package and matching
cart from a development snapshot. The independent Mistral oracle and ARM Go
linker agreed byte for byte for both plain and expanded outputs. The leased
kit passed plain, expanded, expanded relaunch, and private-host restart;
HDMI showed the expected 1 KiB/16 KiB BASIC RAMTOP values. Original services
were restored on the same boot. Exact identities, timing, classification,
and restoration evidence are in
[the dated diagnostic](../../validation/2026-09-23-zx81-rom-linking.md).

An uncommitted development snapshot also passed `make host` and `make dev`;
the diagnostic native image receipt includes the ZX81 ROM map. Its package ID
differs from the kit-tested package because provenance differs, so it does
not inherit that hardware result. The full parent Python suite passed.

Next integration step: use a reviewed committed FES source selection for
`make check`, cold image verification and distinct exact-image acceptance. Do not infer
acceptance for other cores; migrate their ROM producers and requirements
individually under the format-3 contract.
