# FES SMS OSS / nextpnr / Mistral / formic gap ladder

This note is the step-2 inventory for package `fes.sms`. It diffs the
working Quartus 17.0.2 oracle recipe against the Coleco / SG-1000
Yosys/nextpnr/Mistral path, lists every support gap with file and symbol
evidence, and records the misteross test ladder. The ladder **is** the
schedule.

The OSS producer, Coleco-lock copy, OSS constraint subset, HIP `--router gpu`
route, and format-2 seal are in tree. It does not claim a FES parent pin or
kit HIL. Do not use `fes.mastersystem`.

## Sources compared

| Lane | Tree / recipe | Constraints | Clocks | Tool pin |
| --- | --- | --- | --- | --- |
| SMS Quartus (working) | `scripts/build_fes_sms.py` (`QUARTUS=1`, seed 1) | `cores/fes-sms/constraints.qsf` | `cores/fes-sms/clocks.sdc` | Quartus Prime Lite 17.0.2 |
| SG-1000 OSS (sibling) | `scripts/build_fes_sg1000_oss.py` (`TV80_REFRESH=1`, `FES_SG1000_OSS=1`, `FES_COLECO_OSS=1`, seed 4, `--router gpu`) | `cores/fes-sg1000/constraints-oss.qsf` | `cores/fes-sg1000/clocks-oss.sdc` | `cores/fes-sg1000/toolchain.lock`: Yosys `da6373c0`, nextpnr `2d3c216`, Mistral `b28e30a` |
| Coleco OSS (sibling) | `scripts/build_fes_coleco_oss.py` | `cores/fes-coleco/constraints-oss.qsf` | `cores/fes-coleco/clocks-oss.sdc` | `cores/fes-coleco/toolchain.lock` (byte-identical to SG-1000) |
| misteross mainline OSS | generic `make oss` / `toolchain.lock` | experiment QSF | experiment SDC | Yosys `ec34fcf3`, nextpnr `9cbbf735`, Mistral `b28e30a` |

Powerboat worktree `/home/deano/fes-worktrees/misteross-sms-quartus` at
`bbbcef4` (dirty SMS tree) is the source of truth for `cores/fes-sms`
hardware artifacts. Mac hashes for the Quartus recipe files match Powerboat.

SMS Quartus `clocks.sdc` is byte-identical to Coleco and SG-1000 Quartus
`clocks.sdc` (sha256 `cf6279c2429ff27bf15dbd5529fd0d895d88a22d715cc71940b74d6c6225760a`).
SMS Quartus `constraints.qsf` is byte-identical to Coleco and SG-1000 Quartus
`constraints.qsf` (sha256 `3c0e27262c4931bd3e9e17240e18c98beb48918cd525369664c357faed16d5fa`).
SG-1000 OSS `clocks-oss.sdc` / `constraints-oss.qsf` / `toolchain.lock` are
byte-identical to the Coleco copies.

Shared RTL is Coleco: TV80, `coleco_vdp`, `coleco_dpram`, `coleco_video_dpram`,
`fes_computer_gp`, `sys_pll`, `pixel_pll`, `coleco_video_720p`. SMS-only RTL
is `sms_machine.sv` and `cores/fes-sms/rtl/top.v`.

SMS-specific vs SG-1000: 8 KiB CPU RAM at `0xc000` mirrored at `0xe000`
(`coleco_dpram` `ADDRWIDTH(13)` / `NUMWORDS(8192)`), VDP IRQ on Z80 INT
(`.INT_n(vdp_irq_n)`, `.NMI_n(1'b1)`), no BIOS shim. SG-1000 uses 1 KiB RAM
(`ADDRWIDTH(10)` / `NUMWORDS(1024)`).

Step-1 Quartus compile-only RBF remains
`1868bbaacafb125a17efa573ebdd6f625acb501b3c403c15d7531b6cd863e171`
(`build/fes-sms-quartus/core.rbf`, 2,396,332 bytes). This job does not redo
Quartus.

## Gap inventory

Each row is a present support gap, not a plan. Product-scope items that are
not Yosys/nextpnr/Mistral limits are labelled as such.

### G1. OSS producer is in tree; HIP seal is not yet claimed

