# Described FPGA cores and ABI dispatch

Status: approved by the user on 2026-09-08. This document defines the first
implementation milestone, not features present in the selected image.

Baseline: FES `ee4df0e1b9a4e5b3606d385ec2a927293490572b`, including its selected
mister-packages, misteross, libmister-runtime and FogCast commits.

## Outcome and scope

Load an FPGA core using an explicit, versioned description of its software
interface. A bare `.rbf` continues to mean MiSTer. A described core selects a
known programming profile and ABI driver before any hardware mutation. Unknown
ABIs are inspectable but cannot run until the runtime gains the appropriate
driver; descriptions do not manufacture support for arbitrary protocols.

The first complete example is **FES Pong**, a standalone, ROM-free core using
the Mistral/nextpnr build path and a small HPS GP interface. It must show video,
accept the existing controller input, Stop to the current launcher and relaunch.
It is distinct from the existing MiSTer-compatible Pong, which remains available.

This milestone includes packages, shared definitions, runtime dispatch, target
upload/status support, CLI use and integration evidence. It excludes menu
accelerator implementation, UI redesign, a general register scripting language,
downloadable native drivers, package signing, partial reconfiguration and
simultaneous FPGA tenants. Toolchain development remains with the existing
Mistral/nextpnr work; using a Quartus comparison artifact does not prove the
Mistral milestone.

## Existing paths that constrain the change

- `misteross/scripts/export_core_bundle.py` and FES `scripts/bundle.py` implement
  closed format-1 TOML bundles with `abi = "mister"` and a fixed system identity.
  Extend this artifact boundary instead of starting an unrelated package system.
- Runtime `src/native/linux/fpga_manager.cpp` mixes Cyclone V programming with
  MiSTer GPO reset values and unconditional bridge/SDRAM release. Runtime
  `src/native/hardware.cpp` also synchronizes and probes MiSTer after development
  programming. Both assumptions must become explicit dispatch decisions.
- Runtime `src/native/video.cpp` combines ADV7513 I2C setup with MiSTer SPI timing
  commands. Sharing the transmitter code must not send MiSTer commands to Pong.
- FogCast `internal/misterruntime/client.go` strictly decodes local protocol 1.
  Extra response fields and invented state names would break existing clients.
- The FogCast agent owns the renewable kit lease and admission during appliance
  updates. New entry points must use those same gates.
- FogCast `host/tenfoot/gfx/fpga_protocol.md` already describes the FC2D command
  stream and software replay. Its suggested hardware mailbox is not implemented.

## Artifact format and interpretation

Keep bitstream bytes unchanged. Use **format 2** for a TOML manifest and adopt
`.fcore` for its distributable archive. Build directories and archives contain
exactly two regular files at their root: `manifest.toml` and `core.rbf`.

| Input | Interpretation |
| --- | --- |
| Bare `.rbf`, including the existing raw upload API | MiSTer compatibility ABI and MiSTer programming profile |
| Existing format-1 bundle | Validate with the existing reader; adapt its declared MiSTer system to the internal descriptor |
| Format-2 directory or `.fcore` | Validate the entire package; use its declared ABI and profile |
| Existing non-MiSTer experiment | Use an explicit development-only profile as described below; never infer it from a filename or failed MiSTer probe |
| Malformed package or unsupported required contract | Reject; never retry its bytes as a raw RBF |

There is no automatic sibling-sidecar lookup for a bare file. Loading a package
is an explicit operation. Renaming a custom bitstream to `.rbf` does not make it
MiSTer-compatible. Raw MiSTer loading preserves the existing bounded identity
checks; it does not imply launch support for every MiSTer system.

### Archive and admission bounds

The `.fcore` encoding is an **uncompressed POSIX ustar** archive, manifest first,
payload second, followed by two zero blocks. This avoids target decompression
and permits inspection with ordinary archive tools. Export deterministically:
regular-file type, mode 0644, uid/gid/mtime zero, empty owner/group/link/prefix
fields and zero padding. The reader accepts this restricted encoding only,
validates header checksums, and rejects extra members or trailing bytes. No PAX,
GNU extensions, links, directory entries, alternate paths or base-256 sizes.

