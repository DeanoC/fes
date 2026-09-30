# ZX81 Shared Audio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the socketed `fes.zx81` package to the shared 48 kHz HDMI audio path while preserving ROM, tape, keyboard and registered expansion behavior; qualify a publishable Zon X cart separately.

**Architecture:** Keep the 52 MHz ZX81 system clock and 74.25 MHz pixel clock. Add the existing 12.288 MHz `fes_audio_pll` and coherent `fes_audio_output`, feeding the existing signed Zon X sample; remove the pixel-clock phase-accumulator serializer. Add audio capability bit 4 to ZX81's *local* GP mailbox, which must retain its `media_busy` live-tape guard. The standard OSS format-3 package is the product path; the legacy Quartus 1.0 oracle must still compile the changed top but is not a replacement product package.

**Tech Stack:** Verilog/SystemVerilog, Verilator, Python producer tests, pinned Yosys/Mistral/HIP nextpnr on GPU 0, FogCast library/kit diagnostic.

**Spec:** `docs/superpowers/specs/2026-09-28-sg1000-shared-audio-design.md`, especially the ZX81 migration, shared I2S contract and hardware-evidence sections.

## Global Constraints

- Keep `fes.simple-computer` ABI 1.0 and capability bits 0–3 unchanged; require `fes.audio.pcm-s16-stereo-48k` 1.0 only on the new audio-capable package.
- Preserve the 52 MHz CPU/ULA/mailbox, 74.25 MHz video, 8 KiB `machine-rom` linker map, `fes.media.blob` live tape path and registered `fes.expansion.zx81-bus` 1.0 socket.
- Preserve the local GP mailbox's `media_busy` rejection of mid-session tape mutation; do not replace it with the generic shared mailbox.
- Use signed 16-bit stereo samples, 48,000 frames/s, 3.072 MHz SCLK and 12.288 MHz MCLK. Hold/reset and lost audio lock must mute.
- Do not alter the reserved socket rectangle or infer cart acceptance from a shell-only route. Use GPU 0; coordinate any kit test through the target lease.
- A source seal and package-only hardware diagnostic do not update or accept the installed factory image. A later native-image update must select and qualify the new ZX81 package separately.
- Zon X remains diagnostic RTL, not a publishable library cart in this slice. No vacant-socket launch can prove audible Zon X output.

## Review Focus

- A package requiring audio but reporting no live capability bit must fail identity admission: test the ZX81 GP identity and exact manifest together in Task 1.
- HOLD during an active Zon X tone must produce zero output quickly and restart cleanly: exercise the shared output simulation in Task 1.
- Mid-session tape begin while `media_busy` is high must still reject without disturbing the active load: rerun the existing ZX81 mailbox/tape simulation in Task 1.
- New PLL/routing outside the fixed socket rectangle must not be mistaken for a legal cart edit: route the shell, then compose the validation cart and compare its frozen bytes in Task 2.
- A vacant socket or RAM-present cart must remain silent rather than output stale Zon X data: test sample gating in Task 1 and observe idle/Stop silence in Task 3.

---

### Task 1: ZX81 audio identity and coherent output

**Files:**
- Modify: `sources/misteross/cores/fes-zx81/rtl/top.v`, `rtl/fes_computer_gp.v`
- Delete: `sources/misteross/cores/fes-zx81/rtl/zx81_hdmi_i2s.v`
- Modify: `sources/misteross/Makefile` (ZX81 simulation source lists and shared output dependency)
- Test: `sources/misteross/cores/fes-zx81/sim/` GP/board cases and `sources/misteross/tests/test_build_fes_zx81_oss.py`

**Interfaces:** The local `fes_computer_gp` gains `parameter ENABLE_AUDIO = 0`; the product top sets it to 1. `fes_audio_output` receives the existing signed Zon X sample on both channels, `.source_clk(clk_sys)`, `.audio_clk(audio_clk)`, `.locked(audio_locked)` and `.hold(exec_reset)`. `HDMI_MCLK` is the direct `fes_audio_pll` output.