| Evidence | Present state |
| --- | --- |
| `scripts/build_fes_sms_oss.py` | present; SG-1000 copy (`TV80_REFRESH=1`, `FES_SMS_OSS=1`, `FES_COLECO_OSS=1`, seed 1, `--router gpu`, HIP `gfx1100;gfx1201`, CPU-fallback reject) |
| `make build-fes-sms` / `toolchain-fes-sms` | present |
| `cores/fes-sms/toolchain.lock` | Coleco/SG-1000 lock byte copy (Yosys `da6373c0`, nextpnr `2d3c216`, Mistral `b28e30a`) |
| `tests/test_build_fes_sms.py` | asserts Quartus + OSS sim + OSS producer entrypoints |
| Coleco / SG-1000 siblings | unchanged |

R12 synth-only, R13 HIP route, and R14 format-2 seal are done. FES parent
pin and kit HIL are not claimed.

### G2. OSS constraint subset is a Coleco byte copy

Coleco / SG-1000 OSS drop Quartus-only SDC/QSF forms:

```text
cores/fes-coleco/clocks-oss.sdc
  create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
  # nextpnr derives 52 MHz / 74.25 MHz from altera_pll cells

cores/fes-coleco/constraints-oss.qsf
  pin IO_STANDARD assignments only
  # omits HPS_LOCATION; comment says QSF HPS_LOCATION does not place internal OSS cells
```

SMS Quartus still has the full forms. `cores/fes-sms/clocks-oss.sdc` and
`constraints-oss.qsf` are byte copies of the Coleco OSS subset. Quartus files
remain:

```text
cores/fes-sms/clocks.sdc
  create_clock ...
  derive_pll_clocks
  derive_clock_uncertainty
  set_clock_groups -asynchronous -group [get_clocks {*system_clock*}] -group [get_clocks {*video_clock*}]

cores/fes-sms/constraints.qsf
  set_instance_assignment -name HPS_LOCATION HPSINTERFACEPERIPHERALI2C_X52_Y60_N111 -entity top -to hdmi_i2c
```

`scripts/build_fes_sms.py` pins `QSF_PINS` / `SDC` to those Quartus files
and requires `I2C_SITE = "HPSINTERFACEPERIPHERALI2C_X52_Y60_N111"` in the
generated project QSF. `scripts/build_fes_sms_oss.py` pins the Coleco OSS
subset.

On Coleco nextpnr `2d3c216` the Quartus SDC commands themselves are no longer
a hard parser miss: rung R9 routed a 50→25 MHz PLL with `derive_pll_clocks` /
`get_clocks` / `set_clock_groups` and printed `PASS`. Coleco production still
ships `clocks-oss.sdc`; SMS copies that subset byte-for-byte.

### G3. `FES_SMS_OSS` still does not select Coleco M10K/VDP OSS shapes

Registered media in the SMS machine is gated on `FES_SMS_OSS` (or
`QUARTUS`):

