# FES ColecoVision Bring-up Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a reduced 16 KiB ColecoVision FPGA bring-up with Verilator, Quartus, and OSS nextpnr/Mistral lanes in an isolated `misteross` worktree.

**Architecture:** Add a self-contained `cores/fes-coleco` package around the existing Verilog TV80 CPU and `fes.simple-computer` mailbox. The machine implements the ColecoVision reset/cartridge/RAM map, a synchronous 16 KiB VRAM Graphics I tile/VBlank subset, active-low controller rows, and a fixed 720p shell. Build scripts mirror FES ZX81, with explicit resource/evidence checks and a tracked workaround record.

**Tech Stack:** SystemVerilog/Verilog RTL, Verilator C++ simulations, Python 3 build recipes, Yosys `synth_intel_alm`, nextpnr-mistral/Mistral, Quartus Prime Lite 17.0.2, DE10-Nano Cyclone V.

**Spec:** `docs/superpowers/specs/2026-09-12-fes-coleco-bringup-design.md`

## Global Constraints

- The implementation stays in `misteross`; do not modify FogCast, libmister-runtime, or mister-packages.
- Reuse `fes.simple-computer` 1.0 for reset, active-low rows, 720p, and one bounded media blob; do not invent a new public ABI.
- The first slice accepts a raw 1–16 KiB cartridge blob, mirrors it over `0x8000–0xffff`, and uses an open 8 KiB reset shim that jumps to `0x8000`.
- The machine map is `0x0000–0x1fff` reset ROM, `0x6000–0x7fff` mirrored 1 KiB RAM, `0x8000–0xffff` mirrored cartridge, I/O `0xbe`/`0xbf` VDP, and I/O `0xfc`/`0xff` controllers.
- The OSS lane uses only Verilog/SystemVerilog TV80 sources, `synth_intel_alm -nolutram -nodsp`, the pinned Mistral/nextpnr tools, and no Quartus executable.
- The OSS mailbox-to-machine media bridge accounts for the registered RAM read result with an explicit prefetch/final-byte flush; media reloads re-arm on `media_ready` falling or reset rising.
- Keep Quartus tri-state I²C and OSS `MISTRAL_IO` I²C forms conditional, with the HPS I²C BEL at `cyclonev_hps_interface_peripheral_i2c.52.60.0`.
- Infer synchronous memories as M10Ks where possible; do not add asynchronous or mixed-width RAM shapes.
- Start nextpnr with device `5CSEBA6U23I7`, 74.25 MHz pixel constraint, seed 7, router1, and `--tmg-ripup`.
- Do not program hardware, commit, push, or open a PR without explicit authorization; leave the component branch as an uncommitted reviewable diff.
- Tests must be written and observed failing before the corresponding production RTL or recipe implementation.

---

## File map

Create the following component files:

- `cores/fes-coleco/README.md` — present first-slice boundary, controller map, media format, and build/simulation commands.
- `cores/fes-coleco/generated/fes_simple_computer.vh` — copied generated ABI constants used by the mailbox and top-level RTL.
- `cores/fes-coleco/constraints.qsf` — board pins and Quartus assignments for the DE10-Nano target.
- `cores/fes-coleco/constraints-oss.qsf` — OSS-compatible pin/resource constraints.
- `cores/fes-coleco/clocks.sdc` — Quartus timing constraint for the 50 MHz reference.
- `cores/fes-coleco/clocks-oss.sdc` — nextpnr-compatible timing constraint subset.
- `cores/fes-coleco/rtl/sys_pll.v` — 50→52 MHz system PLL wrapper.
- `cores/fes-coleco/rtl/pixel_pll.v` — 50→74.25 MHz pixel PLL wrapper.
- `cores/fes-coleco/rtl/fes_computer_gp.v` — self-contained copy of the proven simple-computer mailbox, constrained to the existing ABI.
- `cores/fes-coleco/rtl/t80pa.v` and `cores/fes-coleco/rtl/tv80/*.v` — Verilog-only TV80 path copied from FES ZX81.
- `cores/fes-coleco/rtl/coleco_reset_rom.hex` — open 8 KiB reset shim image with a jump to `0x8000`.
- `cores/fes-coleco/rtl/coleco_reset_rom.mif` — Quartus range-form copy of the reset shim for `altsyncram` initialization.
- `cores/fes-coleco/rtl/coleco_machine.sv` — CPU enable, memory map, cartridge loader, controller ports, and VDP integration.
- `cores/fes-coleco/rtl/coleco_vdp.sv` — synchronous 16 KiB VRAM, VDP ports/registers, Graphics I tile raster, and bounded VBlank status behavior.
- `cores/fes-coleco/rtl/coleco_video_720p.v` — logical-raster capture, 2× scaling, fixed 720p timing, and palette.
- `cores/fes-coleco/rtl/top.v` — DE10-Nano shell and module wiring.

