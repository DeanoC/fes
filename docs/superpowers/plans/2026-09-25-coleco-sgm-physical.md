# Coleco SGM Physical Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a separately linked Opcode SGM module for a version-2 Coleco socket, with shell-owned 32 KiB RAM, module-owned mapping and AY sound, exact host/runtime admission, and gated hardware acceptance.

**Architecture:** Keep the existing version-1 shell and diagnostic asset immutable. A distinct development shell supplies registered RAM and a 28-bit response edge; the independently routed module controls RAM claims and AY audio. Existing Go composition, FogCast selection, and runtime verification gain a closed version-2 Coleco policy. Factory selection changes only after timing and exact-artifact hardware acceptance.

**Tech Stack:** SystemVerilog, Verilator/C++ simulation, Python 3 build recipes, Yosys/nextpnr-mistral and Quartus 17.0.2 timing oracle, Go linker/FogCast, C++ libmister-runtime.

**Spec:** `docs/superpowers/specs/2026-09-25-coleco-sgm-physical-design.md`

## Global Constraints

- The current version-1 `fes.expansion.coleco-bus` shell, diagnostic archive, and factory `fes.coleco` producer stay valid and unchanged until version-2 acceptance.
- Version-2 request is 31 bits in the version-1 order. Response is 28 bits: direct data `[7:0]`, direct claim `8`, WAIT `9`, INT `10`, shell-RAM claim `11`, signed PCM `[27:12]`.
- The shell lends a 32,768×8 registered RAM below `$8000`; only a module claim makes it CPU-visible. Module reset disables both windows without clearing RAM.
- `$53` bit 0 enables `$2000–$7FFF`; `$7F` bit 1 clear enables `$0000–$1FFF`; `$50/$51/$52` are AY address/write/read.
- Use the 52.224 MHz system clock with fractional AY enable for 7.15909 MHz ÷ 4. Mix AY and SN with saturation before the existing 48 kHz I2S path; a zero AY sample preserves existing bytes.
- No Python or FPGA compiler runs on the kit. No new GP mailbox operation or shared `mister-packages` definition is needed.
- The timing gate is final structured Fmax ≥52.224 MHz system, ≥74.25 MHz pixel, and ≥12.288 MHz audio. `--timing-allow-fail` is diagnostic only.
- Another agent owns routing issue [#184](https://github.com/DeanoC/fes/issues/184). This plan consumes its findings; it does not assign nextpnr optimization work or alter that issue.
- The old RAM-probe timing gap was a stale-router-pin artifact: nextpnr `5dea3ecd` closes that fixture. The v2-only socket now reserves `24 1 28 19` and CRAM `(1769,32,2806,1800)` following [#203](https://github.com/DeanoC/fes/issues/203). With pin `f7370550`, an unsealed shell at seed 3 / weight 2000 and full SGM cart at seed 3 / weight 300 both route and meet timing; the cart's 37,781 CRAM changes remain inside the v2 region. The v1 rectangle and archive policy remain unchanged. Sealed package and kit acceptance remain pending.
- Work in the existing isolated FES worktree. Preserve the uncommitted SGM memory simulation diff and unrelated changes. Do not commit, push, open a PR, or program hardware without separate authorization; report an uncommitted diff.

## Review Focus

- A selected v1 diagnostic against a v2 shell must fail before any CRAM composition; Task 6 pins this.
- A RAM claim on cartridge, console I/O, or `$8000–$FFFF` must never steal a console access; Task 2 pins this.
- Two adjacent Z80 writes separated by WAIT must each commit once, without a mirrored console-RAM write; Task 2 pins this.
- AY register reads while sound counters advance must return register state, not transient PCM; Task 3 pins this.
- Changed shell RAM bits or an extra out-of-socket response patch must be rejected even if the resulting RBF parses; Tasks 5 and 6 pin this.

---

### Task 1: Freeze a separate v2 response edge

**Files:** Create `sources/misteross/cores/fes-coleco/rtl/coleco_bus_v2_pack.vh`, `sources/misteross/cores/fes-coleco/rtl/coleco_expansion_socket_v2.v`, and `sources/misteross/cores/fes-coleco/sim/sgm_socket_tb.cpp`; modify `sources/misteross/Makefile` and `sources/misteross/cores/fes-coleco/README.md`.

**Interfaces:** `coleco_expansion_socket_v2(clock, request[30:0], response[27:0], plug_request[30:0], plug_response[27:0])`. Define `COLECO_V2_BUS_REQ=31`, `COLECO_V2_BUS_RSP=28`, `COLECO_V2_BUS_RAM_CLAIM=11`, `COLECO_V2_BUS_PCM=27:12`. The v1 header/module remain untouched.

- [ ] Write a Verilator test that drives request bit 30 and response bits 0, 8, 9, 10, 11, 12, and 27 individually. Assert exactly one request register and one response register of latency and an all-zero vacant response.
- [ ] Add a focused `sim-fes-coleco-sgm-socket` Makefile target; run it and confirm failure because the v2 module/header do not exist.
- [ ] Implement v2 packed constants and registered socket. Pin the 31 request FFs to the existing v1 BELs and pin 28 response FFs to explicit, unique BELs inside X24–28 Y1–19; prove each candidate BEL exists with the authenticated device database before freezing the list. Keep synth/Verilator behavior aligned. For example, bit 11 must connect `plug_response[11]` to `response[11]` through one FF. The pinned FFs remain in X24/Y1–3; later rectangle growth follows the separately reviewed #203 placement evidence.
- [ ] Run `make -C sources/misteross sim-fes-coleco-sgm-socket` and existing `sim-fes-coleco-oss`; both must pass. Inspect the new BEL list for overlaps with v1 request FFs before proceeding.

### Task 2: Implement the dormant shell RAM and CPU arbitration

**Files:** Create `sources/misteross/cores/fes-coleco/rtl/coleco_expansion_ram.v` and `sources/misteross/cores/fes-coleco/sim/sgm_shell_ram_tb.cpp`; modify `sources/misteross/cores/fes-coleco/rtl/coleco_machine.sv`, `sources/misteross/cores/fes-coleco/rtl/top.v`, `sources/misteross/Makefile`. Adapt the existing uncommitted `sources/misteross/cores/fes-coleco/sim/sgm_machine_tb.cpp` and `expansions/sgm_memory.v` only as needed; preserve their behavioral assertions.

**Interfaces:** `coleco_expansion_ram(clk, address[14:0], write_data[7:0], write_enable, read_data[7:0])` has a synchronous read. `coleco_machine` consumes the registered v2 response; only `response[11] && !MREQ_N && address<16'h8000` selects RAM data or RAM write. The module remains the sole owner of enable-state decode.

- [ ] Extend CPU-through-socket tests for `$0000/$1FFF/$2000/$5FFF/$6000/$7FFF/$8000`, reset, BIOS overlay, console-RAM preservation, adjacent reads/writes, and WAIT-stretched cycles. Assert no RAM write when claim is zero or address is outside lower 32 KiB.
- [ ] Run focused SGM machine/RAM simulation and confirm at least one RAM-claim or write-phase assertion fails with the present software-only model.
- [ ] Add the physical registered RAM and wire the v2 response through `top.v`. At the CPU write sampling phase, generate one pulse for a claimed write; suppress the console-RAM write on that same cycle. Keep reads addressed continuously, select data only after both socket response and RAM output settle, and retain reset contents as unspecified rather than clearing the M10K.
- [ ] Run `make -C sources/misteross sim-fes-coleco-oss` and the focused SGM target. Confirm the vacant response reproduces existing Coleco CPU behavior and all overlay tests pass.

### Task 3: Build an independently testable AY sound block

**Files:** Create `sources/misteross/cores/fes-coleco/expansions/sgm_ay.v` and `sources/misteross/cores/fes-coleco/sim/sgm_ay_tb.cpp`; modify `sources/misteross/Makefile`.

**Interfaces:** `sgm_ay(clk, reset, address_write, data_write, data_read, write_data[7:0], read_data[7:0], sample_signed[15:0])`. It implements AY registers 0–15, three tone channels, noise, envelope, mixer and defined reset behavior. Its clock enable is a fractional 1,789,772.5 Hz average derived from 52,224,000 Hz without a second clock domain.

- [ ] Write register readback tests for periods, mixer, amplitude and envelope; drive 52.224 MHz cycles to verify tone/noise/envelope transitions, reset phase, and bounded signed PCM. Include simultaneous register read and advancing sound counters.
- [ ] Run `make -C sources/misteross sim-fes-coleco-sgm-ay`; expect compile failure before the AY block exists.
- [ ] Implement the AY register file and counters using the cited AY manual's tone/noise/envelope rules. Use the existing fractional-enable idiom in `coleco_machine.sv`; expose a registered PCM sample in the system-clock domain.
- [ ] Rerun the focused target and `sim-fes-coleco-oss`. Reject a phase/frequency mismatch rather than changing the test to the implementation.

### Task 4: Connect module control, AY I/O and saturated audio

**Files:** Modify `sources/misteross/cores/fes-coleco/expansions/sgm_memory.v`, `sources/misteross/cores/fes-coleco/rtl/coleco_machine.sv`, `sources/misteross/cores/fes-coleco/rtl/top.v`, `sources/misteross/cores/fes-coleco/sim/sgm_memory_tb.cpp`, and `sources/misteross/cores/fes-coleco/sim/audio_output_tb.cpp`; create `sources/misteross/cores/fes-coleco/expansions/sgm.v` and `sources/misteross/cores/fes-coleco/sim/sgm_audio_tb.cpp`.

**Interfaces:** `sgm` consumes v2 `plug_request[30:0]` and drives `plug_response[27:0]`; it publishes RAM claim only for enabled lower-memory cycles, direct claim/data for `$52`, and PCM in `[27:12]`. The shell's audio mix is a saturating signed 16-bit sum of existing SN and module PCM.

- [ ] Extend SGM tests for `$53` and `$7F` write transitions, `$50/$51/$52` I/O, unchanged cartridge/VDP/controller reads, and reset-to-disabled claims. Add audio vectors for zero PCM byte-for-byte equivalence, positive and negative saturation, and an audible AY-plus-SN interval at the I2S output.
- [ ] Run focused SGM control/audio tests; observe failures for AY I/O and mixing before wiring them.
- [ ] Refactor `sgm_memory.v` into control/claim logic with no embedded 32 KiB array. Instantiate `sgm_ay` in `sgm.v`; pack the v2 response exactly as Task 1 defines. In shell audio, sign-extend both inputs, add in 17 bits, and clamp to `16'sh7fff`/`-16'sh8000` before `fes_audio_output`.
- [ ] Run all focused SGM tests plus `make -C sources/misteross sim-fes-coleco-oss`; compare the vacant-shell audio fixture with its pre-change bytes.

### Task 5: Produce a sealed v2 shell and independent SGM archive

**Files:** Create `sources/misteross/scripts/build_fes_coleco_socket_v2_dev.py`, `sources/misteross/scripts/build_coleco_sgm.py`, and `sources/misteross/tests/test_coleco_sgm_build.py`; modify `sources/misteross/scripts/coleco_expansion.py` only for versioned geometry/BEL validation, `sources/misteross/Makefile`, and `sources/misteross/cores/fes-coleco/README.md`.

**Interfaces:** Shell package advertises optional `fes.expansion.coleco-bus` 2.0 and map `fes.coleco-bus.socket/2`; SGM archive contains canonical `manifest.json` and `cart.rbf`, bound to exact shell package ID, BUILD_ID, RBF digest, device and map. Version-1 producer scripts and outputs are unchanged. The v2 map's measured half-open CRAM rectangle is `(1769,32,2806,1800)`.

- [ ] Test that shell build input closure includes the new RAM, v2 socket and audio RTL; that 59 request/response boundary FFs are correctly typed and placed; that the socket is vacant; and that an archive with a changed shell RAM bit, changed outside-rectangle bit, or unlisted response-boundary bit is rejected.
- [ ] Run `python3 -m unittest discover -s sources/misteross/tests -p 'test_coleco_sgm_build.py' -v`; expect failures for missing v2 recipes.
- [ ] Derive the shell recipe from `build_fes_coleco_socket_dev.py` and the independent module recipe from `build_coleco_bus_diagnostic.py`. Authenticate the locked toolchain and exact shell, route only the SGM module against the frozen shell, verify CRAM containment and the explicit v2 response-boundary exception, and seal deterministic outputs only when all timing thresholds pass.
- [ ] Run the focused Python tests. Then run the v2 shell and module recipes with their `--help` documented arguments; record full route reports only after the source is stable. Compare the shell and module routes with the separate #184 findings and run Quartus 17.0.2 as a timing oracle. A route below threshold remains an unsealed diagnostic, with no module or package admission.

### Task 6: Admit only the exact v2 pair in the non-Python linker

**Files:** Modify `sources/misteross/expansion/asset.go`, `sources/misteross/expansion/rbf.go`, `sources/misteross/expansion/coleco_test.go`, and `sources/misteross/expansion/asset_test.go`.

**Interfaces:** Add `ColecoMapV2 = "fes.coleco-bus.socket/2"`. `Manifest.validate`, `policyFor`, `Admit(shell Shell, asset Asset) error`, and `Compose(shell Shell, asset Asset) (Composition, []byte, error)` accept `(ColecoSlot, ColecoMapV2, 2, 0)` only against a shell declaring the same optional interface and exact identities; retain the existing v1 and ZX81 policies. Preserve the archive's existing format-1 canonical JSON and expansion-ID hash unless an actual incompatibility is demonstrated by a failing fixture.

- [ ] Add Go tests for a valid v2 SGM link and rejection of v1/v2 cross-pairs, wrong shell digest, changed shell RAM CRAM, unlisted response-boundary bits, out-of-socket CRAM and altered archive bytes. Add a golden builder-vs-Go linked RBF SHA-256 comparison once Task 5 has a sealed artifact.
- [ ] Run `go test ./...` from `sources/misteross/expansion`; confirm the new v2-valid case fails before admission changes while all v1 tests still pass.
- [ ] Extend the closed slot/map/version table and response-boundary contract in Go. Keep the linker bounded and deterministic; do not shell out to Python or a compiler.
- [ ] Rerun `go test ./...` and the builder-vs-Go byte comparison. A valid archive must produce the exact independently recorded linked RBF.

### Task 7: Carry v2 identity through host and target admission

**Files:** Modify `sources/FogCast/internal/misterruntime/protocol_v2.go`, `sources/FogCast/internal/misterruntime/composition_test.go`, `sources/libmister-runtime/src/native/core_composition.cpp`, `sources/libmister-runtime/tests/unit/core_composition_test.cpp`; modify other FogCast library/staging tests only where their existing v1 fixture needs a distinct v2 case.

**Interfaces:** Existing FogCast expansion selection and `expansion.Composition` wire fields remain unchanged. The host accepts an explicitly selected SGM asset only with the v2 Coleco package; target admission independently checks `slot="fes.expansion.coleco-bus"`, `map="fes.coleco-bus.socket/2"`, `slot_major=2`, `slot_minor=0`, shell IDs and recomposed bytes. Restart adoption preserves the same selected expansion digest.

- [ ] Add host and runtime fixtures for valid v2 selection, wrong-bus or v1-asset rejection, changed relink payload, stale selection after package change, restart/relaunch, and clearing a selection. Reuse the v1 fixtures to prove no change in existing behavior.
- [ ] Run `go test ./internal/misterruntime/...` from `sources/FogCast` and `make build/tests/unit/core_composition_test` then `build/tests/unit/core_composition_test` from `sources/libmister-runtime`; observe the v2-valid fixture fail before implementation.
- [ ] Add the v2 branch to host package-interface checks and the runtime's closed map/version admission. Do not add a GP mailbox operation or infer selection from the title, path or raw RBF.
- [ ] Rerun focused host/runtime tests, then `python3 scripts/test_changed.py --base origin/main` and `make check-generated` from FES. Recheck exact selected digest through stage, relink and programmed identity.

### Task 8: Gate package registration and exact-artifact kit acceptance

**Files:** Modify `config/core-recipes.toml` only after acceptance; add a dated record under `docs/validation/` after an actual kit run. Update `sources/misteross/docs/architecture.md` and `sources/misteross/cores/fes-coleco/README.md` when the build lane changes. No factory recipe edit is part of the host-only implementation.

**Interfaces:** A candidate v2 package is identified by its sealed package ID, shell RBF SHA-256, SGM archive ID and linked RBF SHA-256. FES selection is allowed only after all three final timing thresholds, CRAM policy, byte-for-byte linker match and exact-artifact kit observations pass.

- [ ] Run final Verilator, Python producer, Go linker, FogCast and runtime checks; then `make check-generated` and `git diff --check`. Record the exact source tree and artifact digests used.
- [ ] Obtain a sealed route with final Fmax at or above 52.224/74.25/12.288 MHz and a matching Go-linked RBF. Use Quartus oracle results to identify any remaining Coleco VDP-to-framebuffer critical path; keep routing optimization in #184 with its owner.
- [ ] Only with the designated kit lease and exact device authorization, load the sealed vacant shell and then the linked SGM artifact. Run a diagnostic showing both RAM windows, preserved console RAM, AY plus SN audio, Stop and relaunch; record programmed identity. Hardware work is blocked until that lease and authorization exist.
- [ ] After exact-artifact acceptance, update the FES package selection and current architecture text, run `make check` and incremental `make dev`, and hand off scope, base/result commits or uncommitted diff, all test results, hardware classification, shared-contract effects and next integration step. Factory membership remains an explicit review decision.
