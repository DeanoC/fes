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
    wire pll_locked, config_reset_n, hdmi_config_ready;
    sys_pll video_clock (.refclk(CLOCK0_50), .rst(ninit_done),
                         .outclk_0(pixel_clk), .locked(pll_locked));
    fes_de25_top_reset config_reset (.clk(CLOCK0_50), .ninit_done(ninit_done),
                                     .pll_locked(pll_locked), .reset_n(config_reset_n));
    I2C_HDMI_Config hdmi_config (.iCLK(CLOCK0_50), .iRST_N(config_reset_n),
        .I2C_SCLK(HDMI_I2C_SCL), .I2C_SDAT(HDMI_I2C_SDA), .HDMI_TX_INT(HDMI_TX_INT), .READY(hdmi_config_ready));
    assign {HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S} = 4'b0000;
    assign HDMI_TX_CLK = pixel_clk;
    // The reference domain owns asynchronous lock/configuration qualification.
    // Retain an outstanding token across repeated faults until the pixel side
    // has observed allow-low and closed its visible line for that token.
    reg visibility_epoch = 0, visibility_pending = 0, visible_allow = 0;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] visibility_ack_sync = 0, active_sync = 0;
    always @(posedge CLOCK0_50) begin
        visibility_ack_sync <= {visibility_ack_sync[0], visibility_ack};
        active_sync <= {active_sync[0], video_active};
        if (!config_reset_n) begin
            visible_allow <= 0;
            if (!visibility_pending) begin
                visibility_epoch <= !visibility_epoch;
                visibility_pending <= 1;
            end
        end else if (visibility_pending) begin
            visible_allow <= 0;
            if (visibility_ack_sync[1] == visibility_epoch && !active_sync[1])
                visibility_pending <= 0;
        end else visible_allow <= 1;
    end

    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] hdmi_ready_sync = 0;
    (* preserve *) reg video_ready_source = 0;
    always @(posedge CLOCK0_50 or negedge config_reset_n) begin
        if (!config_reset_n) begin
            hdmi_ready_sync <= 0;
            video_ready_source <= 0;
        end else begin
            hdmi_ready_sync <= {hdmi_ready_sync[0], hdmi_config_ready};
            video_ready_source <= visible_allow && active_sync[1] && hdmi_ready_sync[1];
        end
    end
    assign video_ready = video_ready_source;

    // These are ordinary data crossings. Their state is retained through PLL
    // loss; the token handshake requires a real blanking observation on resume.
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] visible_sync = 0, visibility_epoch_sync = 0;
    reg visibility_ack = 0, previous_hs = 0, visible_line = 0, video_active = 0;
    wire line_boundary = previous_hs && !video_hs && !video_vs;
    wire epoch_changed = visibility_epoch_sync[1] != visibility_ack;
    always @(posedge pixel_clk) begin
        visible_sync <= {visible_sync[0], visible_allow};
        visibility_epoch_sync <= {visibility_epoch_sync[0], visibility_epoch};
        previous_hs <= video_hs;
        if (epoch_changed || !visible_sync[1]) begin
            video_active <= 0;
            if (!visible_sync[1] && !visible_line)
                visibility_ack <= visibility_epoch_sync[1];
            if (line_boundary) begin
                visible_line <= 0;
                if (!visible_sync[1]) visibility_ack <= visibility_epoch_sync[1];
            end
        end else if (line_boundary) begin
            visible_line <= 1;
            video_active <= 1;
        end
    end
    wire visible_request = config_reset_n && visible_allow;
    assign HDMI_TX_DE = visible_request && visible_sync[1] && visible_line && video_active && video_de;
    assign HDMI_TX_HS = visible_line && video_hs;
    assign HDMI_TX_VS = visible_line && video_vs;
    assign HDMI_TX_D = visible_request && visible_sync[1] && visible_line && video_active ? video_rgb : 24'd0;
endmodule
