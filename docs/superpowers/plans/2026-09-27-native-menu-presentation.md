# Native Menu Presentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The user selected native execution for this feature; preserve that choice.

**Goal:** Produce a described menu firmware and a runtime-owned local frame presentation path, then prove alternating frames and menu/game/Stop handoff on the designated kit.

**Architecture:** Extend `fes.application` with an additive menu-display capability and fixed-layout controls. Reuse the qualified menu reader and shared DDR guards, the runtime's package admission/programming/containment path, and its existing Unix socket. A frame transfer uses a runtime-created staging descriptor; only the runtime maps the reserved DDR slots. Tenfoot integration follows this physical transport proof.

**Tech Stack:** Shared YAML and generated C++14/Go/Verilog, Verilog/Verilator, authenticated Yosys/nextpnr/Mistral on GPU 0, C++14 runtime, Linux Unix sockets/SCM_RIGHTS and sealable memfd.

**Spec:** [Approved native HDMI menu design](../specs/2026-09-27-native-menu-display-design.md).

## Global Constraints

- Start from `feat/native-menu-scanout` commit `9beaa440`; main dependency `e21c49b9`. Keep this task in `/home/deano/fes/out/dev/native-menu-display/fes` and preserve unrelated work.
- Two slots, each 4 MiB, at offsets 0 and `0x00400000` within the shared DDR window. Each frame is exactly 1280×720×4 = 3,686,400 bytes, stride 5,120 bytes. Little-endian XRGB8888 storage contains B,G,R,unused bytes.
- UI supplies RGBA8888 in runtime-created staging memory. Runtime converts to XRGB8888 while copying to an available slot. JSON contains metadata only. No client-supplied physical addresses or `/dev/mem` descriptors.
- Use shared `fes.memory.hps-ddr` 1.0 boot/layout admission and port release. Never rewrite live SDR static configuration. Verify Linux exclusion and the target kernel's noncached mapping/write-ordering behavior before claiming physical acceptance.
- One outstanding frame preparation or submission. A displayed sequence releases the previous slot; a submission ACK alone does not. Quiesce drains before execution hold. Unexpected live hold retains the diagnostic's fail-closed rule.
- One runtime lifecycle and production hardware construction path. Presentation generations are distinct from gameplay generations; every menu reactivation gets a fresh nonzero generation.
- Preserve independent pattern and DDR diagnostics. New described firmware has a separate sealed package output. It is not a playable catalog entry and is not selected in factory images by this plan.
- GPU 0 only. GPU 1 remains available to the router investigator. Illegal routes cannot publish packages; report reproducible nextpnr failures separately.
- Hardware requires the existing designated-kit lease. Earlier pattern or RAM tests do not accept the new described menu bytes.

## Review Focus

- A writable or substituted staging FD must not permit mutation during copying; require matching runtime-created file identity, exact size and complete immutable seals before any DDR write (Tasks 3, 5).
- Stop or game replacement between frame preparation and commit must revoke that generation without waiting for the client; reject the late frame before mapping/copying (Tasks 4, 5).
- A split or truncated ancillary message must close every received FD and leave slot ownership unchanged (Task 5).
- Sequence or generation wrap must not make old completions appear current; reject exhaustion and require reactivation before reuse (Tasks 1, 2, 4).
- A failed display acknowledgement or uncertain drain must prevent slot/window reuse until verified physical containment; fallback must not disguise missing menu acceptance (Tasks 3, 4, 6).

## Contract decisions

Add required interface `fes.video.menu-display` 1.0 at application capability bit 9, alongside fixed video and HPS DDR. Keep ABI/transport major and minor at 1.0; old runtimes reject the newly required interface. Use free application opcodes 18–21; do not change existing opcodes 1–17.