```13:17:cores/fes-sms/rtl/sms_machine.sv
`ifdef FES_SMS_OSS
`define FES_SMS_REGISTERED_MEDIA
`elsif QUARTUS
`define FES_SMS_REGISTERED_MEDIA
`endif
```

The shared wrappers that Yosys must map are gated on **`FES_COLECO_OSS`**, not
`FES_SMS_OSS`:

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

`FES_SMS_OSS` does not appear in `coleco_dpram.v` or `coleco_vdp.sv`.
`make sim-fes-sms` still passes `-DTV80_REFRESH=1` only.
`make sim-fes-sms-oss` passes `-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1`. The
default sim is unchanged.

Yosys `da6373c0` wrap this session:

| Shape | Defines | Result |
| --- | --- | --- |
| 16 KiB `coleco_dpram` | none | `ERROR: no valid mapping found for memory wrap_dpram.u.ram` |
| 16 KiB `coleco_dpram` | `-DFES_SMS_OSS=1` | same mapping error |
| 16 KiB `coleco_dpram` | `-DFES_COLECO_OSS=1` | 16 `MISTRAL_M10K_TDP` in 0.47 s (exit 0) |
| 8 KiB `coleco_dpram` (SMS RAM) | none | same mapping error |
| 8 KiB `coleco_dpram` (SMS RAM) | `-DFES_COLECO_OSS=1` | 8 `MISTRAL_M10K_TDP` (exit 0) |

An SMS OSS synth that only defines `FES_SMS_OSS` still takes the unmappable
combo-read RAM and the single-array VDP. Dual defines close this for the
SMS producer and the OSS sim; do not drop `FES_COLECO_OSS`. The 8 KiB
CPU RAM is not a new mapper family: it uses the same `$__MISTRAL_M10K_TDP_`
template as the 16 KiB cartridge table (8 blocks vs 16).

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
SMS Quartus gets that path via `QUARTUS=1`. OSS does not, unless
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

SMS OSS must not silently consume repository `toolchain.lock`. It needs the
Coleco compatibility pin (or a later mainline pair that nextpnr accepts for
this shape). nextpnr tests covering the family live under
`mistral/tests/m10k/` in the DeanoC nextpnr tree (`async_read`, `async_tdp`,
`acceptance`, …), not in `cores/fes-sms`.

### G6. HDMI I²C: Quartus tri-state vs OSS `MISTRAL_IO` + BEL

`cores/fes-sms/rtl/top.v` already contains both boundaries:

- `ifdef QUARTUS`: `cyclonev_hps_interface_peripheral_i2c` with assign-to-`z`
- `else`: `MISTRAL_IO` pads `hdmi_scl_pad` / `hdmi_sda_pad` and
  `(* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)`

That RTL is ready. SMS OSS QSF is the Coleco/SG-1000 copy: pin IO_STANDARD
assignments only, no `HPS_LOCATION`. Quartus still includes `HPS_LOCATION`.
nextpnr `2d3c216` can translate HPS_LOCATION→BEL (misteross experiment
`850_hps_location`, nextpnr `mistral/tests/hps_i2c/`). The SMS OSS recipe uses
the RTL BEL plus `constraints-oss.qsf` without `HPS_LOCATION`.

### G7. `TV80_REFRESH=1` is an OSS frontend requirement, not a Quartus macro

- `cores/fes-coleco/rtl/tv80/tv80_core.v` uses `` `ifdef TV80_REFRESH ``
- Coleco / SG-1000 OSS Yosys program: `-DTV80_REFRESH=1` plus the core OSS define
- `make sim-fes-sms` already passes `-DTV80_REFRESH=1`
- `scripts/build_fes_sms.py` `VERILOG_MACRO` is only `QUARTUS=1` and
  `FES_SMS_BUILD_ID=...` — Quartus does not define `TV80_REFRESH`

This is an OSS language/frontend choice (Verilog TV80 vs any VHDL T80 path),
not a missing nextpnr BEL. The future OSS recipe must pass `TV80_REFRESH=1`.

### G8. Dual `altera_pll` 52 MHz + 74.25 MHz is packed; no SMS HIP fmax yet

Shared wrappers:

- `cores/fes-coleco/rtl/sys_pll.v` — `output_clock_frequency0("52.0 MHz")`,
  `fractional_vco_multiplier("false")`
- `cores/fes-coleco/rtl/pixel_pll.v` — `output_clock_frequency0("74.25 MHz")`,
  `fractional_vco_multiplier("true")`

Coleco / SG-1000 OSS producers require route-log `50 MHz -> 52 MHz` and
structured fmax rows at 52.0 / 74.25. misteross experiments `760_pll_52`
and `610_pll_frac_7425`, plus nextpnr `mistral/tests/pll/`, already cover the
profiles. SMS does not lack a PLL BEL. R12/R13 produce the SMS netlist.

### G9. HIP GPU router, seed, and CPU-fallback reject are wired and pass

Coleco / SG-1000 production:

- `ROUTER = "gpu"`, `SEED = 4`, `--timing-allow-fail`, no `--tmg-ripup`
- GPU backend `"hip"`, architectures `gfx1100;gfx1201`
- `_require_gpu_backend` rejects `backend cpu-reference`

The SMS OSS recipe copies that production shape. Powerboat already has the
two HIP cache slots:

| Slot | Yosys | nextpnr | Role |
| --- | --- | --- | --- |
| `.../slots/ddcd4905...` | `da6373c0` | `2d3c216` | Coleco lock (required for G5) |
| `.../slots/cc150969...` | `ec34fcf3` | `9cbbf735` | repository `toolchain.lock` (wrong mapper for sprite banks) |

nextpnr GPU-router fixtures: `mistral/tests/gpurouter/`. R13/R14 HIP-routed
the SMS netlist on Powerboat (`backend hip:AMD Radeon RX 7900 XTX`, seed 4).

