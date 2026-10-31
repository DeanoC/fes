// Clock/configuration shell only. Actual pixels come from the allocated-buffer
// endpoint in Qsys, not a local test-pattern or fake DDR responder.
module fes_de25_scanout_board (
    input wire CLOCK0_50, system_clk, top_reset_n, ninit_done, HDMI_TX_INT,
    input wire [23:0] video_rgb,
    input wire video_de, video_hs, video_vs,
    output wire pixel_clk, scanout_reset_n, clock_fault,
    output wire HDMI_TX_CLK, HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    output wire HDMI_I2C_SCL,
    inout wire HDMI_I2C_SDA,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire pixel_locked, config_reset_n, qualified_lock;
    (* preserve, dont_merge *) reg first_lock_seen = 1'b0;
    (* preserve, dont_merge *) reg fault = 1'b0;
    sys_pll video_clock (.refclk(CLOCK0_50), .rst(ninit_done),
                         .outclk_0(pixel_clk), .locked(pixel_locked));
    fes_de25_top_reset config_reset (.clk(CLOCK0_50), .ninit_done(ninit_done),
        .pll_locked(1'b1), .reset_n(config_reset_n));
    // Asynchronous assertion stretches even a brief lock-loss event across
    // system-clock edges; release is synchronized to the independent 100 MHz.
    // This qualifier monitors lock and does not reset live CDC/accounting.
    fes_de25_top_reset lock_qualifier (.clk(system_clk), .ninit_done(!top_reset_n),
        .pll_locked(pixel_locked), .reset_n(qualified_lock));
    always @(posedge system_clk or negedge top_reset_n) begin
        if (!top_reset_n) begin first_lock_seen <= 1'b0; fault <= 1'b0; end
        else begin
            if (qualified_lock) first_lock_seen <= 1'b1;
            if (first_lock_seen && !qualified_lock) fault <= 1'b1;
        end
    end
    // This asynchronous source asserts the child reset bridges immediately.
    // Each bridge releases synchronously in its own receiving clock domain.
    // Initial release waits for the first lock; later lock loss never erases
    // traffic or resets either live bridge.
    assign scanout_reset_n = first_lock_seen;
    assign clock_fault = fault;
    I2C_HDMI_Config hdmi_config (.iCLK(CLOCK0_50), .iRST_N(config_reset_n),
        .I2C_SCLK(HDMI_I2C_SCL), .I2C_SDAT(HDMI_I2C_SDA),
        .HDMI_TX_INT(HDMI_TX_INT), .READY());
    assign {HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S} = 4'b0000;
    wire visible_request = scanout_reset_n && pixel_locked && qualified_lock && !fault;
    // The qualified-lock signal asserts asynchronously and releases on the
    // system clock. Resetting this synchronizer on its assertion preserves an
    // immediate blank when the pixel clock stops; visibility itself only
    // leaves this pixel domain through registered state.
    (* async_reg = "true" *) reg [1:0] visible_sync = 2'b00;
    reg previous_hs = 1'b0;
    reg visible_line = 1'b0;
    reg video_active = 1'b0;
    always @(posedge pixel_clk or negedge qualified_lock) begin
        if (!qualified_lock) visible_sync <= 2'b00;
        else visible_sync <= {visible_sync[0], visible_request};
    end
    always @(posedge pixel_clk) begin
        previous_hs <= video_hs;
        // Apply visibility after the complete sync pulse, in the horizontal
        // porch. Hold it through vertical sync so VS cannot be clipped.
        if (previous_hs && !video_hs && !video_vs)
            visible_line <= visible_sync[1];
    end
    // This registered gate is the only data/DE visibility control at the
    // pins. Its async assertion is driven by a pixel-domain synchronizer Q;
    // lock loss clears the chain immediately, while release is pixel-clocked.
    always @(posedge pixel_clk or negedge visible_sync[1]) begin
        if (!visible_sync[1]) video_active <= 1'b0;
        else if (previous_hs && !video_hs && !video_vs) video_active <= 1'b1;
    end
    assign HDMI_TX_CLK = pixel_clk;
    // Data/DE use pixel-domain registered visibility only. qualified_lock
    // asynchronously clears the synchronizer on reset/PLL loss, then picture
    // returns only after the complete sync pulse outside VS. Sync keeps using
    // the unmasked raster until its own safe line boundary.
    assign HDMI_TX_D = video_active && visible_line ? video_rgb : 24'd0;
    assign HDMI_TX_DE = video_active && visible_line && video_de;
    assign HDMI_TX_HS = visible_line && video_hs;
    assign HDMI_TX_VS = visible_line && video_vs;
endmodule
