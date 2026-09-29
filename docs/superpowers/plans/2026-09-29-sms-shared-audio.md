# SMS Shared Audio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the ROM-linked `fes.sms` package declare and deliver its existing SN76489 output through the verified `fes.simple-computer` PCM audio contract.

**Architecture:** Replace SMS's pixel-clock phase-accumulator serializer and per-bit PCM crossing with the shared coherent `fes_audio_output` and two-output 52.224/12.288 MHz Coleco PLL. Enable live audio capability bit 4 and require `fes.audio.pcm-s16-stereo-48k` 1.0 in the package. Keep SMS PSG port decode, ROM map, video, and package-only standing; independently seal and qualify the changed RBF.

**Tech Stack:** Verilog/SystemVerilog, Verilator, Python producer tests, Yosys/nextpnr-mistral HIP, FES format-3 packages.

**Spec:** `docs/superpowers/specs/2026-09-28-sg1000-shared-audio-design.md` (staged SMS section).

## Global Constraints

- `fes.simple-computer` stays ABI 1.0; audio is capability bit 4 and `fes.audio.pcm-s16-stereo-48k` 1.0 is required.
- PCM is signed 16-bit stereo at 48,000 frames/s. Hold/reset/PLL loss must yield silence; the runtime owns ADV7513 I2C policy.
- Preserve the exact 32 KiB linked `cartridge-rom`, `fes.sms` identity, SN76489 ports `0x7e`/`0x7f`, and package-only parent registration.
- Seal with authenticated HIP/nextpnr on GPU 0. A Quartus oracle may compare timing but cannot replace the OSS seal.

## Review Focus

- ROM-linked live mask must be `0x13`, without blob or stream; the ROM-link mailbox simulation checks it.
- Default/Quartus stream mailbox must add audio without losing stream; its simulation checks `0x1f`.
- A missing audio timing row, unconnected/wrong-pad I2S output, or wrong shared PLL must fail the producer's evidence gate.
- Reset/Hold and clock loss must mute across audio frames; the shared output simulation checks it.
- A new seal must not inherit an old SMS kit result; the exact package, map, RBF, image and capture are recorded separately.

---

### Task 1: Replace SMS audio RTL and live identity

**Files:** `sources/misteross/cores/fes-sms/rtl/top.v`, `sources/misteross/Makefile`, SMS GP/audio simulations; remove `rtl/sms_hdmi_i2s.v` and its obsolete unit test.

**Interfaces:** SMS PSG still produces one signed 16-bit system-domain sample. Shared output consumes that sample for both channels and drives existing HDMI audio pins. `fes_computer_gp` enables audio in both ROM-linked and diagnostic builds.

- [ ] Write failing assertions for ROM-linked mask `0x13`, default stream mask `0x1b`, and SMS source/simulation selection of coherent shared output.
- [ ] Run focused tests to observe the expected failures.
- [ ] Wire the shared PLL/output and capability, remove the legacy serializer, and update all Verilator and build source lists.
- [ ] Run mailbox, shared-audio, PSG, machine and OSS simulation lanes; fix only failures caused by this change.

### Task 2: Seal admission and documentation

**Files:** `sources/misteross/scripts/build_fes_sms_oss.py`, `build_fes_sms.py`, `sources/misteross/tests/test_build_fes_sms.py`, SMS README, misteross architecture, FES `docs/core-status.md`.

**Interfaces:** ROM-linked package version 1.4.0 requires audio 1.0; the Quartus diagnostic manifest also declares audio. OSS seal requires the two-output PLL, routed I2S pins, and passing 52.224/74.25/12.288 MHz timing rows.

- [ ] Add producer tests that fail on missing required audio, invalid PLL/pad routing and missing/failing audio timing.
- [ ] Run the focused tests to observe failures.
- [ ] Implement producer gates, manifest/version changes, source lists and current-state docs.
- [ ] Run producer tests, `make check`, affected component checks and `git diff --check`.

### Task 3: Clean-source seal and hardware classification

**Files:** committed source from Tasks 1–2; dated FES validation note only if exact-artifact hardware evidence is obtained.

- [ ] Commit tested source, then run the authenticated SMS OSS producer from that clean commit on GPU 0.
- [ ] Confirm format-3 ROM-map authentication, four audio pads and all three routed timing rows; compare Quartus only if the documented oracle is available and useful.
- [ ] If kit 1 is free, launch the exact package with the open 32 KiB diagnostic through a leased private host; capture video/audio, Stop and silence. Record hashes and classification.
- [ ] Run final checks, push and open a PR for review. Do not merge it.