Manifest size is 1 through 65,536 bytes; RBF size is 1 through 33,554,432 bytes.
The HTTP archive bound is 33 MiB including framing. Bound integer arithmetic
before allocation, reject duplicate TOML keys and duplicate interface IDs, and
require valid UTF-8. Unknown format-2 fields are errors; future optional
interfaces use the existing interface array, not unrecognized schema fields.

Do not run a general-purpose extraction command on uploaded content. Validate
and stream only the two permitted members into a private staging directory;
publish it atomically after verification. Directory imports reject symlinks,
extra entries and nonregular files and are copied into private immutable staging
before use. Retain the validated file descriptors through programming. Admission
must not hash one pathname and later reopen potentially different bytes.

Package identity is lowercase hexadecimal SHA-256 of:

```text
ASCII("FES-CORE-PACKAGE-2\n") ||
u64le(manifest byte length) || exact manifest bytes ||
u64le(payload byte length) || exact payload bytes
```

The manifest also contains the payload SHA-256. Archive framing is excluded from
package identity; changing even descriptive manifest bytes changes package
identity. Caches and selection receipts key on package identity, not RBF hash
alone. A digest establishes byte identity, not publisher authenticity.

### Manifest fields

The following field table is normative. Exporters write deterministic TOML;
consumers hash its original bytes without reserializing it. The schema and
conformance fixtures belong to mister-packages; each consumer uses those same
fixtures rather than acquiring a target dependency on the Go emitter.

| Section | Required fields and meaning |
| --- | --- |
| Root | `format = 2` |
| `core` | `id`, `name`, `description`, `version`; release version is independent of ABI version |
| `target` | `platform`, `device`, `programming_profile`; initial platform is `de10_nano`, device `5CSEBA6U23I7` |
| `payload` | `file = "core.rbf"`, positive integer `size`, lowercase 64-hex `sha256` |
| `abi` | `id`, integer `major`, integer `minor`; these describe the core's implemented wire contract |
| Each `interfaces` entry | `id`, integer `major`, integer `minor`, Boolean `required` |
| `build` | `id`, `repository`, `revision`, `recipe_sha256`, `toolchain`; source/build provenance, never executable instructions |

The only additional optional field is `core.system`, used for a supported
MiSTer system recipe. It is required for packaged MiSTer game launch and must
name an existing generated system definition. Omission permits MiSTer
development loading, not inferred media/reset recipes. Non-MiSTer packages in
this milestone omit it. `interfaces` may be empty.

Core, ABI, interface, platform and profile IDs use lowercase ASCII matching
`[a-z][a-z0-9_.-]{0,95}`; device uses the exact platform device string. Names are nonempty
and at most 128 UTF-8 bytes; descriptions may be empty and are at most 2,048
bytes. Versions use SemVer release syntax; ABI majors are 1 through 65,535 and
minors 0 through 65,535. All strings exclude control characters. Repository is
an HTTPS URL without embedded credentials, revision is a full 40-hex Git commit,
recipe hash is 64 lowercase hex digits, and toolchain is a nonempty description
of at most 1,024 bytes, including exact synthesis/PnR versions or commits.

`build.id` is 32 lowercase hex digits. For new FES cores it is generated before
synthesis from the first 16 bytes of SHA-256 over a deterministic build-input
record: source commit, dependency pins, recipe digest, ABI-definition digest,
tool identities and build parameters. Export retains that record as build
evidence outside the two-file package. The RTL embeds this ID. Never derive it
from the final RBF hash: that would create a circular build dependency. It is
a build correlation value, not a signature or replacement for the payload hash.
Sealed exports require clean, pinned source inputs. A format-2 MiSTer wrapper
may derive its build ID from the recorded legacy provenance, but reports live
build identity as unavailable rather than claiming the upstream core embeds it.

