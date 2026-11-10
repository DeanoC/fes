# #1581 S1c: Quartus-netlist simulation feasibility

Spike S1c of [RTL front-end lanes](../../design/rtl-frontend-lanes.md) (#1581, child issue #1631).
Run on Powerboat on 2026-11-10 from main `97a589e89`. Host simulation only: no kit, no GPU,
no fit, no timing, no seal. No production code changed.

## Result

| Leg | Netlist | Outcome | Evidence |
| --- | --- | --- | --- |
| Z80 | `sms_machine` of fes.sms, read natively by Quartus Lite 17.0.2 | **(b)** | The unchanged `machine_tb.cpp` suite passes on the `.vo` with five FES trial cells (24 min 22 s) and with six (1 min 06 s). The RTL takes 12.8 s. |
| i386 | `fes_i386_cpu` (Yosys elaboration, then Quartus 17.0.2) | **(b)** | The unchanged lockstep harness passes the short lockstep on the `.vo` with six FES trial cells. Every trace hash and every other report field equals the RTL run's. It takes 6 min 32 s against 1 min 26 s on the RTL. |

Outcome (a) is ruled out for both legs. Verilator 5.032 cannot simulate either `.vo` against
Intel's models alone: five of the six cell types in the netlists need an FES model
([Verilator obstacles](#verilator-obstacles-question-2)).

Five of the six FES trial cells are not checked against Intel's models. The plan asks for
unit benches under Icarus; Icarus is not installed on this host, there is no vendor
simulator, and `cyclonev_mac` is encrypted, so no open tool can check that cell at all.
The only evidence for those five cells is that the suites pass. The sixth,
`cyclonev_lcell_comb`, is optional (Intel's model works) and is checked against Intel's
model under Verilator (`lcell_comb_eq.sh`).

## Tools

| Tool | Version | Where |
| --- | --- | --- |
| Quartus | 17.0.2 Build 602 07/19/2017 SJ Lite Edition | `~/intelFPGA_lite/17.0/quartus` |
| Yosys (i386 leg only) | i386 lock, `238eac9a` | toolchain slot `053d3998…` |
| Verilator, used for every run here | **5.032** 2025-01-01 (Debian 5.032-1+b2) | `/usr/bin/verilator` |
| Verilator, locked | 5.051 devel, `5e4151e3` (`toolchain.lock`) | toolchain slot `053d3998…` |
| Intel models | `cyclonev_atoms.v`, `altera_lnsim.sv`, `altera_primitives.v` | `quartus/eda/sim_lib` of the same install |

The design names Verilator 5.032 and the lock pins 5.051 devel. These are different
binaries. The i386 lockstep harness is run on this host with the system 5.032, because the
locked 5.051 hits an internal error on the RTL at `fes_i386_seq.sv:317`. Everything below
was done with 5.032. The locked 5.051 was tried once, on the SMS `.vo` with Intel's models
and `--timing`: it stops with 57 errors on the encrypted DSP model (obstacle 8).

No Icarus, ModelSim, Questa, VCS or Xcelium is installed (`which` finds none, and the
Quartus install has no `modelsim_ase`).

## Writing the netlist (question 1)

Both legs use the same two commands in a project directory with a `.qpf` and `.qsf`
(Cyclone V, `5CSEBA6U23I7`, `NUM_PARALLEL_PROCESSORS 2`, no `EDA_*` setting):

```
quartus_map <top>
quartus_eda <top> --simulation --tool=modelsim --format=verilog --functional=on
```

`quartus_eda` writes `simulation/modelsim/<top>.vo`, `<top>.sft` and
`<top>_modelsim.xrf`, with 0 errors and 0 warnings. `--tool=modelsim` is required: it only
picks the output directory and dialect; no ModelSim is needed to write the file. The
header says "This Verilog file should be used for ModelSim (Verilog) only", and the file
uses `` `timescale 1 ps/ 1 ps ``.

| Netlist | Source read by Quartus | `quartus_map` | `quartus_eda` | `.vo` size |
| --- | --- | --- | --- | --- |
| `fes_i386_cpu` | Yosys netlist (`i386_vo.sh`) | 1 min 59 s | 18 s | 47.6 MB, 1.30 M lines |
| `spike_cpu` | Yosys netlist (`i386_vo.sh`) | 2 min 18 s | (not timed) | 48.4 MB |
| `sms_machine` | RTL, native (`z80_vo.sh`) | 36 s | 1 s | 5.5 MB |
| fes.sms `top` | RTL, native (`z80_vo.sh`) | 45 s | (not timed) | 7.6 MB |

Quartus 17.0.2 cannot read the i386 RTL, so the i386 leg reads the Yosys elaboration that
`quartus_fes_i386.py` uses (`read_verilog -sv`, `hierarchy`, `proc -norom`, `flatten`,
`opt -purge`, `write_verilog -noattr`). `spike_cpu` has three pins, so a lockstep cannot
drive it. It was built for the cell census only, and the lockstep netlist is
`fes_i386_cpu` with every port kept. The Z80 leg reads the fes.sms RTL directly with the
`build_fes_sms.py` source list and macros (`QUARTUS=1`, a zero `FES_SMS_BUILD_ID`).

**Port naming.** Ports are not bit-blasted. Each keeps its RTL name as one vector.

- `sms_machine.vo`: 27 ports, the same names and widths as the RTL.
- `fes_i386_cpu.vo`: 127 ports with the RTL names. Packed two-dimensional ports become one
  flat vector: `input logic [7:0][31:0] init_gpr` is `input [255:0] init_gpr`, and
  `output logic [7:0][7:0] trace_store_data` is `output [63:0] trace_store_data`. Yosys
  already flattens them, and Verilator gives a packed 2-D port the same C++ shape, so
  `lockstep_tb.cpp` and `machine_tb.cpp` compile against the `.vo` unchanged.

The `.vo` declares `tri1 devclrn, devpor, devoe` at module level and passes them to the
cells. Verilator 5.032 pulls them high (checked on a two-register unit case).

## What the netlists contain (question 3)

| Cell | `fes_i386_cpu` | `spike_cpu` | `sms_machine` | fes.sms `top` |
| --- | ---: | ---: | ---: | ---: |
| `cyclonev_lcell_comb` | 38,130 | 38,522 | 3,283 | 4,198 |
| `dffeas` | 16,875 | 17,876 | 1,442 | 1,875 |
| `cyclonev_ram_block` | 1,523 | 1,523 | 146 | 214 |
| `cyclonev_io_ibuf` | 1,598 | 2 | 83 | 3 |
| `cyclonev_io_obuf` | 1,914 | 1 | 146 | 34 |
| **`cyclonev_mac`** | **14** | **14** | **1** | **1** |

**DSP atoms are present in both legs.**

- i386: 14 `cyclonev_mac`, all unregistered: 7 `m9x9`, 6 `m18x18_full` and 1
  `m18x18_plus36` (the governor multiply-add). One has a signed `ax` and three a signed
  `ay`.
- SMS: 1 `cyclonev_mac`, `vdp|legacy_vdp|Mult0~mac`, `m18x18_plus36` with a signed addend
  (`coleco_vdp.sv:161`).

Other facts the cell models depend on:

- Registers are `dffeas` from `altera_primitives.v`, not `cyclonev_ff`. All have
  `power_up = "low"`. Asynchronous clear is a constant in the i386 netlist and a signal on
  185 SMS registers. Asynchronous load and preset are never used.
- Every `cyclonev_ram_block` is one bit wide, `ram_block_type = "M20K"` (a post-map
  netlist is not yet fitted to M10K), read-during-write `new_data_no_nbe_read`, no clears.
- i386 RAM: 1,396 `dual_port` and 127 `rom` (microcode and governor costs, initialised by
  `mem_init0`). 1,116 are mixed-port `old`. The other 280 are `dont_care` with a clock
  enable and a registered output: they are the three `altshift_taps` that Quartus inferred
  from shift registers.
- SMS RAM: 146 `bidir_dual_port`, mixed-port `dont_care`. 112 have port B on `clock1`,
  which is wired to the same `clk_sys`, with address-decode clock enables.
- fes.sms `top` also holds PLL and HPS atoms (`cyclonev_fractional_pll`,
  `cyclonev_pll_output_counter`, `cyclonev_pll_reconfig`,
  `cyclonev_hps_interface_peripheral_i2c`, `cyclonev_hps_interface_mpu_general_purpose`).
  No simulation of `top` was tried. The suite drives `sms_machine`.

An extra i386 build with `DSP_BLOCK_BALANCING "LOGIC ELEMENTS"` gives 0 `cyclonev_mac`. It
was written to see the option work and was not simulated. It is not a sealed
configuration (D6: never forbid DSPs in the seal).

## Synthesis defaults (question 4)

From the Analysis & Synthesis settings table of `map.rpt`, the same in both legs. Nothing
is set in the QSF, so these are the 17.0.2 Lite defaults:

| Setting | Value |
| --- | --- |
| Remove Duplicate Registers | On |
| Remove Redundant Logic Cells | Off |
| Perform WYSIWYG Primitive Resynthesis | Off |
| Optimization Technique | Balanced |
| Auto ROM / RAM / DSP Block Replacement | On |
| Auto Shift Register Replacement | Auto |
| Allow Shift Register Merging across Hierarchies | Auto |
| Auto Clock Enable Replacement | On |
| State Machine Processing | Auto |
| DSP Block Balancing | Auto |
| Disable Register Merging Across Hierarchies | Auto |

17.0.2 Lite `quartus_map` has no retiming and no register-duplication setting: the report
and QSF contain no "retim", "duplicat" (other than the removal above) or "physical
synthesis" line. Those are Fitter options and do not apply to a post-map netlist. A
post-fit netlist was not written.

What `quartus_map` did change:

| | `fes_i386_cpu` | `sms_machine` |
| --- | ---: | ---: |
| Registers kept | 16,875 | 1,442 |
| Registers removed | 591 | 39 |
| Inferred `altshift_taps` (shift registers moved into RAM) | 3 | 0 |
| State machines re-encoded | 1 (`exc_class`) | 0 |

So internal state does not keep its RTL names or encoding, and three i386 shift registers
live in RAM blocks. None of this changes behaviour at the ports if the tools are correct.
The short lockstep confirms it for the i386 leg at the level the harness observes. The
`.vo` run and the RTL run give the same `report.json` in every field: the 11 corpus, 1
random and 1 protected-mode trace hashes (EIP, the eight registers and EFLAGS at every
retirement), the retirement and cycle counts, and the latency histograms of cycles between
retirements (`logs/i386-report-compare.txt`). Internal state was not compared.

## Verilator obstacles (question 2)

All with Verilator 5.032 unless marked. Each row is what stops the build or the run once
the rows above it are worked around.

| # | Cell or model | What happens | Workaround |
| --- | --- | --- | --- |
| 1 | `altera_lnsim.sv` | Default flags: `%Error-NEEDTIMINGOPT altera_lnsim.sv:2789` (`buf #(start_delay)` in the RAM pulse generator). | `--timing` or `--no-timing` |
| 2 | `cyclonev_mac` | `cyclonev_atoms.v:4449: Cannot find file containing module: 'cyclonev_mac_encrypted'`, and 82 "Input port connection … to inout signal" errors. The model is in `mentor/cyclonev_atoms_ncrypt.v`, which is encrypted (3 `%Error-BADSTDPRAGMA`). | `cyclonev_mac_fes.v` |
| 3 | `cyclonev_io_ibuf` | 1,598 × `%Error-UNSUPPORTED: Unsupported: tristate in top-level IO: 'i'` (the model tests `i === 1'bz`). A wrapper top does not help. | `cyclonev_io_ibuf_fes.v` |
| 4 | `dffeas` | `%Error-UNSUPPORTED: altera_primitives.v:1257:5: Unsupported: Verilog 1995 UDP Tables` (`PRIM_GDFF_LOW`). | `dffeas_fes.v` |
| 5 | `cyclonev_ram_block`, `--no-timing` | Compiles. The model makes its write and read pulses from 1 to 2 ps delays, so without timing it does nothing: the SMS suite fails at its first RAM check ("cartridge base byte"). | none |
| 6 | `cyclonev_ram_block`, `--timing` | 13 × `%Error-BLKANDNBLK` at `altera_lnsim.sv:3140`, `:3147`, `:3151`, `:3155` (`dataout_a`, `dataout_b`, `read_data_latch`, `read_unit_data_latch`). | `cyclonev_ram_block_fes.v` |
| 7 | `cyclonev_io_obuf` | Compiles with no error and simulates wrongly. The model drives the pad through `'bz` assignments and a `pmos` primitive; under Verilator the pad keeps a 1 after the register behind it returns to 0. The SMS suite still failed at "cartridge base byte"; a `$display` monitor showed `media_addr[0]~reg0_q` toggling while port `media_addr` read 0, 1, 3, 3, 7, 7, 7, 7, f. | `cyclonev_io_obuf_fes.v` |
| 8 | Locked Verilator 5.051, Intel models, `--timing` | 57 × `Dotted reference to instance that refers to missing module/interface: 'cyclonev_mac_encrypted'`. Nothing more was tried with 5.051. | not tried |

`cyclonev_lcell_comb` is the only Intel model that works as shipped (a two-bit counter
unit case counts correctly). It costs simulation speed: each instance is one
level-sensitive `always` block that infers a latch (`%Warning-LATCH … cout_tmp`), and the
netlist simulations are far slower than the RTL. A sixth FES cell,
`cyclonev_lcell_comb_fes.v`, replaces it with continuous assignments and removes most of
that cost (see the legs below).

With the FES cells nothing from `altera_lnsim.sv` is used, so neither `--timing` nor
`--no-timing` is needed and the suites build with their normal flags. The obstacles the
design expected in `cyclonev_ff` and the specify blocks did not arise: `cyclonev_ff` is
not in a functional `.vo`, and Verilator ignores the specify blocks of
`cyclonev_lcell_comb`.

Obstacle 7 matters beyond this spike: a model that compiles is not shown to work until a
suite passes on it.

## FES trial cells

These are spike code, kept here as the record of what outcome (b) needs. Each stops the
simulation with `$fatal` on a configuration it does not model.

| File | Models | Known differences from Intel's model |
| --- | --- | --- |
| `cyclonev_mac_fes.v` | Unregistered `m9x9`, `m18x18_full`, `m18x18_plus36` | The `plus36` addend and `loadconst` readings come from the Cyclone V documentation and the two netlists. They cannot be compared with the encrypted model. |
| `cyclonev_io_ibuf_fes.v` | `o = i` | No differential input, no bus hold. |
| `cyclonev_io_obuf_fes.v` | `o = oe & i` | Output is 0, not Z, when `oe` is low. Every obuf in both netlists has `oe` tied high. |
| `dffeas_fes.v` | Clear, preset, async load, then enable with sync clear, sync load, data | `sclr_over_ena` is not modelled. Async load and preset are never exercised by these netlists. |
| `cyclonev_lcell_comb_fes.v` (optional, for speed) | LUT, extended LUT, arithmetic and shared arithmetic outputs | Two-state. `lcell_comb_eq.sh` compares it with Intel's model in Verilator 5.032: 64 masks in each of the four `extended_lut` / `shared_arith` modes, all 262,144 ordered pairs of the 512 input values, 0 mismatches. A deliberately wrong copy gives 65,536. |
| `cyclonev_ram_block_fes.v` | One-bit `rom`, `dual_port` and `bidir_dual_port` blocks with input, core and output clock enables, optional output registers and `mem_init0` | A mixed-port read of an address written on the same edge returns the old data in every mode. Intel's model returns X for `dont_care`. Clears, ECC and other read-during-write modes are not modelled. |

## Z80 leg

`z80_machine_suite.sh` builds `cores/fes-sms/sim/machine_tb.cpp` unchanged, first on the
RTL and then on `sms_machine.vo`. This is the `sim-fes-sms` machine suite: cartridge load
and replacement, mapper, RAM mirrors, joystick ports, the Mode 4 diagnostic ROM running to
HALT, a rendered CRAM pixel and the PSG tone and rate checks.

The Quartus build registers the cartridge data as the OSS build does, so the testbench is
compiled with `FES_SMS_OSS=1` and the RTL reference uses the OSS RTL configuration.

| Run | Build | Run | Result |
| --- | --- | --- | --- |
| RTL | 5.8 s | 12.8 s | `FES SMS machine checks passed` |
| `.vo`, FES mac, ibuf, dffeas; Intel RAM; `--no-timing` | 1 min 12 s | 5.1 s | fails: `cartridge base byte` (obstacle 5) |
| `.vo`, the same with `--timing` | fails in 27 s | — | 13 `BLKANDNBLK` errors (obstacle 6) |
| `.vo`, four FES cells (adds the RAM block) | 1 min 05 s | 2.8 s | fails: `cartridge base byte` (obstacle 7) |
| `.vo`, five FES cells (adds the obuf) | 47 s | **24 min 22 s** | **`FES SMS machine checks passed`** |
| `.vo`, six FES cells (adds `cyclonev_lcell_comb`) | 10.5 s | **1 min 06 s** | **`FES SMS machine checks passed`** |

With Intel's `cyclonev_lcell_comb` the passing run is about 110 times slower than the RTL.
A copy of the testbench with a progress print measured about 13,000 clocks a second on
that build (`-O3`), against 11 million clocks in under 2.5 s on the RTL. That run was
stopped once the rate was read. With the FES `cyclonev_lcell_comb` the suite is about 5
times slower than the RTL.

Not covered: the Icarus Quartus sims named in the design table (no Icarus), the other
native-Lite Z80 tops, and the fes.sms `top` with its PLL and HPS atoms.

## i386 leg

`i386_lockstep_vo.py` runs `scripts/test_fes_i386_lockstep.py` unchanged and replaces only
its `verilate()` step, so `lockstep_tb.cpp`, the Bochs oracle, the SST corpus and the
report are the harness's own. The short lockstep is one seed and an SST subset:

```
--opcode 00 --opcode 0FAF --opcode 69 --opcode 6669 --opcode F7.4 --opcode F7.7 --opcode D3.4
--opcode A5 --opcode E8 --opcode CF --opcode 8B --limit-per-file 200
--random-seeds 1 --seed 1 --random-programs 200 --pm-programs 50 --jobs 8
```

The opcodes were picked to reach the multipliers (`0FAF`, `69`, `6669`, `F7.4`), the
divider (`F7.7`), a shift, a string move, a call and an IRET. The directed, unreal, protected-mode,
paging, no-387 and memory case lists always run in full. The harness's BIU DMA test builds
`fes_i386_biu` from the RTL in both runs; it is not a netlist test.

| Run | Verilator build | Whole harness | Result |
| --- | --- | --- | --- |
| RTL | 20 s, 28 C++ files, 17 MB | 1 min 26 s (3 min 34 s CPU) | `i386 lockstep passed` |
| `.vo`, five FES cells, Intel `cyclonev_lcell_comb`, first attempt | 24 min 03 s, 599 C++ files, 643 MB, Verilator peak 11.4 GB | 24 min 07 s | stopped: `reset-store: testbench exited with -6` |
| `.vo`, five FES cells, second attempt | — | — | stopped by hand during the build; no result |
| `.vo`, six FES cells (adds `cyclonev_lcell_comb`) | 3 min 00 s, 114 C++ files, 120 MB, Verilator peak 3.3 GB | **6 min 32 s** (18 min 10 s CPU) | **`i386 lockstep passed`** |

The first attempt stopped on an error in the trial RAM cell, not in the netlist. Its
`$fatal` guard compared `port_b_data_in_clock` with the port B address clock in
`dual_port` mode, where Quartus leaves that parameter at its default
(`…l1i.data2_rtl_0|auto_generated|ram_block1a20`). The guard now makes that comparison in
`bidir_dual_port` mode only. The rebuild with Intel's `cyclonev_lcell_comb` was stopped
once the six-cell run had passed, so **the i386 `.vo` has no lockstep result with Intel's
`cyclonev_lcell_comb`**. The Z80 leg has one, and `lcell_comb_eq.sh` shows the two
`cyclonev_lcell_comb` models agree.

The passing run, the same numbers as the RTL run:

- corpus: 11 files, 2,200 tests pass, 4,401 retirements compared with the oracle
- random: 200 programs, 56,305 retirements, pass
- protected mode: 50 programs, 4,449 retirements, pass
- directed 77, unreal 7, protected-mode 147, paging 54, no-387 1,933, memory 27 and
  reset-store 1: all pass
- 18 lines in the divergence list, the same as the RTL run

Without the build, the `.vo` lockstep takes about 3.5 min against about 1.1 min on the
RTL (8 jobs). The cost is in the build: 3 min and 3.3 GB with six FES cells, 24 min and
11.4 GB with Intel's `cyclonev_lcell_comb`.

The multiplier opcodes pass against Bochs, so the `m9x9` and `m18x18_full` readings of
`cyclonev_mac_fes.v` are exercised. The governor multiply-add (`Mult8~mac`,
`m18x18_plus36`) is probably not: the lockstep runs with governor preset 0.

## Answers to the gating questions

1. **Yes.** 17.0.2 `quartus_eda` writes a functional post-map `.vo` for Cyclone V with the
   flags above. Ports are not bit-blasted: a packed 2-D port is one flat vector with the
   RTL name.
2. **No, not as shipped, with or without `--timing`.** Of the six cell types, only
   `cyclonev_lcell_comb` works from Intel's file. `cyclonev_mac` is encrypted, `dffeas`
   is a UDP, `cyclonev_io_ibuf` needs top-level tristate, `cyclonev_ram_block` needs
   delays under `--no-timing` and fails to compile under `--timing`, and
   `cyclonev_io_obuf` compiles and gives wrong values.
3. **Yes, in both legs.** 14 `cyclonev_mac` in the i386 CPU and 1 in the SMS machine.
4. **No retiming and no register duplication at map; no such setting exists in 17.0.2
   Lite `quartus_map`.** Duplicate-register removal, shift-register-to-RAM replacement and
   state-machine re-encoding are on by default and did apply. The i386 short lockstep
   gives the same report on the `.vo` as on the RTL, trace hashes, cycle counts and
   latency histograms included.
5. **Yes, with FES cells.** `sms_machine.vo` passes the unchanged `machine_tb.cpp` suite
   in Verilator 5.032 with five FES trial cells, about 110 times slower than the RTL, and
   with a sixth (`cyclonev_lcell_comb`) about 5 times slower. `fes_i386_cpu.vo` passes
   the short lockstep with the six cells.

## What this means

**Outcome (b) for both legs.** Verilator can simulate a Quartus 17.0.2 functional netlist
of either design, but only on FES models of the cells: five are required and a sixth is
needed for a usable build time. Outcome (a) is ruled out, and (c) is not reached.

The (b) here is weaker than the plan's wording. The plan's (b) has the FES cells checked
against Intel's models on unit benches under Icarus. That check was done for one cell
(`cyclonev_lcell_comb`, under Verilator). It was not done for `dffeas`,
`cyclonev_ram_block`, `cyclonev_io_ibuf` or `cyclonev_io_obuf`, because Intel's models of
them do not run under Verilator and Icarus is not installed. It cannot be done for
`cyclonev_mac` with any open tool.

**D6.** DSP atoms are in both netlists, so the D6 question is live. The answer to
question 3 is not "FES can model the DSP and check it": Intel's `cyclonev_mac` model is
encrypted for Mentor tools, and the only check on `cyclonev_mac_fes.v` is that the
multiplier opcodes pass lockstep. The three D6 options still stand:

- Seal the native-Quartus i386 lane on a netlist lockstep only where the DSP model is
  trusted. That needs either a simulator entitlement (ModelSim or Questa can read the
  encrypted model, and would also check the other cells), or an accepted FES
  `cyclonev_mac` checked by other means, for example against the kit.
- Keep i386 on the Yosys lane, where the lockstep runs on the RTL that Yosys reads.
- Never forbid DSPs in the seal. The `DSP_BLOCK_BALANCING "LOGIC ELEMENTS"` build is not a
  way out.

**S13** (netlist lockstep for the i386 lane) needs outcome (a) or (b) on the i386 leg. It
has (b) on trial cells, so S13 is not blocked by Verilator. Before S13 can gate a seal it
needs: the cell models written as production code with unit benches; a decision on how
`cyclonev_mac` is checked (D6); Icarus or a vendor simulator on the host for the benches
of the other four cells; and a full lockstep on the `.vo`, which was not run here. The
measured cost of the short one is 3 min of build and about 3 times the RTL run time.

**S14** (netlist suites for the Z80 lane) is conditional on outcome (a) or (b) on the Z80
leg. It has (b), with the same cell-model work as S13 and one DSP atom to model. The
fes.sms `top` adds PLL and HPS atoms that were not tried, so S14 as measured covers the
machine level only.

## Not verified

- The FES trial cells against Intel's models, on unit benches or otherwise, except
  `cyclonev_lcell_comb_fes.v`. `cyclonev_mac` cannot be checked with open tools at all.
- `m18x18_plus36` and `loadconst` semantics in `cyclonev_mac_fes.v` beyond what the two
  suites exercise.
- RAM mixed-port collisions in `dont_care` blocks (the FES model returns old data).
- The locked Verilator 5.051 beyond one lint of the SMS `.vo`.
- Icarus on any netlist; any vendor simulator.
- fes.sms `top` (PLL and HPS atoms) and every other native-Lite core.
- A post-fit netlist, fit, timing or a seal. No timing or seal check was run or relaxed.
- A full lockstep. Only the short one above was attempted.
- The i386 `.vo` with Intel's `cyclonev_lcell_comb`: the first build stopped on the trial
  RAM cell's guard and the second was stopped by hand.
- The governor `m18x18_plus36` multiply-add in the i386 netlist (lockstep preset 0).
- `spike_cpu.vo` and the `DSP_BLOCK_BALANCING` netlist in any simulator.
- The figures for the first five-cell i386 attempt have no file in `logs/`: they were read
  from its build log, and that build directory was removed before the rerun.

## Files

| File | Purpose |
| --- | --- |
| `i386_vo.sh` | Yosys netlist, `quartus_map`, `quartus_eda` for `fes_i386_cpu` or `spike_cpu` |
| `z80_vo.sh` | Native read, `quartus_map`, `quartus_eda` for `sms_machine` or fes.sms `top` |
| `i386_lockstep_vo.py` | The i386 lockstep harness on the RTL or on a `.vo` |
| `z80_machine_suite.sh` | The SMS machine suite on the RTL or on a `.vo` |
| `cyclonev_*_fes.v`, `dffeas_fes.v` | The six FES trial cells |
| `lcell_comb_eq.sh`, `.v`, `.cpp` | Unit bench: Intel's `cyclonev_lcell_comb` against the FES cell |
| `s1c_timed_eval.h` | Timed-eval shim, needed only to reproduce obstacle 6 (`TIMED=1`) |
| `logs/` | Trimmed logs: Quartus summaries and cell census, synthesis settings, Verilator error classes, suite and lockstep output, the report comparison and the `cyclonev_lcell_comb` bench |

No `.vo` netlist and no vendor library file is in the record. To reproduce, set `REPO` to
`sources/misteross`, `QUARTUS_ROOTDIR` to the 17.0.2 `quartus` directory, `YOSYS` to the
i386 lock's Yosys, `VERILATOR` to `/usr/bin/verilator` and `SIM_LIB` to
`$QUARTUS_ROOTDIR/eda/sim_lib`, then:

```
i386_vo.sh fes_i386_cpu <dir>
z80_vo.sh sms_machine <dir>
z80_machine_suite.sh vo <out> <dir>/simulation/modelsim/sms_machine.vo \
    cyclonev_mac_fes.v cyclonev_io_ibuf_fes.v cyclonev_io_obuf_fes.v dffeas_fes.v cyclonev_ram_block_fes.v \
    cyclonev_lcell_comb_fes.v
lcell_comb_eq.sh <out>
i386_lockstep_vo.py $REPO <out> --vo <dir>/simulation/modelsim/fes_i386_cpu.vo --sim-lib $SIM_LIB \
    --extra cyclonev_mac_fes.v ... --extra cyclonev_lcell_comb_fes.v -- --corpus <SST corpus> <args above>
```
