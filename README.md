# Open MiSTer FPGA development environment

This repository builds small experimental RBF files for the MiSTer/DE10-Nano
Cyclone V FPGA (`5CSEBA6U23I7`). It exists to make ordinary MiSTer core
development possible with both the open-source Mistral toolchain and Quartus.

## What works now

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
  Locked Yosys still emits a clocked read, so OSS sets `CFG_ASYNC_READ`
  and drops `B1EN`/`CLK2`. Run `make sim EXP=770_m10k_async_read` and
  `make oss EXP=770_m10k_async_read`; no Quartus comparison lane is
  implemented.
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
raster. It is simulation, not a Quartus RBF or kit evidence.

`make build-fes-zx81-quartus` is the Quartus Prime Lite 17.0.2 bring-up
recipe for package `fes.zx81` 1.0.0. Set
`QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0/quartus`. It requires a
clean committed tree, writes `build/fes-zx81-quartus/build-inputs.json`,
embeds that build id, and seals a format-2 package when timing passes. This
does not program hardware.

`make build-fes-zx81` authenticates the repository-local Yosys,
nextpnr-mistral and Mistral cache against `toolchain.lock`, synthesizes the
same board shell with Verilog T80pa/TV80 and M10K allowed, and seals
`build/fes-zx81-oss/` when the 52 MHz system clock and 74.25 MHz pixel
clock pass timing. The 52 MHz integer uses the 520 MHz PLL feedback
profile (M=52 N=5 C6=10). Recipe presence alone is no RBF, timing or
hardware-support evidence. The command never programs a kit.

`make sim-fes-pong` tests the separate `fes.simple-game` GP transport and exact
74.25 MHz-domain 720p raster model. It reuses only `pong_game.sv` from the
MiSTer Pong and simulates the board top with independently driven,
simulation-only HPS and PLL boundaries. The board test also checks HDMI I2C
low-or-release drive and feedback from an external device. The standalone shell
uses the HPS I2C bridge at X52/Y60 with U10/AA4 pads; its export checks require
that exact route and constant-low output data.

`make build-fes-pong` authenticates the repository-local Yosys,
nextpnr-mistral and Mistral cache against `toolchain.lock`, constructs canonical
`build/fes-pong/build-inputs.json` before synthesis, and passes its 128-bit ID
as `top.BUILD_ID`. It rejects missing or failing 50/74.25 MHz timing, incomplete
routing, unexpected hard resources, a changed tool identity, or a dirty source
checkout before calling the format-2 exporter. The command never programs a
kit.

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
