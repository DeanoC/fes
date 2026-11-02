// SPDX-License-Identifier: GPL-2.0-or-later
// HDMI shell for the preserved GHRD; the computer/control stays on its 100MHz clock.
module fes_de25_zx81_board (
    input wire CLOCK0_50,
    input wire ninit_done,
    output wire pixel_clk,
    output wire video_ready,
    input wire [23:0] video_rgb,
    input wire video_de, video_hs, video_vs,
    input wire HDMI_TX_INT,
    output wire HDMI_TX_CLK, HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    output wire HDMI_I2C_SCL,
    inout wire HDMI_I2C_SDA,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire pll_locked, config_reset_n, pixel_reset_n, hdmi_config_ready;
    sys_pll video_clock (.refclk(CLOCK0_50), .rst(ninit_done),
                         .outclk_0(pixel_clk), .locked(pll_locked));
    fes_de25_top_reset config_reset (.clk(CLOCK0_50), .ninit_done(ninit_done),
                                     .pll_locked(1'b1), .reset_n(config_reset_n));
    fes_de25_top_reset pixel_reset (.clk(pixel_clk), .ninit_done(ninit_done),
                                    .pll_locked(pll_locked), .reset_n(pixel_reset_n));
    I2C_HDMI_Config hdmi_config (.iCLK(CLOCK0_50), .iRST_N(config_reset_n),
        .I2C_SCLK(HDMI_I2C_SCL), .I2C_SDAT(HDMI_I2C_SDA), .HDMI_TX_INT(HDMI_TX_INT), .READY(hdmi_config_ready));
    assign {HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S} = 4'b0000;
    assign HDMI_TX_CLK = pixel_clk;
    wire visible_request = pixel_reset_n;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] hdmi_ready_sync = 2'b00;
    always @(posedge pixel_clk or negedge pixel_reset_n)
        if (!pixel_reset_n) hdmi_ready_sync <= 2'b00;
        else hdmi_ready_sync <= {hdmi_ready_sync[0], hdmi_config_ready};
    (* async_reg = "true" *) reg [1:0] visible_sync = 2'b00;
    reg previous_hs = 1'b0;
    reg visible_line = 1'b0;
    reg video_active = 1'b0;
    always @(posedge pixel_clk) begin
        visible_sync <= {visible_sync[0], visible_request};
        previous_hs <= video_hs;
        // Blank data immediately on lock/reset loss. Re-enable only after a
        // complete HS pulse in the horizontal porch, outside vertical sync.
        if (previous_hs && !video_hs && !video_vs) begin
            visible_line <= visible_sync[1];
            video_active <= visible_sync[1];
        end else if (!visible_sync[1]) video_active <= 1'b0;
    end
    // Cross a registered observation into the computer clock domain, never a
    // combinational collection of independently changing status/reset terms.
    (* preserve *) reg video_ready_source = 1'b0;
    always @(posedge pixel_clk or negedge pixel_reset_n)
        if (!pixel_reset_n) video_ready_source <= 1'b0;
        else video_ready_source <= video_active && hdmi_ready_sync[1];
    assign video_ready = video_ready_source;
    assign HDMI_TX_DE = visible_request && visible_sync[1] && visible_line && video_active && video_de;
    assign HDMI_TX_HS = visible_line && video_hs;
    assign HDMI_TX_VS = visible_line && video_vs;
    assign HDMI_TX_D = visible_request && visible_sync[1] && visible_line && video_active ? video_rgb : 24'd0;
endmodule
