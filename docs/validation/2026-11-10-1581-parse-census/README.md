# #1581 S1a: front-end parse census

Spike S1a of [RTL front-end lanes](../../design/rtl-frontend-lanes.md) (#1581, child issue #1626). It runs one minimal module per construct through every front end. It then runs the OPL2 tree and three genvar-heavy i386 modules file by file and as whole blocks, and fits the P3 true-dual-port RAM both ways. Run on Powerboat on 10 November 2026. No production code changed.

## Result

- **Quartus Pro 26.3 reads every form in the census, correctly.** Every rejection and misread by Quartus 17.0.2 Lite below is Lite only.
- **Lite rejects:**
  - `for (genvar …)`, with or without `generate`/`endgenerate`;
  - generate loops and generate `if` blocks without `generate`/`endgenerate`;
  - a named assignment pattern whose key names a module signal (`'{data: …}` beside `logic data`);
  - `signed'()` straight after a unary minus.
- **Lite silently misreads one `signed'()` form.** With `int`-typed localparams, `signed'(D - S - 2) < 0` takes the wrong arm, with no message. The same expression with `$signed()`, or with no cast, is right. No FES file Lite reads has the misread form: OPL2's localparams are untyped, and Lite gets those right.
- **Lite reads the struct-index shadow correctly in the one form tested:** a variable index into a packed-struct member named like a module signal. The i386 report of a misread is not reproduced, so the rule stays on the list.
- **The undefined-macro check does not stop Quartus.** Lite and Pro give a Critical Warning, expand the macro to nothing and carry on. A missing `` `include `` stops all seven readers tested and names the reason in the file name, so the header should use that.
- **`(* no_carry *)` needs no fence.** Lite warns that it does not know the attribute (10335); Pro ignores it. Both compute the right sum.
- **Neither Quartus predefines `SYNTHESIS`.** Yosys does; Verilator does not.
- **P3:**
  - Lite infers the same true-dual-port M10K from the `FES_RAM_TDP` form as from the altsyncram form: 24 M10Ks, 55 ALMs, and `NEW_DATA_NO_NBE_READ` on both ports. The inferred form uses 2 more registers.
  - At seed 1 the inferred form meets 200 MHz at every corner, and the altsyncram form misses by 0.052 ns at −40 °C. One seed cannot rank them.
  - So the P3 inferred form can replace altsyncram on Lite, subject to S11's per-producer measurement.
- **Yosys:**
  - The pc386 and i386 locks behave identically on every form.
  - The census found one new fork bug, filed as [DeanoC/yosys#74](https://github.com/DeanoC/yosys/issues/74).
  - The shared lock (`3a5accbd`, every core except pc386, i386 and Coleco) rejects any `import` inside a module.
  - Coleco's own lock (`5391eeb1`) was not run: no build of it is installed.
- **Icarus was not run.** It is not installed on Powerboat or fes-build-3090, and Powerboat lacks `gperf`, which its source build needs.

## Tools

| Reader | Version | Notes |
| --- | --- | --- |
| `lite2005` | Quartus Prime 17.0.2 Build 602 07/19/2017 SJ Lite Edition (`~/intelFPGA_lite/17.0`) | `VERILOG_INPUT_VERSION SYSTEMVERILOG_2005`, Cyclone V `5CSEBA6U23I7`. 17.0.2 has no SV2009 setting: `SYSTEMVERILOG_2009` stops the project with error 125036 (`logs/lite-sv2009-setting.log`). |
| `pro2005`, `pro2009` | Quartus Prime 26.3.0 Build 114 10/14/2026 SC Pro Edition (`/data/altera_pro/26.3`) | Agilex 5 `A5EB013BB23BE4SCS` (DE25-Nano), at `SYSTEMVERILOG_2005` and `SYSTEMVERILOG_2009`. No newer Lite is installed, so Pro stands in for the "newer Quartus" column. |
| `yosys-pc386` | Yosys 0.70+ `0a7927bf1` (pc386 lock), binary sha256 `b6cd1fd2…74fbf5f` | The pc386 producer's front-end passes: `read_verilog -sv; hierarchy; proc -norom; flatten; opt -purge`. |
| `yosys-i386` | Yosys 0.70+ `238eac9a4` (i386 lock), binary sha256 `bbce80f2…16abe053` | Same passes. |
| `yosys-shared` | Yosys 0.69+ `3a5accbd7` (`sources/misteross/toolchain.lock` and every core lock except pc386, i386 and Coleco, PC/XT included), binary sha256 `6747da54…47131efa` | Same passes. |
| Coleco's Yosys | `5391eeb1e` (`cores/fes-coleco/toolchain.lock`, read by `coleco_socket_v2`) | Not run: no build is installed. It is the shared lock's previous pin, so it likely shares the `import` gap, but that is untested. |
| `verilator` | Verilator 5.032 2025-01-01 (Debian `5.032-1+b2`, `/usr/bin/verilator`) | `--lint-only -Wno-lint -Wno-style`, with warnings left fatal. This is the system build, not one built from the lock's commit `5e4151e3`. |
| Icarus | — | Not run (see Result). |

## How each form is judged

Accepting a file is not enough, since Lite has misread forms without rejecting them. So each form drives an output `ok` that should be a constant 1 only if the construct was read correctly:

- **Quartus:** full synthesis (`quartus_map`, `quartus_syn`) on a tiny top, so `ok` folds to a constant. The result is Quartus's stuck-pin warning (13410). Where the value itself matters, the form also drives a bus, decoded from the same warnings.
- **Yosys:** a SAT proof that `ok` is 1 for every input (`sat -prove ok 1 -verify`).
- **Verilator:** lint, plus a simulation testbench for the struct-index shadow (`tb_f04.sv`) and the preprocessed text for `SYNTHESIS`.

Some forms carry their own observable:

- `f09` compares the block RAM bits Quartus reports against a control (`f09b`) whose macro expands to nothing; for Yosys, it checks that the attribute reaches the memory.
- `f12a`–`f12c` pass when the reader stops.

Two forms prove only acceptance. Their `ok` cannot come out wrong, so the table says "accepted":

- `f01a`: `ok` compares the result at its own width;
- `f06c`: `ok` compares the cast with itself.

## Minimal forms

[`results.md`](results.md) is the full matrix, generated by `summarize.py`. Abbreviated, with "right" meaning accepted with `ok` constant 1:

| Form | Lite 17.0.2 (SV2005) | Pro 26.3 (2005 and 2009) | Yosys pc386 and i386 locks | Yosys shared lock | Verilator 5.032 |
| --- | --- | --- | --- | --- | --- |
| `f01a` `-signed'(…)`, the `vibrato.sv:73` form | **rejected** (10170) | accepted | accepted | accepted | accepted |
| `f01b` `W'(signed'(…) + x)`, approximating `calc_phase_inc.sv:70` | right | right | right | right | accepted |
| `f01c` `signed'(a[0]) + signed'(a[1])`, approximating `operator.sv:230` | right | right | right | right | accepted |
| `f01d` `signed'(x + (y << 3))`, approximating `ksl_add_rom.sv:86` | right | right | right | right | accepted |
| `f01e` `signed'()` in a package localparam on `int` localparams | **wrong value, no message** | right | right | rejected (`import`) | accepted |
| `f01f` the same, showing the value | **−7 for 0, no message** | 0 | right | rejected (`import`) | accepted |
| `f01g` the `opl2_pkg.sv:79` and `dac_prep.sv:59` forms with OPL2's real untyped localparams | right (shift 0) | right | right | right | accepted |
| `f02` `for (genvar …)` at module level, no `generate` | **rejected** (10170) | right | right | right | accepted |
| `f02b` `for (genvar …)` inside `generate`/`endgenerate` | **rejected** (10170, "genvar is a reserved keyword") | right | right | right | accepted |
| `f02c` `genvar i;`, then the loop inside `generate`/`endgenerate` (the prescribed rewrite) | right | right | right | right | accepted |
| `f03` `genvar i;` then a `for` loop with no `generate` | **rejected** (10170) | right | right | right | accepted |
| `f03b` a generate `if` with no `generate` | **rejected** (10170) | right | right | right | accepted |
| `f03c` the generate `if` inside `generate`/`endgenerate` | right | right | right | right | accepted |
| `f04` variable index into a packed-struct member named like a module signal | right (10036: the module signal is never read) | right | right | rejected (`import`) | right (simulated) |
| `f04b` named assignment-pattern key named like a module signal | **rejected** (10734, "data is not a constant") | right | right | rejected (`import`) | accepted |
| `f05` `module X import pkg::*; #(…)` | right | right | right | **rejected** (`import`) | accepted |
| `f06a` `IW'(k)` in a `for` loop, the `fes_i386_tlb.sv:58` form | right (`slot` = 1) | right | right | right | accepted |
| `f06b` enum cast of a variable | right | right | right | rejected (`import`) | accepted |
| `f06c` `f06a` and `f06b` in one module | accepted | accepted | **rejected** (yosys#74) | rejected (`import`) | accepted |
| `f06d` enum cast of a variable as an operand | right | right | right | rejected (`import`) | accepted |
| `f07` struct-typed ports across an instance | right | right | right | rejected (`import`) | accepted |
| `f08` `a + (* no_carry *) b` | right, warning 10335 (unknown attribute) | right, silent | right | right | accepted |
| `f09` `` `define `` holding `(* ramstyle = "logic" *)` | applied (0 block RAM bits; control 2,048) | applied (0; control 2,048) | reaches the memory | reaches the memory | accepted |
| `f10` is `SYNTHESIS` predefined? | no | no | yes | yes | no |
| `f11` failed check expands an undefined macro | **does not stop** (Critical Warning 10191) | **does not stop** (Critical Warning 13432) | stops | stops | stops |
| `f12a` failed check includes a missing file | stops (10054) | stops (13363, 16827) | stops | stops | stops |
| `f12b` failed check emits an unparsable token | stops (10170) | stops (13411) | stops | stops | stops |
| `f12c` failed check calls `$error` in `generate`/`endgenerate` | stops, but as a syntax error at `$error` (10170): Lite has no elaboration `$error` | stops (21899) | stops | stops | stops (`USERERROR` is a warning; fatal unless `-Wno-fatal`, which lets it pass: `logs/bisect-yosys74-and-neg.log`) |

All accepted Pro rows also carry warning 21620 (a Design Assistant rule on the tiny top), and some carry 23762 (hierarchy swept away); neither is about the form. "rejected (`import`)" in the shared-lock column is the same syntax error, "unexpected TOK_IMPORT", at the `import`; that reader never reaches the construct under test.

`f01b`–`f01d` approximate the widths and signedness of their sites rather than copying them. For example, the real `tmp0_p1` in `ksl_add_rom.sv` is unsigned 6-bit; `f01d`'s operand is signed 5-bit. So "right" says those cast positions parse and compute as `$signed()` would. It does not say the real sites compute correctly. The whole-block Lite OPL2 run below elaborates the real sites but checks no values.

### Narrowing the `signed'()` results

- **Lite's rejection is the unary minus.** `-signed'(x)` is rejected in any position (`bisect/neg_*.sv`, `logs/bisect-neg-cast.log`). The plain cast, a cast in a `?:` arm and `-(signed'(x))` all parse. `-(signed'(x))` is also accepted by all three Yosys locks and Verilator (`logs/bisect-yosys74-and-neg.log`), and by Pro at its default input version (`logs/bisect-neg-cast.log`). So `vibrato.sv:73` is the only OPL2 `signed'()` site that Lite rejects.
- **Lite's misread needs `int`-typed operands** (`bisect/h_*.sv`, `logs/bisect-signed-cast.log`):
  - With untyped localparams, in a package or a module, Lite computes 0 as it should.
  - With `int`-typed localparams in either place, `signed'(D - S - 2) < 0` is false and Lite takes the other arm.
  - The same expression with `$signed(D - S - 2)` (`h_mod_int_12_dollar`, `h_pkg_int_16_dollar`), or with no cast at all (`h_pkg_int_12_nocast`), is right.
  - Only this one shape was tested: a cast compared with 0 in a localparam ternary.
- **The shipped OPL2 is not affected.** `opl2_pkg.sv:79` uses untyped localparams (`DAC_OUTPUT_WIDTH = 16`, `SAMPLE_WIDTH = 16`), and `f01g` reproduces it with Lite giving the right shift.

### Yosys: the new bug and the shared lock

- **yosys#74.** The pc386 and i386 locks reject a cast to a package-imported type (wildcard or named import, in the header or the body) when the cast comes after a procedural `for` loop that declares its own loop variable. The error is "Static cast with non constant expression!", and the operand does not matter. The cast passes when:
  - the package scope is written, `p::e_t'(x)`, even with the import kept;
  - the loop is removed;
  - the loop comes after the cast.

  The repros are `bisect/yosys74_*.sv`, with results in `logs/bisect-yosys74-and-neg.log`; `yosys74_constant_operand.sv` and `yosys74_loop_after_cast.sv` show the operand and loop-order cases. This is a different path from yosys#72 (a cast as a `case` selector). A search of every `.sv` under `sources/misteross` that imports a package finds no cast to an unscoped typedef, so no FES RTL hits it. Run over `bisect/`, the same search finds the three failing import repros, so it does detect the pattern. The issue has a comment with the widened trigger.
- **The shared lock (`3a5accbd`) has no module-level `import`.** It rejects `module X import pkg::*;`, a named header import, and an `import` in the module body, all with "unexpected TOK_IMPORT". Every form that imports fails there before the construct under test is read. Its other cells match the newer locks.

## OPL2 tree and i386 modules

**File by file** (`sweep.sh`, `quartus_map --analyze_file`, Lite at SV2005, each file with its package in the project). Quartus stops at the first errors in a file, so a failing file's log names only its first failing constructs. `--analyze_file` is parse-only.

| Tree | Result |
| --- | --- |
| OPL2 (`fes-pc386/vendor/opl2_fpga`, 20 files) | Only `vibrato.sv` fails: line 73 (10170). The other 19 parse, including `control_operators.sv`, whose `` `ifndef SYNTHESIS `` assertion Quartus reads because it does not define `SYNTHESIS`. |
| `fes_i386_l1i.sv` | 10170 at each inline genvar loop (lines 195, 225, 259, …) |
| `fes_i386_qalign.sv` | 10170 at the inline genvar loops (79, 156); then 10149, `j` already declared, at 156 |
| `fes_i386_tlb.sv` | 10170 at the inline genvar loops (36, 76); then 10149, `k` already declared, at 76 |
| `fes_i386_pkg.sv` | parses |

The 10149 errors come after Lite's recovery from the syntax errors on the same loops, so this census cannot tell whether they are a separate rule. Pro has no `--analyze_file` (error 23024), so Pro was run on whole blocks instead.

**Whole blocks** (`elab.sh`, `--analysis_and_elaboration`; `logs/elab.log`):

| Block | Reader | Result |
| --- | --- | --- |
| `pc386_opl2` with the 20 OPL2 files and `pc386_rate.sv`, `vibrato.sv:73` rewritten to `$signed()` in a scratch copy | Lite SV2005 | Elaborates. No errors and no critical warnings; one warning, 10958 (`unique case` made complete) at `calc_envelope_shift.sv:156`. So line 73 is the only parse or elaboration error between Lite and a native OPL2 read. Values were not checked. |
| `fes_i386_tlb` | Pro SV2009 | Elaborates (inline genvars, `IW'(k)`). |
| `fes_i386_qalign` | Pro SV2009 | Elaborates (inline genvars, the three `(* no_carry *)` sites at lines 162–164). |
| `fes_i386_l1i` with `fes_i386_tlb` | Pro SV2009 | Elaborates. |

The only critical warning in the Pro runs is 20759 (use the Reset Release IP in Agilex 5 designs), which belongs to the board shell, not the RTL.

## P3: inferred true-dual-port RAM against altsyncram

The design doc asks S1a whether 17.0.2 infers the same RAM from the `FES_RAM_TDP` form, at an equal or smaller fit, as from the `FES_VENDOR_RAM` altsyncram form.

**Setup** (`tdp/`, `logs/tdp-fit.log`):

- **Shape:** the 720p framebuffer, 49,152 words of 4 bits on a 16-bit address, with two independent clocks.
- **Inferred form:** `(* ramstyle = "M10K, no_rw_check" *)`, with registered reads and new data on a same-port read during write.
- **Vendor form:** `coleco_video_dpram.v`'s altsyncram settings (BIDIR_DUAL_PORT, `NEW_DATA_NO_NBE_READ`, mixed ports `DONT_CARE`), but with both outputs unregistered so each port has one registered read stage, as P3's form does.
- **Build:** both forms sit in the same harness, with every port registered and virtual pins. The constraint is 200 MHz on each clock, as asynchronous groups. Lite map, fit and STA, seed 1.

| Form | RAM | ALMs | Registers | Read during write | Slow 100 °C Fmax (clk_a / clk_b) | Slow −40 °C Fmax (clk_a / clk_b) | Worst slack |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Inferred `FES_RAM_TDP` | 24 M10K, True Dual Port, 196,608 bits | 55 | 58 | `NEW_DATA_NO_NBE_READ` on both ports, no pass-through logic | 212.9 / 222.87 MHz | 205.89 / 213.95 MHz | +0.142 ns |
| altsyncram | 24 M10K, True Dual Port, 196,608 bits | 55 | 56 | `NEW_DATA_NO_NBE_READ` on both ports | 218.91 / 204.62 MHz | 213.95 / 197.94 MHz | −0.052 ns (−40 °C setup, clk_b) |

**Decision.** The inferred form gives the same RAM in the same blocks, with the same ALMs and the same read-during-write modes. At map both have 56 registers, and the inferred form uses fewer ALUTs (40 against 48, `tdp.map.rpt`); the fitter adds 2 registers to the inferred form. At seed 1 the inferred form meets the 200 MHz constraint everywhere and the altsyncram form misses it by 0.052 ns at −40 °C. One seed cannot rank them on timing, but neither is worse on the doc's measures. That meets the design doc's "equal or smaller fit" condition for this shape. So on Lite, P3 can use the inferred form and record the fit, as the doc allows.

Two things stay open for S11:

- **Every wrapper still needs its own measurement**, especially the ZX81 ROM with `INIT_FILE`. The doc already requires this for each producer that reads a converted wrapper.
- **The Icarus Quartus sims still need `FES_VENDOR_RAM`** for the `altera_mf.v` model. That is about simulation, not synthesis.

## What this decides

**`FES_HAS_SIGNED_CAST`: not added.**
- Lite rejects one form, `-signed'(…)`, at one site. Rewriting it as `-(signed'(…))` or `$signed(…)` is accepted by every reader.
- A macro cannot fix the silent `int` misread anyway: the form parses, so nothing could switch on it. `$signed()` and a plain expression are both right in the misread shape.
- The common-subset rule is therefore a source rule: files that Lite reads write `$signed()`, not `signed'()`. Only `$signed()` was tested in place of the misread form, so the rule bans `signed'()` outright rather than guessing which other uses are safe.
- No file Lite reads natively has a `signed'()` today: pc386 reaches Lite through Yosys. If S13 ever makes Lite read OPL2 natively, only `vibrato.sv:73` stops Lite. Under this rule, though, all five OPL2 sites (seven casts: `vibrato.sv:73`, `calc_phase_inc.sv:70`, `operator.sv:230`, `ksl_add_rom.sv:86`, `opl2_pkg.sv:79`) move to `$signed()` as one D5 vendored edit, recorded in `source.json` `local_changes`.

**`FES_NO_CARRY`: not added.** Both Quartus editions accept `(* no_carry *)` on a two-operand `+` and compute the sum. Lite only warns that the attribute is unknown (10335). Pro elaborated `fes_i386_qalign.sv`'s three sites. The other i386 sites use shapes no Quartus read here, such as chained attributes and an attribute after a ternary. The design doc already keeps the i386 off any native Quartus front end, so no fence is needed; a future native read would check those shapes.

**Final common-subset list (17.0.2 rejections).** These apply only to files Lite reads as SystemVerilog; Pro reads them all.
- **Genvar loops:** declare the genvar before the loop (`genvar i;`), not in the loop header (10170, even inside `generate`). Wrap the loop in `generate`/`endgenerate` (10170 without). That rewrite (`f02c`) is accepted by every reader.
- **Generate `if` and `case`:** wrap them in `generate`/`endgenerate` (10170 without; `f03c`, wrapped, is accepted by every reader). Only `if` was tested; `case` follows the same grammar rule.
- **Assignment patterns:** a named key must not name a signal of the enclosing module (10734).
- **Sign casts:** no `signed'()`; use `$signed()`. `-signed'(…)` is rejected (10170), and `signed'()` on `int`-typed constants is silently wrong.

- **Struct member index (reported, not reproduced):** a variable index into a packed-struct member whose name matches a module signal. `quartus_fes_i386.py:11-14` reports Lite misreading it in the i386. `f04` read correctly, so the failing shape is unknown. The rule stays on the list until a census finds the shape or shows it safe.

For the Yosys locks:
- **Until yosys#74 is fixed,** a cast to a package-imported type writes the package scope (`pkg::type_t'(x)`) when it comes after a loop-scoped `for` variable.
- **Shared code read by the shared lock** (`3a5accbd`, every core but pc386, i386 and Coleco) uses no module-level `import`. It refers to package items as `pkg::name` until that lock moves. Treat Coleco's untested `5391eeb1` the same way.

**Header check mechanism: a missing include, not an undefined macro.**
- Quartus does not stop on an undefined macro: Lite gives Critical Warning 10191, Pro Critical Warning 13432, and both expand it to nothing.
- A failed check should instead write `` `include "FES_TOOLCHAIN_ERROR_<reason>.svh" ``. All seven readers tested stop on it, and every one prints the file name, so the reason reaches the log.
- The doc's fallback, instantiating an undefined module, is not needed.
- `$error` in a generate block cannot serve: Lite has no elaboration `$error` (a syntax error even inside `generate`), and Verilator stops only while `USERERROR` stays fatal.
- S8's header test should also check that no file named `FES_TOOLCHAIN_ERROR_*` exists on any include path.
- Icarus is untested. S8 must run the include check under Icarus before relying on it there.

**`SYNTHESIS`.** Neither Quartus edition defines it, so a Quartus front end reads `` `ifndef SYNTHESIS `` blocks; OPL2's assertion at `control_operators.sv:444` parses under Lite. This supports the design's rule that the header never defines `SYNTHESIS`.

**Lite lock move: not worth proposing now.**
- No newer Lite is installed, so this record cannot test one.
- Pro 26.3 accepts every form Lite rejects. That suggests a newer release fixes them, but Pro's front end is not the same product as Lite's.
- No lane in the plan needs the change. The i386 reaches Quartus through Yosys, and native-Lite OPL2 needs only the `$signed()` edit above.
- S15 stays conditional. If S13 or S14 ever needs Lite to read the i386 natively, test a newer Lite then, against the 36 genvar sites.

**P3:** the inferred `FES_RAM_TDP` form, as above.

## Not verified

- **Icarus:** not run. Every Icarus cell, including the header-check mechanism, is open.
- **A newer Lite:** none is installed.
- **Verilator build:** the lock's Verilator (commit `5e4151e3`) was not built. The system 5.032 was used, which is the version the design doc names.
- **Lite at a later SV standard:** Lite was run only at SV2005, the only SystemVerilog setting 17.0.2 has.
- **i386 elaboration under Lite:** not attempted, since every module stops at parse.
- **Elaboration-level errors in the 19 OPL2 files:** the per-file sweep is parse-only. The whole-block Lite elaboration covers them only in the `pc386_opl2` configuration, with `vibrato.sv:73` rewritten, and checks no values.
- **The struct-index misread:** not reproduced. The shape `quartus_fes_i386.py` describes was not found.
- **P3 seeds and shapes:** one seed and one shape. The ZX81 ROM with `INIT_FILE` and the MIF path were not tried.
- **Coleco's Yosys lock (`5391eeb1`):** not run; no build is installed.
- **`no_carry` shapes:** the chained and ternary `(* no_carry *)` shapes were not read by any Quartus.

## Files

- `forms/`: the minimal forms, `census_pkg.sv` and `tb_f04.sv`.
- `bisect/`:
  - `h_*.sv`: the `int`-typed `signed'()` misread and its `$signed()` and no-cast twins;
  - `neg_*.sv`: the unary-minus rejection;
  - `yosys74_*.sv`: the yosys#74 repros and passing variants, including the constant-operand and loop-after-cast cases.
- `tdp/`: the P3 harness, both RAM forms, the SDC and `tdp.sh` (map, fit, STA).
- `census.sh` and `run_all.sh`: one form through one reader, and the matrix (three jobs at a time).
  - Set `YOSYS_PC386`, `YOSYS_I386` and `YOSYS_SHARED` to the locked binaries first.
  - `run_all.sh` refuses tools other than the ones this record used: the three Yosys binaries by sha256, Verilator 5.032, Lite 17.0.2 and Pro 26.3.0. It writes their identities to `<out>/tools.txt`.
  - A subset (`READERS`, `FORMS`) needs its own `TAG`; only the full matrix writes `ALL_DONE.all`, and every old marker is removed first.
- `summarize.py`: writes `results.md` from `census.sh` output, one row per form in `forms/`.
  - A cell is a result only when the reader's own verdict is in its output: Quartus's "was successful" line and summary file, or an `Error (NNNNN)` message; Yosys's "End of script." or an `ERROR:` message; Verilator's report banner or a `%Error` message.
  - Anything else (a timeout, a crash, a tool that did not run, a missing report, `f04`'s bench or `f10`'s preprocess not finishing) shows as **not run**, never as a rejection.
  - `summarize.py --check <out>` exits 1 unless every selected cell is a result and every reader accepts the control form `f08_no_carry`; `run_all.sh` writes its marker only after that check passes.
- `sweep.sh` (Lite only; Pro has no `--analyze_file`) and `elab.sh`: the per-file sweep and the whole-block elaboration. Both resolve their inputs to absolute paths, and exit 1 (writing `notrun`) for any run without a Quartus verdict.
- `logs/`: the message lines of every run, with paths shortened:
  - `forms-<reader>.log`, `sweep-*.log`, `elab.log`;
  - `bisect-*.log`;
  - `lite-sv2009-setting.log`, `tdp-fit.log`.
  - `tools.txt`: the tool identities `run_all.sh` checked for the run behind `results.md`.

To rerun: `run_all.sh <out>`, then `summarize.py <out>`. `results.md` is the output of one full `run_all.sh` run of this harness, which wrote `ALL_DONE.all`. Rerun `bisect/` and `tdp/` by hand: each bisect file is one top for `quartus_map`, and `tdp/tdp.sh <work> inferred|vendor` fits one form.
