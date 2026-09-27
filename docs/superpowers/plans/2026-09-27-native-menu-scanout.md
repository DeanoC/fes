# Native Menu Scanout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution, or superpowers:subagent-driven-development if the user selects delegation. Steps use checkbox syntax for tracking.

**Goal:** Prove a bounded, full-color DDR framebuffer reader and fixed HDMI scanout before adding runtime/UI presentation.

**Architecture:** A read-only Avalon master feeds a bounded pixel FIFO at the pixel clock. It consumes the other agent's shared HPS DDR interface and never changes its implementation. Two fixed reserved-window slots switch only after prior reads drain at a frame boundary.

**Tech Stack:** Verilog, Verilator/C++, Python producer tests, pinned Yosys/nextpnr/Mistral.

**Spec:** [Native HDMI menu display design](../specs/2026-09-27-native-menu-display-design.md).

## Global Constraints

- Display: 1280x720 at 720p60; pixel clock 74.25 MHz, total 1650x750.
- Pixels: little-endian XRGB8888 (B,G,R,unused); stride 5120; frame bytes 3,686,400.
- Two 4 MiB slots at offsets 0 and 0x00400000 within the shared core DDR window.
- No writes from the FPGA display reader; no addresses outside the selected frame.
- Reuse merged `fes_hps_ddr`/guards and `fes.memory.hps-ddr` constants. No independent SDR layout or bridge-release path.
- Other agent owns DDR RTL, reservation, boot layout, runtime admission and RAM testing.
- GPU 0 only. Quartus is an optional timing oracle, never product fallback.
- No kit programming, stop-idle replacement, kernel change or menu UI implementation in this slice.
- Hardware qualification and integration are later gates; an exported artifact is not acceptance.

## Review Focus

- Long DDR stalls: HDMI timing continues, missing pixels become black and underflow is reported (Task 2).
- Late read data after a switch/reset: old-frame pixels cannot enter a new frame (Tasks 1–2).
- Repeated switch requests: one pending request only; displayed sequence never acknowledges a buffer still in use (Tasks 1–2).
- Last burst crosses the frame end: every issued beat remains inside exactly 3,686,400 bytes (Task 1).
- Boot layout differs from live mirrors: no artifact or simulation result qualifies the boot dependency (Task 3 prerequisite).

## Task 1: Bounded framebuffer reader

**Files:** Create `sources/misteross/cores/fes-menu/rtl/fes_menu_reader.v`, `sources/misteross/cores/fes-menu/sim/reader_tb.cpp`, and `sources/misteross/scripts/sim_fes_menu.py`.

**Interfaces:** `fes_menu_reader` consumes clock/reset, enable, slot (1 bit), start, downstream ready and a read-only 128-bit Avalon response. It produces pixel_valid, pixel_bgrx[31:0], pixel_index[19:0], done, idle, address[27:0], burstcount[7:0], read. Connect waitrequest/readdatavalid/readdata to shared port 0; port addresses are byte addresses divided by 16. The selected slot is latched at start. A second start while busy is rejected by the caller.

- [x] Write reader simulation tests named `exact_frame`, `stalled_command`, `delayed_response`, `last_burst`, and `reset_drain`. A frame yields 921,600 ordered pixel words; accepted DDR beats total 230,400. Assert all burst counts are 1–128, every byte range is in the selected frame, commands remain stable under waitrequest, and disabling stops new reads while issued responses drain. In reset testing, returned old data is discarded before idle becomes true.
- [x] Run `python3 scripts/sim_fes_menu.py --case reader` from misteross; confirm the missing implementation/test assertions fail.
- [x] Implement the reader. Use a bounded FIFO with room reserved for every outstanding response, a maximum of one outstanding burst, and up to 128 beats per burst. Use shared window constants, not duplicate physical base literals. No asynchronous framebuffer read or arbitrary host address port.
- [x] Run the reader cases with deterministic varied waitrequest/read latency and consumer backpressure. Expected: all assertions pass and both slots produce their distinct patterns.
- [x] Commit reader, harness and script together with `cores/fes-menu/README.md` documenting simulation-only status.

## Task 2: Timing, frame switching and underflow behavior

**Files:** Create `sources/misteross/cores/fes-menu/rtl/fes_menu_video.v`, `sources/misteross/cores/fes-menu/sim/video_tb.cpp`; modify the simulation script and README.

**Interfaces:** `fes_menu_video` consumes pixel clock/reset, enable, quiesce, submit_valid, submit_slot and submit_sequence[31:0]. It produces submit_ready, displayed_sequence[31:0], quiesced, underflows[31:0], RGB[23:0], DE/HS/VS, and the reader's port-0 signals. `submit_ready` is false while one request is pending; no request overwrites the pending slot/sequence. `quiesced` requires no pending reads and no future command emission.

