# SG-1000 Shared Audio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `fes.sg1000` produce SN76489 sound through the declared 48 kHz FES audio interface, with runtime admission and HDMI setup driven by verified simple-computer identity.

**Architecture:** Extend the existing `fes.simple-computer` capability table with audio bit 4, then teach the runtime to recognize that declaration and activate ADV7513 audio after identity. SG-1000 reuses Coleco's TI PSG and shared PCM-to-I2S block; SMS and ZX81 are separate follow-up changes.

**Tech Stack:** FES monorepo; mister-packages YAML/Go emitter; libmister-runtime C++14; misteross Verilog/SystemVerilog, Verilator, Python producer, Yosys/HIP nextpnr; FES parent checks.

**Spec:** `docs/superpowers/specs/2026-09-28-sg1000-shared-audio-design.md`

## Global Constraints

- `fes.audio.pcm-s16-stereo-48k` remains interface 1.0; `fes.simple-computer` remains ABI 1.0. Bit 4 is audio; bits 0–3 retain their meanings.
- An audio-emitting package requires the interface in its manifest. Silent packages omit it. Manifest and live GP identity must match exactly.
- PCM is signed 16-bit stereo at 48,000 frames/s, standard I2S in 32-bit slots; board pins are data T13, LRCLK T11, MCLK U11, SCLK T12 at 3.3 V.
- FPGA Hold/reset/invalid audio clock give silence. Runtime owns ADV7513 I2C and packet policy. No new GP opcode, network method, host audio stream, or factory-image entry.
- SG-1000 keeps its exact 16 KiB format-3 linked ROM and package-only standing. SMS and ZX81 behavior and manifests stay unchanged in this slice.
- Work only in the FES worktree. Seal only committed source with the SG-1000 locked OSS producer; Quartus remains an oracle. A kit claim requires a designated lease and exact-artifact evidence.

## Review Focus

1. A ROM-linked SG-1000 that accidentally advertises legacy blob bit 2 must fail identity; Task 3 tests its production GP mask of keyboard, video, audio only.
2. A manifest that declares audio but an FPGA that omits bit 4, or vice versa, must fail before HDMI audio setup; Task 2 tests both mismatches.
3. A silent simple-computer launched after an audio core must get video-only ADV7513 configuration; Task 2 tests the transition.
4. A held Z80 write strobe in the PSG port range must register once; Task 3 drives a real OUT sequence across multiple CPU enables.
5. An apparently routed RBF with wrong, missing, or constant I2S pads or a failing 12.288 MHz domain must not seal; Task 4 injects rejected producer evidence.

---

### Task 1: Add the simple-computer audio declaration

**Files:** Modify `sources/mister-packages/packages/abi/fes_simple_computer.yaml`, `sources/mister-packages/internal/pack/abi_test.go`, `sources/mister-packages/docs/` (the simple-computer contract section), and all generated consumers/oracles changed by `make generate` and `make fixtures`.

**Interfaces:** Produces `FesSimpleComputerInterfaceAudioPcmS16Stereo48k*` and `FesSimpleComputerCapabilityAudioPcmS16Stereo48k` in generated C++/Go, plus the matching Verilog capability mask. Task 2 consumes the C++ names; Task 3 consumes the Verilog macro.

- [ ] **Step 1: Write the failing ABI test.** In `abi_test.go`, assert audio ID/version 1.0, capability bit 4/mask `0x10`, ABI version still 1.0, and existing four masks unchanged; assert duplicate bit assignment is rejected by the existing validator.
- [ ] **Step 2: Prove failure.** Run `cd sources/mister-packages && go test ./internal/pack -run 'Test.*ABI' -count=1`; expect the new audio assertion to fail.
- [ ] **Step 3: Add the YAML interface and document its inherited I2S/Hold semantics.** Regenerate via root `make generate` and `cd sources/mister-packages && make fixtures`; include every changed runtime, FogCast, misteross, and oracle copy in this same commit.
- [ ] **Step 4: Verify.** Run `make check-generated`, `cd sources/mister-packages && make test && make check-fixtures`, then root `make check`; all must pass.
- [ ] **Step 5: Commit** the declaration, docs, tests, and generated files together.

