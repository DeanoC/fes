// SPDX-License-Identifier: GPL-2.0-or-later
// FPGA-only DE25 display diagnostics. No HPS, DDR, GP or runtime menu ABI.
module de25nano_display #(
    parameter [0:0] MENU_PATTERN = 1'b0
) (
    input wire CLOCK0_50,
    input wire HDMI_TX_INT,
    output wire HDMI_TX_CLK, HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    output wire HDMI_I2C_SCL,
    inout wire HDMI_I2C_SDA,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire ninit_done, pixel_clk, pll_locked, pixel_reset_n, config_reset_n;
    agilex_reset_release configuration_release (.ninit_done(ninit_done));
    sys_pll video_clock (.refclk(CLOCK0_50), .rst(ninit_done),
                         .outclk_0(pixel_clk), .locked(pll_locked));
    fes_de25_top_reset config_reset (.clk(CLOCK0_50), .ninit_done(ninit_done),
                                     .pll_locked(1'b1), .reset_n(config_reset_n));
    fes_de25_top_reset pixel_reset (.clk(pixel_clk), .ninit_done(ninit_done),
                                    .pll_locked(pll_locked), .reset_n(pixel_reset_n));

    // Use the board reference's FPGA-side ADV7513 initializer, not DE10 HPS I2C.
    I2C_HDMI_Config hdmi_config (.iCLK(CLOCK0_50), .iRST_N(config_reset_n),
        .I2C_SCLK(HDMI_I2C_SCL), .I2C_SDAT(HDMI_I2C_SDA),
        .HDMI_TX_INT(HDMI_TX_INT), .READY());
    // No audio source in these diagnostics. The reference register table is
    // retained verbatim; audible output/transmitter behavior needs board checks.
    assign {HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S} = 4'b0000;

    wire [23:0] rgb;
    wire de, hs, vs;
    wire [31:0] underflows /* verilator public_flat_rd */;
    generate if (MENU_PATTERN) begin : menu
        reg last_vs = 1'b0;
        reg [5:0] frames = 6'd0;
        reg [31:0] desired_sequence = 32'd0;
        wire [31:0] displayed_sequence;
        always @(posedge pixel_clk) begin
            last_vs <= vs;
            if (!pixel_reset_n) begin
                last_vs <= 1'b0;
                frames <= 6'd0;
                desired_sequence <= 32'd0;
            end else if (vs && !last_vs) begin
                if (frames == 6'd59) begin
                    frames <= 6'd0;
                    desired_sequence <= desired_sequence + 32'd1;
                end else frames <= frames + 6'd1;
            end
        end
        // Real menu reader, FIFO and scanout; local responder only. Its private
        // synthetic addresses are not a physical DDR map or an HPS interface.
        fes_menu_video #(.TEST_PATTERN(1'b1)) scanout (
            .clk(pixel_clk), .rst(!pixel_reset_n), .enable(pixel_reset_n), .quiesce(1'b0),
            .raster_h(11'd0), .raster_v(10'd0),
            .submit_valid(desired_sequence != displayed_sequence),
            .submit_slot(desired_sequence[0]), .submit_sequence(desired_sequence),
            .submit_ready(), .displayed_sequence(displayed_sequence),
            .underflows(underflows), .quiesced(), .rgb(rgb), .alpha(), .de(de), .hs(hs), .vs(vs),
            .address(), .burstcount(), .read(), .waitrequest(1'b0),
            .readdata(128'd0), .readdatavalid(1'b0));
    end else begin : idle
        assign underflows = 32'd0;
        fes_splash_core splash (.pixel_clk(pixel_clk), .rst(!pixel_reset_n),
            .hdmi_rgb(rgb), .hdmi_de(de), .hdmi_hs(hs), .hdmi_vs(vs), .frame_tick(), .phase());
    end endgenerate
    assign HDMI_TX_CLK = pixel_clk;
    assign HDMI_TX_DE = pixel_reset_n && de;
    assign HDMI_TX_HS = pixel_reset_n && hs;
    assign HDMI_TX_VS = pixel_reset_n && vs;
    assign HDMI_TX_D = !pixel_reset_n ? 24'd0 :
        (underflows != 32'd0 && rgb == 24'hffffff ? 24'hff0000 : rgb);
endmodule