Create simulation files:

- `cores/fes-coleco/sim/gp_tb.cpp` — mailbox identity, reset, row, media, and error exchange checks.
- `cores/fes-coleco/sim/machine_tb.cpp` — synthetic cartridge boot, memory mirrors, controller ports, VDP writes, and status checks.
- `cores/fes-coleco/sim/vdp_tb.cpp` — direct VDP tile/VBlank-status behavior.
- `cores/fes-coleco/sim/video_tb.cpp` — logical capture and 720p timing checks.
- `cores/fes-coleco/sim/board_models.v` — deterministic HPS GP/I²C models for top-level simulation.
- `cores/fes-coleco/sim/board_tb.cpp` — top-level reset, HDMI clock/video, and mailbox connectivity checks.

Create build/test support:

- `scripts/build_fes_coleco.py` — authenticated Quartus project generation, compile, evidence, manifest, and package export.
- `scripts/build_fes_coleco_oss.py` — authenticated OSS synthesis/place/route, evidence, manifest, and package export.
- `tests/test_build_fes_coleco.py` — recipe-level tests for source lists, commands, resources, and manifest fields.

Modify:

- `Makefile` — help, `.PHONY`, simulation targets, and Quartus/OSS build targets.
- `README.md` — current working target list and commands.
- `docs/architecture.md` — canonical present-tense FES ColecoVision architecture and measured workaround notes.

## Task 1: Establish the failing simulation contract

**Files:** Create `cores/fes-coleco/sim/vdp_tb.cpp`, `cores/fes-coleco/sim/machine_tb.cpp`, `cores/fes-coleco/sim/gp_tb.cpp`, `cores/fes-coleco/sim/video_tb.cpp`; modify `Makefile` only to add the target after tests are red.

**Interfaces:** The tests define the required top modules and public ports:

- `fes_computer_gp` follows the FES ZX81 mailbox ports and takes the existing generated header.
- `coleco_vdp` exposes `clk`, `reset`, `cpu_ce`, `cpu_iorq_n`, `cpu_rd_n`, `cpu_wr_n`, `cpu_a[7:0]`, `cpu_din[7:0]`, `cpu_dout[7:0]`, `raster_ce`, `raster_x[7:0]`, `raster_y[8:0]`, `raster_pixel[1:0]`, `raster_blank`, and `status_collision`.
- `coleco_machine` exposes `clk_sys`, `reset`, `keyboard[39:0]`, `media_ready`, `media_size[14:0]`, `media_data[7:0]`, `media_addr[13:0]`, `peek_addr[15:0]`, `peek_data[7:0]`, and logical raster/controller observability ports.
- `coleco_video_720p` follows the ZX81 video shell with system-domain logical input and pixel-domain HDMI outputs.

- [ ] **Step 1: Write the VDP test first.**

Add a C++ test that drives a reset, writes VDP register 2 to name-table base `0x0000`, register 4 to pattern-table base `0x0800`, register 3 to color-table base `0x2000`, writes name `0x01`, pattern byte `0x80`, and color byte `0xf1`, then clocks the logical raster until `(x,y)=(8,0)` and asserts a foreground pixel. Add a second assertion that a status read clears the pending status bit.