### Task 2: Admit verified simple-computer audio in the runtime

**Files:** Modify `sources/libmister-runtime/src/native/core_package.cpp`, `fes_gp.cpp`, `hardware.cpp`, relevant `tests/unit/{core_package_test,fes_gp_test,native_hardware_test}.cpp`, protocol capability fixtures if their generator changes, `sources/libmister-runtime/docs/support-matrix.md`, and runtime audio documentation. Reuse `video.cpp` unless a test demonstrates a lifecycle gap.

**Interfaces:** Consumes Task 1 generated constants. `CorePackage` admits only required audio 1.0; `FesGpDriver` expects bit 4; `NativeHardware` lists the interface and passes `audio=true` to `BringUpCustom` only after successful identity. No new public method.

- [ ] **Step 1: Add failing focused tests.** Cover required audio 1.0 admission, optional or unsupported version rejection, expected capability `0x10`, absent/extra live bit mismatch, advertised capability, successful audio I2C setup after identity, silent-package transition to video-only, and no audio setup after failed identity.
- [ ] **Step 2: Prove failure.** Run the runtime's focused unit target (`cd sources/libmister-runtime && make test` if no narrower target is available); the new assertions must fail before code changes.
- [ ] **Step 3: Implement admission, expected/live mask, capability advertisement, and launch audio selection** using Task 1 constants. Preserve application/computer and silent simple-computer behavior; leave transmitter register recipes owned by the existing video path.
- [ ] **Step 4: Verify.** Run `cd sources/libmister-runtime && make all && make test` and the parent generated/fixture checks; all must pass.
- [ ] **Step 5: Commit** runtime behavior, tests, fixtures, and current support documentation.

### Task 3: Build SG-1000 sound in RTL

**Files:** Modify `sources/misteross/cores/fes-common/rtl/fes_computer_gp.v`, `sources/misteross/cores/fes-sg1000/rtl/{sg1000_machine.sv,top.v}`, `sources/misteross/cores/fes-sg1000/sim/machine_tb.cpp`, SG-1000 simulation Makefile dependencies, and add a narrow I2S/board testbench under `cores/fes-sg1000/sim/`. Reuse `cores/fes-common/rtl/{fes_sn76489.sv,fes_audio_pll.v,fes_audio_output.v,fes_audio_i2s.v}` unchanged unless tests expose a defect.

**Interfaces:** `fes_computer_gp #(ENABLE_MEDIA_BLOB=0, ENABLE_AUDIO=1)` in the ROM-linked top reports bits 0, 1, and 4; legacy diagnostic media builds keep the media bit as appropriate. `sg1000_machine` exposes a signed `[15:0]` PSG sample. The top duplicates that sample into both `fes_audio_output` channel inputs and drives four I2S pins.

- [ ] **Step 1: Add failing GP/machine/board simulations.** Assert default `ENABLE_AUDIO=0` preserves SMS/ZX81 masks; ROM-linked SG-1000 reports `0x13` (keyboard/video/audio, no blob); port `0x40`, `0x7f`, and an interior address write once per OUT; out-of-range/read/memory cycles do not; reset is muted; a deterministic tone and noise sequence changes PCM. Board simulation checks stereo equality, 48 kHz I2S framing and Hold/PLL-loss silence.
- [ ] **Step 2: Prove failure.** Run `cd sources/misteross && make sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-rom-link` plus the new board simulation target; new assertions must fail.
- [ ] **Step 3: Add `ENABLE_AUDIO=0` default and generated bit-4 mask to the GP module.** Set SG-1000 ROM-linked `ENABLE_MEDIA_BLOB=0`; enable audio in the SG-1000 top. Do not change SMS/ZX81 GP instances.
- [ ] **Step 4: Add SG-1000 PSG decode and clock enable.** Use `fes_sn76489` with a one-write-per-OUT strobe and a fractional enable averaging 3,579,545 chip pulses/s from 52 MHz; pass signed mono PCM to the top. Keep existing video, memory, and joystick decode intact.
- [ ] **Step 5: Connect shared audio PLL/output and validate.** Run the SG-1000 default/OSS/ROM-link and board simulations, then `make sim-fes-coleco-audio sim-fes-sms`; all must pass, including mute and frame-rate assertions.
- [ ] **Step 6: Commit** the RTL, simulation, and Makefile changes.

