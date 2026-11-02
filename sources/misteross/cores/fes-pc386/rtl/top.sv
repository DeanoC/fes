// SPDX-License-Identifier: MIT
`include "fes_application.vh"
`include "fes_computer.vh"
`ifndef FES_PC386_BUILD_ID
`define FES_PC386_BUILD_ID 128'h00000000000000000000000000000000
`endif

// DE10-Nano shell of the pc386 package (fes.computer 1.1;
// docs/pc386-machine-design.md, phase M5). pc386_system holds the mailbox,
// the machine and fes_i386_cpu, all at SYSTEM_CLOCK: identity, execution, the
// session-display plane, the ATA disk the runtime writes into the DDR window,
// the fes.pc386.machine record and the wall clock. Its ddr_hold holds every
// HPS DDR port, which are all in the system clock. 720p60 HDMI comes from the
// presenter through the plane; the mixer's 48 kHz samples leave through I2S.
module top #(
    parameter [127:0] BUILD_ID = `FES_PC386_BUILD_ID,
    parameter SYSTEM_CLOCK = "61.44 MHz",
    parameter integer ISA_N = 625, ISA_D = 4608,
    parameter integer PIT_N = 875, PIT_D = 22528,
    parameter integer RTC_N = 1, RTC_D = 1875,
    parameter integer US_N = 25, US_D = 1536,
    parameter integer DMA_N = 625, DMA_D = 9216,
    parameter integer DOT0_N = 5035, DOT0_D = 12288,
    parameter integer DOT1_N = 14161, DOT1_D = 30720,
    parameter integer OPL2_N = 875, OPL2_D = 1081344,
    parameter integer AUDIO_N = 1, AUDIO_D = 1280,
    parameter integer GOV_N = 5, GOV_D = 3072,
    parameter integer ATA_RESET_CLOCKS = 615
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA,
    output wire        HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire clk_sys, audio_clk, audio_locked, pixel_clk, pixel_locked;
    pc386_system_pll #(.SYSTEM_CLOCK(SYSTEM_CLOCK)) system_clock (
        .refclk(FPGA_CLK1_50), .system_clk(clk_sys), .audio_clk(audio_clk), .locked(audio_locked)
    );
    pixel_pll video_clock (.refclk(FPGA_CLK1_50), .rst(1'b0), .outclk_0(pixel_clk), .locked(pixel_locked));

    wire [31:0] fpga_to_hps, hps_to_fpga;
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(fpga_to_hps), .gp_out(hps_to_fpga));

    // Quartus drives the open-drain I2C pins from the hard block. The OSS
    // lane keeps explicit pads and the fixed Cyclone V I2C site.
    wire hdmi_scl_low, hdmi_sda_low;
`ifdef QUARTUS
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(HDMI_I2C_SCL), .sda(HDMI_I2C_SDA), .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );
    assign HDMI_I2C_SCL = hdmi_scl_low ? 1'b0 : 1'bz;
    assign HDMI_I2C_SDA = hdmi_sda_low ? 1'b0 : 1'bz;
`else
    wire hdmi_scl_in, hdmi_sda_in;
    MISTRAL_IO hdmi_scl_pad (.I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO hdmi_sda_pad (.I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in), .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );
`endif

    wire exec_reset, ddr_hold;
    wire p0_reset, p0_drained, p0_read, p0_write, p0_waitrequest, p0_readdatavalid;
    wire [27:0] p0_address;
    wire [7:0] p0_burstcount;
    wire [127:0] p0_writedata, p0_readdata;
    wire [15:0] p0_byteenable;
    wire p1_reset, p1_drained, p1_read, p1_write, p1_waitrequest, p1_readdatavalid;
    wire [28:0] p1_address;
    wire [7:0] p1_burstcount, p1_byteenable;
    wire [63:0] p1_writedata, p1_readdata;
    wire p2_reset, p2_drained, p2_read, p2_write, p2_waitrequest, p2_readdatavalid;
    wire [28:0] p2_address;
    wire [7:0] p2_burstcount, p2_byteenable;
    wire [63:0] p2_writedata, p2_readdata;
    wire [27:0] video;
    wire signed [15:0] sample;
    // The Quartus seal reads the machine as the netlist Yosys elaborates with
    // these rates (scripts/build_fes_pc386_quartus.py), so there it takes no
    // parameters.
    pc386_system
`ifndef QUARTUS
    #(
        .ISA_N(ISA_N), .ISA_D(ISA_D), .PIT_N(PIT_N), .PIT_D(PIT_D), .RTC_N(RTC_N), .RTC_D(RTC_D),
        .US_N(US_N), .US_D(US_D), .DMA_N(DMA_N), .DMA_D(DMA_D), .DOT0_N(DOT0_N), .DOT0_D(DOT0_D),
        .DOT1_N(DOT1_N), .DOT1_D(DOT1_D), .OPL2_N(OPL2_N), .OPL2_D(OPL2_D),
        .AUDIO_N(AUDIO_N), .AUDIO_D(AUDIO_D), .GOV_N(GOV_N), .GOV_D(GOV_D), .ATA_RESET_CLOCKS(ATA_RESET_CLOCKS)
    )