```cpp
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vcoleco_vdp dut;
    reset(dut);
    write_register(dut, 2, 0x00);
    write_register(dut, 3, 0x20);
    write_register(dut, 4, 0x01);
    write_vram(dut, 0x0001, 0x01);
    write_vram(dut, 0x0808, 0x80);
    write_vram(dut, 0x2001, 0xf1);
    expect_foreground_at(dut, 8, 0);
    write_register(dut, 1, 0x20);
    run_one_logical_frame(dut);
    if ((read_status(dut) & 0x80) == 0) return 1;
    if (read_status(dut) & 0x80) return 2;
    return 0;
}
```

- [ ] **Step 2: Write the machine test first.**

Construct a 16 KiB in-memory cartridge with bytes at `0x0000`–`0x0002` equal to `0x3e, 0x5a, 0x32` and a loop at the target address. Assert that media loading reaches the final requested byte, that cartridge address `0x8000` returns the first byte, `0xc000` returns the same mirrored byte, RAM writes at `0x6000` read through `0x6400`, controller port reads reflect active-low row bits, and a second media-ready transaction replaces the first cartridge.

```cpp
drive_media_commit(dut, cartridge.data(), cartridge.size());
release_reset(dut);
run_cycles(dut, 20000);
assert(peek(dut, 0x8000) == cartridge[0]);
assert(peek(dut, 0xc000) == cartridge[0]);
assert(dut.ram_mirror_value == 0x5a);
assert((dut.controller1_value & 0x01) == 0);
```

- [ ] **Step 3: Write the mailbox test first.**

Use the existing `cores/fes-zx81/sim/gp_tb.cpp` exchange style, but assert the new test invokes the same simple-computer ABI: identity tag 2, media max 16384, controller-row writes, reset hold/release, media begin/data/commit, and invalid media-data-before-begin error 4.

- [ ] **Step 4: Write the video test first.**

Drive one logical frame of a known checkerboard, clock the 74.25 MHz side through one line and one frame boundary, and assert fixed 1650×750 totals, active video only inside the 512×384 centered image, 2× pixel replication, and one frame tick.

- [ ] **Step 5: Run the new target to observe the expected red failure.**

Run:

```sh
make sim-fes-coleco
```

Expected result before RTL exists: the command fails because the new top modules/sources are missing. If Verilator is unavailable, first run the repository-local tool lookup and report the environment failure separately; do not call that a passing or behavior-specific red test.

## Task 2: Add the mailbox, TV80 sources, clocks, and reset ROM

**Files:** Create `cores/fes-coleco/generated/fes_simple_computer.vh`, `rtl/fes_computer_gp.v`, `rtl/t80pa.v`, `rtl/tv80/tv80_core.v`, `rtl/tv80/tv80_alu.v`, `rtl/tv80/tv80_mcode.v`, `rtl/tv80/tv80_reg.v`, `rtl/sys_pll.v`, `rtl/pixel_pll.v`, `rtl/coleco_reset_rom.hex`.

**Interfaces:** Keep the mailbox ports and behavior byte-for-byte compatible with FES ZX81. Keep the PLL module names/ports compatible with the existing top-level recipes. The reset ROM is an 8192-byte image whose first three bytes encode `JP 0x8000` (`c3 00 80`) and whose remaining bytes are zero.

- [ ] **Step 1: Copy the generated ABI/header and proven Verilog modules.**

Copy the tracked text of `cores/fes-zx81/generated/fes_simple_computer.vh`, `rtl/fes_computer_gp.v`, `rtl/t80pa.v`, and the four `rtl/tv80/*.v` files into the new package with `apply_patch`, preserving SPDX/license headers and the `TV80_REFRESH` conditional.

- [ ] **Step 2: Add the red-to-green mailbox assertions.**

Run the focused mailbox Verilator command from `Makefile` after adding its source list. The expected green result is the existing simple-computer golden exchange sequence plus the controller-row and 16 KiB boundary assertions.

- [ ] **Step 3: Add clock and reset assets.**

Use the FES ZX81 PLL wrappers as the baseline, change only module/file names if required, and add the reset image. Verify with a textual test that the image has 8192 bytes and starts with `c30080`.

