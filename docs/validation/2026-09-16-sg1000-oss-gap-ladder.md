# FES SG-1000 OSS / nextpnr / Mistral / formic gap ladder

This note is the step-2 inventory for package `fes.sg1000`. It diffs the
working Quartus 17.0.2 oracle recipe against the Coleco Yosys/nextpnr/Mistral
path, lists every support gap with file and symbol evidence, and records the
misteross test ladder. The ladder **is** the schedule.

It does not claim an OSS/HIP format-2 package, FES parent pin, or kit HIL.

## Sources compared

| Lane | Tree / recipe | Constraints | Clocks | Tool pin |
| --- | --- | --- | --- | --- |
| SG-1000 Quartus (working) | `scripts/build_fes_sg1000.py` (`QUARTUS=1`, seed 1) | `cores/fes-sg1000/constraints.qsf` | `cores/fes-sg1000/clocks.sdc` | Quartus Prime Lite 17.0.2 |
| Coleco OSS (sibling) | `scripts/build_fes_coleco_oss.py` (`TV80_REFRESH=1`, `FES_COLECO_OSS=1`, seed 4, `--router gpu`) | `cores/fes-coleco/constraints-oss.qsf` | `cores/fes-coleco/clocks-oss.sdc` | `cores/fes-coleco/toolchain.lock`: Yosys `da6373c0`, nextpnr `2d3c216`, Mistral `b28e30a` |
| misteross mainline OSS | generic `make oss` / `toolchain.lock` | experiment QSF | experiment SDC | Yosys `ec34fcf3`, nextpnr `9cbbf735`, Mistral `b28e30a` |

Powerboat worktree `/home/deano/fes-worktrees/misteross-sg1000-quartus` at
`1308f94` (dirty SG-1000 tree) is the source of truth for
`cores/fes-sg1000`. Mac hashes for the Quartus recipe files match Powerboat.

SG-1000 Quartus `clocks.sdc` is byte-identical to Coleco Quartus `clocks.sdc`.
SG-1000 Quartus `constraints.qsf` is byte-identical to Coleco Quartus
`constraints.qsf`. Shared RTL is Coleco: TV80, `coleco_vdp`, `coleco_dpram`,
`coleco_video_dpram`, `fes_computer_gp`, `sys_pll`, `pixel_pll`. SG-1000-only
RTL is `sg1000_machine.sv` and `cores/fes-sg1000/rtl/top.v`.

Step-1 Quartus compile-only RBF remains
`cc6888089410b378cd6c246aeb6a1b7b60bee2ee19697a0bf58f227832986984`
(`build/fes-sg1000-quartus/core.rbf`). This job does not redo Quartus.

## Gap inventory

Each row is a present support gap, not a plan. Product-scope items that are
not Yosys/nextpnr/Mistral limits are labelled as such.

### G1. OSS producer files exist; HIP seal is not claimed

| Evidence | Present state |
| --- | --- |
| `scripts/build_fes_sg1000_oss.py` | present; Yosys defines `TV80_REFRESH=1`, `FES_SG1000_OSS=1`, `FES_COLECO_OSS=1`; `--synth-only` skips clean-tree seal |
| `make build-fes-sg1000` / `toolchain-fes-sg1000` | present; lock is `cores/fes-sg1000/toolchain.lock` |
| `cores/fes-sg1000/toolchain.lock` | byte-identical Coleco lock (Yosys `da6373c0`, nextpnr `2d3c216`, Mistral `b28e30a`) |
| `tests/test_build_fes_sg1000.py` | asserts both Quartus and OSS entrypoints, dual defines, and the Coleco pin |
| Coleco sibling | unchanged |

The producer path is scaffolded. Full HIP `--router gpu` route, format-2
seal, FES parent pin and kit HIL are not claimed. Seed 4 is copied from
Coleco and is not SG-1000 route evidence.

### G2. No accepted OSS constraint subset for SG-1000

Coleco OSS drops Quartus-only SDC/QSF forms:

```text
cores/fes-coleco/clocks-oss.sdc
  create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
  # nextpnr derives 52 MHz / 74.25 MHz from altera_pll cells

cores/fes-coleco/constraints-oss.qsf
  pin IO_STANDARD assignments only
  # omits HPS_LOCATION; comment says QSF HPS_LOCATION does not place internal OSS cells
```