First Pong selection: `core.id = "fes.pong"`, `abi.id = "fes.simple-game"`,
ABI 1.0, programming profile `fes-gp-v1`. Required interfaces are
`fes.gamepad` 1.0 and `fes.video.fixed-720p60` 1.0. Its human name is `FES Pong`.
Existing Pong's system ID and files are not reused for this artifact.

## ABI versions and programming profiles

The runtime has a compiled registry of supported `(platform, profile, ABI ID, ABI major)`
combinations. A profile describes electrical/platform setup, containment,
reset ordering and which bridges may be released. An ABI driver describes live
identity, control, media, input and shutdown. Manifests choose registry entries;
they cannot supply MMIO addresses, reset scripts or driver code. A profile and
an ABI that are individually known but not approved together are rejected.

Use `mister` 1.0 as the FES compatibility-family label. This does not claim that
upstream MiSTer advertises a universal version register. Existing generated
per-system definitions remain responsible for CONF_STR identity, status/reset,
media, save and input protocols, including NES's narrow transfer format.

Breaking wire semantics require a new ABI major. Minor revisions are additive
and preserve older commands. Initially a driver accepts only core minors at or
below its explicitly tested maximum for that major. A newer minor is rejected,
even with the same major; acceptance must not be guessed. Required interfaces
need a supported ID, matching major and sufficient supported minor. Unsupported
optional interfaces are ignored and reported unavailable; the core must remain
usable without them. Dependencies needed to start the core must be required.

| Initial profile | Contract |
| --- | --- |
| `mister-v1` | Preserve the tested MiSTer reset/program/bridge/video ordering; dispatch MiSTer probing and system recipes |
| `fes-gp-v1` | Program through the existing Cyclone V manager, initialize GP deterministically while FPGA configuration reset is asserted, keep unused fabric bridges and SDRAM ports contained; use only the FES GP handshake |
| `development-contained-v1` | Explicit diagnostic programming with bridges/SDRAM contained, no inferred ABI traffic and no controller/video service; intended for existing GP-only experiments |

The diagnostic profile is selected only through a separately explicit developer
option with the existing lease. It is not the default for raw files. It reports
identity as unverified and does not advertise a game ABI. Experiments requiring
other bridges need a reviewed runtime-owned profile, not an option accepting
arbitrary register values. Preserve the old raw development endpoint's default
MiSTer behavior while adding this explicit local protocol-2/CLI mode.

## Programming and lifecycle boundaries

Refactor the current programmer around three responsibilities:

1. The active driver quiesces input and device work. Required save persistence
   completes before replacement; an SNES save failure retains the active game.
2. The platform programmer contains bridges and streams the pinned RBF through
   the FPGA manager, checks completion and applies only the selected profile's
   release recipe. MiSTer GPO meanings are no longer unconditional operations.
3. The selected new driver validates live identity, initializes services,
   neutralizes input and starts execution. No generic MiSTer probe is inserted
   between programming and this driver.

Outgoing-core reset uses the **active** driver's protocol. Initialize the
destination profile's GP values only after the outgoing fabric is contained
and FPGA configuration reset is asserted. In particular, returning from FES
Pong to MiSTer Menu must not send MiSTer reset words to the still-running Pong
mailbox. Preserve the established ordering for MiSTer-to-MiSTer transitions
through the active MiSTer driver, and test every cross-profile direction.

Validate the package, target/profile pairing, required interfaces, driver,
staged files and any required media **before quiescing the current session**.
Check compatibility again at actual activation, even if an earlier inspect
request succeeded. The lifecycle lock serializes this with Stop, input and other
launches. The existing mutation-attempted tracking remains authoritative for
whether failed activation needs physical recovery.

After a mutation, failed programming, live identity, video or input setup follows
the existing bounded idle recovery path. Successful recovery restores the current
MiSTer Menu and controller-ready launcher. Failed recovery reports
`reboot_required`, keeps kit ownership blocked and uses existing recovery rules.
Never claim that restoring the previous game is automatic. Do not probe a
mismatched core using a second ABI as an attempted repair.