### Task 4: Seal the sound-capable SG-1000 package

**Files:** Modify `sources/misteross/cores/fes-sg1000/{constraints-oss.qsf,constraints.qsf,clocks-oss.sdc,clocks.sdc,README.md}`, `sources/misteross/scripts/{build_fes_sg1000_oss.py,build_fes_sg1000.py}`, `sources/misteross/tests/test_build_fes_sg1000.py`, and FES `docs/core-status.md`/`docs/core-development.md` where the recipe or status is described. Update shared source input lists, not the factory profile.

**Interfaces:** Both recipes include PSG, PLL, CDC/serializer RTL and I2S pins. OSS manifest is `fes.sg1000` 1.2.0, format 3, with required audio 1.0 and unchanged exact 16 KiB `cartridge-rom`. OSS evidence requires three PLLs, actual audio pads and three timing domains (52, 74.25, 12.288 MHz).

- [ ] **Step 1: Add failing producer tests.** Check source hashes/input closure, both recipes' pin assignments, `fes.sg1000` version/interface/ROM invariants, rejected missing/constant/wrong I2S outputs, missing audio PLL and clock row, and failing audio fmax.
- [ ] **Step 2: Prove failure.** Run `cd sources/misteross && python3 -m unittest tests.test_build_fes_sg1000`; the new assertions must fail.
- [ ] **Step 3: Update constraints and producers.** Follow the Coleco shared-audio evidence pattern, adjusting only SG-1000-specific source/resource expectations; do not relax existing ROM-map, GPU-backend, or system/pixel checks.
- [ ] **Step 4: Verify software.** Run producer tests, relevant SG-1000 simulations, parent `make check`, and affected `make test-changed TEST_CHANGED_ARGS='--base origin/main'`; all must pass before sealing.
- [ ] **Step 5: Commit** all source, recipe, tests, and current documentation; confirm clean worktree.
- [ ] **Step 6: Seal the exact commit.** Run the locked `make build-fes-sg1000` producer with the appropriate absolute cache; inspect authenticated HIP route, RBF/package identity, audio pads, ROM map, and three passing timing domains. Run `make build-fes-sg1000-quartus` as a separate oracle check. If OSS fit/timing fails, record the measured gap and keep the package unaccepted.

### Task 5: Integrate and classify the result

**Files:** Modify FES status/validation documentation only if evidence from Task 4 or a kit session warrants it; no default image profile change.

**Interfaces:** Produces a reviewable FES branch and, if sealed, a `fes.sg1000` package-selection/evidence set tied to the exact FES commit. Hardware evidence requires an operator lease and records kit/image/RBF/ROM identities.

- [ ] **Step 1: Verify parent consistency and package-only preparation** with the sealed selection using `make check` and `make core-dev CORE_DEV_ARGS='prepare --core fes.sg1000 --output out/core-dev/sg1000-audio'`; distinguish software, compiler, and kit results.
- [ ] **Step 2: If the kit and audio observation path are available, claim the designated kit under `docs/kit-sharing.md`** and run the open diagnostic tone/noise ROM; check audible/captured sound, Hold/Stop mute, and silent-package transition. Record the exact artifact and observation. Without those conditions, report hardware audio pending.
- [ ] **Step 3: Review the final diff against the spec, run `git diff --check`, record tests/results and any remaining limits, and commit any final evidence docs.** Push/open a PR only once the result is concrete and reviewable; never merge without the required review/authorization.