SG-1000 Quartus still has the full forms. The OSS recipe now copies the
Coleco subset to `cores/fes-sg1000/clocks-oss.sdc` and
`constraints-oss.qsf`. Quartus files remain:

```text
cores/fes-sg1000/clocks.sdc
  create_clock ...
  derive_pll_clocks
  derive_clock_uncertainty
  set_clock_groups -asynchronous -group [get_clocks {*system_clock*}] -group [get_clocks {*video_clock*}]

cores/fes-sg1000/constraints.qsf
  set_instance_assignment -name HPS_LOCATION HPSINTERFACEPERIPHERALI2C_X52_Y60_N111 -entity top -to hdmi_i2c
```

`scripts/build_fes_sg1000.py` pins `QSF_PINS` / `SDC` to those Quartus files
and requires `I2C_SITE = "HPSINTERFACEPERIPHERALI2C_X52_Y60_N111"` in the
generated project QSF.

This is no longer a missing-file gap. On Coleco nextpnr `2d3c216` the SDC
commands themselves are no longer a hard parser miss: rung R9 routed a
50→25 MHz PLL with `derive_pll_clocks` / `get_clocks` / `set_clock_groups`
and printed `PASS`. Coleco production still ships `clocks-oss.sdc`; the
SG-1000 OSS recipe copies that subset.

### G3. `FES_SG1000_OSS` still does not select Coleco M10K/VDP OSS shapes; the producer now passes both defines

Registered media in the SG-1000 machine is gated on `FES_SG1000_OSS` (or
`QUARTUS`):

```11:15:cores/fes-sg1000/rtl/sg1000_machine.sv
`ifdef FES_SG1000_OSS
`define FES_SG1000_REGISTERED_MEDIA
`elsif QUARTUS
`define FES_SG1000_REGISTERED_MEDIA
`endif
```

The shared wrappers that Yosys must map are gated on **`FES_COLECO_OSS`**, not
`FES_SG1000_OSS`:

```76:78:cores/fes-coleco/rtl/coleco_dpram.v
`ifdef FES_COLECO_OSS
    // Registered true dual-port M10K. Combo-read 16 KB tables explode ABC.
    (* ram_style = "m10k_tdp" *) reg [DATAWIDTH-1:0] ram [0:NUMWORDS-1];
```

```31:38:cores/fes-coleco/rtl/coleco_vdp.sv
`ifdef FES_COLECO_OSS
`define FES_COLECO_REGISTERED_VDP
`elsif QUARTUS
`define FES_COLECO_REGISTERED_VDP
`endif
`ifndef FES_COLECO_REGISTERED_VDP
    (* ramstyle = "M10K" *) reg [7:0] vram [0:16383];
```

`FES_SG1000_OSS` does not appear in `coleco_dpram.v` or `coleco_vdp.sv`.
`make sim-fes-sg1000` still passes `-DTV80_REFRESH=1` only.
`make sim-fes-sg1000-oss` and `scripts/build_fes_sg1000_oss.py` pass
`-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1`. The default sim is unchanged.

Yosys `da6373c0` wrap of a 16 KiB `coleco_dpram` this session:

| Defines | Result |
| --- | --- |
| none | `ERROR: no valid mapping found for memory wrap_dpram.u.ram` (combo-read, `ramstyle = m10k`, no address/output FF) |
| `-DFES_SG1000_OSS=1` | same mapping error |
| `-DFES_COLECO_OSS=1` | 16 `MISTRAL_M10K_TDP` in 0.41 s |

An SG-1000 OSS synth that only defines `FES_SG1000_OSS` still takes the
unmappable combo-read RAM and the single-array VDP. Dual defines close
this for the SG-1000 producer and OSS sim; do not drop `FES_COLECO_OSS`.

### G4. Combo-read block RAM / multi-read VRAM (Yosys + Mistral)

Default `coleco_dpram` (simulation lane, no `QUARTUS`, no `FES_COLECO_OSS`)
does combinational `assign q_a = ram[address_a]`. Locked Yosys
`synth_intel_alm -nolutram` then fails closed (`no valid mapping found`).
Coleco README / `docs/architecture.md` record the historical ABC explosion
on unmappable 16 KiB tables.