### G10. OSS machine sim exists; no board/GP/VDP-only SMS lane

Coleco: `make sim-fes-coleco-oss` builds GP, VDP, machine, and board with
`-DFES_COLECO_OSS=1` and registered-media host timing in the C++ harness.

SG-1000: `make sim-fes-sg1000-oss` builds the machine with
`-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1`.

SMS: `make sim-fes-sms-oss` builds the machine with
`-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1`. `cores/fes-sms/sim/machine_tb.cpp`
already has the registered-media host timing under `FES_SMS_OSS`. There is
still no `cores/fes-sms/sim/board_tb.cpp` / `board_models.v`. Default
`make sim-fes-sms` does not compile the OSS RAM/VDP branches.

### G11. Formic: no in-tree or Powerboat tree

Searched this worktree, Powerboat `/home/deano`, `/opt`, `/usr/local`, `/usr`,
and public DeanoC GitHub repositories. No formic package, worktree, or binary.

Coleco/ZX81/SG-1000 OSS gaps currently live as:

- misteross experiments (`530`/`540` TDP, `480`/`490` independent-clock SDP,
  `610` 74.25 MHz, `760` 52 MHz, `780` Quartus SDC, `850` HPS_LOCATION, …)
- DeanoC nextpnr `mistral/tests/{pll,m10k,hps_i2c,quartus_constraints,gpurouter}`
- DeanoC Yosys PRs (`da6373c0` vs `ec34fcf3`)

If a separate out-of-tree formic development exists, it is not reachable from
this job and cannot be a current SMS rung. Recorded as **blocked / absent**,
not as a silent skip.

### G12. Product-scope, not a Yosys/nextpnr/Mistral miss

- 16 KiB mailbox aperture vs retail 32/48 KiB Master System images
- no SN76489 audio
- no Mode 4 / Sega mappers
- pause NMI unused

These stay outside the first OSS package, as they are outside the Quartus
first slice.

### G13. Later jobs, not this ladder's finish line

HIP format-2 seal for `fes.sms` is done (R14). FES parent pin and kit HIL
remain later jobs.

## Formic / nextpnr / Mistral map

What an SMS OSS build would ask of the toolchain, and where that is tested
today (none of these are SMS-owned tests yet):

| Need | Tool | Current home | SMS status |
| --- | --- | --- | --- |
| 16 KiB registered `ram_style="m10k_tdp"` | Yosys `da6373c0` | Coleco `coleco_dpram.v`; nextpnr `mistral/tests/m10k/` | Mapper works when `FES_COLECO_OSS=1` (R7); SMS OSS sim now passes that define (R10) |
| 8 KiB registered TDP (SMS RAM) | Yosys `da6373c0` | same wrapper, `ADDRWIDTH(13)` | 8 `MISTRAL_M10K_TDP` (R7b); not a new family |
| Combo-read 16 KiB / 8 KiB `ramstyle=M10K` | Yosys | fails closed | Confirmed (R5, R6, R7c) |
| 256×4 registered `ramstyle=M10K` sprite banks | Yosys pin split | Coleco lock vs mainline | Confirmed (R8); SMS lock is a Coleco byte copy |
| Four-copy VDP VRAM | RTL workaround, not a new BEL | `coleco_vdp.sv` `FES_COLECO_REGISTERED_VDP` | Needs `FES_COLECO_OSS`; OSS sim passes it |
| 50→52 MHz integer PLL | nextpnr + Mistral | `sys_pll.v`; `mistral/tests/pll/`; exp `760_pll_52` | BEL present; SMS recipe requires the route-log row |
| 50→74.25 MHz fractional PLL | nextpnr + Mistral | `pixel_pll.v`; exp `610_pll_frac_7425` | BEL present; SMS recipe requires the 74.25 MHz fmax row |
| `MISTRAL_IO` HDMI I²C + I2C BEL 52.60.0 | nextpnr | `top.v` else branch; `mistral/tests/hps_i2c/` | RTL ready; OSS QSF is the Coleco copy |
| Quartus SDC subset | nextpnr | `mistral/tests/quartus_constraints/` | Parser OK on Coleco pin (R9); SMS still uses Quartus `clocks.sdc` |
| HIP `--router gpu` | nextpnr | `mistral/tests/gpurouter/`; Coleco/SG-1000/SMS recipe | Wired in `build_fes_sms_oss.py`; R13/R14 run it |
| Mistral bitstream tables | Mistral `b28e30a` | both locks | Unchanged; no SMS-specific table gap identified |
| formic | n/a | **no tree** | G11 blocked |