Every successful activation receives a runtime generation. Input workers and
later device handles carry that generation and cannot operate after replacement.
Stop drains the active driver before programming Menu. A runtime restart starts
from the existing recovery-to-idle procedure; it does not adopt an unknown live
core merely because its GP signature resembles a known one.

## FES GP transport and standalone Pong

Use the Cyclone V HPS general-purpose connection already exercised by misteross
`experiments/020_linux_mailbox`. This is a new protocol, not a promise that the
existing experiment implements it. Discovery accesses the HPS-side GP registers,
not a speculative fabric MMIO address. GP use with contained bridges must pass
the designated-kit acceptance test before this profile is called supported.

The first contract is a one-request-at-a-time, 16-bit command mailbox:

| Word | Bits |
| --- | --- |
| HPS to FPGA (GPO) | 31 request toggle; 30:24 opcode; 23:16 index; 15:0 argument |
| FPGA to HPS (GPI) | 31:24 signature `0xF5`; 23 acknowledged toggle; 22 error; 21:16 zero; 15:0 response |

Configuration initializes GPO to zero before releasing configuration reset. RTL
starts with ACK zero, stable signature, neutral buttons and gameplay held in
reset. The host presents command fields with the previous toggle, executes the
MMIO ordering barrier and then toggles bit 31. It holds the entire request until
ACK matches. RTL synchronizes the toggle and samples the held payload after it
has settled; it must not independently synchronize and immediately consume a
changing multibit command. The response is held until the next completed request;
the host confirms a stable response/ACK before using it. Duplicate reads/writes
with unchanged toggle cannot execute a command twice.

One exchange has a 100 ms deadline bounded by the enclosing operation deadline;
complete discovery has a 2 s deadline. No blind retry after an ambiguous timeout.
Unsupported opcode/index/argument returns error with response 1/2/3 respectively
and has no gameplay side effect. A reset command never resets the mailbox ACK.

| Opcode | Meaning |
| --- | --- |
| `0x01` | Read immutable identity word at index; argument must be zero |
| `0x02` | Gameplay control; index 0; argument 0 holds reset and clears input, argument 1 releases reset; success response zero |
| `0x03` | Set player buttons; index 0 is the only player; success response echoes accepted mask |

Identity indices 0..15 are: `0x4546`, `0x3153` (bytes `FES1`), transport major 1,
transport minor 0, ABI tag 1 (`fes.simple-game`), ABI major, ABI minor, capability
mask, then eight build-ID words. Build-ID words contain consecutive byte pairs
from the hexadecimal build ID, low byte first. Capability bit 0 is
`fes.gamepad` 1.0 and bit 1 is `fes.video.fixed-720p60` 1.0. The shared definition
owns numeric assignments. Required recognized capabilities must be present;
an advertised recognized interface that is absent from live identity is a
mismatch. Unknown capability bits are ignored, never treated as permission to
enable another service. Driver verifies full magic, transport version, ABI
tag/version and build ID against the package before sending gameplay commands.

Buttons bits 0..7 are Up, Down, Left, Right, A, B, Select, Start. Bits 8..15 must
be zero. Opposite directions are neutralized by the input adapter. The first
Pong uses Up/Down for the player's paddle and an automatic opponent; the other
buttons may be unused by its game logic. The existing Select+Start hold remains
a software Stop gesture. Neutral input on attach/disconnect and lease cleanup
uses the active driver, including when status calls the core a development run.

Pong emits fixed 1280x720 at 60 Hz: 74.25 MHz pixel clock, horizontal
1280/110/40/220 and vertical 720/5/5/20 active/front/sync/back, positive sync,
RGB888 with data enable. Scale the 320x240 playfield to 960x720 centered with
black side bars. Keep synchronization running while gameplay is reset. Reuse
the existing Pong game logic where useful, but provide the small fixed raster
and HDMI pin/clock shell without MiSTer `hps_io`, scaler or external SDRAM.
Audio, ROM loading and saves are absent from this ABI slice.