- [ ] **Step 4: Re-run the focused GP and source checks.**

Run:

```sh
make sim-fes-coleco
python3 -m compileall scripts/build_fes_coleco.py scripts/build_fes_coleco_oss.py
```

At this point GP may pass while machine/VDP/video tests remain red because those RTL modules do not exist yet.

## Task 3: Implement the ColecoVision memory map and TV80 machine

**Files:** Create `cores/fes-coleco/rtl/coleco_machine.sv`; modify `cores/fes-coleco/sim/machine_tb.cpp` only when an assertion reveals a testbench timing mistake.

**Interfaces:** `coleco_machine` consumes mailbox reset/media/controller signals and instantiates `T80pa` plus `coleco_vdp`. It produces `media_addr[13:0]`, `logical_x[7:0]`, `logical_y[7:0]`, `logical_pixel[1:0]`, `logical_blank`, `controller1[7:0]`, `controller2[7:0]`, and `vdp_status[7:0]` for simulation/top-level wiring.

- [ ] **Step 1: Add only the CPU-enable and memory-map skeleton.**

Implement a 32-bit phase accumulator that adds `3_579_545` each 52 MHz cycle against a `52_000_000` modulus equivalent; emit one TV80 enable pulse when the accumulator wraps. Connect reset ROM, registered 1 KiB RAM with address `cpu_a[9:0]`, and registered 16 KiB cartridge storage with address `cpu_a[13:0]`. Keep cartridge write enable active only while reset is asserted and the mailbox reports a committed byte.

- [ ] **Step 2: Run the machine test and confirm the intended red failure.**

Run the machine-only Verilator binary. It should fail at the first VDP access because the VDP module is not present; memory-map assertions must already report useful values rather than an elaboration error.

- [ ] **Step 3: Add reset-shim and memory-read behavior.**

Return reset-ROM data for `cpu_a < 0x2000`, RAM data for `0x6000 <= cpu_a < 0x8000`, and cartridge data for `cpu_a >= 0x8000`. Use the upper address bits only for decode and mirror the 16 KiB cartridge with `cpu_a[13:0]`.

- [ ] **Step 4: Add controller port decoding.**

Decode `IORQ_n=0`, `RD_n=0`, and `cpu_a[7:0] == 8'hfc` or `8'hff`. Map rows 0 and 1 to controller 1 and rows 2 and 3 to controller 2; inactive active-low inputs read as ones, and unimplemented port bits read high. Expose controller values for the testbench.

- [ ] **Step 5: Run the machine test again.**

The reset, media, cartridge mirror, RAM mirror, and controller assertions must pass; VDP assertions may remain red until Task 4.

## Task 4: Implement the synchronous VDP subset

**Files:** Create `cores/fes-coleco/rtl/coleco_vdp.sv`; modify `sim/vdp_tb.cpp` and `sim/machine_tb.cpp` only for verified cycle alignment.

**Interfaces:** The VDP receives CPU bus controls and emits one logical pixel per `raster_ce` with `raster_x`, `raster_y`, `raster_pixel[1:0]`, `raster_blank`, and an 8-bit status read. VRAM is 16384 bytes and uses one CPU write/read port plus one registered raster read path.

- [ ] **Step 1: Implement VDP control-port latching.**

On the second write to `0xbf`, treat bit 7 as register-write mode and store register index bits 3:0; otherwise store the 14-bit VRAM address and direction. A read of `0xbf` returns status bit 7 and clears the status latch. A read/write of `0xbe` accesses VRAM and advances the address.

- [ ] **Step 2: Run the VDP test to observe the tile-path red failure.**

Run the VDP-only binary. It must fail at the foreground-pixel assertion while proving port latching and status-clear behavior.

- [ ] **Step 3: Implement Graphics I tile addressing.**

For a 256×192 logical raster, calculate `name = name_base + y[7:3]*32 + x[7:3]`, `pattern = pattern_base + name_byte*8 + y[2:0]`, and `color = color_base + name_byte`. Select foreground/background nibbles from the color byte and emit a two-bit palette index from the pattern bit. Register all VRAM reads so the OSS lane sees a synchronous M10K shape.

