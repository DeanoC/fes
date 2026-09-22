# Open MiSTer FPGA development environment

Development lives only in the [FES repository](https://github.com/DeanoC/fes),
under `sources/misteross/`. The former standalone repository is archived.
Use a FES worktree and open PRs against FES; do not clone or update the old repository.

This module builds small experimental RBF files for the MiSTer/DE10-Nano
Cyclone V FPGA (`5CSEBA6U23I7`). It exists to make ordinary MiSTer core
development possible with both the open-source Mistral toolchain and Quartus.
FES `sources/misteross` is the source of truth. Do not open day-to-day PRs
against standalone `https://github.com/DeanoC/misteross`.

Shared producer operations live in `scripts/fes_build_common.py`; fixed
DE10-Nano video/audio evidence lives in `scripts/fes_de10nano_evidence.py`.
Recipes retain their own resource expectations. The five OSS producer CLIs
and their Make entrypoints default to functional identity v2 for module checkouts;
`--identity-version 1` retains explicit standalone legacy builds. Synthesis-only
diagnostics remain unsealed. Reusable Coleco/SMS/SG-1000
CPU, RAM, video and mailbox RTL lives in `cores/fes-common/rtl`; core-specific
machines and constraints remain in their core directories. These three cores
select one qualified compiler lock, `toolchains/registered-memory.lock`.

## What works now

- Coleco includes a TI SN76489 PSG (three tones and noise) at ports E0–FF,
  feeding shared coherent PCM clock crossing and 48 kHz HDMI I2S. HOLD clears
  the PSG and serializes silence while audio clocks continue. Host simulation
  covers real CPU writes, tone/noise rates, attenuation and asynchronous stereo
  transfer; routed and physical audio acceptance are separate checks.
  System/audio share a dual-output PLL: 52.224/12.288 MHz. The reduced CPU
  and logical raster run 0.43% faster than the previous 52 MHz profile;
  HDMI pixel timing and 48 kHz sample rate stay fixed.
  `make sim-fes-coleco-audio` runs the focused audio simulations.

- Coleco fixed 32 KiB cartridges use the shared CRC-checked blob-stream endpoint;
  `make coleco-stream-diagnostic` emits BIOS-free upper-ROM CPU/video checks.
  Small cartridges retain the legacy 16 KiB mirrored map.
- Coleco synthesis enables `ENABLE_FIRMWARE` so the shared application mailbox
  can overlay an exact 8192-byte firmware image on `0x0000–0x1fff` while reset
  is held. Host simulations keep the parameter at its RTL default 0 and use the
  open `JP 0x8000` shim. The default package still ships that shim; household
  firmware binds at launch. `--bios` remains a separate private build-time
  embed. Firmware mailbox behavior is software-tested; exact-package household
  BIOS, Frogger audio/input and lifecycle hardware evidence is recorded in
  [the FES diagnostic](../../docs/validation/2026-09-21-playable-audio.md).
- Shared controller ports: Coleco uses the composable application endpoint
  with two native digital gamepads and two twelve-key keypads.
  `make sim-fes-coleco` covers actual CPU reads and HDMI controller panels.
- Composable application reference RTL and simulations: `make sim-fes-demo`
  checks video-only, gamepad/palette-media, and gamepad/stereo-audio configurations.
  It also tests the original ROM-less FES Catch game, including shared mailbox
  input, fixed video and stereo catch feedback. Build its sealed package with
  `python3 scripts/build_fes_catch.py` (or FES `make core-dev` with
  `--core fes.catch`); output is `build/fes-catch/core.rbf` plus the immutable
  archive in `build/packages/`. See the FES
  [custom application workflow](../../docs/core-development.md#build-and-play-an-original-application-fes-catch).
  `make build-fes-demo`, `make build-fes-demo-media`, and `make build-fes-demo-audio` use the authenticated
  HIP producer and require clean committed inputs. These new recipes have
  host simulation/recipe coverage; no routed-RBF or hardware acceptance is
  claimed for the audio extension. The audio demo emits 1000/500 Hz stereo at
  48 kHz; Right doubles both tones. See [application development](docs/architecture.md#composable-application-reference).
- Board-firmware HDMI splash (U-Boot / intended Stop-idle bitstream):
  `make sim-fes-splash` checks CTA-770.3 1280×720p60 timing, the FogCast/FES
  mark plus autonomous motion, and the HPS I2C ADV7513 bridge. `make
  build-fes-splash` is the generic OSS Yosys/nextpnr-mistral producer. It
  writes `build/fes-splash/core.rbf` plus provenance for FES `native-inputs`
  and does **not** seal a format-2 play package. The tracked seal is
  `sealed/fes-splash.rbf`. The core has no MiSTer user-io (`0x0014` Probe /
  `0x002f` HPS fb); U-Boot and `LoadIdle` must not Probe. See
  [board-firmware splash](cores/fes-splash/README.md) and
  [architecture](docs/architecture.md#board-firmware-splash).
- Verilator simulation for the included experiments.
- A pinned repository-local Yosys, nextpnr-mistral, Mistral, and
  openFPGALoader toolchain.
- Open-source synthesis, place-and-route, and RBF generation.
- Single-output PLL support for checked whole-MHz frequencies from a 50 MHz
  reference, including 20, 40, 52, and 100 MHz, with fabric reset/relock. The
  closed PLL experiments below retain their 25 MHz output. 52 MHz uses the
  520 MHz feedback profile.
- Dual-output PLL support for compatible whole-MHz pairs that share one checked
  300/320/400 MHz feedback configuration, including 25/40 MHz.
- Three- and four-output integer PLL support for compatible exact decimal
  frequencies from one checked 300/320/400 MHz configuration, including
  25/50/100 MHz and 25/50/100/75 MHz. Zero-phase multi-output profiles also
  accept 25 and 100 MHz V11 references; the DE10-Nano onboard oscillator is
  50 MHz.
- Exact integer PLL duty cycles on one or several outputs (for example 25 MHz
  at 25%, or 25/50/100 MHz at 25/50/25) with mixed-edge fabric paths.
  Fractional-N profiles still require 50% duty.
- Checked static 0°/90°, 0°/180° and 0°/270° pairs, four-output 25 MHz
  quadrature, selectable 0°/90°/180°/270° repeats on three- and four-output
  25 MHz profiles, four-output 50 MHz 0°/90°/180°/270° from a 50 MHz
  reference (`0`/`5000`/`10000`/`15000` ps), four-output 100 MHz
  0°/90°/180°/270° (`0`/`2500`/`5000`/`7500` ps), and 45° steps on 50 MHz
  outputs. 25 MHz phase profiles also accept 25 and 100 MHz V11 references.
- Two independent `altera_pll` cells on the 50 MHz V11 reference, and a
  fabric-controlled `cyclonev_clkena` between a PLL output and clocked logic,
  including low startup, running/gated branches, `enaout` status, and
  double-register mode.
- Three parallel 9×9 multipliers packed into one physical Cyclone V DSP block.
- Cyclone V DSP `M18X18P36` and `M27X27` modes, native 18x19 dual products,
  M9 preadder subtract, M18 36-bit addend, and DSP input/output registers.
- Cyclone V MLAB power-up contents through a numeric `INIT` on each
  `MISTRAL_MLAB` lane. Yosys infers those parameters from initialized RTL.
- Cyclone V mixed-width M10K simple dual-port RAM through
  `ram_style="m10k_mixed"`: independent 10/20/40-bit write and read ports on
  one block, including 40↔10. Place-and-route uses router1 for those
  experiments.
- Cyclone V mixed-width M10K byte enables on a 512×20 write port with a
  1024×10 read port. Locked Yosys does not infer that combined shape; the
  experiment instantiates one `MISTRAL_M10K` with `CFG_BYTE_ENABLE=1`.
  Place-and-route uses router1.
- Cyclone V equal-width M10K true dual-port RAM through
  `ram_style="m10k_tdp"`: 1024×10 and 512×20 with two enabled read/write
  ports, independent clocks, and own-port NEW_DATA. Place-and-route uses
  default router2.
- Cyclone V byte-masked true dual-port M10K RAM through
  `ram_style="m10k_tdp_byte"`: 512×20 (two 10-bit lanes) and 512×16 (two
  padded 8-bit bytes). Each port has an independent two-bit write mask.
  Place-and-route uses default router2.
- Cyclone V mixed-width true dual-port M10K RAM through
  `ram_style="m10k_tdp_mixed"`: physical 20/10 and 10/20, including padded
  16/8 and 8/16. A wide word maps to two adjacent narrow lanes; a narrow
  write preserves the neighbor. Place-and-route uses default router2.
- Checked fractional-N PLL profiles when `fractional_vco_multiplier` is
  `"true"`: 50 MHz → 12.288 MHz, 50 MHz → 11.2896 MHz, and the dual
  12.288/24.576 MHz pair.
- An optional Quartus Prime Lite 17.0.2 reference build using the same RTL.
- Semantic comparison between the OSS and Quartus outputs.
- `010_blinky`, a small LED counter.
- `020_linux_mailbox`, a small HPS GPI/GPO mailbox experiment.
- `030_m10k_rom`, an initialized table driving one LED through one M10K.
- `040_mlab_ram`, a 32-by-8 writeable table on HPS GP, mapped to eight MLABs.
- `050_lut_mul`, an eight-by-eight unsigned product on HPS GP, kept in logic cells.
- `060_dsp_mul`, an eight-by-eight unsigned product on HPS GP. Yosys emits one
  `MISTRAL_MUL9X9`; nextpnr-mistral places one DSP BEL. Quartus measures one
  DSP block.
- `070_mixed_mem`, lab and block tables on HPS GP (eight MLABs and one M10K).
- `080_dsp_mem`, DSP product plus lab and block tables on HPS GP.
- `090_pll_clock`, a fixed 50→25 MHz PLL measured through HPS GP. Run
  `make sim EXP=090_pll_clock` and `make oss EXP=090_pll_clock`.
  This experiment supports the simulation and OSS lanes; its Quartus comparison
  lane is not implemented.
- `100_dsp_rom`, DSP product of an HPS operand and one byte from an initialized
  M10K table on HPS GP.
- `110_pll_reset`, fabric-driven reset and repeated relock of the same PLL,
  observed through HPS GP. Run `make sim EXP=110_pll_reset` and
  `make oss EXP=110_pll_reset`; no Quartus comparison lane is implemented.
- `120_pll_dsp`, an eight-by-eight DSP product clocked by that 25 MHz PLL
  output and observed through HPS GP. Run `make sim EXP=120_pll_dsp` and
  `make oss EXP=120_pll_dsp`; no Quartus comparison lane is implemented.
- `130_pll_dsp_40`, the same DSP product on a 50→40 MHz integer PLL output.
  Run `make sim EXP=130_pll_dsp_40` and `make oss EXP=130_pll_dsp_40`; no
  Quartus comparison lane is implemented.
- `140_pll_dsp_20`, the same product on 20 MHz (Pong's game/video clock).
- `150_pll_dsp_80` and `160_pll_dsp_100`, the same product on 80 MHz and
  100 MHz, the other checked integer outputs used for faster fabric clocks.
- `170_pll_dual`, one PLL driving 25 MHz and 40 MHz together, measured through
  HPS GP. Run `make sim EXP=170_pll_dual` and `make oss EXP=170_pll_dual`; no
  Quartus comparison lane is implemented.
- `180_pll_frac`, the checked 50→12.288 MHz fractional-N profile measured
  through HPS GP. Run `make sim EXP=180_pll_frac` and `make oss EXP=180_pll_frac`;
  no Quartus comparison lane is implemented.
- `190_pll_frac_441`, the checked 50→11.2896 MHz fractional-N profile (256 ×
  44.1 kHz) measured through HPS GP. Run `make sim EXP=190_pll_frac_441` and
  `make oss EXP=190_pll_frac_441`; no Quartus comparison lane is implemented.
- `200_pll_frac_dual`, one fractional PLL driving 12.288 MHz and 24.576 MHz
  together, measured through HPS GP. Run `make sim EXP=200_pll_frac_dual` and
  `make oss EXP=200_pll_frac_dual`; no Quartus comparison lane is implemented.
- `210_pll_duty`, a 50→25 MHz integer PLL at 25% duty with mixed-edge capture
  and a frequency meter through HPS GP. Run `make sim EXP=210_pll_duty` and
  `make oss EXP=210_pll_duty`; no Quartus comparison lane is implemented. This
  does not measure pulse width.
- `220_pll_phase`, two 25 MHz outputs at 0° and +90°, with a 0-to-90 capture
  path and a 0° frequency meter through HPS GP. Run `make sim EXP=220_pll_phase`
  and `make oss EXP=220_pll_phase`; no Quartus comparison lane is implemented.
  This does not measure analog phase accuracy.
- `230_pll_phase_180` and `240_pll_phase_270`, the same meter and capture
  protocol at +180° and +270°. Run `make sim EXP=230_pll_phase_180` /
  `make oss EXP=230_pll_phase_180` and the 270 equivalents; no Quartus
  comparison lane is implemented. This does not measure analog phase accuracy.
- `250_pll_triple`, one PLL driving 25, 50 and 100 MHz together, measured
  through HPS GP. Run `make sim EXP=250_pll_triple` and
  `make oss EXP=250_pll_triple`; no Quartus comparison lane is implemented.
- `260_pll_quad`, one PLL driving 25, 50, 100 and 75 MHz together, measured
  through HPS GP. Run `make sim EXP=260_pll_quad` and
  `make oss EXP=260_pll_quad`; no Quartus comparison lane is implemented.
- `270_pll_multi_duty`, the 25/50/100 MHz triple with independent 25/50/25
  duties, measured through HPS GP. Run `make sim EXP=270_pll_multi_duty` and
  `make oss EXP=270_pll_multi_duty`; no Quartus comparison lane is implemented.
  This does not measure pulse width.
- `280_pll_quadrature`, four 25 MHz outputs at 0°/90°/180°/270°, measured
  through HPS GP. Run `make sim EXP=280_pll_quadrature` and
  `make oss EXP=280_pll_quadrature`; no Quartus comparison lane is implemented.
  This does not measure analog phase accuracy.
- `290_pll_phase_select`, four 25 MHz outputs at 0°/180°/180°/0°, measured
  through HPS GP. Run `make sim EXP=290_pll_phase_select` and
  `make oss EXP=290_pll_phase_select`; no Quartus comparison lane is
  implemented. This does not measure analog phase accuracy.
- `300_pll_ref25` and `310_pll_ref100`, the 25/50/100 MHz triple from a 25 MHz
  or 100 MHz V11 reference. Run `make sim EXP=300_pll_ref25` /
  `make oss EXP=300_pll_ref25` and the 100 MHz-reference equivalents. Analog
  kit measurement needs that physical reference; the onboard oscillator is
  50 MHz. No Quartus comparison lane is implemented.
- `320_pll_phase50`, four 50 MHz outputs at 0°/90°/180°/270° from the 50 MHz
  V11 reference, measured through HPS GP. Run `make sim EXP=320_pll_phase50`
  and `make oss EXP=320_pll_phase50`; no Quartus comparison lane is
  implemented. This does not measure analog phase accuracy.
- `330_pll_phase100`, four 100 MHz outputs at 0°/90°/180°/270° from the 50 MHz
  V11 reference. Run `make sim EXP=330_pll_phase100` and
  `make oss EXP=330_pll_phase100`; no Quartus comparison lane is implemented.
  This does not measure analog phase accuracy.
- `340_pll_phase45`, four 50 MHz outputs at 0°/90°/270°/315° from the 50 MHz
  V11 reference. Run `make sim EXP=340_pll_phase45` and
  `make oss EXP=340_pll_phase45`; no Quartus comparison lane is implemented.
  This does not measure analog phase accuracy.
- `350_pll_two`, two independent PLLs on V11: 25 MHz integer and 12.288 MHz
  fractional-N, measured through HPS GP. Run `make sim EXP=350_pll_two` and
  `make oss EXP=350_pll_two`; no Quartus comparison lane is implemented.
- `360_pll_clkena`, a 50→25 MHz PLL with a fabric-controlled `cyclonev_clkena`
  on the output, measured through HPS GP. Run `make sim EXP=360_pll_clkena`
  and `make oss EXP=360_pll_clkena`; no Quartus comparison lane is implemented.
  This does not characterize enable setup/hold.
- `370_pll_clkena_low`, the same gated 25 MHz meter with enable power-up low.
- `380_pll_clkena_branch`, an always-running 25 MHz PLL output plus a gated
  branch of the same counter.
- `390_pll_clkena_status`, sampled `enaout` status from the gated clock enable.
- `400_pll_clkena_reg2`, two-stage falling-edge (`double register`) clock
  enable with sampled `enaout`. Run `make sim EXP=370_pll_clkena_low` /
  `make oss EXP=370_pll_clkena_low` and the 380/390/400 equivalents; no
  Quartus comparison lane is implemented. This does not characterize enable
  setup/hold.
- `410_dsp_triple`, three eight-by-eight unsigned DSP products (`a*b`,
  `a*~b`, `a*(b^1)`) on HPS GP packed into one physical DSP block. Yosys emits
  three `MISTRAL_MUL9X9`; nextpnr-mistral places them on z-lanes 0/1/2 of
  one DSP site. Run `make sim EXP=410_dsp_triple` and
  `make oss EXP=410_dsp_triple`; no Quartus comparison lane is implemented.
- `420_dsp_mul18`, a sixteen-by-sixteen unsigned DSP product on HPS GP. Yosys
  emits one `MISTRAL_MUL18X18`; nextpnr-mistral places one `M18X18P36` DSP.
  Run `make sim EXP=420_dsp_mul18` and `make oss EXP=420_dsp_mul18`; no Quartus
  comparison lane is implemented.
- `430_dsp_mul27`, a twenty-by-eight unsigned DSP product on HPS GP. Yosys
  emits one `MISTRAL_MUL27X27`; nextpnr-mistral places one `M27X27` DSP.
  Omitted DSP controls encode low. Run `make sim EXP=430_dsp_mul27` and
  `make oss EXP=430_dsp_mul27`; no Quartus comparison lane is implemented.
- `440_dsp_preadder`, an eight-by-eight unsigned product with the M9 preadder
  `left * (right - preadd)` on HPS GP. Run `make sim EXP=440_dsp_preadder` and
  `make oss EXP=440_dsp_preadder`; no Quartus comparison lane is implemented.
- `450_dsp_mac`, an M18 product plus the 36-bit C addend on HPS GP. Kit checks
  `A*B+C` (`10*12+5` is 125). Run `make sim EXP=450_dsp_mac` and
  `make oss EXP=450_dsp_mac`; no Quartus comparison lane is implemented.
- `460_dsp_reg`, an eight-by-eight unsigned M18 product with input and output
  registers on HPS GP. Clock is routed; enable and ACLR are omitted. Run
  `make sim EXP=460_dsp_reg` and `make oss EXP=460_dsp_reg`; no Quartus
  comparison lane is implemented.
- `470_mlab_init`, the 32-by-8 MLAB table with preserved power-up contents on
  HPS GP. Address 0 reads `0xA6` after configuration. Run
  `make sim EXP=470_mlab_init` and `make oss EXP=470_mlab_init`; no Quartus
  comparison lane is implemented.
- `480_m10k_sdp20`, a 512-by-20 M10K simple dual-port table on HPS GP with a
  50 MHz write clock and an independently gated 25 MHz read clock. Address 0
  reads `0xA6` after configuration. Run `make sim EXP=480_m10k_sdp20` and
  `make oss EXP=480_m10k_sdp20`; no Quartus comparison lane is implemented.
- `490_m10k_sdp40`, the 256-by-40 independent-clock M10K table on HPS GP. The
  high 20 bits are the complement of the low 20 bits. Run
  `make sim EXP=490_m10k_sdp40` and `make oss EXP=490_m10k_sdp40`; no Quartus
  comparison lane is implemented.
- `500_m10k_be20`, a 512-by-20 M10K table on HPS GP with two independently
  writable 10-bit lanes, a 50 MHz write clock, and a gated 25 MHz read clock.
  Run `make sim EXP=500_m10k_be20` and `make oss EXP=500_m10k_be20`; no Quartus
  comparison lane is implemented.
- `510_m10k_mix40r10`, a mixed-width M10K table on HPS GP with 256-by-40 writes
  and 1024-by-10 reads. Address 0 reads `0xA6` after configuration. Run
  `make sim EXP=510_m10k_mix40r10` and `make oss EXP=510_m10k_mix40r10`; no
  Quartus comparison lane is implemented.
- `520_m10k_mix10r40`, the reverse mixed-width table: 1024-by-10 writes and
  256-by-40 reads. Run `make sim EXP=520_m10k_mix10r40` and
  `make oss EXP=520_m10k_mix10r40`; no Quartus comparison lane is implemented.
- `530_m10k_tdp10`, a 1024-by-10 M10K true dual-port table on HPS GP with a
  50 MHz port-A clock and an independently gated 25 MHz port-B clock. Address 0
  reads `0xA6` after configuration. Run `make sim EXP=530_m10k_tdp10` and
  `make oss EXP=530_m10k_tdp10`; no Quartus comparison lane is implemented.
- `540_m10k_tdp20`, the 512-by-20 true dual-port table on the same protocol.
  Run `make sim EXP=540_m10k_tdp20` and `make oss EXP=540_m10k_tdp20`; no
  Quartus comparison lane is implemented.
- `550_m10k_tdp_be20`, a 512-by-20 true dual-port table with two 10-bit write
  lanes on HPS GP. Address 0 reads `0xA6` after configuration. Run
  `make sim EXP=550_m10k_tdp_be20` and `make oss EXP=550_m10k_tdp_be20`; no
  Quartus comparison lane is implemented.
- `560_m10k_tdp_be16`, the 512-by-16 padded-byte true dual-port table on the
  same protocol. Run `make sim EXP=560_m10k_tdp_be16` and
  `make oss EXP=560_m10k_tdp_be16`; no Quartus comparison lane is implemented.
- `570_m10k_tdp_mix20_10`, a mixed-width true dual-port table with 512-by-20
  port A and 1024-by-10 port B. Address 0 reads `0xA6` after configuration.
  Run `make sim EXP=570_m10k_tdp_mix20_10` and
  `make oss EXP=570_m10k_tdp_mix20_10`; no Quartus comparison lane is
  implemented.
- `580_m10k_tdp_mix10_20`, the reverse 1024-by-10 / 512-by-20 mixed-width
  true dual-port table. Run `make sim EXP=580_m10k_tdp_mix10_20` and
  `make oss EXP=580_m10k_tdp_mix10_20`; no Quartus comparison lane is
  implemented.
- `590_m10k_tdp_mix16_8`, the padded 512-by-16 / 1024-by-8 mixed-width true
  dual-port table. Run `make sim EXP=590_m10k_tdp_mix16_8` and
  `make oss EXP=590_m10k_tdp_mix16_8`; no Quartus comparison lane is
  implemented.
- `600_m10k_tdp_mix8_16`, the reverse padded 1024-by-8 / 512-by-16 mixed-width
  true dual-port table. Run `make sim EXP=600_m10k_tdp_mix8_16` and
  `make oss EXP=600_m10k_tdp_mix8_16`; no Quartus comparison lane is
  implemented.
- `610_pll_frac_7425`, the checked 50→74.25 MHz fractional-N profile measured
  through HPS GP. Simulation uses a 25 MHz digital stand-in; the analog ratio
  is kit-only. Run `make sim EXP=610_pll_frac_7425` and
  `make oss EXP=610_pll_frac_7425`; no Quartus comparison lane is implemented.
- `620_ddr_clock`, dedicated 50 MHz DDR clock forwarding onto PIN_W15 with a
  fabric GPI beat. Simulation copies the reference onto the output; the analog
  pin waveform is not measured. Run `make sim EXP=620_ddr_clock` and
  `make oss EXP=620_ddr_clock`; no Quartus comparison lane is implemented.
- `630_sdr_output`, dedicated SDR output register on PIN_W15 with
  `FAST_OUTPUT_REGISTER ON` and a fabric GPI beat. Simulation uses the
  Verilog flop; analog GPIO-register delay is not modelled. Run
  `make sim EXP=630_sdr_output` and `make oss EXP=630_sdr_output`; no
  Quartus comparison lane is implemented.
- `640_sdr_input`, dedicated SDR input register on PIN_Y15 with
  `FAST_INPUT_REGISTER ON` and a fabric GPI beat. Simulation uses the
  Verilog flop; analog GPIO-register delay is not modelled. Run
  `make sim EXP=640_sdr_input` and `make oss EXP=640_sdr_input`; no
  Quartus comparison lane is implemented.
- `650_ddr_input`, dedicated DDR input register on PIN_Y15 with a fabric
  GPI beat. Simulation uses a digital `altddio_in` stand-in; analog
  GPIO-register delay is not modelled. Run `make sim EXP=650_ddr_input`
  and `make oss EXP=650_ddr_input`; no Quartus comparison lane is
  implemented.
- `660_ddr_data`, dedicated fabric-data DDR output register on PIN_W15 with
  a fabric GPI beat. Simulation uses a digital `altddio_out` stand-in;
  analog GPIO-register delay is not modelled. Run `make sim EXP=660_ddr_data`
  and `make oss EXP=660_ddr_data`; no Quartus comparison lane is
  implemented.
- `670_altiobuf`, width-one `altiobuf_in` on PIN_Y15, `altiobuf_out` on
  PIN_W15, and `altiobuf_bidir` on PIN_V16 with a fabric GPI beat.
  Simulation uses digital buffer stand-ins; analog pad delay is not
  modelled. Run `make sim EXP=670_altiobuf` and `make oss EXP=670_altiobuf`;
  no Quartus comparison lane is implemented.
- `680_m10k_mix20be10`, mixed-width M10K SDP with 512-by-20 byte-masked writes
  and 1024-by-10 reads on HPS GP. Locked Yosys does not infer that combined
  shape, so the experiment instantiates one `MISTRAL_M10K`. Run
  `make sim EXP=680_m10k_mix20be10` and `make oss EXP=680_m10k_mix20be10`;
  no Quartus comparison lane is implemented.
- `690_ddr_bidir`, dedicated DDR bidirectional I/O register on PIN_W15 with
  a fabric GPI beat. Simulation uses a digital `altddio_bidir` stand-in;
  analog GPIO-register delay is not modelled. Run `make sim EXP=690_ddr_bidir`
  and `make oss EXP=690_ddr_bidir`; no Quartus comparison lane is
  implemented.
- `700_m10k_aclr`, independent-clock 512-by-20 M10K with fabric `ACLR1` on
  GPO[5]. Locked Yosys omits that port, so OSS attaches it after synthesis.
  Run `make sim EXP=700_m10k_aclr` and `make oss EXP=700_m10k_aclr`; no
  Quartus comparison lane is implemented.
- `710_m10k_aclr_prim`, explicit `MISTRAL_M10K` with `.ACLR1(gp_out[5])`.
  Yosys emits that port; OSS does not patch it. Run
  `make sim EXP=710_m10k_aclr_prim` and `make oss EXP=710_m10k_aclr_prim`;
  no Quartus comparison lane is implemented.
- `720_m10k_aclr_infer`, inferred `ramstyle=M10K` 512-by-20 SDP with an
  asynchronous zero clear of the registered read output on GPO[5]. Yosys
  maps that reset onto `ACLR1`. Run `make sim EXP=720_m10k_aclr_infer` and
  `make oss EXP=720_m10k_aclr_infer`; no Quartus comparison lane is
  implemented.
- `730_m10k_tdp_tclk`, true-dual-port 512-by-20 M10K with `CLK2` and `B1EN`
  tied low. nextpnr folds that constant clock off `CLKIN[1]`. Run
  `make sim EXP=730_m10k_tdp_tclk` and `make oss EXP=730_m10k_tdp_tclk`; no
  Quartus comparison lane is implemented.
- `740_m10k_dual_pll`, two PLLs plus independent-clock 512-by-20 M10K. Default
  router2 may retry with router1 when timing margin is under 10%. Run
  `make sim EXP=740_m10k_dual_pll` and `make oss EXP=740_m10k_dual_pll`; no
  Quartus comparison lane is implemented.
- `750_dsp18x19`, two unsigned 18x19 products in one Cyclone V DSP block.
  Locked Yosys has no 18x19 cell, so OSS maps a keep blackbox after
  synthesis. Run `make sim EXP=750_dsp18x19` and `make oss EXP=750_dsp18x19`;
  no Quartus comparison lane is implemented.
- `760_pll_52`, 50→52 MHz integer PLL on the 520 MHz feedback profile.
  Simulation uses a digital toggling stand-in. Run `make sim EXP=760_pll_52`
  and `make oss EXP=760_pll_52`; no Quartus comparison lane is implemented.
- `770_m10k_async_read`, 512-by-20 M10K with a combinational read port.
  The native Yosys mapper emits `CFG_ASYNC_READ` with a constant-high
  `B1EN`; nextpnr routes the physical read enable and keeps the read data
  combinational. Run `make sim EXP=770_m10k_async_read` and `make oss
  EXP=770_m10k_async_read`; no Quartus comparison lane is implemented.
- `780_quartus_sdc`, 50→25 MHz PLL routed with Quartus SDC/QSF forms
  (`get_clocks`, `derive_pll_clocks`, multiline `set_clock_groups`,
  `-entity`). Simulation uses the 090 digital toggling stand-in. Run
  `make sim EXP=780_quartus_sdc` and `make oss EXP=780_quartus_sdc`; no
  Quartus comparison lane is implemented.
- `790_m10k_addrstall`, TDP M10K A-port address stall. Locked Yosys has
  no stall ports, so OSS attaches GPO[29] to `ADDRSTALLA`. Packed
  `ADDRSTALLA` holds while GPO[29] is 0. Run
  `make sim EXP=790_m10k_addrstall` and `make oss EXP=790_m10k_addrstall`;
  no Quartus comparison lane is implemented.
- `800_m10k_out_reg`, 512-by-20 SDP M10K with a registered B-port read.
  Locked Yosys has no output-register parameter, so OSS sets
  `CFG_OUT_REG_B`. Run `make sim EXP=800_m10k_out_reg` and
  `make oss EXP=800_m10k_out_reg`; no Quartus comparison lane is
  implemented.
- `810_m10k_async_defaults`, 512-by-20 M10K with a combinational read
  packed to initialized async defaults (omitted `B1EN`, no `ENABLE[0]`).
  Run `make sim EXP=810_m10k_async_defaults` and
  `make oss EXP=810_m10k_async_defaults`; no Quartus comparison lane is
  implemented.
- `820_m10k_async_enable`, 512-by-20 M10K with a combinational read and a
  packer-generated constant-high `ENABLE[0]`. Run
  `make sim EXP=820_m10k_async_enable` and
  `make oss EXP=820_m10k_async_enable`; no Quartus comparison lane is
  implemented.
- `830_pll_frac_27`, 50→27 MHz fractional-N PLL from the bounded
  calculator (400–500 MHz reported VCO). Simulation uses a digital
  toggling stand-in. Run `make sim EXP=830_pll_frac_27` and
  `make oss EXP=830_pll_frac_27`; no Quartus comparison lane is
  implemented.
- `840_m10k_rdw`, TDP M10K same-port write-through. OSS sets
  `CFG_RDW_MODE_A`/`CFG_RDW_MODE_B` to `NEW_DATA_NO_NBE_READ`. Run
  `make sim EXP=840_m10k_rdw` and `make oss EXP=840_m10k_rdw`; no
  Quartus comparison lane is implemented.
- `850_hps_location`, HPS I2C placed from QSF `HPS_LOCATION` with no RTL
  BEL. Run `make sim EXP=850_hps_location` and
  `make oss EXP=850_hps_location`; no Quartus comparison lane is
  implemented.
- `860_m10k_selectors`, two dual-clock 512-by-20 M10Ks with live CLK2 and
  unique sites. Run `make sim EXP=860_m10k_selectors` and
  `make oss EXP=860_m10k_selectors`; no Quartus comparison lane is
  implemented.
- `870_m10k_narrow`, 8192-by-1 true-dual-port M10K. Run
  `make sim EXP=870_m10k_narrow` and `make oss EXP=870_m10k_narrow`; no
  Quartus comparison lane is implemented.
- `880_m10k_async_rom`, 1024-by-10 read-only async M10K. Run
  `make sim EXP=880_m10k_async_rom` and `make oss EXP=880_m10k_async_rom`;
  no Quartus comparison lane is implemented.
- `890_slot_m10k` / `891_slot_m10k_base` / `892_slot_m10k_cart`, a reserved
  M10K column at `MISTRAL_M10K.26.1.0` for static CRAM overlay. Run
  `make sim EXP=890_slot_m10k` and the base/cart siblings. Overlay with
  `python3 scripts/link_static_rbf.py`. Quartus comparison is not implemented.
  Locked nextpnr `d672fade` honours `FES_RESERVED_BEL` / `FES_RESERVED_RECT`.
- `900_expansion_bus`, independent cart A (`cart` top) with one BEL-locked
  slot cell and the INIT oracle. Synth-only: `make oss EXP=900_expansion_bus`.
  Verilator: `make sim EXP=900_expansion_bus`. Compose onto the 901 shell as
  described in [Freeze-scaffold cartridges](#freeze-scaffold-cartridges).
- `901_plugged_base`, empty socket (`0xD901`) with locked `MISTRAL_FF` plugs
  outside reserved rect `25 1 27 16` (addr column 24, rdata 28.1–28.10).
  Run `make sim EXP=901_plugged_base` and `make oss EXP=901_plugged_base`.
- `903_wide_cart`, independent cart B: four slot cells and a 2-bit decode
  into the same 901 socket. Synth-only: `make oss EXP=903_wide_cart`.
  Verilator: `make sim EXP=903_wide_cart`.
- `904_zx81_socket`, empty ZX81 expansion socket (`0xD904`) with write-data
  and I/O/memory strobes plus a taller reserved rect `25 1 27 32`. Run
  `make sim EXP=904_zx81_socket` and `make oss EXP=904_zx81_socket`.
- `905_zx81_ram16`, Sinclair 16K validation-cart window `4000–7FFF` (sixteen
  slot M10Ks). Synth-only: `make oss EXP=905_zx81_ram16`. Verilator:
  `make sim EXP=905_zx81_ram16`.
- `906_zx81_zonx`, Bi-Pak Zon X-81 AY register file with `(port & 008F)`
  select/data decode. Synth-only: `make oss EXP=906_zx81_zonx`. Verilator:
  `make sim EXP=906_zx81_zonx`.
- `907_zx81_qs_chrs`, QS Character Board 1 KiB window `8400–87FF`.
  Synth-only: `make oss EXP=907_zx81_qs_chrs`. Verilator:
  `make sim EXP=907_zx81_qs_chrs`.

## Freeze-scaffold cartridges

The DE10-Nano has no partial reconfiguration. A composed cartridge is one
full-chip RBF: a frozen empty socket (the shell) plus an independent cart
whose cells occupy a reserved rectangle. The linker copies only that
rectangle's CRAM from the pass-2 bitstream onto the pass-1 shell.

### Roles

| Piece | Experiment | What it is |
| --- | --- | --- |
| Shell | `901_plugged_base` | Empty socket. Signature `0xD901`. Primitive `MISTRAL_FF` plugs outside reserved rect `25 1 27 16` (addr column 24, rdata `28.1`–`28.10`). No slot M10K. |
| Cart A | `900_expansion_bus` | `cart` top, one BEL-locked `MISTRAL_M10K.26.1.0`, INIT oracle. No HPS, no signature. |
| Cart B | `903_wide_cart` | Four slot M10Ks and a 2-bit decode on `plug_addr[11:10]`. Same plug names. |
| Map | `experiments/901_plugged_base/link.toml` | `overlay_mode = "cram_rect"`, tile columns 21–33, `require_slot_only`. |
| ZX81 socket | `904_zx81_socket` | Empty ZX81 plug. Signature `0xD904`. Adds `plug_wdata` (column 23) and mem/I/O strobes (column 29). Reserved rect `25 1 27 32`. |
| 16K validation cart | `905_zx81_ram16` | Sinclair `4000–7FFF` window, sixteen column-26 M10Ks. |
| Zon X-81 | `906_zx81_zonx` | AY register file, `(port & 008F)` select `xxDF`/`xxCF` and data `xx0F`. |
| QS CHRS | `907_zx81_qs_chrs` | 1 KiB at `8400–87FF`, one slot M10K. |

The 890/891/892 trio is the older INIT-only M10K overlay
(`overlay_mode = "m10k_ram"`). Use 901 when the cart is unknown at shell
place-and-route time.

`overlay_m10k_init_bt` writes a byte image into an already placed 1024×10
M10K by replacing only its 256 RAM muxes. Lane `address` is the same
10-bit INIT slice the `880_m10k_async_rom` port reads, with the payload
byte in bits `[7:0]`. nextpnr stores each 40-bit chunk permuted and
inverted (`permute_init` in `bitstream.cc`); `init` writes that stored
form. Simulation and the Quartus oracle keep the machine ROM as `zx81_dpram`
16384×8, addressed `{1'b0, rom_a[12], rom_a[11:0]}`, from
`cores/fes-zx81/rtl/zx8x.hex`. The low 8 KiB is BASIC (reset bytes
`D3 FD`). `make sim-fes-zx81-rom` reads that port.
`python3 scripts/link_static_rbf.py init` decompiles one placed RBF,
replaces RAM muxes from a 1024-byte hex lane, recompresses, and reads those
muxes back. `init --basic` writes all eight lanes onto the column-26 proof sites.
`init --machine` writes them onto the machine ROM at `MISTRAL_M10K.5.73.0`
through `MISTRAL_M10K.5.80.0` and records the 8 KiB image digest on the
launch receipt. `make oss EXP=890_slot_m10k` places one proof block at
`MISTRAL_M10K.26.1.0`. `make oss EXP=893_zx81_basic8` places the eight
legal column-26 proof M10Ks (`26.1`, `26.2`, `26.5`, `26.6`, `26.9`,
`26.10`, `26.13`, `26.14`). The OSS `fes.zx81` recipe defines
`FES_ZX81_ROM_LINK` and does not hash `zx8x.hex` into the package. Those
column-5 lanes are empty until launch splices BASIC. Simulation still
reads the hex. This does not reseal `fes.zx81`. The 8192×1 geometry uses
a different physical order and is not a ROM-link map.

### Build a composed RBF

Pass-2 uses `--router gpu`, so install HIP nextpnr first:

```sh
make toolchain-fes
make oss EXP=901_plugged_base
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/901_plugged_base/routed.json \
  --shell-rbf build/oss/901_plugged_base/top.rbf \
  --cart 900_expansion_bus \
  --output build/oss/composed_901_plus_900.rbf
```

Replace `--cart 900_expansion_bus` with `903_wide_cart` for cart B. ZX81
carts use the 904 shell, map and QSF:

```sh
make oss EXP=904_zx81_socket
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/904_zx81_socket/routed.json \
  --shell-rbf build/oss/904_zx81_socket/top.rbf \
  --cart 905_zx81_ram16 \
  --map experiments/904_zx81_socket/link.toml \
  --qsf experiments/904_zx81_socket/pins.qsf \
  --output build/oss/composed_904_plus_905.rbf
```

Replace `--cart 905_zx81_ram16` with `906_zx81_zonx` or `907_zx81_qs_chrs`.
The script synthesizes the cart, merges it into the routed shell with
`--fes-scaffold --fes-cart`, and runs `scripts/link_static_rbf.py overlay`.
Locked nextpnr `30ac6f47` provides those flags. `NEXTPNR_MISTRAL` still
overrides the binary. A nextpnr without the flags fails closed. The linker
writes `composed_901_plus_900.rbf.receipt.json` beside the output.

To overlay two already-built RBFs without resynthesizing:

```sh
python3 scripts/link_static_rbf.py overlay \
  --base build/oss/901_plugged_base/top.rbf \
  --cart path/to/pass2.rbf \
  --map experiments/901_plugged_base/link.toml \
  --output build/oss/composed.rbf
python3 scripts/link_static_rbf.py diff \
  --a build/oss/901_plugged_base/top.rbf \
  --b build/oss/composed.rbf
```

`require_slot_only` refuses any CRAM bit outside the map rectangle.
Classify ignores sx120f ECC/CRC columns 41, 42, 45 and 49. A taller 904
occupancy also flips companion strips 43, 46, 47 and 50.

### Write another cart

1. Independent experiment with `top = "cart"` and `synth_only`.
2. Ports `plug_addr[15:0]` and `plug_rdata[9:0]` matching the shell. ZX81
   carts also take `plug_wdata[7:0]`, `plug_mem_we`, `plug_io_we` and
   `plug_io_rd`.
3. BEL-lock every slot cell inside the shell reserved rect (901: `25 1 27 16`;
   904: `25 1 27 32`, M10K column 26).
4. `setattr -set FES_SLOT 1 c:*` after synth so nextpnr treats those cells as the cart.
5. No HPS, LED, GPIO, or signature; the shell keeps `0xD901` or `0xD904`.
6. Primitive `MISTRAL_FF` `BEL` attributes survive Yosys; inferred `reg` `BEL` does not.
7. Compose with `--cart <experiment>` onto the matching shell. Do not rebuild the shell for a new cart.

### Kit probes

Claim the designated kit with `scripts/kit.py session`. Load the RBF through
the development-RBF path. Vacant 901:
`experiments/901_plugged_base/hardware/probe.sh`. Composed cart A:
`probe_cart.sh`. Cart B: `probe_cart_b.sh`. Vacant 904:
`experiments/904_zx81_socket/hardware/probe.sh`. 16K pack:
`probe_ram16.sh`. Zon X-81: `probe_zonx.sh`. QS CHRS: `probe_qs_chrs.sh`.
GPI is `{SIGNATURE, plug_addr[5:0], plug_rdata}`. GPO for 904 is
`{io_rd, io_we, mem_we, wdata[7:0], addr[15:0]}`. 904 probes settle
addr/data with strobes low, then pulse; do not apply `0x13579BDF` (it is
an I/O write to xxDF). This is a
development-RBF diagnostic, not image acceptance, and it does not seal
`fes.zx81`.

- Deterministic ROM-less Pong game and raster simulation with `make sim-pong`.
  `make build-pong` stages the pinned MiSTer framework and compiles the wrapper
  with explicitly configured Quartus 17.0.2. Outputs and provenance are under
  `build/rebuild/pong/`. The diagnostic build has passed native gameplay,
  controls and HDMI audio checks.
- FES GP mailbox and fixed 1280x720p60 Pong shell with `make sim-fes-pong`.
  Package `fes.pong` 1.1.0 exposes staged persistence for paddle speed and best
  rally, with snapshot/freeze and resume that preserve the running game.
  `make build-fes-pong` uses the pinned OSS tools and the checked 50→74.25 MHz
  fractional PLL to build and seal its format-2 package. The build requires a
  clean committed source tree; recipe presence alone is no RBF, timing, video
  or hardware-support evidence.

`make sim-pong` tests the standalone digital-control Pong game and continuous
320x240 raster with Verilator. Set `VERILATOR=/absolute/path/to/verilator` to
reuse an installed tool. `make build-pong` adds the MiSTer board wrapper and
produces a programmable RBF.

`make sim-fes-zx81` tests the `fes.simple-computer` 1.0 mailbox, a 16 KB PAL
ZX81 machine that reaches BASIC, types `LOAD ""`, consumes a 16-byte `.p`
through the tape-loader patch, and 1650×750 HDMI timing for the scaled
raster. It also reads the machine ROM port for the 8 KiB BASIC image in
`zx8x.hex`. It is simulation, not a Quartus RBF or kit evidence.

`make build-fes-zx81-quartus` is the Quartus Prime Lite 17.0.2 legacy oracle
recipe for package `fes.zx81` 1.0.0; it is not a nextpnr fallback or the
standard package. Set
`QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0/quartus`. It requires a
clean committed tree, writes `build/fes-zx81-quartus/build-inputs.json`,
embeds that build id, and seals a format-2 package when timing passes. This
does not program hardware.

`make build-fes-zx81` authenticates Yosys, nextpnr-mistral and Mistral against
the scoped `toolchains/zx81-expansion.lock` HIP slot, routes with `--router gpu`,
and rejects a CPU-reference fallback. Provision the local HIP tools with
`make toolchain-fes-zx81` (`make toolchain` stays GPU-router OFF for generic OSS
experiments). Pass `CACHE_ROOT=/absolute/cache` for a shared compiler slot;
omit it for the local HIP install. It synthesizes the standard board shell with
the registered expansion bus, Verilog T80pa/TV80 and M10K allowed, and seals
`build/fes-zx81-oss/` when the 52 MHz
system clock and 74.25 MHz pixel clock pass timing. The 52 MHz integer uses
the 520 MHz PLL feedback profile (M=52 N=5 C6=10). Recipe presence alone is
no RBF, timing or hardware-support evidence. The command never programs a kit.

`make sim-fes-coleco` tests the `fes.application` Coleco slice in both
default and OSS-conditional lanes: a reduced ColecoVision machine with an open
`JP 0x8000` reset shim, a raw 1–32 KiB cartridge aperture, mirrored CPU RAM,
Graphics I/II VDP tile/status paths with four-bit colors, buffered reads and VBlank NMI, two joystick/keypad controllers with two fire
buttons and twelve encoded keypad keys each, and the fixed
1650×750 HDMI shell. The board tests upload the exact open diagnostic bytes
through GP, release execution immediately, and check CPU-driven pixels across
compact/full-size/repeated loads. `make coleco-diagnostic` generates the
MIT-licensed raw cartridge and 720p reference image; see
[the core guide](cores/fes-coleco/README.md#open-graphics-i-diagnostic).
The optional joystick and raw-controller-byte diagnostics use the native
controller ports; see [controller mapping](cores/fes-coleco/README.md#standard-controller-mapping).
These are host simulations, not hardware acceptance. Quartus and OSS producers set `ENABLE_FIRMWARE=1` on `coleco_application_gp`
and declare optional `fes.firmware.blob` 1.0 so BIOS-free Graphics I titles share
the firmware-capable bitstream. Host simulations leave the parameter at 0, so
capability bit 7 stays off and the open reset shim is used. Runtime firmware
upload overlays the 8 KiB aperture through mailbox opcodes 15–17 and does not
release execution; cartridge media still owns release. For a privately supplied
8192-byte BIOS, the OSS producer accepts `--bios PATH`; this embeds the BIOS in a
separate `fes.coleco.private-bios` package under `build/private-packages` and binds
its digests into identity v2. It does not change the default reset-shim package
or establish retail compatibility. See [private BIOS bring-up](cores/fes-coleco/README.md#private-bios-bring-up)
and [runtime firmware overlay](cores/fes-coleco/README.md#runtime-firmware-overlay).

`make coleco-vdp-diagnostic` generates a BIOS-free CPU read/status/NMI test.
`make sim-fes-coleco-vdp-io` and its `-oss` counterpart check its real CPU
results and pass picture, including reset/reload. Enabling cartridges provide
their NMI handler at 8066; see [VDP interfaces](cores/fes-coleco/README.md#vdp-reads-and-interrupts).

`make sim-fes-coleco-quartus` separately checks the actual Quartus RAM/media
branches with a locally supplied Quartus 17 `altera_mf.v` and Icarus Verilog.
See the core guide for prerequisites and reproducible before/after probes.

For parallel software verification, `sim-fes-coleco-unit[-oss]` runs the
focused endpoint, VDP, CPU and video checks. The independent
`sim-fes-coleco-board-CASE[-oss]` targets run `graphics`, `stream`, `interactive`,
`controllers`, `vdp-io` or `sprites`. Each case prepares its diagnostics and
builds the matching board model. The existing aggregate commands include all
their prior cases; `make -j4 sim-fes-coleco` shares the two board builds within
one invocation. FES CI runs the fourteen targets in isolated jobs.

`make sim-fes-sg1000` tests the next `fes.simple-computer` Coleco sibling: a
reduced SG-1000 machine with the cartridge at `0x0000`, 1 KiB RAM at `0xc000`,
the shared TMS9918-style VDP, and SG-1000 8255 joystick ports `0xdc`/`0xdd`.
It is simulation, not a Quartus RBF or kit evidence.
`make sim-fes-sg1000-oss` compiles the same machine with
`-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1` (registered media plus the Coleco
M10K/VDP OSS shapes). The default sim target stays on combinational RAM.

`make build-fes-sg1000-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe
for package `fes.sg1000` 1.0.0. It reuses Coleco TV80, VDP, video, GP and PLL
modules and is not a nextpnr fallback. Set
`QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0/quartus`. The recipe
requires a clean committed tree to seal a format-2 package; `--compile-only`
produces the RBF and timing evidence without sealing. This does not program
hardware.

`make build-fes-sg1000` is the OSS Yosys/nextpnr-mistral recipe. It copies the
Coleco lock (Yosys `e2d425de`, nextpnr `0fad53a7`) and the Coleco OSS
constraint subset. Yosys must define both `FES_SG1000_OSS=1` and
`FES_COLECO_OSS=1`. `--synth-only` is the dirty-tree synth probe and does
not seal. The producer uses `--router gpu` and seed 4 with a live HIP
backend required. HIP format-2 seal, FES parent pin and kit HIL remain later
jobs.

`make sim-fes-sms` tests the next `fes.simple-computer` Coleco/SG-1000 sibling:
a bounded Master System machine (`fes.sms`, not `fes.mastersystem`) with a
32 KiB fixed cartridge map at `0x0000–0x7fff` (`0x8000–0xbfff` unmapped), 8 KiB
RAM at `0xc000` mirrored at `0xe000`, legacy TMS modes plus an SMS Mode 4 VDP
on Z80 INT, six-bit CRAM video, SN76489 on ports `0x7E`/`0x7F`, FPGA→ADV7513
I2S, SMS 8255 joystick ports `0xdc`/`0xdd`, and required `fes.media.blob-stream`
1.0. The mailbox sim consumes
`cores/fes-sms/generated/stream-exchanges.json`.
After a commit of length N, unused mapped bytes read `0xff`. HoldReset aborts
an incomplete legacy blob when stream is enabled. `make sms-diagnostic` emits
a 32 KiB-capable image that jumps to `0x4000` and programs a square wave (sim
HALT vs HIL interactive). It is simulation, not a Quartus RBF or kit evidence.
`make sim-fes-sms-oss` compiles the registered-media, legacy VDP and Mode 4
line-renderer branches with `-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1`.

`make build-fes-sms-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe
for package `fes.sms` 1.2.0. It reuses Coleco TV80, legacy VDP, GP, RAM and PLL
modules plus the SMS-owned Mode 4 VDP, SN76489, HDMI I2S and video shell; it is
not a nextpnr fallback. Set
`QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0/quartus`. The recipe
requires a clean committed tree to seal a format-2 package; `--compile-only`
produces the RBF and timing evidence without sealing. This does not program
hardware.

`make build-fes-sms` is the OSS Yosys/nextpnr-mistral recipe. It copies the
Coleco lock (Yosys `e2d425de`, nextpnr `0fad53a7`, Mistral `b28e30a`) and Coleco
`clocks-oss.sdc`, and uses SMS `constraints-oss.qsf` (video/I2C plus ADV7513
I2S). Yosys must define both `FES_SMS_OSS=1` and `FES_COLECO_OSS=1`.
`--synth-only` is the dirty-tree synth probe and does not seal. The producer
uses `--router gpu` and a first-pass HIP seed/weight search (starts at
seed 10 / HeAP 1000, then the remaining `PLACER_SEEDS` and weight 300). Final
structured `clk_sys` and `pixel_clk` rows must meet 52 MHz and 74.25 MHz. FES
parent pin and kit HDMI-audio HIL remain later jobs.

`make build-fes-coleco-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe
for `fes.coleco` 1.0.0; it is not a nextpnr fallback. `make build-fes-coleco`
is the authenticated HIP nextpnr/Mistral production recipe for `5CSEBA6U23I7`.
Provision its local HIP tools with `make toolchain-fes-coleco`. Pass
`CACHE_ROOT=/absolute/cache` for a shared compiler slot; omit it for the local
HIP install. Both require a clean committed tree before sealing and neither
programs a kit. The OSS path uses
Verilog TV80 with `TV80_REFRESH=1`, explicit registered M10K TDP wrappers,
four coherent VDP VRAM copies for the raster read ports (the fourth feeds the
serial Graphics II sprite walker), `MISTRAL_IO` at HPS
I²C BEL 52.60.0, and the accepted OSS 50 MHz `create_clock` constraint subset.
The clean integration OSS build of implementation revision
`b60e1aaccc5ec0c6f93d654c5cb2e6caf9f3e873` routes at 59.62 MHz system /
90.88 MHz pixel with no unrouted nets; see `cores/fes-coleco/README.md` and the
architecture note for the measured workaround handoff. It uses 85
`MISTRAL_M10K_TDP` and 48 `MISTRAL_M10K` cells and seals package
`3b1b9dbcf2a30e8b389ee2aaf11dc0d9facfd3c4b9461b4c9f301afa6244f368`. The exact
clean package loaded through the native FogCast path and stopped back to idle
under the designated target-agent lease. The HDMI sample was black; this is
exact-artifact load/stop diagnostic evidence, not Coleco functional or video
acceptance.

`make sim-fes-pong` tests the separate `fes.simple-game` GP transport and exact
74.25 MHz-domain 720p raster model. It reuses only `pong_game.sv` from the
MiSTer Pong and simulates the board top with independently driven,
simulation-only HPS and PLL boundaries. The board test also checks HDMI I2C
low-or-release drive and feedback from an external device. The standalone shell
uses the HPS I2C bridge at X52/Y60 with U10/AA4 pads; its export checks require
that exact route and constant-low output data.

`make build-fes-pong` authenticates Yosys, nextpnr-mistral and Mistral against
`toolchain.lock` on the standard HIP lane (`--router gpu`, live HIP backend
required). Provision the local HIP tools with `make toolchain-fes`. Pass
`CACHE_ROOT=/absolute/cache` for a shared compiler slot; omit it for the local
HIP install. It constructs canonical
`build/fes-pong/build-inputs.json` before synthesis, and passes its 128-bit ID
as `top.BUILD_ID`. It rejects missing or failing 50/74.25 MHz timing, incomplete
routing, a CPU-reference GPU fallback, unexpected hard resources, a changed
tool identity, or a dirty source checkout before calling the format-2 exporter.
The command never programs a kit.

`cores.lock` also selects SNES and NES Release 20260823. `make fetch-core
CORE=snes` and `make fetch-core CORE=nes` use the existing fetch/hash-check
lane. A staged seed-3 Quartus diagnostic passed timing and native LoROM/HiROM
hardware checks for SNES. The normal SNES recipe now explicitly selects fitter
seed 3 and requires passing timing. Export admits Mega Drive, SNES, NES and
Pong; new builds require their own hardware acceptance. The NES slice accepts
standard `.nes` cartridges through the generic `scripts/rebuild_core.py` lane;
FDS, UNIF and other NES peripherals remain outside this contract.
See the architecture document for artifact identities.

Pinned third-party cores live in `cores.lock`. That file is the **upstream
version**: exact git commit plus the official release RBF hash.
`make fetch-core CORE=megadrive` checks those bytes out.
`make rebuild-core CORE=megadrive` compiles our own RBF from that tree
with Quartus Prime Lite 17.0.2. That rebuild has been loaded on real
MiSTer hardware, so the fetch → Quartus → RBF path works end to end.
The two artifacts are not required to bit-match (Lite cannot reproduce
Standard). `make select-core CORE=megadrive` copies the hardware-verified
rebuild to `build/current/megadrive.rbf`. `ARTIFACT=upstream` falls back
to the official release. The fetch checkout is not modified. Rebuild
identity is not a lock failure. Selection is an operator convenience, separate
from compilation and from the immutable FogCast handoff.

The useful outputs are ordinary local files:

```text
build/oss/<experiment>/top.rbf
build/oracle/<experiment>/top.rbf
build/cores/megadrive/releases/MegaDrive_20260603.rbf   # upstream
build/rebuild/megadrive/megadrive.rbf                   # our rebuild
build/current/megadrive.rbf                             # selected
build/bundles/megadrive/<rbf-sha256>/megadrive.rbf      # exported
build/bundles/megadrive/<rbf-sha256>/megadrive-rbf.toml # manifest
build/cores/nes/releases/NES_20260823.rbf              # upstream
build/rebuild/nes/nes.rbf                               # our rebuild
build/bundles/nes/<rbf-sha256>/nes.rbf                 # exported
build/bundles/nes/<rbf-sha256>/nes-rbf.toml             # manifest
build/fes-splash/core.rbf                                # OSS board-firmware splash RBF
build/fes-splash/build-inputs.json                       # pre-synthesis canonical inputs
build/fes-splash/build-summary.json                      # timing/resource/tool evidence when sealed
build/fes-splash/native-inputs-snippet.toml              # FES [splash_rbf]/[idle_rbf] pin candidate
sealed/fes-splash.rbf                                    # tracked FES-local splash/idle seal
build/fes-pong/core.rbf                                 # standalone FES Pong build
build/fes-pong/build-inputs.json                        # pre-synthesis canonical inputs
build/fes-pong/build-summary.json                       # timing/resource/tool evidence
build/fes-pong/manifest.toml                            # generated format-2 manifest
build/fes-zx81-quartus/core.rbf                         # Quartus bring-up FES ZX81 RBF
build/fes-zx81-quartus/build-inputs.json                # pre-compile canonical inputs
build/fes-zx81-quartus/manifest.toml                    # generated format-2 manifest
build/fes-zx81-oss/core.rbf                             # OSS nextpnr/Mistral FES ZX81 RBF
build/fes-zx81-oss/build-inputs.json                    # pre-synthesis canonical inputs
build/fes-zx81-oss/build-summary.json                   # timing/resource/tool evidence
build/fes-zx81-oss/manifest.toml                        # generated format-2 manifest
build/fes-coleco-quartus/core.rbf                       # Quartus bring-up FES ColecoVision RBF
build/fes-coleco-oss/core.rbf                            # OSS nextpnr/Mistral FES ColecoVision RBF
build/fes-sg1000-quartus/core.rbf                       # Quartus bring-up FES SG-1000 RBF
build/fes-sg1000-quartus/build-inputs.json              # pre-compile canonical inputs
build/fes-sg1000-quartus/manifest.toml                  # generated format-2 manifest when sealed
build/fes-sg1000-oss/synth.json                          # OSS Yosys evidence (synth-only or full)
build/fes-sg1000-oss/core.rbf                            # OSS nextpnr/Mistral FES SG-1000 RBF when sealed
build/fes-sms-quartus/core.rbf                           # Quartus bring-up FES Master System RBF
build/fes-sms-quartus/build-inputs.json                  # pre-compile canonical inputs
build/fes-sms-quartus/manifest.toml                      # generated format-2 manifest when sealed
build/fes-sms-oss/synth.json                             # OSS Yosys evidence (synth-only or full)
build/fes-sms-oss/core.rbf                               # OSS nextpnr/Mistral FES Master System RBF when sealed
build/fes-coleco-oss/build-summary.json                  # timing/resource/tool evidence
build/fes-coleco-oss/manifest.toml                       # generated format-2 manifest
build/packages/<package-id>/manifest.toml               # format-2 manifest
build/packages/<package-id>/core.rbf                    # unchanged payload
build/packages/<package-id>.fcore                       # restricted ustar package
build/packages/<package-id>.build-inputs.json           # external build evidence
```

`make export-core-bundle CORE=megadrive` validates the rebuild and its
comparison evidence, then seals those two files in the digest directory. It
prints the completed absolute directory as the printed bundle path. FogCast
receives that printed bundle path, not a mutable build path such as
`build/rebuild/megadrive/megadrive.rbf` or `build/current/megadrive.rbf`.
FogCast owns transferring the bundle's RBF to the disposable MiSTer Pi and
loading it. This repository does not own FogCast deployment, target recovery,
hardware ownership, or network policy.

## Quick start

Check prerequisites and build the pinned OSS tools:

```sh
make toolchain-check
make toolchain
source scripts/env.sh
```

`make toolchain` builds the generic OSS tools with GPU router OFF. FES Pong
and ZX81 local HIP builds need the FES HIP lane instead:

```sh
make toolchain-fes
make build-fes-pong
make build-fes-zx81
```

`make toolchain` and `make toolchain-fes` install into the same
`build/toolchain` prefix; the last one run wins. Coleco keeps
`make toolchain-fes-coleco` and its specialized lock.

Build the mailbox experiment with the open toolchain:

```sh
make sim EXP=020_linux_mailbox
make oss EXP=020_linux_mailbox
```

The resulting development core is:

```text
build/oss/020_linux_mailbox/top.rbf
```

If Quartus 17.0.2 is installed, build and compare the reference output:

```sh
make oracle EXP=020_linux_mailbox
make compare EXP=020_linux_mailbox
```

### Shared immutable toolchain cache

The FES Python Pong, ZX81, Coleco, SG-1000, and SMS recipes use HIP nextpnr
(`--router gpu` with a live HIP backend) as the standard production lane. Shared-cache mode
is selected only by an explicit `--cache-root PATH` on the producer CLI or by
`CACHE_ROOT=/absolute/cache` on `make build-fes-pong`, `make build-fes-zx81`,
`make build-fes-coleco`, `make build-fes-sg1000`, and `make build-fes-sms`,
not by ambient `FES_TOOLCHAIN_CACHE_ROOT`.
Omitting `--cache-root` / `CACHE_ROOT` keeps the repository-local HIP
toolchain from `make toolchain-fes` (Pong), `make toolchain-fes-zx81` (standard
ZX81), `make toolchain-fes-coleco`,
`make toolchain-fes-sg1000`, or `make toolchain-fes-sms`.
Pong authenticates the repository-wide `toolchain.lock` HIP slot
(`gpu-router=HIP; hip-architectures=gfx1100;gfx1201`). The standard ZX81
authenticates the scoped `toolchains/zx81-expansion.lock` slot. Coleco, SG-1000
and SMS share `toolchains/registered-memory.lock` and the same HIP lane without
aliasing another lock's cache slot. Each lock retains its exact qualified bytes.
Quartus recipes remain oracle-only for ZX81, Coleco, SG-1000, and SMS and
are not a nextpnr fallback.
An empty shared cache is provisioned with the same Make variable used by the
producer recipes:

    CACHE_ROOT=/absolute/cache make toolchain-fes
    CACHE_ROOT=/absolute/cache make toolchain-fes-zx81
    CACHE_ROOT=/absolute/cache make toolchain-fes-coleco
    CACHE_ROOT=/absolute/cache make toolchain-fes-sg1000
    CACHE_ROOT=/absolute/cache make toolchain-fes-sms
    CACHE_ROOT=/absolute/cache make doctor-strict

`toolchain-fes` populates the root-lock HIP slot; `toolchain-fes-zx81` populates
the scoped ZX81 slot. Coleco, SG-1000, and SMS toolchain targets populate the
Coleco-lock HIP slot. Later producer commands
reuse those verified slots. An explicit
FES_TOOLCHAIN_CACHE_ROOT is still supported for callers that already use the
internal spelling; if both variables are set they must name the same absolute
path.

For direct producer reuse inside the FES monorepo, derive the compiler cache
from the selected parent's `recipes.TOOLCHAIN_CACHE_ROOT`. It resolves the
primary checkout through Git's common directory and honors `FES_CACHE_ROOT`,
so task worktrees use the same authenticated cache as parent builds. Both
`CACHE_ROOT=… make build-fes-pong` and `make build-fes-pong CACHE_ROOT=…`
are valid; Make clears `MAKEFLAGS`/`MFLAGS` for the producer in either form.
Standalone misteross checkouts can instead supply an explicit absolute
`CACHE_ROOT` using the commands above.

```sh
# From the sources/misteross directory of the selected FES worktree:
FES_ROOT="$(git rev-parse --show-toplevel)"
FES_COMPILER_CACHE="$(python3 - "$FES_ROOT" <<'PY'
from pathlib import Path
import sys
sys.path.insert(0, str(Path(sys.argv[1]) / "scripts"))
from recipes import TOOLCHAIN_CACHE_ROOT
print(TOOLCHAIN_CACHE_ROOT)
PY
)"
CACHE_ROOT="${FES_COMPILER_CACHE}" make build-fes-pong
make build-fes-pong CACHE_ROOT="${FES_COMPILER_CACHE}"
CACHE_ROOT="${FES_COMPILER_CACHE}" make build-fes-zx81
make build-fes-zx81 CACHE_ROOT="${FES_COMPILER_CACHE}"
CACHE_ROOT="${FES_COMPILER_CACHE}" make build-fes-coleco
make build-fes-coleco CACHE_ROOT="${FES_COMPILER_CACHE}"
CACHE_ROOT="${FES_COMPILER_CACHE}" make build-fes-sg1000
make build-fes-sg1000 CACHE_ROOT="${FES_COMPILER_CACHE}"
CACHE_ROOT="${FES_COMPILER_CACHE}" make build-fes-sms
make build-fes-sms CACHE_ROOT="${FES_COMPILER_CACHE}"
python3 scripts/build_fes_pong.py --root "$PWD" --cache-root "${FES_COMPILER_CACHE}"
python3 scripts/build_fes_zx81_oss.py --root "$PWD" --cache-root "${FES_COMPILER_CACHE}"
python3 scripts/build_fes_coleco_oss.py --root "$PWD" --cache-root "${FES_COMPILER_CACHE}"
python3 scripts/build_fes_sg1000_oss.py --root "$PWD" --cache-root "${FES_COMPILER_CACHE}"
python3 scripts/build_fes_sms_oss.py --root "$PWD" --cache-root "${FES_COMPILER_CACHE}"
```

A nextpnr command that merely contains `--router gpu` is not sufficient: the
route log must identify a live HIP backend and is rejected if it falls back to
the CPU reference backend.

The cache key includes the selected lock and recipe bytes, normalized compiler
and host identities, and the OFF/HIP/CUDA configuration. A per-key lock covers
the private source/build directories and publication of a stable slot. A
ready manifest authenticates every installed tool, support file, internal
link, and evidence record; consumers use the recorded absolute install path
and never relocate or modify it. A failed build leaves its partial slot for
manual recovery and is refused until an operator removes that slot.

Sourcing `scripts/env.sh`, `scripts/run_sim.sh`, standalone `make sim*`
targets, `scripts/program.py`, and generic `make oss` still fail early if
`FES_TOOLCHAIN_CACHE_ROOT` is set rather than silently using a local or
ambient tool. FPGA output schemas and the `BUILD_ID` algorithm do not change;
a shared compiler may produce different tool bytes, which remain represented
by the existing digest fields and provenance records.

Fetch the upstream Mega Drive pin and compile the source-built RBF:

```sh
make fetch-core CORE=megadrive
make rebuild-core CORE=megadrive
make export-core-bundle CORE=megadrive
```

The same sequence works for the native NES slice once Quartus Prime Lite
17.0.2 is installed:

```sh
make fetch-core CORE=nes
make rebuild-core CORE=nes
make export-core-bundle CORE=nes
```

The export command is the FogCast handoff. For local operator use, select the
current RBF separately (rebuild is the default; upstream is the fallback):

```sh
make select-core CORE=megadrive
make select-core CORE=megadrive ARTIFACT=upstream
```

`build/current/megadrive.rbf` remains an operator selection and is not the
FogCast release handoff. The handoff is the printed digest directory under
`build/bundles/megadrive/`.

Format-2 packages use a separate exporter and do not change the format-1
`export-core-bundle` command. Put canonical `build-inputs.json` beside the RBF,
then export it with:

```sh
make export-core-package \
  PACKAGE_MANIFEST=/absolute/path/manifest.toml \
  PACKAGE_RBF=/absolute/path/core.rbf \
  PACKAGE_OUTPUT="$PWD/build/packages"
python3 scripts/core_package.py inspect \
  build/packages/<package-id>.fcore
```

The command prints the sealed package directory. The matching `.fcore` contains
only `manifest.toml` followed by `core.rbf` in deterministic, uncompressed POSIX
ustar framing. The `.build-inputs.json` file stays outside the package because it
is build evidence rather than runtime metadata. Export requires a clean source
checkout at the manifest revision, tracked recipe and ABI-definition files with
the recorded digests, and clean dependency checkouts at their recorded commits.
It cannot seal artifacts from an uncommitted implementation tree.

The shared reader conformance corpus under `tests/fixtures/core-bundle-v2/` is
checked-in test input, including its `cases.json` index and two `.rbf` payloads;
the repository ignore rules exempt those exact paths from output-file patterns.

See `docs/oracle-method.md` for the explicit Quartus path and
`docs/linux-mailbox-development.md` for the mailbox experiment.

## Loading an experiment

FogCast owns transfer and FPGA load. The designated native kit has no
`/dev/MiSTer_cmd`. Claim a lease and stream a local RBF with `scripts/kit.py`
(see Shared native development kit below). Direct host
`POST /api/v1/session/development-rbf` is the same physical path when the host
already holds the lease. HDMI stays powered down. These experiments are not
MiSTer-compatible cores: after programming, the runtime probes SPI identity on
the FPGA-manager GPO/GPI pair, that probe fails, and Stop restores idle through
the existing development reboot handshake.

`make program` is a separate Main-FIFO or JTAG diagnostic for a conventional
MiSTer or DE10-Nano. It is not the native kit path and never writes flash or
the SD card.

```sh
PROGRAM_DRY_RUN=1 MISTER_HOST=misterpi MISTER_USER=root \
  make program EXP=020_linux_mailbox BUILD=oss
```

The old `dev-bundle`, `dev-load`, `dev-preflight`, and `dev-fault-inject`
transport was abandoned. It duplicated FogCast's responsibility and is absent
from this recovery branch; Git history retains it.

## Repository layout

- `experiments/`: RTL, simulation, constraints, and minimal Quartus projects.
- `boards/de10nano/`: shared device and pin constraints.
- `scripts/`: tool bootstrap, build, comparison, diagnostics, and optional
  direct programming.
- `toolchain.lock`: pinned OSS tool sources and commits.
- `build/`: ignored generated tools, reports, manifests, and RBF files.

The current build structure is described in `docs/architecture.md`.

## Shared native development kit

Use Python 3.11+ `scripts/kit.py` for an interactive development session against
FogCast's lease-enabled target agent. The agent owns the lease and cleanup;
this client only calls its existing native development-RBF API. Builds do not
claim a kit. Configure a private FogCast host TOML, or set `FOGCAST_BASE_URL`
and `FOGCAST_TOKEN` in your environment (never put tokens in command arguments).

```sh
python3 scripts/kit.py --config /absolute/path/config.toml status
python3 scripts/kit.py --config /absolute/path/config.toml session \
  --owner my-agent --purpose 'mailbox OSS bring-up'
```

At the session prompt, enter `load build/oss/020_linux_mailbox/top.rbf`, `status`,
`stop`, or `release`. Quote paths containing spaces. Repeat loads in the same
session. A non-MiSTer development image programs, then fails the MiSTer SPI
identity probe; `load` keeps the lease and reports `development probe timed out`
so the operator can peek GPI before Stop. Stop posts `/v1/stop`. When the
target reports `recovery: reboot_required`, Stop records `/v1/health` `boot_id`,
posts `/v1/development/reboot`, and waits for a new boot ID plus a free lease.
That reboot restarts the target agent, so the current lease ends. Otherwise
Stop returns to idle while retaining ownership. release, EOF, or Ctrl-C first
Stops if a development image was loaded, so a non-MiSTer bitstream takes the
same reboot handshake instead of a raw release that can block the lease. Keep stdin open between commands (including when using an agent's
persistent terminal session). `make kit-session` is a convenience using the
environment configuration.

The client renews every 20 seconds and checks the held state, generation and
token on each grant. It measures relative expiry against its monotonic clock,
so a target without a working real-time clock is supported. If renewal fails it disables mutations and
ends the session; it never silently reacquires. If the process dies or cannot
release, the target's 90-second lease expires and the agent attempts cleanup.
Failed cleanup leaves the kit unavailable for inspection/recovery, rather than
handing uncertain hardware to another owner. A release response may show
`revoking` until cleanup completes; use `status` to confirm it becomes free.

An operator can deliberately replace a stuck owner using the generation shown
by `status`. The existing bearer credential is the operator credential in this
first version; owner names are descriptive, not authorization identities.

```sh
python3 scripts/kit.py --config /absolute/path/config.toml takeover \
  --owner operator --purpose 'mailbox bring-up' \
  --expected-generation GENERATION_FROM_STATUS --reason 'previous agent exited'
```

Takeover retries the same request for at most 60 seconds while cleanup runs;
it does not interrupt FPGA programming and never forces a different generation.
It opens a normal renewable interactive session once granted. An unconfirmed
claim can expire automatically; inspect status before trying again.

`make program` uses direct Main/SSH or JTAG and **bypasses this lease**. Treat it
as an explicit maintenance escape only after coordinating with the current
owner. Ordinary native bring-up uses `kit.py`; do not run both paths concurrently.
No build target uploads an RBF automatically. The native development loader
still requires a compatible MiSTer framework ABI; ownership does not make an
arbitrary bare experimental RBF compatible.

## Four-system bundles

Run `make export-core-bundle CORE=snes` after `make rebuild-core CORE=snes`, or
`make export-core-bundle CORE=nes` after `make rebuild-core CORE=nes`, or
`make export-core-bundle CORE=pong` after `make build-pong`. Each prints a sealed
`build/bundles/<system>/<sha256>/` directory containing `<system>.rbf` and
`<system>-rbf.toml`. Pong export requires a clean committed source tree and
checks its local/framework input record. SNES and NES source exports reject
failed, missing or stale timing evidence when a build receipt is present.
Neither build nor export programs the kit.
