# Coleco TMS9918 Text and Multicolor Modes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add TMS9918 Text and Multicolor rendering to the shared Coleco VDP while preserving Graphics I/II behavior, the existing memory implementation, status interface, and logical raster.

**Architecture:** Decode the TMS mode bits once in `coleco_vdp.sv`, choose mode-specific name and pattern addresses, and render mode-specific pixels through the existing direct simulation path and registered FPGA read pipeline. Keep the VRAM replicas, sprite line banks, module ports, raster cadence, and SMS Mode 4 implementation unchanged; add explicit pixel oracles and regression coverage at the shared VDP and SMS wrapper boundaries.

**Tech Stack:** SystemVerilog, Verilator C++ simulations, Yosys/nextpnr Coleco OSS package producer, Markdown core documentation.

**Spec:** `docs/superpowers/specs/2026-09-24-coleco-tms9918-modes-design.md`

## Global Constraints

- Keep the logical raster at 256-by-192 pixels and preserve all module ports and package/mailbox interfaces.
- Preserve existing Graphics I/II pixels, VRAM access, color indices, graphics-mode sprites, and status-read acknowledgement.
- Decode M1 from R1[4], M2 from R1[3], and M3 from R0[1]; unsupported combinations produce the R7 backdrop.
- Text uses 40 columns by 24 rows, six visible high bits per glyph byte, eight-pixel side margins, and R7 foreground/background nibbles; sprites do not render in Text mode.
- Multicolor uses the existing 32-by-24 name table, eight-byte pattern segments, row byte pairs 0/1, 2/3, 4/5 and 6/7 repeating, and four-bit color-zero backdrop resolution; sprites remain active.
- Add no module ports, ABI changes, VRAM changes, CPU port behavior changes, or BIOS/ROM-link changes.
- Do not change CPU frequency, `raster_ce`, horizontal/vertical totals, VBlank cadence, HDMI timing, or the system-clock sprite schedule in this slice.
- Keep the Coleco, SG-1000, and SMS legacy TMS consumers covered; keep SMS Mode 4 behavior unchanged.
- Do not claim physical acceptance from simulation or routing. Do not commit, push, or open a PR without explicit authorization.

## Review Focus

- Text left/right margins and the 40th column: pin x=7, 8, 13, 14, 242, 247, and 248 to backdrop/glyph boundaries in the Text pixel oracle (Task 1).
- Text glyph bit order and colors: pin all six high pattern bits, ignored low bits, R7 foreground/background, and color-zero resolving through the backdrop (Task 1).
- Text sprite suppression versus Multicolor sprite visibility: place a nonzero sprite over known background pixels and assert suppression in Text and overlay in Multicolor (Tasks 1 and 3).
- Multicolor byte-pair selection and repeat: assert upper/lower halves across row-pairs 0/1, 2/3, 4/5, 6/7 and verify pair 0/1 repeats on the fifth tile row (Task 3).
- Unsupported mode combinations and status retention: sample backdrop for each invalid mode selector, then latch sprite status, switch to Text, and verify it remains readable and clears on status read (Tasks 1–2 and 5).

## File Map

- `sources/misteross/cores/fes-common/rtl/coleco_vdp.sv` — shared mode decode, mode-specific address generation and output selection for direct and registered-memory paths.
- `sources/misteross/cores/fes-coleco/sim/graphics_tb.cpp` — literal VRAM/pixel tests for Text, Multicolor, invalid selectors, margins, colors, and sprite interaction; it runs against both direct and OSS-registered VDP implementations.
- `sources/misteross/cores/fes-coleco/sim/vdp_tb.cpp` — status mode-transition regression alongside existing status acknowledgement checks.
- `sources/misteross/cores/fes-sms/sim/vdp_tb.cpp` — prove Text and Multicolor pass through the SMS legacy wrapper and palette conversion while the existing Mode 4 checks still pass.
- `sources/misteross/cores/fes-coleco/README.md`, `sources/misteross/cores/fes-sg1000/README.md`, `sources/misteross/cores/fes-sms/README.md`, and `sources/misteross/docs/architecture.md` — describe the shared TMS mode support and keep the separate NTSC timing follow-up explicit.
- `sources/misteross/cores/fes-coleco/rtl/coleco_machine.sv` — update the top-level comment that currently describes only Graphics I/II.