- `MenuInfo` (18), argument 0: indices 0 width=1280, 1 height=720, 2 stride=5120, 3/4 frame bytes low/high, 5/6 slot bytes low/high, 7 pixel format=1 (XRGB8888), 8 slot count=2, 9 state, 10/11 displayed sequence low/high, 12/13 underflows low/high. Read of each low word latches its high word; high without a preceding low returns invalid state. State bits: configured=1, enabled=2, pending=4, quiesced=8, faulted=16.
- `MenuConfigure` (19), index 0, argument 1: select the fixed layout and both fixed slot offsets while disabled and drained. No address upload exists. Invalid arguments leave configuration unchanged.
- `MenuControl` (20), index 0: argument 0 quiesces; ACK waits until drained and scanout is disabled. Argument 1 enables a configured, released, nonfaulted display. No new submission is accepted during quiesce. Execution HoldReset is legal only after quiescence; it cannot hide responses needed by the drain.
- `MenuSubmit` (21): index 0 stages sequence low, index 1 stages high, index 2 commits with argument slot 0 or 1. Require ordered fields and exactly one pending switch. Sequence must be nonzero and strictly greater than the last accepted sequence; no wrap. A commit ACK means accepted, not displayed. Malformed staging never changes the live slot. Runtime chooses the inactive slot and polls the displayed sequence before permitting another copy.

On reset, configuration is absent, scanout disabled, execution held and no sequence pending. Identity/build/layout checks precede port release. Runtime zeros both frame regions, configures, releases execution, then enables scanout; initial slot 0 is therefore safe black. An accepted slot switch occurs only through the existing blank-prefetch/frame-boundary path. No writes or arbitrary reads are exposed by the firmware.

Frame preparation uses a two-phase exchange on one existing Unix connection. `menu_frame_begin` obtains the current generation and one runtime-created, exact-size memfd, delivered using SCM_RIGHTS. The client fills RGBA, unmaps writable mappings, applies WRITE/GROW/SHRINK/SEAL seals, and sends `menu_frame_commit` plus that same FD on the same connection. Runtime validates identity, seals, generation and size before copying. The connection holds the preparation object, not the lifecycle mutex; game load can revoke it immediately. EOF or a fixed five-second preparation deadline closes it. After accepted commit, runtime retains the immutable bytes until completion/containment even if the client disconnects. This bounds allocation to one staging frame and avoids trusting a client promise not to write during copy.

## Task 1: Shared menu contract and generated consumers

**Files:** Modify `sources/mister-packages/packages/abi/fes_application.yaml`, `sources/mister-packages/docs/application-io.md`, `sources/mister-packages/docs/schema.md`, and mapped generated consumers. Create `sources/mister-packages/scripts/menu_display_fixtures.py` and `sources/mister-packages/testdata/menu-display-v1/exchanges.json`; register the fixture copy in the existing FES generation mapping. Update the application oracle record in the established format.

**Interfaces:** Emit `FesApplicationOpcodeMenuInfo/Configure/Control/Submit`, `FesApplicationMenu*` geometry/state/index constants, and `FesApplicationInterfaceVideoMenuDisplay*` symbols. All generated consumers use those exact names and their existing language naming conventions. Golden fixtures specify contiguous mailbox toggles, including errors, delayed quiesce and accepted-versus-displayed submission.

- [x] Add failing real-YAML/emitter/fixture tests for bit 9, opcodes 18–21, exact geometry and existing opcode preservation. Assert invalid configure/state/sequence requests do not change displayed state, and low/high reads form coherent snapshots.
- [x] Run `make -C sources/mister-packages test`; confirm failure is missing menu definitions/fixtures.
- [x] Add the constants, interface and authoritative wire documentation above; generate the synthetic exchanges without changing older fixtures.
- [x] Run `make generate`, review every mapped consumer, then `make check-generated` and `make -C sources/mister-packages test`; require all checks pass.
- [x] Commit the shared contract and generated consumers together.

## Task 2: GP-controlled menu firmware and sealed package