The VDP default is one 16 KiB array with a CPU port plus raster reads.
Both compiler lanes already work around that with four coherent
`coleco_dpram` copies (`vram_name_block`, `vram_pattern_block`,
`vram_color_block`, `vram_sprite_block`) under `FES_COLECO_REGISTERED_VDP`.
SG-1000 Quartus gets that path via `QUARTUS=1`. OSS does not, unless
`FES_COLECO_OSS` is defined.

### G5. Packed 4-bit sprite line banks need the Coleco Yosys pin

Sprite line banks instantiate `coleco_video_dpram` 256×4
(`coleco_vdp.sv` `sprite_pixel_ram_a` / `_b`, `(* ramstyle = "M10K" *)` in
the non-Quartus branch). Coleco lock rationale: Yosys `da6373c0` keeps the
registered packed M10K form; mainline `ec34fcf3` reclassifies it as
`CFG_ASYNC_READ` / async TDP, which nextpnr `2d3c216` rejects.

Isolated wrap this session:

| Yosys | Mapper template | Cells |
| --- | --- | --- |
| Coleco `da6373c0` | `$__MISTRAL_M10K_ACLR_BYTE_` | 2 `MISTRAL_M10K` |
| Mainline `ec34fcf3` | `$__MISTRAL_M10K_ASYNC_TDP_` | 1 `MISTRAL_M10K_TDP` |

SG-1000 OSS must not silently consume repository `toolchain.lock`. It needs
the Coleco compatibility pin (or a later mainline pair that nextpnr accepts
for this shape). nextpnr tests covering the family live under
`mistral/tests/m10k/` in the DeanoC nextpnr tree (`async_read`, `async_tdp`,
`acceptance`, …), not in `cores/fes-sg1000`.

### G6. HDMI I²C: Quartus tri-state vs OSS `MISTRAL_IO` + BEL

`cores/fes-sg1000/rtl/top.v` already contains both boundaries:

- `ifdef QUARTUS`: `cyclonev_hps_interface_peripheral_i2c` with assign-to-`z`
- `else`: `MISTRAL_IO` pads `hdmi_scl_pad` / `hdmi_sda_pad` and
  `(* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)`

That RTL is ready. SG-1000 OSS QSF now copies Coleco: pin IO_STANDARD
assignments only, no `HPS_LOCATION`. Quartus still includes
`HPS_LOCATION`. nextpnr `2d3c216` can translate HPS_LOCATION→BEL
(misteross experiment `850_hps_location`, nextpnr `mistral/tests/hps_i2c/`).
The SG-1000 OSS recipe uses the RTL BEL plus `constraints-oss.qsf` without
`HPS_LOCATION`.

### G7. `TV80_REFRESH=1` is an OSS frontend requirement, not a Quartus macro

