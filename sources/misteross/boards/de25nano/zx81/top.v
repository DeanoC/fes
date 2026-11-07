// SPDX-License-Identifier: GPL-2.0-or-later
// Standalone DE25 ZX81 diagnostic. No HPS, external DDR or runtime ABI.
module de25nano_zx81 (
    input wire CLOCK0_50,
    input wire [1:0] KEY,
    input wire HDMI_TX_INT,
    output wire HDMI_TX_CLK, HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    output wire HDMI_I2C_SCL,
    inout wire HDMI_I2C_SDA,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire ninit_done, pixel_clk;
    wire pixel_pll_locked;
    wire config_reset_n /* verilator public_flat_rd */;
    wire pixel_reset_n /* verilator public_flat_rd */;
    wire system_reset_n = pixel_reset_n;
    wire clk_sys = pixel_clk;
    agilex_reset_release configuration_release (.ninit_done(ninit_done));
    // Rational enables (26/297) preserve the 6.5 MHz ULA/3.25 MHz CPU on the
    // exact 74.25 MHz clock. Capture and output now also share one clock.
    sys_pll video_clock (.refclk(CLOCK0_50), .rst(ninit_done),
                         .outclk_0(pixel_clk), .locked(pixel_pll_locked));
    fes_de25_top_reset config_reset (.clk(CLOCK0_50), .ninit_done(ninit_done),
                                     .pll_locked(1'b1), .reset_n(config_reset_n));
    // Assertion blanks without another pixel edge; release waits for two edges.
    fes_de25_top_reset pixel_reset (.clk(pixel_clk), .ninit_done(ninit_done),
        .pll_locked(pixel_pll_locked), .reset_n(pixel_reset_n));

    I2C_HDMI_Config hdmi_config (.iCLK(CLOCK0_50), .iRST_N(config_reset_n),
        .I2C_SCLK(HDMI_I2C_SCL), .I2C_SDAT(HDMI_I2C_SDA),
        .HDMI_TX_INT(HDMI_TX_INT), .READY());
    assign {HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S} = 4'b0000;

    wire [1:0] keys_stable;
    de25_zx81_keys local_keys (.clk(clk_sys), .reset_n(system_reset_n),
                              .key_n(KEY), .stable_n(keys_stable));
    wire machine_reset /* verilator public_flat_rd */;
    wire [39:0] keyboard /* verilator public_flat_rd */;
    assign machine_reset = !system_reset_n || !keys_stable[0];
    // The machine matrix is active-low. Row 6, column 0 is NEWLINE/ENTER.
    assign keyboard = {9'h1ff, keys_stable[1], 30'h3fffffff};

    wire ce_6m5, video_pixel, hblank, vblank;
    wire [15:0] cpu_addr /* verilator public_flat_rd */;
    wire halt_n /* verilator public_flat_rd */;
`ifdef QUARTUS
    localparam FIRMWARE_INIT = "zx8x.mif";
`else
    localparam FIRMWARE_INIT = "zx8x.hex";
`endif
    zx81_machine #(.EXTERNAL_RAM(0), .FIRMWARE_INIT(FIRMWARE_INIT),
        .CLOCK_NUMERATOR(26), .CLOCK_DENOMINATOR(297)) machine (
        .clk_sys(clk_sys), .reset(machine_reset), .keyboard(keyboard),
        .tape_ready(1'b0), .tape_size(15'd0), .tape_data(8'd0), .tape_addr_out(), .tape_busy(),
        .save_ready(), .save_busy(), .save_changed(), .save_size(),
        .save_addr(14'd0), .save_q(),
        .ce_6m5(ce_6m5), .video_pixel(video_pixel), .hblank(hblank), .vblank(vblank),
        .hsync_out(), .vsync_out(), .halt_n(halt_n), .cpu_addr(cpu_addr),
        .peek_addr(16'd0), .peek_data(), .ram_address(), .ram_write_data(), .ram_write_enable(),
        .external_ram_data(8'hff), .external_peek_data(8'hff),
        .bus_addr(), .bus_wdata(), .bus_mreq_n(), .bus_iorq_n(), .bus_rd_n(),
        .bus_wr_n(), .bus_m1_n(), .bus_rfsh_n(), .bus_cpu_clock(),
        .bus_rdata(8'hff), .bus_peek_data(8'hff), .bus_dsel(1'b0),
        .bus_romcs(1'b0), .bus_wait(1'b0), .bus_ram_present(1'b0));

    wire [7:0] red, green, blue;
    wire de, hs, vs;
    zx81_video_720p video (.clk_sys(clk_sys), .ce_6m5(ce_6m5), .zx_pixel(video_pixel),
        .hblank(hblank), .vblank(vblank), .pixel_clk(pixel_clk),
        .red(red), .green(green), .blue(blue), .de(de), .hsync(hs), .vsync(vs),
        .frame_tick(), .raster_h(), .raster_v(), .src_x_max(), .src_y_max());

    assign HDMI_TX_CLK = pixel_clk;
    assign HDMI_TX_DE = pixel_reset_n && de;
    assign HDMI_TX_HS = pixel_reset_n && hs;
    assign HDMI_TX_VS = pixel_reset_n && vs;
    assign HDMI_TX_D = pixel_reset_n ? {red, green, blue} : 24'd0;
endmodule