Runtime reuses the verified ADV7513 I2C initialization and link validation with
a dedicated fixed-video path. It must not call the existing combined SPI/I2C
`ApplyMode` on the custom core. Simulation verifies raster timing and mailbox
CDC/ordering; Mistral build evidence must include PLL/clock constraints, resource
use, timing results and exact output digest. A bitstream file alone is not proof
of working clocks, correct video or timing closure.

## FogCast and local protocol compatibility

Keep local protocol 1 byte-shape compatible: the same accepted fields, state
names and errors for existing operations. Add protocol 2 on the same Unix
socket for read-only capability/status discovery, package inspection and
`load_core` by staged package identity/path, plus the explicit contained
development mode. Both protocols share one lifecycle controller. Preserve the
65,536-byte request/response bound and strict per-version decoding.

A new agent first makes a read-only protocol-2 status request. A valid protocol-1
`unsupported_protocol` response permits fallback to existing MiSTer operations.
Malformed responses, timeouts and failed custom loads never trigger fallback.
Old agents can continue requesting protocol 1 from a new runtime.

Protocol 2 reports supported profiles, ABI/interface versions and the active
package ID, declared/observed identity, build ID, generation and capabilities.
Use existing lifecycle states for this slice: standalone Pong is
`running_development`, execution `development`, with a core name and no system
field. Protocol-1 status projects that same existing shape without new fields;
startup has no identity until confirmed. Do not invent a catalog system merely
to satisfy `running_game`. The input admission path must explicitly support
the active development driver when it advertises `fes.gamepad`.

New custom operations return structured protocol-2 compatibility errors; old
operations continue using their existing error vocabulary. Report the failed
phase and expected/observed contract, not only “Kit not ready.” A successful
upload or FPGA programming step is not a successful core activation.

Add authenticated target `POST /v1/development/core` for `.fcore` upload and
activation, alongside the existing raw `/v1/development/rbf`. It requires
Content-Length, the existing kit lease, upload bounds and appliance-update
exclusion throughout admission/activation. Update all agent/host mutation-route
lists and lease-cleanup handling together. The host CLI can invoke it through
the existing target/session client without a new UI route or control design.
Local package inspection remains usable without reserving or changing the kit.

The agent stages the package in its private content store. The runtime validates
the descriptor and payload itself and accepts only configured staging/installed
roots. Network clients do not choose arbitrary target file paths. Cleanup cannot
remove active or in-flight staged artifacts. The library shares the same parsing
and driver admission used by the daemon; direct library use must not be a less
strict path. Raw-development compatibility and custom-package input need separate
tests because today's raw development loader does not start the game input path.

## Future menu accelerator boundary

Later define `fes.menu-2d` as another ABI driver over the same package and live
identity mechanism. Reuse FC2D's existing software vocabulary and replay oracle;
negotiate the subset actually implemented in hardware. Do not label today's
FPGA recorder/stub as an accelerator, or freeze proposed DMA/MMIO and texture
heap layouts as part of this Pong milestone.

The menu core can occupy the FPGA while the launcher is idle. Before a game
replaces it, rendering must stop accepting commands, drain bounded in-flight
work, release runtime-owned buffers and invalidate every generation-bound
texture/queue handle. Stop reloads the menu and creates a new generation.
The existing kit lease remains the sole session owner; local rendering receives
revocable access within that lifecycle, not a competing lease service.

Bulk graphics should use a local runtime-owned data path, not an HTTP request
per draw. FogCast retains `gfx.Device` ownership; runtime owns physical buffers,
register access and fencing. Preserve software rendering as the fallback. The
UI team and runtime team will define the acceleration contract together in its
own milestone; no UI component edits are required here.

## Component work and integration order

