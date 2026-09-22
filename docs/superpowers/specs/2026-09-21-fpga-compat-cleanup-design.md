# FES package-only FPGA support cleanup

Status: approved by the user on 2026-09-21. No product changes or hardware
validation are represented by this document.

Audited base: `2d54817cdd6ee40922f99d4fc4d4383445528bf1`.
Worktree: `out/dev/fpga-compat-cleanup/fes`, branch
`refactor/fpga-compat-cleanup`. Return an uncommitted diff; commits, pushes and
PRs are not authorized.

## Intent and scope

The user chose: **FES described packages are the sole supported FPGA product
path; retire conventional Main_MiSTer launches and old raw-core game profiles;
retain explicit hardware diagnostics.**

Success means one package launch path from library to physical runtime, one
current local control protocol, and build/documentation entrypoints that no
longer offer retired game backends. Removing a launch button while keeping its
fallback implementation does not satisfy this goal.

FES owns integration; FogCast owns library, host and target coordination;
libmister-runtime owns physical transitions; misteross owns FPGA production;
mister-packages owns shared contracts and generation. All changes belong in
this FES worktree, including generated consumers.

This scope concerns FPGA support. Existing non-FPGA emulation/casting, library
metadata, user ROMs, saved data, boot/appliance rollback and unrelated settings
are preserved. This work does not implement the future rooms/attract ABI,
replace the selected splash, or alter the default image's package set.

## Approach

Use a coordinated removal in dependency order, with focused checks between
steps and one integrated final diff. Extract the shared functionality still
used by FES, switch its callers, then delete the superseded implementations.

A permanent feature flag or deprecation shim would retain the confusing dual
model and is rejected. A bulk deletion based on names such as `legacy`,
`mister`, `v1` or `pong` is also rejected: current packages use several such
interfaces and source files.

## Evidence from current execution paths

Paths in this section are relative to the repository root.

| Current code | Consequence for removal |
| --- | --- |
| `sources/FogCast/fogcast/service.go` calls `launchCoreEntry` for package entries, but ordinary catalog entries can still reach `launchContent` / `launchFPGANative` | Close both old launch paths; preserve catalog data independently of FPGA eligibility. |
| `sources/FogCast/cmd/mister-agent/main.go` defaults `--runtime` to `main` | Remove the backend selector and construct the native adapter directly. |
| `sources/FogCast/internal/misterruntime/runtime.go` uses protocol 1 for health, idle confirmation and some Stop paths | Move surviving package and recovery callers to protocol 2 before deleting the old client. |
| `sources/FogCast/internal/agent` and `targetcache` depend on `core.Spec`, `mister.PreparedLaunch` and `core.Registry` | Separate content handling and package lifecycle interfaces from physical game profiles. |
| `sources/libmister-runtime/src/native/core_package.cpp` and `core_driver.cpp` accept `mister-v1` / ABI `mister` packages | Removing protocol-1 `launch` alone does not remove MiSTer gameplay support. |
| Runtime `video.cpp` and `input.cpp` combine FES paths with Menu/SPI paths | Extract ADV7513 video/splash setup and callback-based input before deleting old drivers. |
| `image/Makefile` still builds Main-backed `prod` / `dev` images | Remove these lanes, their overlays and verification branches; retain the native image lane. |
| `image/scripts/build-target-kernel.sh` uses the `work-2-prod` compiler | Repoint the retained kernel toolchain dependency before removing the old image targets. |
| `image/build/native-inputs.toml` retains a Mega Drive RBF pin copied by `scripts/consistency.py` and `scripts/generate.py` | Remove the unused product artifact policy and its synchronized checks. |
| `scripts/recipes.py` defaults omitted identity versions to 1 although every registered recipe selects 2 | Remove the compatibility default and the parent resolver's version-1 branches. |
| `sources/misteross/scripts/build_fes_demo.py`, Quartus producers and splash still use format-1 build records | Build-record versions are distinct from package-manifest versions and retired game bundles. Migrate active demo producers; preserve current diagnostic/firmware provenance deliberately. |
| Current FES Pong imports `sources/misteross/cores/pong/rtl/pong_game.sv` | Preserve or relocate gameplay RTL before deleting the conventional Pong wrapper. |
| Current FES Quartus producers import helpers from `scripts/rebuild_core.py` | Extract those helpers before retiring the old upstream-game build lane. |

## Retained product contract

The product path remains:

`library package selection -> FogCast host -> leased target agent -> local
runtime protocol 2 -> admitted package -> FES GP hardware driver`.

Retain format-2 package manifests and their exact-byte content identity. Do not
rewrite installed packages or user library records. A previously built FES
package remains admissible when its declared ABI and interfaces are supported;
this cleanup is not a package-version or Git-revision allowlist.

Retain `fes.simple-game`, `fes.simple-computer` and `fes.application`, all on
`fes-gp-v1`, and their used interfaces: fixed video/audio, single-pad and
controller ports/keypads, keyboard, blob and stream media, firmware,
persistence and ZX81 composition. In particular, blob 1.0 remains required by
current packages. No ABI consolidation is part of this removal.