No simulation target, module signature, generated consumer, ROM format, or shared package contract needs to change.

---

### Task 1: Add a failing Text-mode pixel oracle

**Files:**
- Modify: `sources/misteross/cores/fes-coleco/sim/graphics_tb.cpp`

**Interfaces:**
- Consumes: the existing `Bench::reg`, `Bench::byte`, and `Bench::run` helpers.
- Produces: a literal expected-pixel fixture for the Text mode in both simulation lanes.

- [ ] **Step 1: Add the Text fixture and expected pixels**

After the existing Graphics I checks, configure Text mode with `R0=0x00`,
`R1=0x50`, `R2=6`, `R4=1`, and `R7=0xa4`. Write code 3 to name-table
`0x1800`, code 4 to `0x1801`, code 5 to `0x1827`, and code 6 to
`0x1828`. Write glyph rows `0xa7` at `0x0818`, `0xc7` at `0x0820`,
`0x84` at `0x0828`, and `0xc0` at `0x0830`; write glyph 3's next row
`0x00` at `0x0819`. Restore one visible sprite at SAT `0x1b00` with
`Y=255`, `X=32`, pattern 0, color 14, and set sprite pattern row 0 at
`0x3800` to `0xff`.

Add one `b.run` oracle with these key/value pixels, where each key is
`(y << 8) | x`:

```cpp
{{0,4}, {7,4}, {8,10}, {9,4}, {10,10}, {11,4}, {12,4}, {13,10},
 {14,10}, {15,10}, {16,4}, {18,4}, {19,10},
 {32,4}, {242,10}, {247,10}, {248,4}, {255,4}, {(1u << 8) | 8u,4},
 {(8u << 8) | 8u,10}}
```

The sample at `(32,0)` must remain background color 4 despite the active
sprite; `(242,0)` and `(247,0)` exercise the last text cell; `(248,0)` is
the right margin. The `(0,0)` and `(255,0)` samples exercise both row ends.
The glyph byte `0xa7` makes the low two bits nonzero while only bits 7..2 are
visible. After this oracle, write `0x80` to `0x0818`, set `R7=0x04`, and
run a second oracle `b.run({{8,4}})`; foreground color zero must resolve to
the nonzero backdrop. Restore `R7=0xa4`. For each unsupported selector
`011`, `101`, `110`, and `111`, set R0/R1 while leaving display enabled and
sample `(64,0)`, which must be backdrop color 4. Before that loop, set
`R3=0x80`, write name 1 at `0x1808`, set patterns `0xff` at `0x0008` and
`0x0808`, and set Graphics I/II color bytes to `0xa2` at `0x2000` and
`0x2008`. These bytes make the old Graphics I/II fall-throughs visibly
different from the expected backdrop. Derive register values directly from
the selector:

```cpp
const unsigned invalid_modes[] = {3, 5, 6, 7};
for (const unsigned selector : invalid_modes) {
    const unsigned m1 = (selector >> 2) & 1;
    const unsigned m2 = (selector >> 1) & 1;
    const unsigned m3 = selector & 1;
    b.reg(0, m3 << 1);
    b.reg(1, 0x40 | (m1 << 4) | (m2 << 3));
    b.run({{64,4}});
}
```

Keep all fixture setup local to this test file; do not change the generated
Verilator interface.

- [ ] **Step 2: Run both focused lanes and confirm the oracle fails**

Run:

```sh
env -u FES_TOOLCHAIN_CACHE_ROOT make -C sources/misteross sim-fes-coleco-graphics
env -u FES_TOOLCHAIN_CACHE_ROOT make -C sources/misteross sim-fes-coleco-graphics-oss
```

Expected: both build and stop at a literal Text pixel mismatch because the
current RTL still renders the fixture as Graphics I. The unsupported-selector
pixels must also be wrong when the test reaches them. Existing pre-Text checks
must continue to pass before that failure.

---

### Task 2: Implement Text-mode selection and rendering