- [x] **Step 4: Bound the first-slice status path.**

Expose deterministic VBlank status and clear it on status read. Sprite
evaluation, collision, and overflow status are intentionally deferred because
the OSS VRAM read-port workaround already requires a staged tile pipeline.

- [x] **Step 5: Run VDP and machine tests to green.**

Run the focused VDP and machine binaries; require tile foreground/background,
VBlank status clear, controller reads, and machine VDP port assertions to pass.

## Task 5: Add capture and fixed 720p video

**Files:** Create `cores/fes-coleco/rtl/coleco_video_720p.v`; modify `sim/video_tb.cpp` and `Makefile`.

**Interfaces:** Follow `zx81_video_720p` for `clk_sys`, `raster_ce`, logical pixel/blank, `pixel_clk`, RGB, DE, HS, VS, and frame tick. The logical input is 256×192; the output image is centered at 512×384.

- [ ] **Step 1: Implement the test-facing capture buffer.**

Use a registered logical write address `{logical_y, logical_x}` and a registered pixel-clock read address. Annotate the buffer with `(* ramstyle = "M10K" *)`; do not use an asynchronous array read.

- [ ] **Step 2: Run the video test to observe the timing red failure.**

Run the video-only binary and confirm it fails because the module is not yet wired to the top-level timing counters.

- [ ] **Step 3: Add 1650×750 timing and 2× scaling.**

Use active 1280×720, front porch 110/5, sync 40/5, totals 1650/750 as in the existing FES shell. Render only when the active pixel lies in the centered 512×384 image, map source coordinates by right shift one, and palette-map the two-bit logical pixel.

- [ ] **Step 4: Run video and machine/video integration tests.**

Require one frame tick per frame, no DE outside the active 1280×720 area, exact 2× replication, and no out-of-range frame-buffer access.

## Task 6: Wire the DE10-Nano shell and board constraints

**Files:** Create `cores/fes-coleco/rtl/top.v`, `constraints.qsf`, `constraints-oss.qsf`, `clocks.sdc`, `clocks-oss.sdc`, `sim/board_models.v`, `sim/board_tb.cpp`; modify `Makefile`.

**Interfaces:** Match `fes-zx81/rtl/top.v` port names exactly: `FPGA_CLK1_50`, HDMI clock/DE/data/HS/VS, and HDMI I²C SCL/SDA. Use the FES simple-computer mailbox and the new machine/video modules.

- [ ] **Step 1: Write the board test assertions before wiring.**

Assert that reset reaches the machine, a mailbox identity response reaches `FPGA_TO_HPS`, the HDMI clock is the pixel PLL output, and I²C pads remain open-drain/released when the HPS model is idle.

- [ ] **Step 2: Run the board test to observe the missing-shell red failure.**

Run the board-only Verilator command and confirm elaboration fails solely because `top`/board models are not yet present.

- [ ] **Step 3: Add top-level wiring and conditional I²C.**

Use the Quartus native inout assignments under `QUARTUS`; under OSS instantiate two `MISTRAL_IO` pads and the HPS I²C primitive with BEL `cyclonev_hps_interface_peripheral_i2c.52.60.0`. Pass `BUILD_ID` from `top` to the mailbox.

- [ ] **Step 4: Add board pins and clocks.**

Start from FES ZX81 pin assignments and accepted clock constraints. Keep only the DE10-Nano HDMI/reference clock/I²C pins needed by this top, and keep OSS QSF/SDC syntax in the known nextpnr subset.

- [ ] **Step 5: Run all simulation targets.**

Run `make sim-fes-coleco`; when Verilator is available, the expected result is green default and OSS-conditional GP, machine, VDP/video, and board binaries.

## Task 7: Add the OSS build recipe and recipe-level tests

**Files:** Create `scripts/build_fes_coleco_oss.py`, `tests/test_build_fes_coleco.py`; modify `Makefile`.

