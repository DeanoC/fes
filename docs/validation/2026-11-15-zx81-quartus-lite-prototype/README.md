# ZX81 family on Quartus Lite 17.0.2: prototype fits

Date: 15 November 2026. Source revision `1a4bb8d41`. Host chip-30x0-ai-dev,
idle (load average under 0.1 at start). Quartus
`Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition`, `quartus_sh` sha256
`71035cc1…d8ab93`, which is the digest `cores/fes-zx81/quartus.lock` pins.

This is exploratory evidence for the
[ZX81 Quartus design note](../../design/zx81-quartus-seal.md). The runs are not
sealed packages. They do not change a producer, the registry, or any lock.
They are not the formal two-flow comparison that
[Quartus-built cores](../../design/quartus-cores.md#what-significantly-means)
asks for.

## What was compiled

`scripts/make_project.py` builds a Quartus project from the production OSS
producer's `RTL_SOURCES` list (`sources/misteross/scripts/build_fes_zx81_oss.py`),
with the Quartus constraints `cores/fes-zx81/constraints.qsf` and
`cores/fes-zx81/clocks.sdc`, `QUARTUS=1`, `FES_ZX81_ROM_LINK=1`,
`EXPANSION_SOCKET=1` and each model's define. Quartus's own front end reads all
of the RTL. No Yosys is involved.

Where the prototype differs from a production shell:

- `zx81_rom_link.v` instantiates `MISTRAL_M10K` lanes. A scratch copy gives each
  lane a `LANE` parameter, and a shim maps `MISTRAL_M10K` to a 1024x10 `altsyncram`
  ROM with that lane's MIF. The two-stage read pipeline is unchanged.
- `zx81_expansion_socket.v` instantiates `MISTRAL_FF`. A shim makes it a plain
  flip-flop. With no cart, Quartus removes the 66 plug flip-flops because nothing
  loads them. The nextpnr shell keeps them at pinned BELs.
- ROM lanes hold random test data so that the map can be checked. A sealed base
  would hold blank lanes.
- `BUILD_ID` is a fixed placeholder constant. Four fitter threads per run.
- The zx81 seed 2 and seed 3 runs set `ON_CHIP_BITSTREAM_DECOMPRESSION ON`, so
  they wrote compressed RBFs. Every other run wrote an uncompressed RBF.
  `make_project.py`'s optional sixth argument, `compressed`, selects this. The
  regenerated projects are identical to the ones compiled, apart from the
  `LAST_QUARTUS_VERSION` line that Quartus appends to the QSF after a compile.
- `pin` runs add `set_location_assignment M10K_X5_Y{73+lane}_N0` for each lane.
  These are the BELs the OSS shell uses (`MISTRAL_M10K.5.73.0` and up).

## Timing

Every run passes `scripts/quartus_timing_summary.validate_timing_report`, the gate
the Z80 Quartus sealers use. That covers setup, hold, recovery, removal and
minimum pulse width at every corner, with no unanalysed clock. The table shows the
lower of the two slow-corner Fmax figures. Targets are 52.224 / 74.25 / 12.288 MHz.

| Run | System MHz | Pixel MHz | Audio MHz | Worst setup slack | ALMs | RAM blocks |
|---|---|---|---|---|---|---|
| zx81, lanes pinned, seed 1 | 77.50 | 102.56 | 379.08 | 3.670 ns | 3,744 | 62 |
| zx81, lanes pinned, seed 2 | 77.83 | 96.86 | 427.90 | 3.140 ns | | |
| zx81, lanes pinned, seed 3 | 73.11 | 99.27 | 424.99 | 2.733 ns | | |
| zx81, lanes free, seed 1 | 75.87 | 96.72 | 427.90 | 2.982 ns | | |
| zx80, lanes pinned, seed 1 | 76.31 | 103.34 | 379.22 | 3.020 ns | 3,458 | 42 |
| ts1000, lanes pinned, seed 1 | 78.26 | 106.16 | 424.81 | 3.685 ns | 3,724 | 63 |
| ts1500, lanes pinned, seed 1 | 75.73 | 96.76 | 378.93 | 2.970 ns | 3,724 | 77 |

`results.json` has every figure, the full timing-gate rows and the RBF digests.
A full compile took about 1 min 25 s (synthesis 19 s, fit 48 s, assembler 5 s,
timing 13 s). No run logged a critical warning. TimeQuest reports 2 input and
33 output ports unconstrained, the same as the existing Quartus SDC. The OSS SDC
does not constrain these ports either.

For comparison, DeanoC/fes#1848's nextpnr `bd7a1c0d` sealed winners reach
55.80 (zx81), 52.26 (zx80), 53.80 (ts1000) and 54.42 (ts1500) MHz on the system
clock.

## ROM map

`scripts/check_bytes.py` decodes the Quartus RBF with the repository's own
`scripts/rom_map.py` (`rom_blocks` over the pinned Mistral database,
`m10k-mux.txt` sha256 `22bb99e4…`) and `scripts/cyclonev_rbf.py`, using the
`m10k-1024x10-v1` permutation from `sources/misteross/expansion/rom.go`.

- zx81 seed 1 (uncompressed RBF), zx81 seed 2 (compressed RBF, 2.3 MB),
  ts1500 (8 lanes) and zx80 (4 lanes): every lane matches its MIF, 256 of 256
  rows. Every padding bit (logical bits 8 and 9 of each word) is blank.
- `scripts/derive_map.py` correlates each stored bit with every candidate
  logical bit. All 32 data positions per 40-bit word match the existing
  permutation, complemented, with addresses `4w+k`. The other 8 positions
  hold data bits 8 and 9. The RTL never reads those bits (`q[7:0]`), so Quartus
  drops them and leaves those positions blank.
- `scripts/mif_selftest.sh` rewrites the lane MIFs on a copy of the seed-1
  project and runs only `quartus_cdb --update_mif` and `quartus_asm` (12 s,
  no refit). The new bytes decode at the map positions. 32,876 CRAM bits
  changed, and none of them lies outside the map's destinations.

## HPS DDR layout

`quartus_eda --simulation --tool=modelsim --format=verilog` writes a post-fit
`.vo` netlist. Its `cyclonev_hps_interface_fpga2sdram` instance shows every
`cfg_*` port as `gnd`/`vcc` constants. It also shows `cmd_valid_1..5` and
`wr_valid_0..3` tied to `gnd`. `scripts/ddr_cfg_from_vo.py` compares the
constants with `cores/fes-common/generated/fes_application.vh`, and all seven
ports match. For example, `cfg_port_width` is `0x16` and `cfg_wfifo_cport_map`
is `0x2100`.

## Partial reconfiguration on Lite

The Lite 17.0.2 libraries carry the messages "Partial Reconfiguration requires
a separate license, in addition to the subscription license" and "Current
license file does not support partial reconfiguration. The Quartus Prime
software automatically disabled partial reconfiguration support on all
partitions". Lite runs without a licence file. No PR compile was attempted.

## Reproducing

From `sources/misteross`, with `S` a scratch directory:

```sh
python3 -I ../../docs/validation/2026-11-15-zx81-quartus-lite-prototype/scripts/make_project.py . $S/zx81-pin-s1 1 zx81 pin
../../docs/validation/2026-11-15-zx81-quartus-lite-prototype/scripts/zx81q_fit.sh $S/zx81-pin-s1
python3 -I ../../docs/validation/2026-11-15-zx81-quartus-lite-prototype/scripts/check_bytes.py . $S/zx81-pin-s1 8
```

For the compressed case, pass `compressed` after `pin`. `zx81q_fit.sh` exits
with `quartus_sh`'s status, which is 124 if the one-hour cap expires.
`check_bytes.py`, `mif_selftest.sh` and `ddr_cfg_from_vo.py` exit non-zero on
any mismatch. The scripts hard-code this host's Mistral database path;
`zx81q_fit.sh` honours `QUARTUS_ROOTDIR`. They are prototype code and not a
producer.