**Files:**
- Modify: `sources/misteross/cores/fes-common/rtl/coleco_vdp.sv`
- Test: `sources/misteross/cores/fes-coleco/sim/graphics_tb.cpp`

**Interfaces:**
- Consumes: the TMS mode-bit oracle and Text VRAM fixture from Task 1.
- Produces: Text name/pattern lookups through existing VRAM copies and the same four-bit `raster_pixel` output.

- [ ] **Step 1: Add explicit mode predicates and Text addresses**

Define one three-bit selector in the order `{R1[4], R1[3], R0[1]}` and
explicit predicates for `000` Graphics I, `001` Graphics II, `010`
Multicolor, and `100` Text. Change the existing Graphics II predicate so
other selector values cannot fall through as Graphics II. For Text, compute
the active area only for `8 <= x < 248`, column `(x - 8) / 6`, row
`y[7:3]`, and name address `name_base + row * 40 + column`. Clamp the
requested column to zero outside the active Text area so the registered RAM
address stays in range; the final output must still be backdrop outside it.
Compute Text pattern address as
`pattern_base + name * 8 + y[2:0]`. Keep sprite pattern address generation
separate from background mode selection.

- [ ] **Step 2: Render six-bit glyphs in the direct and registered paths**

For each Text pixel inside the active area, select glyph bit
`pattern_byte[7 - ((x - 8) % 6)]`; ignore bits 1:0. Select foreground from
`R7[7:4]` and background from `R7[3:0]`, then pass the selected four-bit
color through `visible_color`. Force the R7 backdrop for the side margins,
inactive display, and unsupported selectors. Gate sprite output off only for
Text; leave the sprite path enabled for Graphics I, Graphics II, and the
future Multicolor branch.

- [ ] **Step 3: Run both focused lanes and confirm Text passes**

Run the two commands from Task 1. Expected: both print the graphics simulation
pass message, including the new Text oracle; the existing Graphics I/II sample
pixels remain unchanged.

---

### Task 3: Add a failing Multicolor pixel oracle

**Files:**
- Modify: `sources/misteross/cores/fes-coleco/sim/graphics_tb.cpp`

**Interfaces:**
- Consumes: the existing `Bench` VRAM writes and expected-pixel sampling.
- Produces: explicit row-pair, nibble, backdrop, and sprite tests.

- [ ] **Step 1: Add the Multicolor fixture**

Set `R0=0`, `R1=0x48`, `R2=6`, `R4=1`, and `R7=0x0d`. Put pattern name
2 in the column-1 entries for name-table rows 0 through 4 at
`0x1801`, `0x1821`, `0x1841`, `0x1861`, and `0x1881`. This keeps the
same pattern segment selected while sampling successive screen tile rows.
Write pattern bytes at `0x0810..0x0817` as
`{0x2a,0x4c,0x6b,0x0d,0x31,0x52,0x73,0x84}`. Keep the sprite at
`x=32`, color 14, with a set pattern bit. Add expected samples: row 0 has
`(8,0)->2`, `(12,0)->10`, `(8,4)->4`, `(12,4)->12`; row 1 has
`(8,8)->6`, `(12,8)->11`, `(8,12)->13`, `(12,12)->13`; row 2 has
`(8,16)->3`, `(12,16)->1`, `(8,20)->5`, `(12,20)->2`; row 3 has
`(8,24)->7`, `(12,24)->3`, `(8,28)->8`, `(12,28)->4`; row 4 at
`y=32` repeats row 0 with `(8,32)->2` and `(12,32)->10`. Include
`(32,0)->14` to prove sprites remain active. The high nibble zero in byte
`0x0d` must resolve to backdrop 13.

- [ ] **Step 2: Run both focused lanes and confirm the new checks fail**

Run `sim-fes-coleco-graphics` and `sim-fes-coleco-graphics-oss` as in Task 1.
Expected: the Text and unsupported-selector oracles pass; Multicolor first
fails because Multicolor currently renders only the backdrop.

---

### Task 4: Implement Multicolor addressing and rendering

**Files:**
- Modify: `sources/misteross/cores/fes-common/rtl/coleco_vdp.sv`
- Test: `sources/misteross/cores/fes-coleco/sim/graphics_tb.cpp`