Preserve every current package family, not just the factory trio:

- Registered producers: Pong, ZX81, Coleco, Catch and SMS.
- Developer producers: SG-1000 and demo/demo-media/demo-audio.
- The selected board-firmware splash and defined Stop idle, separately from
  play packages.

Preserve pre-mutation admission, exact retained artifacts, generation-bound
media/input, input neutralization, replacement barriers, persistence flush and
retry behavior, restart reconciliation, and bounded idle recovery. Development
package loads stay volatile; library loads bind explicit durable data.

## FogCast changes

Construct one native target runtime. Remove Main process ownership, FIFO and
CORENAME observation, MGL generation, Main readiness booleans and old backend
configuration. Update strict config decoders, shipped examples and generated
agent provisioning in the same change. Old Main-only configuration is rejected
with actionable configuration errors; do not silently activate another path.

Remove old `/v1/launch` and cached-cartridge launch operations, their clients,
coordinator interfaces and raw-profile selection code. Non-package catalog
entries cannot fall through to an FPGA launch. Preserve browsing and any
independent non-FPGA execution capabilities. Historical library entries are not
deleted, and no package identity is inferred from a title, platform or RBF path.

Remove static FPGA `core.Registry`, profile-specific save paths and installed
legacy-core availability negotiation. Keep catalog metadata and file-extension
handling where still used, with dependencies separated from launch recipes.
Remove permissive behavior when an older target omits legacy availability.

Require runtime protocol 2 for health, status, reconciliation, Stop, package
operations and retained raw diagnostics. Unsupported protocol fails explicitly;
there is no downgrade or ambiguous mutation replay. Keep the existing network
API version independently: HTTP `/v1` is not local runtime protocol 1.

Extract atomic diagnostic RBF staging from `internal/mister/atomic.go` into
the surviving diagnostic owner before deleting `internal/mister`. Preserve
input bridge/uinput, leases, event collection and current package smoke tools.
`misterctl` and `mister-bridge` are not deletion candidates merely by name.

## Runtime and shared contracts

Remove `LaunchGame`, old `Launch`/`Profile` APIs, production profile construction,
protocol-1 request/response handling and error projection, `MisterCoreDriver`,
and MiSTer package activation. Delete cartridge-specific SNES/NES preparation
and SNES SRAM publication once no retained caller uses them. Preserve generic
retained-file primitives, computer media and described-core persistence.

Remove `mister-v1` / ABI `mister` from the shared programming registry and
regenerate its consumer. Structurally well-formed packages with that profile
may still be parsed/inspected, but compatibility is false and activation is
rejected before mutation. Keep generic unknown-profile/ABI inspection behavior;
do not hardcode the supported registry into the package syntax schema.

Extract ADV7513-only fixed video and splash bring-up from the Menu-specific
construction graph. Remove `TransitionalMenuIdle`, Menu probing, HPS-overlay
activation and their unused runtime dependencies. Keep the actual splash
programming/reset/I2C sequence unchanged. Future attract/rooms work remains
separate.

Make FES input delivery callback-based without the MiSTer SPI fallback. Keep
Linux event decoding, mappings, worker joining, neutral input, generations and
fault cleanup. Delete SPI/CoreLoader/framebuffer implementation files only
after the surviving runtime construction and explicit diagnostics have no
references to them. Keep board MMIO and FPGA manager containment primitives.

Keep current protocol-2 package state names, including `running_development`
and `active_package`; library packages already use that wire representation.
Remove old raw-game-only states/fields where no retained consumer needs them,
but do not add a separate state renaming project. Remove the protocol-1
projection that hides package information.

Remove obsolete system YAML/generated Mega Drive, SNES, NES and conventional
Pong consumers together with generation mappings and dead emitter branches.
Keep platform/register definitions, current ABI emitters and shared fixtures.
Update all canonical serializer fixtures and copied consumers together.

## Explicit diagnostics

Retain package inspect/development load, package media/input diagnostics,
event capture, simulator/compiler/oracle checks and explicit raw RBF loading.
Raw loading uses the existing protocol-2 **contained** diagnostic operation;
it does not restore MiSTer gameplay, infer media/input, bind persistence or
promise useful output from arbitrary fabrics. Stop returns to the defined
splash idle through the normal lifecycle.

Keep the diagnostic request explicit: `protocol: 2`,
`operation: "load_development_rbf"`, an admitted absolute `rbf` path, and
`programming_profile: "development-contained-v1"`. Admission requires idle.
Successful loading publishes a positive generation, no active package, no
observed ABI/build and no active interfaces. It does not Probe the core or
start GP discovery, video or input. Preserve its one-attempt idle cleanup.

Retire protocol-1 MiSTer-compatible raw loading and Main-FIFO programming.
Retain separately invoked board/JTAG tools where they are genuine maintenance
diagnostics, with existing kit ownership rules and no automatic deployment
from a compiler/build command. Preserve absence checks proving Main is not
running; replace old game-profile smoke scenarios with package scenarios.