**Files:** Modify `sources/misteross/cores/fes-common/rtl/fes_application_gp.v` with an optional menu request/response hook, default disabled. Create `sources/misteross/cores/fes-menu/rtl/fes_menu_control.v`, `sources/misteross/cores/fes-menu/rtl/menu_top.v`, `sources/misteross/cores/fes-menu/sim/menu_gp_tb.cpp`, `sources/misteross/scripts/build_fes_menu_package.py`, and `sources/misteross/tests/test_build_fes_menu_package.py`. Extend `sim_fes_menu.py`, Makefile, core README and architecture documentation. Reuse diagnostic DDR/video RTL; do not replace `top.v` diagnostic modes.

**Interfaces:** The shared GP module keeps identity/execution and mailbox toggles. With `ENABLE_MENU=1`, it presents a stable latched menu opcode/index/argument and a one-request-at-a-time valid/response handshake to `fes_menu_control`; existing instantiations with default zero continue rejecting menu opcodes. The control module drives configure/enable/quiesce and reader submissions, receives ready/drained/sequence/underflows/fault, and produces menu replies. Run both at the pixel clock to avoid adding an unqualified multiword clock crossing. `menu_top.v` instantiates actual HPS GP, shared DDR and HDMI/I2C atoms.

Package: format 2, core `fes.menu` 1.0.0, no `core.system`, ABI `fes.application` 1.0, profile `fes-gp-v1`, required interfaces fixed video, HPS DDR and menu display only. Output `build/oss/fes-menu-package`; reuse normal package identity/export and compiler evidence helpers. Pin the qualified RAM-test lock explicitly. Preserve all structural DDR inactive-operation, synchronous-memory, electrical and timing gates; add exactly one GP resource and sealed build-ID checks.

- [ ] Add golden-wire simulation through the actual GP module and DDR wrapper: reset/disabled no reads, configure restrictions, black initial slots, identity/build capability, one pending submission, frame-boundary completion, command stalls, sequence exhaustion and quiesce before hold. Assert writes/unused ports remain zero and reads stay in the two exact frame ranges.
- [ ] Run new menu GP/board cases through `sim_fes_menu.py`; confirm missing control/top fails.
- [ ] Implement the optional hook/control/top and producer. Hold quiesce ACK until the shared reader drains. Execute hold only after that acknowledgement; reject illegal direct holds rather than abandoning responses. Keep existing shared GP defaults behaviorally unchanged.
- [ ] Run all menu cases, existing application/demo/Catch simulations and focused producer tests. Validate old runtimes reject the required menu interface and new live identity admits exactly its declared capabilities.
- [ ] Commit clean sources, then build the sealed package on GPU 0. Require legal route, 74.25 MHz pixel closure, exact DDR layout, inactive operations and immutable package/export records. Commit documentation with the actual evidence; no automatic programming.

## Task 3: Runtime menu GP driver, memory mapping and immutable staging

**Files:** Create `sources/libmister-runtime/src/native/menu_display.hpp/.cpp`, `src/native/linux/menu_memory.hpp/.cpp`, and `tests/unit/menu_display_test.cpp`, `tests/unit/menu_memory_test.cpp`. Modify `native/fes_gp.cpp`, `native/core_package.cpp`, production construction and Makefile. Update runtime architecture and support matrix.

**Interfaces:** `MenuGeometry` contains width/height/stride/frame_bytes/slot_bytes. `MenuDisplayInfo` contains configured/enabled/pending/quiesced/faulted, displayed sequence and underflows. `MenuDisplayDriver(FesGp&, Clock&)` provides `ReadInfo(deadline, info*)`, `Configure(deadline)`, `Enable(deadline)`, `Submit(slot, sequence, deadline)` and `Quiesce(deadline)` returning existing `Error` values. `MenuMemory` owns exactly an 8 MiB reserved mapping and provides `InitializeBlack()` and `CopyRgba(slot, immutable_frame)`; its injectable Linux operations expose reservation evidence, mapping, writes and the ARM visibility barrier for focused tests. `MenuFrame` is a move-only runtime-created exact-size staging file with an FD accessor, identity and immutable validation; it never contains a physical address.