**Interfaces:**
- Consumes: the Multicolor pixel oracle from Task 3 and unsupported-selector fallback from Task 2.
- Produces: Multicolor pixels via the existing pattern VRAM read path, with no new memory interface.

- [ ] **Step 1: Generate the Multicolor pattern byte address**

Keep the existing 32-column name lookup. For a name and screen tile row,
select pattern byte offset
`(((y >> 3) & 3) << 1) | ((y >> 2) & 1)` from the name's eight-byte
segment, in addition to `pattern_base + name * 8`. This chooses pair 0/1,
2/3, 4/5, 6/7 by successive tile row and repeats the pair sequence every
four rows. Reuse the current pattern VRAM copy; do not add a fifth copy or a
module port.

- [ ] **Step 2: Select the four 4-by-4 Multicolor blocks**

For Multicolor pixels, choose the pattern byte's high nibble for
`x[2] == 0` and low nibble for `x[2] == 1`; the address above chooses the
byte for the upper/lower 4-pixel block and screen tile row. Pass the selected
color through `visible_color`. Keep the sprite overlay enabled. In the
registered-memory path, use the delayed coordinate paired with
`vram_pattern_read`; in the direct path, use the same formula against `vram`.
Default the background to the R7 low-nibble backdrop for every unsupported
selector and do not disturb status or raster counters.

- [ ] **Step 3: Run both focused lanes and confirm all mode oracles pass**

Run the two Coleco graphics targets. Expected: Text, Multicolor, all four
unsupported selectors, and the original Graphics I/II fixtures pass in both
the asynchronous simulation memory and registered M10K simulation paths.

---

### Task 5: Pin status retention and SMS legacy-wrapper behavior

**Files:**
- Modify: `sources/misteross/cores/fes-coleco/sim/vdp_tb.cpp`
- Modify: `sources/misteross/cores/fes-sms/sim/vdp_tb.cpp`

**Interfaces:**
- Consumes: the existing VDP status register test and `sms_vdp` wrapper.
- Produces: regressions that prove a mode switch does not erase latched status and the SMS wrapper exposes shared Text/Multicolor pixels.

- [ ] **Step 1: Test latched collision across a switch to Text**

In `vdp_tb.cpp`, after the existing overlapping-sprite collision is latched
and before its `0xbf` status read, write `R0=0x00` and `R1=0x50`. Keep the
existing assertion that status bit 5 is set on the first read and add an
immediate second-read assertion that bit 5 is clear. Expected: changing the
mode does not clear the collision; the first status read acknowledges it as
before.

- [ ] **Step 2: Exercise legacy Text and Multicolor through `sms_vdp`**

In `cores/fes-sms/sim/vdp_tb.cpp`, after the existing Mode 4 assertions,
write `R0=0` to select the legacy wrapper, configure Text with `R1=0x50`,
`R2=6`, `R4=1`, `R7=0xa4`, write code 3 at `0x1800` and glyph byte
`0xa7` at `0x0818`, and require `sample_pixel(dut, 8, 0, 3)` to equal
SMS legacy palette code 10 (`6'h1f`). Then select Multicolor with `R1=0x48`,
write name 2 at `0x1801` and pattern byte `0x2a` at `0x0810`, and require
`sample_pixel(dut, 8, 0, 3)` and `sample_pixel(dut, 12, 0, 3)` to equal
`6'h1c` (TMS color 2) and `6'h1f` (TMS color 10). Use the existing
`write_register`, `write_vram`, and `sample_pixel` helpers. Leave the Mode 4
setup and assertions intact.

- [ ] **Step 3: Run focused status and SMS simulations**

Run:

```sh
env -u FES_TOOLCHAIN_CACHE_ROOT make -C sources/misteross sim-fes-coleco
env -u FES_TOOLCHAIN_CACHE_ROOT make -C sources/misteross sim-fes-sms sim-fes-sms-oss
```

Expected: Coleco's default and OSS suites pass, including the status
transition; both SMS VDP lanes pass legacy pixel samples and the existing
Mode 4, PSG, I2S, ROM, and machine checks.

---

### Task 6: Update shared-core descriptions and close SG-1000 integration coverage