## FPGA producer cleanup

Make functional identity 2 the only normal FES package producer route,
including Python entrypoint defaults as well as CLI behavior. Migrate the demo
variants to that route while preserving the helpers used by Catch. Remove
production `--identity-version 1`, standalone operational checkout handling
and parent resolver/cache branches that exist only for those choices.

Retire format-1 game-bundle export/selection, conventional Pong wrapping and
upstream raw-game fetch/rebuild product entrypoints. Preserve the Pong gameplay
module and its simulation. Extract Quartus location/version/error helpers from
`rebuild_core.py` into their current oracle owner first. Shared CPU, RAM, VDP,
audio and mailbox RTL used by FES remain in place.

Remove ZX81 OSS `--legacy`, its alternate output and pre-expansion build
selection. Keep the socket shell and its scoped lock/validation. Preserve
non-socket RTL configurations still used by explicit Quartus oracle checks and
simulations, clearly documented as diagnostics rather than an alternate
product producer.

Format-1 build evidence needed by current explicit Quartus or splash producers
may remain as an explicitly documented diagnostic/firmware record schema;
it is not an identity fallback in the product registry. Rename
`legacy_source.py` around its retained source-provenance purpose and require
the FES module layout for live first-party builds. Preserve canonical origin
normalization and historical artifact verification where they verify actual
retained evidence. Do not remove support simply because a record says `1`.

Producer input closures include tracked scripts and shared source roots.
Deleting helpers or moving gameplay RTL therefore changes functional input
identity and can invalidate every current package cache entry. Expect rebuilds;
never relabel old manifests or inherit their exact-artifact acceptance. Preserve
shared compiler caches and existing immutable artifacts on disk.

## FES image, integration and documentation

Remove Main-backed `prod` / `dev` image targets, defconfigs, post-build paths,
Main services and menu-blanking helpers. Shared overlay network/SSH/supervisor
files remain where consumed by the native image. Update image verification,
QEMU smoke and test wiring to exercise the retained native lane.

Move the retained kernel build's compiler dependency off `work-2-prod` and onto
the native build toolchain without changing the kernel/U-Boot seals. Preserve
appliance release/media formats, leases, known-good images and boot selection.

Remove the unused Mega Drive native artifact pin, old source-pin copies and
generation mappings that solely serve deleted game builds. Remove native init
creation of obsolete per-system caches. Keep source-status historical
gitlink/provenance reading outside this FPGA cleanup; it is not a launch path.

Update parent and component READMEs, canonical architecture, support matrix,
current package/development guides and affected-test routing. Remove obsolete
active setup/smoke instructions. Dated validation remains explicitly historical
and does not become evidence for this cleanup. Old import paths and provenance
URLs are not a renaming target.

## Verification and completion criteria

Replace legacy fixtures used to test shared invariants with package scenarios;
do not delete coverage for busy/recovery/save/input behavior along with the old
launch fixture. Retain negative tests for unsupported profiles and protocols.

Required software coverage:

1. Package-only FPGA eligibility and rejection of retired launch routes/config;
   all remaining health/status/Stop/raw-diagnostic calls use protocol 2.
2. Package activation, replacement, host/agent restart reconciliation, Stop and
   persistence failure/retry retain correct identity and ownership.
3. Pong input/progress, ZX81 keyboard/media/composition, Coleco firmware/media/
   controller ports, and application audio declarations remain covered.
4. Splash start/Stop/recovery and contained raw diagnostics exercise the real
   production construction path in host tests; unsupported MiSTer packages
   cannot reach hardware mutation.
5. Retained producers/exporters, functional identities, oracle helpers and
   affected simulations pass; retired flags cannot select an old producer.
6. Shared generation/fixture consistency, runtime archive/source guards,
   parent resolver/cache tests, native image packaging and kernel dependency
   tests match the new graph.

Run focused component tests while editing, then the affected software runner,
`make check-generated`, image packaging checks and whitespace checks. After an
authorized integration commit, run committed-source `make check` and the
appropriate native build. Image recipe changes require a fresh image; reserve
cold two-pass verification for that stabilized integration, not each edit.

Hardware diagnostics must identify the exact runtime/image/package bytes and
use the designated kit lease. Cover startup splash, package launch/media/input,
Stop/relaunch, package replacement and contained diagnostic recovery. Report
host-only checks, diagnostic hardware checks and exact assembled-artifact
acceptance separately. Old hardware evidence cannot qualify the rebuilt result.

Completion requires no remaining production entrypoint to Main, MGL, raw game
profiles, protocol-1 fallback or MiSTer-style package activation, while all
retained FES package and diagnostic workflows pass their applicable checks.

## Handoff from this design stage

This is an uncommitted design-only addition at the audited base. Component
audits were read-only; product code, module/shared contracts, artifacts and
hardware are unchanged. The written design has been approved. The next step
is review of the implementation plan and selection of its execution method.