**Interfaces:** Export the same public helpers as the ZX81 OSS recipe: `build_commands(root, output, build_id, tools)`, `create_build_record(root, repository, revision, tool_identities)`, `validate_build_evidence(output, source_root)`, and `build(root, package_store)`.

- [ ] **Step 1: Write recipe tests first.**

Add tests that import the module and assert:

```python
def test_oss_command_is_verilog_only(tmp_path):
    yosys, nextpnr = build_commands(tmp_path, tmp_path / "build/fes-coleco-oss", "a" * 32,
                                     {"yosys": Path("/yosys"), "nextpnr-mistral": Path("/nextpnr")})
    assert "-DTV80_REFRESH=1" in yosys
    assert "-DFES_COLECO_OSS=1" in yosys
    assert "t80pa.v" in yosys
    assert "-D" not in " ".join(nextpnr)
    assert "--device" in nextpnr and "5CSEBA6U23I7" in nextpnr
```

Also assert the required cell set contains two PLLs, one HPS GP, one HPS I²C, and at least one M10K, while forbidden DSP/MLAB/oscillator cells are rejected.

- [ ] **Step 2: Run the recipe tests to observe the expected import/constant red failure.**

Run:

```sh
python3 -m pytest tests/test_build_fes_coleco.py -q
```

Expected result before the recipe exists: import failure naming `scripts.build_fes_coleco_oss`.

- [ ] **Step 3: Implement the OSS recipe from the ZX81 validation path.**

Set `OUTPUT_RELATIVE = Path("build/fes-coleco-oss")`, point `RTL_SOURCES` at the new clock/mailbox/TV80/machine/VDP/video/top files, pin the reset ROM/header/QSF/SDC as inputs, and invoke:

```text
read_verilog -sv -DTV80_REFRESH=1 -DFES_COLECO_OSS=1 -I cores/fes-coleco/generated <all RTL_SOURCES>;
chparam -set BUILD_ID 128'h<build_id> top;
synth_intel_alm -nolutram -nodsp -top top;
stat;
write_json build/fes-coleco-oss/synth.json
```

Invoke nextpnr with `--device 5CSEBA6U23I7 --qsf cores/fes-coleco/constraints-oss.qsf --sdc cores/fes-coleco/clocks-oss.sdc --freq 74.25 --seed 7 --router router1 --tmg-ripup --rbf ... --compress-rbf --write ... --report ... --detailed-timing-report`.

- [ ] **Step 4: Implement evidence and manifest checks.**

Require clean source/origin identity, exact input hashes, authenticated pinned tool identities, complete route log, 52 MHz and 74.25 MHz fmax rows, bounded RBF, format-2 manifest core ID `fes.coleco`, ABI `fes.simple-computer` 1.0, and interfaces keyboard/video/media. Reject unknown utilization resources and any forbidden resource.

- [ ] **Step 5: Run recipe tests to green.**

Run the focused pytest file and `python3 -m compileall scripts/build_fes_coleco_oss.py`. If the authenticated tools are not installed, keep command-construction tests green and report the full OSS build as unavailable rather than substituting system tools.

## Task 8: Add the Quartus recipe and generated project checks

**Files:** Create `scripts/build_fes_coleco.py`; modify `Makefile`, `tests/test_build_fes_coleco.py`.

**Interfaces:** Export `write_project`, `compile_command`, `create_build_record`, `validate_quartus_evidence`, and `build`, matching the FES ZX81 Quartus recipe.

- [ ] **Step 1: Write project-generation tests first.**

Assert the generated QSF contains the Cyclone V device, `TOP_LEVEL_ENTITY top`, `QUARTUS=1`, `FES_COLECO_BUILD_ID`, both PLL sources, all Verilog/SystemVerilog sources, the copied reset ROM, the HDMI I²C pin assignments, and Quartus 17.0.2 metadata.

- [ ] **Step 2: Run the project tests to observe the missing-recipe red failure.**

Run the focused pytest file and confirm import/attribute failure for the absent recipe.

- [ ] **Step 3: Implement project generation and source staging.**