**Files:**
- Modify: `sources/misteross/cores/fes-common/rtl/coleco_vdp.sv` comments
- Modify: `sources/misteross/cores/fes-coleco/rtl/coleco_machine.sv` comment
- Modify: `sources/misteross/cores/fes-coleco/README.md`
- Modify: `sources/misteross/cores/fes-sg1000/README.md`
- Modify: `sources/misteross/cores/fes-sms/README.md`
- Modify: `sources/misteross/docs/architecture.md`

**Interfaces:**
- Consumes: implemented four-mode shared TMS path and Tasks 1–5 verification.
- Produces: present-tense core documentation; NTSC timing remains a separately scoped follow-up.

- [ ] **Step 1: Update current implementation descriptions**

Update the shared VDP and Coleco machine comments to say Graphics I, Graphics
II, Text, and Multicolor rather than only Graphics I/II. In the Coleco and
SG-1000 READMEs, describe all four supported TMS modes and keep unsupported
mode combinations documented as backdrop-only. Replace the Coleco README and
architecture prose that says “full VDP modes” remain outside this slice; say
that NTSC timing and cycle-perfect raster behavior remain outside it. In the SMS README, identify
the same four modes on the legacy TMS path while retaining the separate SMS
Mode 4 description. Update the canonical Coleco section of
`docs/architecture.md` and the consumer sections to name the four modes and
the fixed 256-by-192 logical raster.

State that Text mode suppresses sprites and Multicolor keeps them active. Keep
the current NTSC timing/cycle-accuracy limitations explicit; do not update CPU,
clock, line-total, video-shell, BIOS or package claims.

- [ ] **Step 2: Run the remaining SG-1000 consumer simulations**

Run:

```sh
env -u FES_TOOLCHAIN_CACHE_ROOT make -C sources/misteross sim-fes-sg1000 sim-fes-sg1000-oss
```

Expected: both SG-1000 machine checks pass and compile/run the direct and
Coleco-registered shared VDP paths. Task 5 covers Coleco's aggregate default
and OSS unit/board lanes, and both SMS VDP lanes with the legacy pixel checks
and existing Mode 4 behavior.

- [ ] **Step 3: Check the final diff and package integration gate**

Run `git diff --check` and review the diff against the spec. Confirm no changes
to ports, mailbox/package ABI, VRAM depth, ROM linking, clocks, raster totals,
sprite evaluation cadence, or HDMI shell. Do not run a physical kit test.

The product OSS producer requires a clean committed FES tree. After the user
authorizes a commit or an integration operator selects the reviewed commit,
run `make -C sources/misteross toolchain-fes-coleco` if the locked tools are not
already installed, then run `make -C sources/misteross build-fes-coleco` on
that clean committed tree. Expected: the sealed Coleco OSS package completes
its normal structured `clk_sys`/`pixel_clk` timing and resource checks. Until
that clean-tree product build passes, hand off the RTL as simulation-verified
and report package integration as pending; a dirty-tree synth-only probe is
not a substitute for this gate.

- [ ] **Step 4: Return an uncommitted FES handoff unless commit is authorized**

Report the FES base commit, worktree path, changed scope, actual simulation
results, package-build status, absence of hardware testing, and remaining
integration gate. Do not commit, push, or open a PR without explicit user
authorization.

---

## Plan self-review

- **Spec coverage:** Text addressing, glyph bit order, margins, R7 colors,
  sprite suppression, mode decoding, unsupported-selector backdrop,
  Multicolor pair selection/nibbles/backdrop/sprites, preserved Graphics I/II
  and status reads, shared Coleco/SG-1000/SMS use, unchanged ports/timing, and
  the clean-tree Coleco OSS product build each have an owning task.
- **Placeholder scan:** Every task names concrete files, commands, and expected
  outcomes; no validation step is open-ended.
- **Type/interface consistency:** The plan adds no RTL ports or generated
  interfaces. Pixel fixtures use the existing `Bench::run` coordinate key
  convention and SMS test helpers already defined in their respective files.
- **Review focus:** All five uncovered failure classes above have concrete
  assertions in their owning test task; routine Graphics I/II regression is
  retained in every direct/registered graphics run.