- [ ] Add unit tests using fake GP/mapping operations for wrong geometry, boot/window/reservation disagreement, mapping failure, exact RGBA→BGRX conversion (X=0), last-pixel/stride boundaries, no write outside frame bytes, write barrier before submit, and no allocation outside the shared reserved region.
- [ ] Add Linux FD tests for matching memfd identity, exact 3,686,400-byte size, all four seals, substituted/truncated/grown descriptors and cleanup; assert rejection occurs before any DDR write. Run tests and observe missing implementation failures.
- [ ] Implement the GP adapter and bounded mapper. Use `/dev/mem` with `O_SYNC` only inside runtime; do not expose it to clients or assume that flag alone proves noncached ARM behavior. Admission must verify effective Linux RAM exclusion, not merely a matching string in `/proc/cmdline`; fail closed on absent/ambiguous evidence. Record target-kernel mapping and barrier qualification as Task 6 prerequisite.
- [ ] Implement runtime-created sealable staging, validated immutable access and RAII teardown; no path names or arbitrary client allocation enter presentation. Permit one staging object; sequence completion controls DDR slot reuse.
- [ ] Run focused binaries and `make -C sources/libmister-runtime test`; cross-build through the established ARM target when the configured toolchain is available. Commit with host-only support classification.

## Task 4: Runtime idle-menu activation and lifecycle fencing

**Files:** Modify `include/libmister-runtime/runtime.h`, `src/runtime.cpp`, `src/native/hardware.hpp/.cpp`, `src/native/fes_gp.hpp/.cpp`, `src/native/idle_recipe.hpp`, `src/linux/production_hardware.cpp`, runtime/native hardware tests and fake hardware support. Extend architecture/support documentation.

**Interfaces:** Add separate `Status::menu_display` with available, package_id, generation, geometry, displayed_sequence and underflows; gameplay `Status::generation` retains its existing meaning. Public operations: `Runtime::ConfigureMenuPackage(directory, expected_package_id)`, `Runtime::BeginMenuFrame(expected_generation, unique_ptr<MenuFrame>*)`, and `Runtime::PresentMenuFrame(expected_generation, MenuFrame&, MenuDisplayInfo*)`. The internal hardware interface mirrors configuration/staging/presentation and reports menu availability after `LoadIdle()`. Default configuration remains splash. A configured menu is opened/admitted through the existing described-package path; its hardware activation reuses the existing programming implementation rather than introducing a second bridge-release path.

- [ ] Add failing lifecycle tests for startup splash, successful explicit configured menu, wrong package/core/interface/build rejection, zero/new presentation generations, stale frame after game replacement, and no presentation during contained diagnostics/gameplay/reboot-required state.
- [ ] Add failure tests: immutable frame rejected without mutation; display-ACK timeout poisons presentation; quiesce ambiguity prevents window reuse until existing physical containment succeeds; failed containment leads to reboot-required. A pre-mutation game admission failure preserves the menu. Stop restoration creates a fresh generation and requires a fresh complete frame.
- [ ] Implement admission/configuration and activation within the current runtime busy/fault fences. Keep menu in idle status rather than creating a playable active package or input pump. Allocate a nonzero display generation after successful activation; reject generation exhaustion before reuse. Revoke preparation handles before any programming transition. During actual copy/submission, serialize hardware mutation and return explicit busy to concurrent mutation requests using existing conventions.
- [ ] Extend application driver quiescence for menu capability: drain through MenuControl before execution hold and existing bridge containment. Reuse ordinary LoadIdle/Stop/failure recovery; verified containment is required before unmapping/reusing the window. If menu activation fails, contain and restore splash when possible, but report menu unavailable with its error rather than claiming menu success.
- [ ] Run runtime/native hardware/GP tests, then complete runtime test target; verify old no-menu tests and generation semantics remain intact. Commit this behavior with current support documentation.

## Task 5: Existing Unix socket descriptor transport and diagnostic client

**Files:** Create `src/daemon/menu_frame_transport.hpp/.cpp` and `tools/menu_pattern_client.cpp` in libmister-runtime. Modify daemon protocol/controller/server, Makefile, integration daemon tests and protocol documentation. No FogCast API or tenfoot import changes in this task.

