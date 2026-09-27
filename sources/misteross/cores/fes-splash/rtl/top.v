// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"

// Board-firmware splash shell: HDMI pixels plus HPS I2C to the ADV7513.
// No HPS GP mailbox and no MiSTer user-io. U-Boot loads this bitstream and
// latches its FPGA-to-SDRAM port layout, so it carries the fes.memory.hps-ddr
// configuration with every port idle.
// Hold DE/HS/VS/RGB until the soft pixel PLL locks after FPGA reconfig
// (ADV VSYNC-absent black dig 2026-09-22). No extra refclk sequential
// domain — splash timing evidence requires a single pixel clock domain.

module top (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA
);
    wire hdmi_scl_in;
    wire hdmi_sda_in;
    wire hdmi_scl_low;
    wire hdmi_sda_low;
    wire pixel_clk;
    wire pll_locked;

    MISTRAL_IO hdmi_scl_pad (
        .I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL)
    );
    MISTRAL_IO hdmi_sda_pad (
        .I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA)
    );
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in),
        .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk),
        .locked(pll_locked)
    );

    wire [23:0] splash_rgb;
    wire splash_de;
    wire splash_hs;
    wire splash_vs;

    fes_splash_core splash (
        .pixel_clk(pixel_clk),
        .rst(~pll_locked),
        .hdmi_rgb(splash_rgb),
        .hdmi_de(splash_de),
        .hdmi_hs(splash_hs),
        .hdmi_vs(splash_vs),
        .frame_tick(),
        .phase()
    );

    // The controller adopts these cfg_* values once, when U-Boot's bridge
    // enable writes staticcfg.applycfg with this bitstream loaded. Every
    // fes.memory.hps-ddr core drives the same values. No clock and no
    // command reaches the ports here; only pins every pinned Yosys
    // blackbox declares are connected.
    localparam [31:0] DDR_PORT_WIDTH = `FES_APPLICATION_HPS_DDR_CFG_PORT_WIDTH;
    localparam [31:0] DDR_CPORT_TYPE = `FES_APPLICATION_HPS_DDR_CFG_CPORT_TYPE;
    localparam [31:0] DDR_CPORT_WFIFO_MAP = `FES_APPLICATION_HPS_DDR_CFG_CPORT_WFIFO_MAP;
    localparam [31:0] DDR_CPORT_RFIFO_MAP = `FES_APPLICATION_HPS_DDR_CFG_CPORT_RFIFO_MAP;
    localparam [31:0] DDR_WFIFO_CPORT_MAP = `FES_APPLICATION_HPS_DDR_CFG_WFIFO_CPORT_MAP;
    localparam [31:0] DDR_RFIFO_CPORT_MAP = `FES_APPLICATION_HPS_DDR_CFG_RFIFO_CPORT_MAP;
    localparam [31:0] DDR_AXI_MM_SELECT = `FES_APPLICATION_HPS_DDR_CFG_AXI_MM_SELECT;
    cyclonev_hps_interface_fpga2sdram hps_ddr_layout (
        .cfg_axi_mm_select(DDR_AXI_MM_SELECT[5:0]),
        .cfg_cport_rfifo_map(DDR_CPORT_RFIFO_MAP[17:0]),
        .cfg_cport_type(DDR_CPORT_TYPE[11:0]),
        .cfg_cport_wfifo_map(DDR_CPORT_WFIFO_MAP[17:0]),
        .cfg_port_width(DDR_PORT_WIDTH[11:0]),
        .cfg_rfifo_cport_map(DDR_RFIFO_CPORT_MAP[15:0]),
        .cfg_wfifo_cport_map(DDR_WFIFO_CPORT_MAP[15:0]),
        .cmd_port_clk_0(1'b0), .cmd_port_clk_1(1'b0), .cmd_port_clk_2(1'b0),
        .cmd_port_clk_3(1'b0), .cmd_port_clk_4(1'b0), .cmd_port_clk_5(1'b0),
        .cmd_valid_0(1'b0), .cmd_valid_1(1'b0), .cmd_valid_2(1'b0),
        .cmd_valid_3(1'b0), .cmd_valid_4(1'b0), .cmd_valid_5(1'b0),
        .wr_clk_0(1'b0), .wr_clk_1(1'b0), .wr_clk_2(1'b0), .wr_clk_3(1'b0),
        .wr_valid_0(1'b0), .wr_valid_1(1'b0), .wr_valid_2(1'b0), .wr_valid_3(1'b0),
        .rd_clk_0(1'b0), .rd_clk_1(1'b0), .rd_clk_2(1'b0), .rd_clk_3(1'b0),
        .rd_ready_0(1'b1), .rd_ready_1(1'b1), .rd_ready_2(1'b1), .rd_ready_3(1'b1),
        .wrack_ready_0(1'b1), .wrack_ready_1(1'b1), .wrack_ready_2(1'b1),
        .wrack_ready_3(1'b1), .wrack_ready_4(1'b1), .wrack_ready_5(1'b1)
    );

    assign HDMI_TX_CLK = pixel_clk;
    assign HDMI_TX_DE = pll_locked & splash_de;
    assign HDMI_TX_HS = pll_locked & splash_hs;
    assign HDMI_TX_VS = pll_locked & splash_vs;
    assign HDMI_TX_D = pll_locked ? splash_rgb : 24'd0;
endmodule