- `cores/fes-coleco/rtl/tv80/tv80_core.v` uses `` `ifdef TV80_REFRESH ``
- Coleco OSS Yosys program: `-DTV80_REFRESH=1 -DFES_COLECO_OSS=1`
- `make sim-fes-sg1000` already passes `-DTV80_REFRESH=1`
- `scripts/build_fes_sg1000.py` `VERILOG_MACRO` is only `QUARTUS=1` and
  `FES_SG1000_BUILD_ID=...` — Quartus does not define `TV80_REFRESH`

This is an OSS language/frontend choice (Verilog TV80 vs any VHDL T80 path),
not a missing nextpnr BEL. The future OSS recipe must pass `TV80_REFRESH=1`.

### G8. Dual `altera_pll` 52 MHz + 74.25 MHz is already packed; the recipe is missing

Shared wrappers:

- `cores/fes-coleco/rtl/sys_pll.v` — `output_clock_frequency0("52.0 MHz")`,
  `fractional_vco_multiplier("false")`
- `cores/fes-coleco/rtl/pixel_pll.v` — `output_clock_frequency0("74.25 MHz")`,
  `fractional_vco_multiplier("true")`

Coleco OSS `build_fes_coleco_oss.py` requires route-log `50 MHz -> 52 MHz`
and structured fmax rows at 52.0 / 74.25. misteross experiments `760_pll_52`
and `610_pll_frac_7425`, plus nextpnr `mistral/tests/pll/`, already cover the
profiles. SG-1000 does not lack a PLL BEL; it lacks the OSS recipe that
asks nextpnr for those cells with `--freq 74.25`.

### G9. HIP GPU router, seed, and CPU-fallback reject

Coleco production (`scripts/build_fes_coleco_oss.py`):

- `ROUTER = "gpu"`, `SEED = 4`, `--timing-allow-fail`, no `--tmg-ripup`
- `COLECO_GPU_BACKEND = "hip"`, architectures `gfx1100;gfx1201`
- `_require_gpu_backend` rejects `backend cpu-reference`

SG-1000 now copies that recipe (`ROUTER = "gpu"`, `SEED = 4`, HIP
`gfx1100;gfx1201`, CPU-fallback reject). Seed 4 is unproven on the SG-1000
netlist; R13 has not run. Powerboat already has the two HIP cache slots:

| Slot | Yosys | nextpnr | Role |
| --- | --- | --- | --- |
| `.../slots/ddcd4905...` | `da6373c0` | `2d3c216` | Coleco lock (required for G5) |
| `.../slots/cc150969...` | `ec34fcf3` | `9cbbf735` | repository `toolchain.lock` (wrong mapper for sprite banks) |

nextpnr GPU-router fixtures: `mistral/tests/gpurouter/`.

### G10. OSS machine sim exists; no board/GP/VDP-only SG-1000 lane

Coleco: `make sim-fes-coleco-oss` builds GP, VDP, machine, and board with
`-DFES_COLECO_OSS=1` and registered-media host timing in the C++ harness.

SG-1000: `make sim-fes-sg1000-oss` builds the machine with
`-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1`. There is still no
`cores/fes-sg1000/sim/board_tb.cpp` / `board_models.v`. Default
`make sim-fes-sg1000` does not compile the OSS RAM/VDP branches.

### G11. Formic: no in-tree or Powerboat tree

Searched this worktree, Powerboat `/home/deano`, `/opt`, `/usr/local`, and
public DeanoC GitHub repositories. No formic package, worktree, or binary.

Coleco/ZX81 OSS gaps currently live as:

- misteross experiments (`530`/`540` TDP, `480`/`490` independent-clock SDP,
  `610` 74.25 MHz, `760` 52 MHz, `780` Quartus SDC, `850` HPS_LOCATION, …)
- DeanoC nextpnr `mistral/tests/{pll,m10k,hps_i2c,quartus_constraints,gpurouter}`
- DeanoC Yosys PRs (`da6373c0` vs `ec34fcf3`)

If a separate out-of-tree formic development exists, it is not reachable from
this job and cannot be a current SG-1000 rung. Recorded as **blocked / absent**,
not as a silent skip.

### G12. Product-scope, not a Yosys/nextpnr/Mistral miss

- 16 KiB mailbox aperture vs retail 32/48 KiB SG-1000 images
- no SN76489 audio
- no SC-3000 keyboard

These stay outside the first OSS package, as they are outside the Quartus
first slice.

### G13. Later jobs, not this ladder's finish line

HIP format-2 seal for `fes.sg1000`, FES parent pin, and kit HIL remain
later jobs. R12 synth-only does not replace R13/R14.

## Formic / nextpnr / Mistral map

What an SG-1000 OSS build would ask of the toolchain, and where that is
tested today (none of these are SG-1000-owned tests yet):

| Need | Tool | Current home | SG-1000 status |
| --- | --- | --- | --- |
| 16 KiB registered `ram_style="m10k_tdp"` | Yosys `da6373c0` | Coleco `coleco_dpram.v`; nextpnr `mistral/tests/m10k/` | Mapper works when `FES_COLECO_OSS=1` (R7); SG-1000 producer/sim now pass that define (R10/R12) |
| Combo-read 16 KiB `ramstyle=M10K` | Yosys | fails closed | Confirmed (rung R5) |
| 256×4 registered `ramstyle=M10K` sprite banks | Yosys pin split | Coleco lock vs mainline | Confirmed (rung R8); SG-1000 lock is a Coleco byte copy |
| Four-copy VDP VRAM | RTL workaround, not a new BEL | `coleco_vdp.sv` `FES_COLECO_REGISTERED_VDP` | Needs `FES_COLECO_OSS`; producer/sim pass it |
| 50→52 MHz integer PLL | nextpnr + Mistral | `sys_pll.v`; `mistral/tests/pll/`; exp `760_pll_52` | BEL present; Yosys emits 2 `altera_pll` (R12); HIP route not run |
| 50→74.25 MHz fractional PLL | nextpnr + Mistral | `pixel_pll.v`; exp `610_pll_frac_7425` | BEL present; HIP route not run |
| `MISTRAL_IO` HDMI I²C + I2C BEL 52.60.0 | nextpnr | `top.v` else branch; `mistral/tests/hps_i2c/` | RTL + OSS QSF present; 2 `MISTRAL_IO` in R12 synth |
| Quartus SDC subset | nextpnr | `mistral/tests/quartus_constraints/` | Parser OK on Coleco pin (R9); recipe uses `clocks-oss.sdc` |
| HIP `--router gpu` | nextpnr | `mistral/tests/gpurouter/`; Coleco recipe | Recipe present; R13 not run |
| Mistral bitstream tables | Mistral `b28e30a` | both locks | Unchanged; no SG-1000-specific table gap identified |
| formic | n/a | **no tree** | G11 blocked |

`mistral-cv validate` on the Coleco slot dumped reverse-not-found routing
rows and then segfaulted. That is not treated as an SG-1000-specific gap;
Coleco already seals with this Mistral commit.

## Ordered misteross test ladder

Pass criteria are for the rung, not for “OSS package done”. Later rungs must
not start until their dependencies pass.

| Rung | Test / script | Pass criteria | Depends on | Powerboat now? |
| --- | ---: | --- | --- | --- |
| R1 | `python3 -m unittest tests.test_build_fes_sg1000 -v` | tests OK (now includes OSS entrypoints/dual defines) | — | **yes** |
| R2 | `make sg1000-diagnostic` | `graphics-i.rom` 998 B sha256 `2ed0543d…2ff131`; 16 KiB pad `f02f3079…bfd5ee`; entry `0x0000` | — | **yes** |
| R3 | `make sim-fes-sg1000 VERILATOR=<cached 5.051>` | `FES SG-1000 machine checks passed` (media, 1 KiB mirror, DC/DD, diagnostic `A5`/`DC=ff`) | R2 | **yes** (unset `FES_TOOLCHAIN_CACHE_ROOT`; pass absolute Verilator) |
| R4 | Static OSS-absence inventory (this doc’s file list) | listed OSS files absent; Coleco OSS files present; `FES_SG1000_OSS` not in `coleco_dpram.v` | — | **yes** |
| R5 | Yosys `da6373c0` wrap 16 KiB `coleco_dpram` **without** OSS defines | `ERROR: no valid mapping found for memory …ram` | Coleco HIP slot | **yes** |
| R6 | Same wrap with only `-DFES_SG1000_OSS=1` | same mapping error (glue gap G3) | R5 | **yes** |
| R7 | Same wrap with `-DFES_COLECO_OSS=1` | 16 `MISTRAL_M10K_TDP`; Yosys exit 0 | Coleco HIP slot | **yes** |
| R8 | Yosys 256×4 `coleco_video_dpram` on `da6373c0` vs `ec34fcf3` | Coleco pin → `$__MISTRAL_M10K_ACLR_BYTE_` / 2 `MISTRAL_M10K`; mainline → `$__MISTRAL_M10K_ASYNC_TDP_` / 1 `MISTRAL_M10K_TDP` | both HIP slots | **yes** |
| R9 | Coleco nextpnr `mistral/tests/quartus_constraints/check.py` | `PASS: Quartus SDC/QSF subset routed one PLL and met 25 MHz` | Coleco HIP slot | **yes** (sibling fixture; not an SG-1000 netlist) |
| R10 | `make sim-fes-sg1000-oss` with `-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1` | machine checks pass on registered media + registered VDP/RAM | G3 Makefile/defines; R3 | **yes** |
| R11 | Optional `make sim-fes-coleco-oss` | Coleco OSS sim still green (shared modules) | Coleco tree | **yes but long**; not required |
| R12 | `scripts/build_fes_sg1000_oss.py --synth-only` Yosys of `top` | synth.json contains 2 `altera_pll`, `MISTRAL_IO`, `MISTRAL_M10K`/`_TDP`, no forbidden DSP/MLAB | G1–G7, R7, R10 | **yes** (synth-only; not sealed) |
| R13 | nextpnr HIP route of that netlist (`--router gpu`, Coleco pin, seed TBD) | no unrouted nets; live HIP backend; 52 MHz and 74.25 MHz fmax rows pass | R12, G9 | **no** |
| R14 | Format-2 OSS seal | clean tree, `build-inputs.json`, manifest `fes.sg1000` | R13 | **no** — not this job |
| R15 | formic execution of G4/G5/G8 | formic tree exists and names the failing shapes | G11 | **no** — no formic tree |
| R16 | FES pin / kit HIL | out of scope | R14 | **no** — do not start |

Schedule = this table. R10 and R12 synth-only are done. R13 HIP route and
R14 format-2 seal are not claimed.

## Execution this session (2026-09-16)

Host: Powerboat `192.168.10.202`, worktree
`/home/deano/fes-worktrees/misteross-sg1000-quartus` @ `1308f94` dirty.
Coleco tools:
`/home/deano/fes/out/cache/misteross-toolchains/slots/ddcd49051df430f25b58a05011bc3bf0379a33cf482384da288703d18bd1a8f4/install/bin/{yosys,nextpnr-mistral,verilator}`.
Mainline Yosys for R8:
`.../slots/cc150969dad68a4363678bf99ba81da80f16028175a0cf247f91f1372a7875f9/install/bin/yosys`.
Mac worktree `/Users/clawzai/Developer/misteross-wt-sg1000-quartus` ran R1–R2
as well. Probe logs: `/tmp/sg1000-gap-ladder/` on Powerboat.

| Rung | Result | Notes |
| --- | --- | --- |
| R1 | **PASS** | 7/7 on Mac and Powerboat |
| R2 | **PASS** | diagnostic hashes match step-1 bring-up |
| R3 | **PASS** | `FES SG-1000 machine checks passed`; Verilator `v5.050-268-g5e4151e3` |
| R4 | **PASS** | OSS producer/QSF/SDC/lock/board_tb absent; Coleco OSS files present; `iverilog` absent |
| R5 | **PASS** (gap confirmed) | Yosys mapping error on combo-read 16 KiB |
| R6 | **PASS** (gap confirmed) | `FES_SG1000_OSS` alone does not map |
| R7 | **PASS** | 16 `MISTRAL_M10K_TDP` with `FES_COLECO_OSS` |
| R8 | **PASS** (pin split confirmed) | `da6373c0` vs `ec34fcf3` mapper templates as in G5 |
| R9 | **PASS** | Coleco nextpnr SDC fixture; `clk25` achieved 296.65 MHz vs 25 MHz; `Program finished normally.` |
| R10 | **PASS** | `make sim-fes-sg1000-oss` Verilator 5.051; `FES SG-1000 machine checks passed`; both OSS defines |
| R11 | **not run** | optional Coleco OSS sim |
| R12 | **PASS** (synth-only) | Yosys `da6373c0`: 2 `altera_pll`, 2 `MISTRAL_IO`, 52 `MISTRAL_M10K`, 97 `MISTRAL_M10K_TDP`, zero forbidden DSP/MLAB; sealed=false |
| R13–R16 | **not run** | no HIP route; no format-2 seal; no formic; no FES pin/HIL |

`make sim-fes-sg1000-quartus` is **not runnable** on Powerboat (`iverilog`
absent). That is a vendor-model probe, not an OSS gap.

R12 wrote `build/fes-sg1000-oss/{synth.json,yosys.log,build-summary.json}`
on Powerboat. It does **not** seal a format-2 OSS package, does not run
nextpnr, and does not load a kit.

## Next rung after this GREEN

R13: HIP `--router gpu` of the R12 netlist on the Coleco pin (seed 4 is the
Coleco copy and is unproven on SG-1000). Do not FES-pin or kit-HIL until
R13/R14 pass.