**Interfaces:** Protocol 2 gains `configure_menu` (absolute package_path, exact package_id), `menu_frame_begin` (expected_generation, byte_count=3686400), and same-connection `menu_frame_commit` (generation, byte_count=3686400). A successful begin sends one staging FD with metadata; commit carries exactly that one FD and no paths/pixels. Final response reports displayed sequence and underflows. Existing operations remain one JSON-line request/response with no descriptors; receiving FDs on them is invalid. Limit metadata to the existing 65,535-byte request bound.

- [ ] Add socketpair/daemon integration tests for real SCM_RIGHTS delivery, split JSON/newlines, misplaced/multiple descriptors, MSG_CTRUNC, EOF between begin and commit, seal validation, substituted FD, five-second preparation timeout, concurrent busy, and game replacement before commit. Assert all received/transferred FDs close on every error, no late frame copies into a new core and preparation does not hold the lifecycle mutex.
- [ ] Run the new transport/integration tests and observe missing operations/descriptor handling failures.
- [ ] Implement bounded `recvmsg`/`sendmsg` framing with close-on-exec and RAII FD ownership. Keep ancillary bytes associated with their request phase; do not silently discard extra bytes/FDs after a newline. Detect disconnect/timeout and cancel uncommitted staging. If commit has been accepted, retain immutable data and safe slot ownership through completion or verified containment regardless of response delivery.
- [ ] Implement the small diagnostic client using generated geometry and exact transport: acquire, paint alternating full-frame patterns with pixel/row/sequence markers, unmap/seal, commit, verify displayed completion and underflows. No library/catalog or raw memory access in the client. Make bounded frame count the default; sustained mode is explicit.
- [ ] Run protocol and daemon integration binaries, runtime tests, `make check-generated`, `make check`, and affected software tests using the existing dependency-equipped Python environment. Commit and obtain one whole-branch review before physical acceptance.

## Task 6: Exact-artifact kit acceptance and handoff

**Files:** Update dated FES validation evidence and runtime support matrix only after the exact paths exercised pass. No image-selection or physical-card operation is implicit.

**Interfaces:** Use the existing target agent lease and designated kit. Select exact menu package and runtime binary identities. Explicitly configure that package through the local daemon diagnostic protocol; retain the current splash fallback and release the lease at finish.

- [ ] Confirm availability and claim the existing kit lease. Verify boot-latched DDR evidence, actual reserved Linux range, target kernel noncached mapping semantics and ARM store ordering; reject the test if these cannot be established. Do not infer boot admission from historical mirrors or provision media without separate exact-device authorization.
- [ ] Test alternating markers, full RGB/stride/last-pixel boundaries and displayed-sequence acknowledgements with the diagnostic client. Measure latency and runtime CPU/memory usage. Require no underflows for ten minutes under normal kit load; sustained stalls are failure, not accepted black output.
- [ ] Exercise prepare/disconnect, accepted-commit/disconnect, late generation, menu→known accepted game→Stop→menu, repeated cycles and malformed frame recovery. Confirm fresh generation and fresh complete frame after Stop; use existing containment/reboot-required diagnostics for ambiguous failures.
- [ ] Stop the diagnostic client, restore the previously selected idle configuration and release the lease. Record source/package/RBF/runtime identities, boot identity, measurements, all failures and exact hardware classification.
- [ ] Commit/push the tested branch and hand off for a user-authorized PR. Next separate plan: tenfoot kit renderer and mesh-aware shell reuse, then FES image/boot selection. This plan does not ship a factory on-kit menu by itself.

## Self-review and execution handoff

The approved spec's pixel bounds, descriptor transport, firmware admission, lifecycle,
quiescence and feasibility gates map to Tasks 1–6. UI/library behavior and image/media
selection are explicitly later work. No fresh execution-method selection is required:
native execution is preserved. Review this plan before implementation, particularly
the fixed-layout GP configuration and immutable two-phase staging transfer.