- [ ] Add failing GP/board tests for bit 4 when enabled, default-off compatibility, unchanged busy-tape rejection, vacant/RAM-present silence, and shared output Hold behavior.
- [ ] Run focused failing tests; record their observed failure before RTL changes.
- [ ] Add the audio PLL/output and GP parameter, keep the existing sample gating, and remove the old serializer/source references.
- [ ] Run `make sim-fes-zx81 sim-fes-zx81-expansion sim-fes-audio-output` in `sources/misteross`; keep the generated expansion definition and tape tests green.
- [ ] Commit the coherent RTL, simulation and current architecture explanation.

### Task 2: Product producer, signoff and registered socket

**Files:**
- Modify: `sources/misteross/scripts/build_fes_zx81_oss.py`, `sources/misteross/tests/test_build_fes_zx81_oss.py`
- Modify: `sources/misteross/scripts/build_zx81_bus_validation_cart.py`, `sources/misteross/tests/test_build_fes_zx81_expansion.py` (account for the frozen shell's added audio clock)
- Modify: `sources/misteross/scripts/build_fes_zx81.py`, its focused tests if the legacy oracle shares `top.v`; do not silently drop its compile path.
- Modify: `scripts/affected.py`, `tests/test_affected.py` (shared audio RTL now has a ZX81 consumer)
- Modify: `docs/core-status.md`, `docs/fes-zx81.md`, `sources/misteross/docs/architecture.md`

**Interfaces:** Bump the socketed OSS product from `fes.zx81` 1.2.0 to 1.3.0, declare required PCM audio and require the GP bit. Keep format 3, `machine-rom` source size 8192, optional ZX81 bus 1.0 and existing versioned cart/linker contract. The Quartus 1.0 oracle remains explicitly diagnostic.

- [ ] Add failing producer tests for audio source closure, required manifest interface, three shell timing rows, routed I2S pads, PLL output/lock evidence and unchanged ROM/socket metadata. Test how the validation-cart producer handles the added audio clock in a frozen shell.
- [ ] Update producer source pins, timing/route evidence and package metadata; update the Quartus oracle source list and timing check if that recipe continues to compile the shared top.
- [ ] Run focused producer, planner and `make check` tests; commit the source before sealing.
- [ ] Seal the exact product package with the pinned ZX81 toolchain on GPU 0; require signoff at 52, 74.25 and 12.288 MHz and inspect the routed audio pads and socket vacancy.
- [ ] Build and link the existing validation cart against that sealed shell using its current cart producer; require no non-CRC changes outside the reserved rectangle and passing timing for every clock reported by the cart route. If the no-pack route excludes the unchanged audio clock, document that limitation and rely on the shell's separate audio-domain signoff. Rerun expansion/ROM-link regression tests.

### Task 3: Exact package and silent-path diagnostic

**Files:**
- Add: `docs/validation/YYYY-MM-DD-zx81-shared-audio-hil.md` and bounded capture artifacts after a successful run.
- Update: `docs/core-status.md` with the exact-artifact classification.

**Interfaces:** Use the existing FogCast package library and named `machine-rom` selection. Zon X has diagnostic RTL but no publishable library asset; the previous diagnostic route used two CRAM bits outside the reserved socket. Never admit or launch those out-of-region bits as a product cart. Classify audible Zon X hardware acceptance as pending.

- [ ] Confirm kit 1 image/runtime supports audio-required `fes.simple-computer`; inspect lease and current host state without modifying either kit.
- [ ] Import the exact sealed shell and an allowed 8192-byte ROM into an isolated FogCast library with the expansion selection vacant; launch through the normal owned session and verify returned package, ROM and programmed RBF identities plus the active audio interface.
- [ ] Capture HDMI and a 48 kHz audio stream to verify the vacant socket stays silent, then Stop and verify silence. Restore host and release the kit lease. Record that no published Zon X cart was tested, and keep audible audio acceptance pending.
- [ ] Record artifact hashes, source/tool/image revisions, timing and exact hardware observations. If the vacant-socket diagnostic cannot establish silence, record the failure; do not claim audible ZX81 audio acceptance.
- [ ] Run `make check`, appropriate affected tests and `git diff --check`, commit evidence, push and open the PR. Leave factory-image rebuild/update as a separately verified integration step.