`endif
    /* verilator lint_off PINCONNECTEMPTY */
    system (
        .clk(clk_sys), .pixel_clk(pixel_clk), .pixel_locked(pixel_locked),
        .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
        .exec_reset(exec_reset), .ddr_hold(ddr_hold),
        .p0_reset(p0_reset), .p1_reset(p1_reset), .p2_reset(p2_reset),
        .video(video), .sample(sample), .status(), .debug_valid(), .debug_byte(),
        .p0_address(p0_address), .p0_burstcount(p0_burstcount), .p0_read(p0_read), .p0_write(p0_write),
        .p0_writedata(p0_writedata), .p0_byteenable(p0_byteenable), .p0_waitrequest(p0_waitrequest),
        .p0_readdata(p0_readdata), .p0_readdatavalid(p0_readdatavalid),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount), .p1_read(p1_read), .p1_write(p1_write),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_waitrequest(p1_waitrequest),
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid),
        .p2_address(p2_address), .p2_burstcount(p2_burstcount), .p2_read(p2_read), .p2_write(p2_write),
        .p2_writedata(p2_writedata), .p2_byteenable(p2_byteenable), .p2_waitrequest(p2_waitrequest),
        .p2_readdata(p2_readdata), .p2_readdatavalid(p2_readdatavalid)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    fes_hps_ddr memory (
        .hold(ddr_hold),
        .p0_clk(clk_sys), .p0_reset(p0_reset), .p0_drained(p0_drained),
        .p0_address(p0_address), .p0_burstcount(p0_burstcount),
        .p0_waitrequest(p0_waitrequest), .p0_readdata(p0_readdata),
        .p0_readdatavalid(p0_readdatavalid), .p0_read(p0_read),
        .p0_writedata(p0_writedata), .p0_byteenable(p0_byteenable), .p0_write(p0_write),
        .p1_clk(clk_sys), .p1_reset(p1_reset), .p1_drained(p1_drained),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount),
        .p1_waitrequest(p1_waitrequest), .p1_readdata(p1_readdata),
        .p1_readdatavalid(p1_readdatavalid), .p1_read(p1_read),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_write(p1_write),
        .p2_clk(clk_sys), .p2_reset(p2_reset), .p2_drained(p2_drained),
        .p2_address(p2_address), .p2_burstcount(p2_burstcount),
        .p2_waitrequest(p2_waitrequest), .p2_readdata(p2_readdata),
        .p2_readdatavalid(p2_readdatavalid), .p2_read(p2_read),
        .p2_writedata(p2_writedata), .p2_byteenable(p2_byteenable), .p2_write(p2_write)
    );
    wire unused = ^{p0_drained, p1_drained, p2_drained};

    assign HDMI_TX_D = video[23:0];
    assign HDMI_TX_DE = video[24];
    assign HDMI_TX_HS = video[25];
    assign HDMI_TX_VS = video[26];
    assign HDMI_TX_CLK = pixel_clk;

    fes_audio_output audio (
        .source_clk(clk_sys), .audio_clk(audio_clk), .locked(audio_locked), .hold(exec_reset),
        .left_sample(sample), .right_sample(sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
    assign HDMI_MCLK = audio_clk;
endmodule