- [x] Write `video_timing`, `frame_switch`, `late_response`, `underflow`, `busy_submit`, and `quiesce` cases. Assert positive HS/VS, 1650x750 totals, 1280x720 active pixels; decode known B,G,R byte markers correctly. Assert each complete frame contains only one slot pattern, and displayed_sequence changes only at a frame boundary after old reads drain. Under forced stalls, sync timing remains unchanged, missing pixels are black and the counter increases. Counter saturates at UINT32_MAX. Quiesce leaves no accepted commands outstanding.
- [x] Run `python3 scripts/sim_fes_menu.py --case video`; observe failing tests before adding the video implementation.
- [x] Implement fixed timing and frame-local pixel indexing. Tag prefetched pixels by frame/index; discard missed/stale pixels instead of shifting the rest of a scan line. Preload before enabling visible output. If the next slot cannot be safely adopted, retain the current slot and defer its displayed acknowledgment to a later frame boundary. Do not stretch timing to wait for DDR.
- [x] Run reader and video cases including enable/reset during delayed responses and quiesce during a pending switch. Export marker/color-bar PPM frames as inspection artifacts. Expected: assertions pass; no underflow in the unstalled case, deliberate underflow only in the stalled case.
- [x] Commit video behavior and its tests with updated README. Document which simulated stall envelopes were tested; do not extrapolate to real DDR bandwidth.

## Task 3: DDR dependency adoption and diagnostic synthesis

**Prerequisite:** The DDR owner has merged its final shared interface/guard and runtime admission, supplied passing RAM/boot evidence, and released the kit before any subsequent physical work. Inspect the merged changes and final shared port convention; if incompatible with this plan, reconcile the design before integration. Do not cherry-pick the other agent's uncommitted files or recreate its support.

**Files:** Create `sources/misteross/cores/fes-menu/rtl/top.v`, `sources/misteross/cores/fes-menu/sim/board_tb.cpp`, `sources/misteross/scripts/build_fes_menu.py`, `sources/misteross/tests/test_fes_menu.py`; extend the menu simulation script and README and `sources/misteross/docs/architecture.md`. Reuse selected splash pixel PLL and established board pin/electrical evidence; do not alter splash RTL.

**Interfaces:** Top connects `fes_menu_video` to `fes_hps_ddr` port 0 at the pixel clock and uses its p0_reset. Tie unused writes and ports inactive according to the dependency. Diagnostic top scans deterministic slot 0 only after an explicit simulation-controlled enable; synthesis diagnostic enable is fixed and named in its manifest. No GP presentation contract is claimed in this slice. The resulting RBF remains contained-diagnostic-only until a later described-menu/runtime plan adds identity and admission.

- [ ] Write producer tests proving the final shared DDR source and generated constants are selected, correct layout evidence is mandatory, alternate layout is rejected, and no unrelated splash/recipe/artifact policy changes occur. Board simulation must check initial reset/PLL lock and disabled DDR commands before enable.
- [ ] Run `python3 -m unittest tests.test_fes_menu`; confirm missing producer behavior fails.
- [ ] Implement a producer using existing common compiler/electrical/provenance helpers, with distinct `build/oss/fes-menu/` outputs. Synthesis and routing diagnostics include DDR layout and clock/resource evidence. Preserve the normal authenticated execution/source audit path; do not mutate another producer's globals or invent a sealing shortcut. An exported diagnostic has no play-package claim.
- [ ] Run board simulation and focused producer tests. Run synthesis, then GPU-0 routing only once the simulation and source selection are clean and committed. Require 74.25 MHz pixel timing and the same DDR layout evidence as the dependency. If nextpnr fails, use Quartus only to characterize the gap and report it separately; do not switch output policy to Quartus.
- [ ] Record source commit, tool pins, artifact hashes, simulation assertions and timing result in the existing validation convention. Commit producer/documentation. No physical programming follows automatically.

## Handoff and subsequent plans

Run `git diff --check`, generated-consumer consistency if shared consumers changed, and `python3 scripts/test_changed.py --base origin/main` at FES root. State base/result commits, dependency version, clock closure, and simulation-only classification. Request whole-branch review before integration.

The next plan must add the shared GP menu capability and protocol, a described
menu package, runtime reserved-window presenter, generation/SCM_RIGHTS handling,
and an exact-artifact scanout kit test under the existing lease. Only after that
proof should the UI-shell plan reuse tenfoot and mesh source/executor selection.
Image/boot policy comes last. These requirements are deferred explicitly rather
than hidden inside this simulation slice; this plan does not complete the full
approved design by itself.

## Execution record

Tasks 1–2 are implemented and independently reviewed at `74e025e3`.
Review found and fixed a prefetch-edge submission race and failure to discard
stale pixels during horizontal blanking. Regressions now cover both, plus
held-command cancellation and request retention across failed prefetch.
Task 3 remains gated on the other agent's merged, qualified DDR support.
No board build, kit operation, runtime contract or image selection is claimed.
