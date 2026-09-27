# Mesh-aware menu and core setup — draft

## Intent and approved constraints

The person at the kit should browse systems, supply their own required ROMs, launch games and return to the menu without agent-operated CLI commands. SMS and SG-1000 should be visible alongside the existing cores. Start with an available FogCast host service and cached browsing when offline. Do not encode a permanent one-host/one-kit relationship: multiple catalog/content nodes and a kit running the same FogCast host are the intended direction. The other agent owns current Apple II hardware testing; this work is host-only until the kit is explicitly available.

## Evidence from current main

Base: aaf7de2c. FES registers Catch, Pong, ZX81, Coleco, SMS, SG-1000 and Apple II in config/core-recipes.toml; the default profile includes only Pong, ZX81 and Coleco. Registered does not mean built, installed, compatible or hardware-qualified.

FogCast already imports immutable packages/media, creates core entries, binds named ROM inputs and expansion slots, and projects described entries into mesh content identities. Discovery advertises FPGA execute/display/input capabilities. Placement, lease admission and Ensure already have code. Existing mesh documents contain historical status statements; current sources and ARCHITECTURE.md take precedence.

The launcher currently has one configured service endpoint and paired authentication. The mesh capability bag does not yet advertise host Catalog/Content/Shell capabilities, and the local catalog projection is not cross-host catalog federation. This design must not claim either is delivered.

The current splash has no HPS framebuffer or GP mailbox. Existing fogcast-kit browse rendering therefore cannot appear on its HDMI merely by enabling a configuration flag. gfx FPGA/FC2D is a software stub, not a display driver. A menu-capable FPGA display path is a separate hardware deliverable.

## Recommended delivery order

### 1. Reproducible core catalog and setup services

FES publishes a curated package catalog from prepared, validated producer outputs. Each catalog entry records core id, immutable package id, archive digest/location, display name and release standing. An available recipe without a produced artifact is shown as unavailable for installation; normal user setup never runs Yosys/nextpnr. Artifacts may be shipped with a host distribution or served through an explicitly configured catalog source. The initial slice uses a local published catalog; it does not invent an internet update service.

Keep producer registration in FES and runtime requirements in sealed manifests. Small catalog metadata may map a core to its browse system; never duplicate ABI compatibility, mapper rules, ROM limits or transport recipes in UI metadata. New compatible cores need producer registration and curated publication, not a UI core allowlist.

FogCast reconciles catalog packages into its existing package store idempotently. It exposes available systems even before games have been created, with package availability and missing inputs separately visible. ROM-less packages can provide one default library entry. ROM-requiring packages remain setup items until explicit inputs are supplied; no synthetic ROM is silently installed and no proprietary BIOS is shipped.

Initial target coverage is the existing factory set plus SMS and SG-1000, subject to authenticated package preparation and current compatibility checks. Catch may appear as a demo. Apple II remains explicitly experimental until its current development/acceptance work is complete; this task does not alter that agent's artifacts or claim its result.

### 2. Guided game creation in the existing library

The setup service reads named ROM and media/firmware requirements from the selected descriptor. The user chooses a system/package, supplies ROMs from an existing library source or upload, confirms the title, and receives a normal package-backed game entry. Use existing immutable media storage, named-ROM selection, firmware and expansion APIs. Validate all selections before declaring Ready; exact SMS 32 KiB and SG-1000 16 KiB cartridge requirements remain enforced. A filename or extension does not establish mapper compatibility.

Matching scanned files may be offered as candidates. Multiple candidates require a choice. Do not silently convert old raw-core catalog rows, replace user-selected package versions, bind firmware by display name, or infer persistence from package paths. Package updates are explicit selections; repeated catalog sync does not create duplicates or overwrite selections.

Setup screens show meaningful actions such as Install core, Choose cartridge, Choose BIOS and Cannot play here yet. All shells consume the same setup service; start with browser management and shared client models, then connect the existing kit browse surface. Do not build a second kit-only catalog.

### 3. Native menu display and lifecycle

Implement a supported native display transport separately, with mister-packages owning the contract, misteross owning its FPGA implementation and sealed producer, libmister-runtime owning physical access/idle lifecycle, and FogCast owning rendering. Retain the boot splash. Stop-idle and boot firmware may diverge through the existing FES artifact policy.

Prefer a bounded pixel/framebuffer presentation transport that can carry the existing software renderer before building a full graphics accelerator. This preference is a design direction, not approval of a wire layout or proof of throughput. Resolve memory capacity, transfer budget, pixel format, double buffering, HDMI timing and runtime access in a dedicated display spec and simulation before implementation. Compare it against a minimal native tile renderer; do not restore conventional Main/MiSTer SPI or claim FC2D hardware exists.

Launch yields the menu display before programming a game. Stop restores the defined menu-capable idle, then the shell repaints the same browse location. Rendering does not own FPGA programming, leases or recovery. No overlay writes during active gameplay. No changes to boot/media provisioning until the separately reviewed display artifact is ready.

## Mesh boundaries

Treat the chosen catalog service as a source of library/setup operations, not the identity of the executor. Preserve source identity alongside its local game id so future federation cannot collide on identical title slugs. A publication source keeps its stable namespace across catalog revisions; catalog generation hashes are separate from that source identity. Package id and each BIOS/cartridge/expansion content id stay independent; paths are provider-local implementation details.

A kit may later deploy the existing FogCast host locally and expose the same services. Do not add a permanent Host/Kit enum, a second coordinator/lease, or a separate offline library implementation. Selecting a service source must not steal a live session or reassign its coordinator. Different shells may use different available services and executors through existing placement and agent lease admission.

The first slice uses a configured service endpoint and explicit source context. Automatic Catalog/Content host discovery, trusted multi-source enrollment and federated title merging remain the existing mesh program's work; make future integration possible without pretending those contracts are implemented now. Cached browsing is labeled stale/offline and does not authorize a launch. When the kit runs a healthy local FogCast host later, that is an online service deployment, not an exception to the offline rule.

## Alternatives

Shipping every registered core in the rootfs improves immediate inventory but alone leaves manual library creation and cannot represent cross-host availability. A separate kit database would duplicate existing services and conflict with kit-as-host. A full graphics accelerator from the outset makes basic menu availability depend on a much larger RTL project. The recommended catalog/setup-first order delivers useful software while display work is specified independently.

## Validation and completion

Host-only tests cover catalog integrity, repeat sync, preserved user selections, missing artifacts, manifest-derived required inputs, explicit ROM choices, source-scoped identity and compatibility states. Existing mesh projection, Ensure, placement and lease tests must remain valid. Cached offline views never become Ready solely because a title was cached. Render/model tests cover systems with no games and actionable missing-input states.

Package preparation proves artifact identity, not hardware acceptance. SMS/SG-1000 require bounded exact-package hardware testing once the kit is available. Menu display requires simulations, timing closure, sealed provenance and exact-image launch/Stop/video/input acceptance. A browser setup pass does not qualify HDMI menu support. No hardware operations are performed during the other agent's Apple II session.

## First implementation scope for review

Approve and plan slices 1–2 first: published local core catalog, package reconciliation and guided ROM/game setup, including SMS and SG-1000. In parallel conceptually, prepare the separate native display design; no display RTL, new runtime protocol or mesh federation is included in that first implementation. Written implementation plan and file ownership must precede code changes.