| Owner | Deliverable |
| --- | --- |
| mister-packages | Format-2 schema/fixtures, ABI/profile/interface definitions and numeric registry; generated C++14, Go and synthesis-compatible Verilog constants |
| misteross | Deterministic format-2 exporter; FES GP shell, simulations and standalone Pong/Mistral recipe; source/tool/timing evidence |
| libmister-runtime | Package preflight, profile/programming split, driver dispatch, GP Pong and fixed video, generation/input/Stop recovery, protocol 2 |
| FogCast | Protocol negotiation, private staging/upload, lease/update admission, capability/error reporting and development CLI; no UI redesign |
| FES | Format-2 selection/receipts and installed package layout, compatible component pins, assembled-image and kit acceptance evidence |

Freeze the shared fixtures and GP definitions first. Runtime hardware fakes and
RTL simulations must consume the same golden exchanges. Then component owners
can work in separate worktrees against those contracts; only the FES integrator
changes parent pins. Generated consumers are checked in and regenerated from
the shared authority. Extend `make check` to catch drift, including RTL constants.

Install the demonstration as an immutable package beneath
`/usr/share/mister-runtime/core-packages/<package-id>/`, and record both package
and payload digests in the FES selection receipt. Leave format-1 selection
available; migrate existing artifacts individually instead of forcing four
unrelated core rebuilds. Reuse compiler, Linux and image caches during development.

## Acceptance

Software acceptance requires shared positive/negative package fixtures in every
reader, deterministic directory/archive identity, corruption/path/size/duplicate
rejection, and simulated lifecycle assertions showing **zero hardware mutation**
for every preflight compatibility failure. Cover same payload with changed ABI
metadata, unsupported optional versus required interfaces, incompatible versions,
staged-file replacement, old/new client combinations, lease expiry/update races,
input generation and failed cleanup. Exercise transport timeouts, duplicate
toggles, stale startup GP values and live identity/build mismatches.

Hardware acceptance requires one designated, leased kit and an exact assembled
image/package/RBF record:

1. Load an existing bare MiSTer RBF and existing format-1 selection successfully.
2. Submit invalid/unsupported packages while the launcher or a game is usable;
   verify rejection preserves that session and its input.
3. Load the standalone Mistral-built Pong package. Record live ABI/build identity,
   visible correct video and paddle response through the existing controller.
4. Hold Select+Start to Stop. Verify a responsive launcher; relaunch Pong and a
   supported MiSTer game, including a return to Menu between them.
5. Verify lease cleanup for the custom run and an intentional identity-mismatch
   artifact recover to responsive idle or explicitly blocked recovery state.

Use simulated failures for cases that do not need physical evidence. Record
hardware limitations honestly: if Mistral routing, PLL support or video fails,
the package/runtime foundation may be software-complete, but this milestone is
not hardware-accepted. A Quartus oracle is useful diagnostic evidence only.

## Design rationale

A separate JSON sidecar is easy to lose or mismatch, and duplicates the existing
TOML bundle. Appending a trailer to RBF bytes risks compatibility with existing
programmers. A constrained bundle provides one distributable identity while
preserving raw-RBF tools and the established build boundary.