`mistral-cv validate` is not re-run here. Coleco already seals with this
Mistral commit; SG-1000 recorded a reverse-not-found dump plus segfault that
is not treated as an SMS-specific gap.

## Ordered misteross test ladder

Pass criteria are for the rung, not for “OSS package done”. Later rungs must
not start until their dependencies pass.

| Rung | Test / script | Pass criteria | Depends on | Powerboat now? |
| --- | ---: | --- | --- | --- |
| R1 | `python3 -m unittest tests.test_build_fes_sms -v` | tests OK (Quartus + default/OSS sim + OSS producer entrypoints) | — | **yes** (producer tests added after original PASS) |
| R2 | `make sms-diagnostic` | sim HALT `graphics-i.rom` 17394 B sha256 `92ef4fb5…4b471eca`; HIL `graphics-i-hil.rom` 17466 B; 32 KiB pads; entry `0x0000` JP `0x4000` | — | **yes** |
| R3 | `make sim-fes-sms VERILATOR=<cached 5.051>` | `FES SMS machine checks passed` (media, 8 KiB mirror, DC/DD, diagnostic `A5`/`DC=ff`) | R2 | **yes** (unset `FES_TOOLCHAIN_CACHE_ROOT`; pass absolute Verilator) |
| R4 | Static OSS-absence inventory (this doc’s file list) | original PASS: producer files were absent. Producer/QSF/SDC/lock now present as Coleco copies | — | **yes** (historical) |
| R5 | Yosys `da6373c0` wrap 16 KiB `coleco_dpram` **without** OSS defines | `ERROR: no valid mapping found for memory …ram` | Coleco HIP slot | **yes** |
| R6 | Same wrap with only `-DFES_SMS_OSS=1` | same mapping error (glue gap G3) | R5 | **yes** |
| R7 | Same wrap with `-DFES_COLECO_OSS=1` | 16 `MISTRAL_M10K_TDP`; Yosys exit 0 | Coleco HIP slot | **yes** |
| R7b | Yosys wrap 8 KiB SMS RAM with `-DFES_COLECO_OSS=1` | 8 `MISTRAL_M10K_TDP`; Yosys exit 0 | Coleco HIP slot | **yes** |
| R8 | Yosys 256×4 `coleco_video_dpram` on `da6373c0` vs `ec34fcf3` | Coleco pin → `$__MISTRAL_M10K_ACLR_BYTE_` / 2 `MISTRAL_M10K`; mainline → `$__MISTRAL_M10K_ASYNC_TDP_` / 1 `MISTRAL_M10K_TDP` | both HIP slots | **yes** |
| R9 | Coleco nextpnr `mistral/tests/quartus_constraints/check.py` | `PASS: Quartus SDC/QSF subset routed one PLL and met 25 MHz` | Coleco HIP slot | **yes** (sibling fixture; not an SMS netlist) |
| R10 | `make sim-fes-sms-oss` with `-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1` | machine checks pass on registered media + registered VDP/RAM | G3 Makefile/defines; R3 | **yes** (this job) |
| R11 | Optional `make sim-fes-coleco-oss` | Coleco OSS sim still green (shared modules) | Coleco tree | **not run**; not required |
| R12 | `scripts/build_fes_sms_oss.py --synth-only` Yosys of `top` | synth.json contains 2 `altera_pll`, `MISTRAL_IO`, `MISTRAL_M10K`/`_TDP`, no forbidden DSP/MLAB | G1–G7, R7, R10 | **yes** |
| R13 | nextpnr HIP route (`--router gpu`, Coleco pin, seed 4) | no unrouted nets; live HIP backend; 52 MHz and 74.25 MHz fmax rows pass | R12, G9 | **yes** (sealed netlist; full recipe) |
| R14 | Format-2 OSS seal | clean tree, `build-inputs.json`, manifest `fes.sms` | R13 | **yes** |
| R15 | formic execution of G4/G5/G8 | formic tree exists and names the failing shapes | G11 | **no** — no formic tree |
| R16 | FES pin / kit HIL | out of scope | R14 | **no** — do not start |