Generate `build/fes-coleco-quartus/project/top.qpf` and `top.qsf`, copy both the byte-per-line OSS `.hex` and Quartus `.mif` reset images into the project path required by the RTL, use `VERILOG_MACRO "QUARTUS=1"`, and set the build-ID macro. Use the explicit Quartus authentication and runner behavior from FES ZX81.

- [ ] **Step 4: Implement timing/RBF evidence and package manifest.**

Seal `core.rbf`, `top.sta.rpt`, `build-summary.json`, and `manifest.toml` only after timing evidence proves the system/pixel constraints and the RBF is bounded. Use core ID `fes.coleco`, name `FES ColecoVision`, version `1.0.0`, programming profile `fes-gp-v1`, and the three simple-computer interfaces.

- [ ] **Step 5: Run project tests and print-only validation.**

Run:

```sh
python3 -m pytest tests/test_build_fes_coleco.py -q
python3 scripts/build_fes_coleco.py --root . --print-commands
```

If Quartus 17.0.2 is absent, report that exact availability failure; do not call generated-project tests a Quartus compile.

## Task 9: Update component documentation and canonical architecture

**Files:** Create `cores/fes-coleco/README.md`; modify `README.md`, `docs/architecture.md`.

- [ ] **Step 1: Write documentation assertions/checks.**

Add recipe tests that require the README and architecture text to name `fes-coleco`, the 16 KiB limit, `fes.simple-computer`, both build commands, and the workaround keywords `TV80_REFRESH`, `M10K`, `MISTRAL_IO`, and `52.60.0`.

- [ ] **Step 2: Add the core README.**

Document the machine map, raw media limit, reset shim, controller row mapping, supported VDP path, simulation commands, OSS/Quartus commands, and explicit non-goals. Say “first slice” wherever the implementation is intentionally incomplete.

- [ ] **Step 3: Add one present-tense canonical architecture section.**

Place the section beside FES Pong and FES ZX81. Describe ownership, data flow, clock domains, resource expectations, artifact boundary, and each workaround only with evidence available from this branch. Do not add a roadmap/status document.

- [ ] **Step 4: Update the root component README command list.**

Add the simulation and build targets to the current working list, preserving all unrelated experiment descriptions.

- [ ] **Step 5: Run documentation checks and diff hygiene.**

Run:

```sh
python3 -m pytest tests/test_build_fes_coleco.py -q
git diff --check
```

## Task 10: Full validation and workaround handoff

**Files:** Modify `docs/architecture.md` and `cores/fes-coleco/README.md` only if fresh evidence requires a correction; create `build/fes-coleco-oss/workaround-report.json` only as an ignored build output if the recipe emits it.

- [ ] **Step 1: Run all available focused simulation checks.**

Run `make sim-fes-coleco`; capture exit status and full output. Run the new pytest suite and `python3 -m compileall scripts`. A missing Verilator installation is an environment limitation; a Verilator compile/assertion failure is an implementation failure.

- [ ] **Step 2: Run the OSS build with the authenticated repository-local tools.**

Run `make build-fes-coleco`. Inspect `synth.json`, `routed.json`, `timing.json`, `nextpnr.log`, `build-summary.json`, `manifest.toml`, and `core.rbf`. Record exact failures for M10K inference, PLL modeling, I²C BEL routing, SDC/QSF parsing, or route pressure in the architecture/README handoff.

- [ ] **Step 3: Run the Quartus build if the exact compiler is available.**

Run `make build-fes-coleco-quartus`; verify project, STA, RBF, manifest, and source-hash evidence. If Quartus is unavailable, preserve print-only evidence and classify compile validation as unavailable.

- [ ] **Step 4: Re-run fresh verification before any completion statement.**

Run:

```sh
make sim-fes-coleco
python3 -m pytest tests/test_build_fes_coleco.py -q
python3 -m compileall scripts
git diff --check
git status --short --branch
```

Only report tests/builds as passing when their fresh command exits zero. State hardware classification as “not programmed/not tested,” parent pin effect as “none; parent still selects the prior `misteross` revision,” and the next integration step as review/selection of this branch’s commit or diff.