The distinction between programming an image and driving its application
interface is also consistent with the Linux
[FPGA manager abstraction](https://docs.kernel.org/driver-api/fpga/fpga-mgr.html).
This design continues using the repository's existing programmer; it does not
require a migration to a different kernel programming API.

## Protocol-2 wire encoding clarification

The following encoding fixes the nested wire contract for implementation.
It preserves the lifecycle and compatibility semantics above.

### Envelope and inspection

Every v2 response has the existing eight members plus capabilities,
active_package, generation, and inspected_package. All twelve are always present.
Existing fields retain their v1 types/null semantics, except protocol is2 and
error has the v2 shape below. inspected_package is null except a successful
inspect_core response. This additive inspection field avoids falsely replacing
active_package when inspecting an inactive package. Status always describes the
real lifecycle, including during inspection.

active_package is null unless a described package is confirmed active. Otherwise:
{package_id: string, descriptor: Descriptor, observed: Observed}.
Observed is {abi: {id:string, major:integer, minor:integer}|null, build_id:string|null}.
MiSTer live probing proves neither an ABI version nor an embedded build, so
its observed abi and build_id are both null. The selected/declared ABI remains
in the descriptor and registry. FES GP uses the verified live ABI version and
exact32lowerhex build ID.
The existing top-level core is the runtime's observed/logical core identity;
no new claim that the GP wire announces a textual core name is made.

inspected_package is {package_id:string, descriptor:Descriptor,
compatible:boolean, compatibility_error:Error|null}. A well-formed unsupported
package is inspectable: ok=true, compatible=false, compatibility_error explains
why it cannot activate. Invalid content or wrong claimed identity is a failed
request with inspected_package=null. An inspection never changes generation,
active_package, input or hardware.

Descriptor mirrors the closed manifest model as JSON: format, core, target,
payload, abi, interfaces, build with the exact same scalar types and bounds.
Optional core.system is omitted if absent; no private path or raw manifest text
is included. This is a typed projection, not a second manifest schema.

### Capabilities and generation

capabilities is always an object with exactly these members:
- programming_profiles: sorted array of supported profile ID strings;
- abis: sorted array of {id,major,minor,interfaces}; interfaces is a sorted
  array of {id,major,minor} supported by that ABI driver;
- active_interfaces: sorted array of {id,major,minor} that the current verified
  described core actually enables.

Registry data comes from the actual installed driver/profile registry and shared
generated definitions. active_interfaces is empty before successful activation,
in idle/reboot_required, and for raw diagnostics. It includes fes.gamepad only
after the complete custom activation has succeeded. Native catalog input retains
its existing native eligibility; do not invent a FES ABI for bare MiSTer.
Unknown optional manifest interfaces are inspectable but never advertised as
active supported interfaces. Use exact declared supported versions, not a fake
maximum. Arrays have unique IDs and deterministic ID ordering.

generation is JSON null without a confirmed active runtime generation, otherwise
a positive uint64 JSON integer. C++/Go consumers must decode it exactly, never
through float64. Status during starting exposes null until confirmation; idle,
Stop-complete and reboot_required expose null. A preserved pre-mutation session
keeps its prior generation; successful replacement changes it. Inspection
consumes no generation. This exposes the existing active generation, not a
second session counter. Successful raw diagnostics also receive a lifecycle
generation even though they advertise no input capability or active package.
A generation alone never authorizes input; raw input remains disabled.

### Errors

error and compatibility_error use {code:string,message:string,phase:string}
with optional expected:string and observed:string. Optional values are omitted
when unavailable, never encoded as null. Keep existing message bounds, and
bound phase/expected/observed through the same fixed protocol output limit.
Never include private package paths or secrets in returned error metadata.

Allowed error codes: the existing non-none codes plus invalid_package,
unsupported_target, unsupported_programming_profile, unsupported_abi, and
unsupported_interface. Existing core_mismatch identifies live identity/build
mismatch; io_failed covers exchange failures. Do not add alternate synonymous
codes. These are wire distinctions backed by structured runtime admission data,
not parsing human-readable error strings.

Allowed phases: request, admission, compatibility, save, quiesce, programming,
transport, identity, video, input, recovery, lifecycle. Use the concrete failure
phase when known; lifecycle is the fallback for existing unclassified errors.
Protocol1 serializes only code/message. New v2-only codes project to existing
invalid_request for v1; existing codes remain unchanged. No new error keys leak
into v1, and compatibility errors are not converted to successful v1 replies.

### Request bounds

Keep the existing strict absolute-path string grammar/length bound for rbf and
package_path. Additionally package paths must be contained beneath configured
private roots before opening; no lexical traversal or symlink-based escape.
package_id is exactly64lowerhex and required for inspect/load. No alternate
normalization or implicit raw fallback. Diagnostic profile is the exact literal
development-contained-v1. Unknown fields/operations/versions fail under the
version-dispatched decoder; preserve the existing v1 unsupported_protocol
response required for safe read-only negotiation fallback.