Schedule = this table. R1–R10 and R12–R14 are done.

## Execution this session (2026-09-17)

Host: Powerboat `192.168.10.202`, worktree
`/home/deano/fes-worktrees/misteross-sms-quartus`. R1–R10 ran at `bbbcef4`
(dirty). R12–R14 ran on clean `9c6dc96`. Mac worktree
`/Users/clawzai/Developer/misteross-wt-sms-quartus`. Coleco tools:
`/home/deano/fes/out/cache/misteross-toolchains/slots/ddcd49051df430f25b58a05011bc3bf0379a33cf482384da288703d18bd1a8f4/install/bin/{yosys,nextpnr-mistral,verilator}`.
Mainline Yosys for R8:
`.../slots/cc150969dad68a4363678bf99ba81da80f16028175a0cf247f91f1372a7875f9/install/bin/yosys`.
Probe logs: `/tmp/sms-gap-ladder/` on Powerboat.

| Rung | Result | Notes |
| --- | --- | --- |
| R1 | **PASS** | 7/7 then 8/8 after OSS sim entrypoint test; Mac and Powerboat |
| R2 | **PASS** | diagnostic hashes match step-1 bring-up |
| R3 | **PASS** | `FES SMS machine checks passed`; Verilator `v5.050-268-g5e4151e3` |
| R4 | **PASS** | OSS producer/QSF/SDC/lock/board_tb absent; Coleco/SG-1000 OSS files present; `iverilog` absent |
| R5 | **PASS** (gap confirmed) | Yosys mapping error on combo-read 16 KiB |
| R6 | **PASS** (gap confirmed) | `FES_SMS_OSS` alone does not map |
| R7 | **PASS** | 16 `MISTRAL_M10K_TDP` with `FES_COLECO_OSS`; template `$__MISTRAL_M10K_TDP_` |
| R7b | **PASS** | 8 `MISTRAL_M10K_TDP` for SMS 8 KiB RAM with `FES_COLECO_OSS` |
| R7c | **PASS** (gap confirmed) | 8 KiB combo-read also unmappable |
| R8 | **PASS** (pin split confirmed) | `da6373c0` vs `ec34fcf3` mapper templates as in G5 |
| R9 | **PASS** | Coleco nextpnr SDC fixture; `clk25` achieved 296.65 MHz vs 25 MHz |
| R10 | **PASS** | `make sim-fes-sms-oss` Verilator 5.051; `FES SMS machine checks passed`; both OSS defines |
| R11 | **not run** | optional Coleco OSS sim |
| R12 | **PASS** | `--synth-only` 10s; `altera_pll=2` `MISTRAL_IO=2` `MISTRAL_M10K=52` `MISTRAL_M10K_TDP=104`; synth.json sha256 `99cb79e8…3e3b5738` |
| R13 | **PASS** (HIP route of sealed netlist) | nextpnr `2d3c216` `--router gpu` seed 4; `backend hip:AMD Radeon RX 7900 XTX ready`; GPU router 6.87s; 0 unrouted; `clk_sys` 53.17 MHz PASS at 52.00; `pixel_clk` 107.10 MHz PASS at 74.25; provisional `clk_sys` 38.87 MHz FAIL before GPU repair (`--timing-allow-fail`); `validate_build_evidence` OK |
| R14 | **PASS** | clean `9c6dc96`; format-2 `fes.sms` package_id `c9f2f7d71e77ab6153ddf7224d2beabede6258c1265bb3bfcfa2b5641e1de1b4`; BUILD_ID `7088fdb52c3ea75b636d13d07a46587a`; `.fcore` sha256 `f172d94d64f1c0d9301f244cb7c3c671961948b271895ecc8b3f3223f3bdc4b2` (2,579,968 bytes); RBF sha256 `b95fb1af1825ed62cb7a71f076f0a7ec7aba4d9547584b9cada7fb9bfb03878e` (2,576,153 bytes) |
| R15–R16 | **not run** | no formic; no FES pin/HIL |

`make sim-fes-sms-quartus` is **not runnable** on Powerboat (`iverilog`
absent). That is a vendor-model probe, not an OSS gap.

## Next rung after this GREEN

R15 formic is blocked (no formic tree). R16 FES pin / kit HIL out of scope;
do not start. Package acceptance is parked.
