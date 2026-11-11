// Clock/configuration shell only. Actual pixels come from the allocated-buffer
// endpoint in Qsys, not a local test-pattern or fake DDR responder.
module fes_de25_scanout_board (
    input wire CLOCK0_50, system_clk, top_reset_n, ninit_done, HDMI_TX_INT,
    input wire [23:0] video_rgb,
    input wire video_de, video_hs, video_vs,
    output wire pixel_clk, scanout_reset_n, clock_fault, display_ready, stream_ready,
    output wire HDMI_TX_CLK,
    output reg HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output reg [23:0] HDMI_TX_D,
    output wire HDMI_I2C_SCL,
    inout wire HDMI_I2C_SDA,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire pixel_locked, config_reset_n;
    (* preserve, dont_merge *) reg first_lock_seen = 1'b0;
    (* preserve, dont_merge *) reg fault = 1'b0;
    sys_pll video_clock (.refclk(CLOCK0_50), .rst(ninit_done),
                         .outclk_0(pixel_clk), .locked(pixel_locked));
    fes_de25_top_reset config_reset (.clk(CLOCK0_50), .ninit_done(ninit_done || !top_reset_n),
        .pll_locked(pixel_locked), .reset_n(config_reset_n));
    // Reference-owned fault generation. A running epoch
    // never acknowledges another token, so a brief loss cannot disappear while
    // the system clock is stopped and cannot reset live FIFO/accounting state.
    reg epoch = 0, pending = 0, visible_allow = 0;
    (* async_reg="true", altera_attribute="-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] system_ack_sync = 0, pixel_ack_sync = 0, pixel_active_sync = 0;
    always @(posedge CLOCK0_50) begin
        system_ack_sync <= {system_ack_sync[0], system_ack};
        pixel_ack_sync <= {pixel_ack_sync[0], pixel_ack};
        pixel_active_sync <= {pixel_active_sync[0], video_active};
        if (!config_reset_n) begin
            visible_allow <= 0;
            if (!pending) begin epoch <= !epoch; pending <= 1; end
        end else if (pending) begin
            visible_allow <= 0;
            if (system_ack_sync[1] == epoch && pixel_ack_sync[1] == epoch && !pixel_active_sync[1])
                pending <= 0;
        end else visible_allow <= 1;
    end
    (* preserve *) reg monitor_ready_source = 0;
    always @(posedge CLOCK0_50 or negedge config_reset_n)
        if (!config_reset_n) monitor_ready_source <= 0;
        else monitor_ready_source <= system_ack_sync[1] == epoch;
    (* async_reg="true", altera_attribute="-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] monitor_ready_sync = 0, system_epoch_sync = 0;
    reg system_ack = 0;
    // Keep the request/ACK generation across global reset. Reinitializing ACK
    // can alias the newly toggled token before the incoming request settles.
    always @(posedge system_clk) begin
        system_epoch_sync <= {system_epoch_sync[0], epoch};
        if (!first_lock_seen) system_ack <= system_epoch_sync[1];
    end
    always @(posedge system_clk or negedge top_reset_n) begin
        if (!top_reset_n) begin
            first_lock_seen <= 0; fault <= 0;
            monitor_ready_sync <= 0;
        end else begin
            monitor_ready_sync <= {monitor_ready_sync[0], monitor_ready_source};
            if (!first_lock_seen) begin
                if (monitor_ready_sync[1] && system_ack == system_epoch_sync[1])
                    first_lock_seen <= 1;
            end else if (!monitor_ready_sync[1] || system_epoch_sync[1] != system_ack)
                fault <= 1;
        end
    end
    // This asynchronous source asserts the child reset bridges immediately.
    // Each bridge releases synchronously in its own receiving clock domain.
    // Initial release waits for the first lock; later lock loss never erases
    // traffic or resets either live bridge.
    assign scanout_reset_n = first_lock_seen;
    assign clock_fault = fault;
    // Both pins originate outside the FPGA clock domains. Raw SDA is used
    // only by the first input stage and the unchanged open-drain driver.
    // ACK sampling remains in the original slow controller; it sees the
    // synchronized level after the existing full SCL-high interval.
    (* preserve, dont_merge, async_reg="true", altera_attribute="-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] hdmi_sda_sync = 2'b11;
    (* preserve, dont_merge, async_reg="true", altera_attribute="-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] hdmi_int_sync = 2'b11;
    always @(posedge CLOCK0_50 or negedge config_reset_n) begin
        if (!config_reset_n) begin hdmi_sda_sync <= 2'b11; hdmi_int_sync <= 2'b11; end
        else begin
            hdmi_sda_sync <= {hdmi_sda_sync[0], HDMI_I2C_SDA};
            hdmi_int_sync <= {hdmi_int_sync[0], HDMI_TX_INT};
        end
    end
    wire hdmi_ready;
    fes_de25_scanout_hdmi_config hdmi_config (.iCLK(CLOCK0_50), .iRST_N(config_reset_n),
        .I2C_SCLK(HDMI_I2C_SCL), .I2C_SDAT(HDMI_I2C_SDA),
        .I2C_SDAT_IN(hdmi_sda_sync[1]), .HDMI_TX_INT(hdmi_int_sync[1]), .READY(hdmi_ready));
    assign {HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S} = 4'b0000;
    // Register the configuration result in its source domain before crossing
    // into the independent system clock. No combinational I2C state drives
    // visibility or the endpoint's observed readiness.
    (* preserve, dont_merge *) reg hdmi_ready_source = 1'b0;
    always @(posedge CLOCK0_50 or negedge config_reset_n)
        if (!config_reset_n) hdmi_ready_source <= 1'b0;
        else hdmi_ready_source <= hdmi_ready;
    (* async_reg="true", altera_attribute="-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] hdmi_ready_sync = 2'b00;
    always @(posedge system_clk or negedge top_reset_n)
        if (!top_reset_n) hdmi_ready_sync <= 2'b00;
        else hdmi_ready_sync <= {hdmi_ready_sync[0], hdmi_ready_source};
    // Register the request before it crosses into the pixel domain. The
    // reference-owned qualification is synchronized before this request;
    // the raw PLL lock does not enter its data path.
    (* preserve, dont_merge *) reg visible_request = 1'b0;
    always @(posedge system_clk or negedge top_reset_n)
        if (!top_reset_n) visible_request <= 1'b0;
        else visible_request <= first_lock_seen && monitor_ready_sync[1] && !fault && hdmi_ready_sync[1];
    // Ordinary pixel data crossings retain state across PLL faults. The
    // reference controller waits for allow-low and a closed line before rearm.
    (* async_reg="true", altera_attribute="-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] visible_sync = 0, allow_sync = 0, pixel_epoch_sync = 0, qualified_sync = 0;
    reg previous_hs = 0, visible_line = 0, video_active = 0, pixel_ack = 0;
    wire boundary = previous_hs && !video_hs && !video_vs;
    always @(posedge pixel_clk) begin
        qualified_sync <= {qualified_sync[0], config_reset_n};
        visible_sync <= {visible_sync[0], visible_request};
        allow_sync <= {allow_sync[0], visible_allow};
        pixel_epoch_sync <= {pixel_epoch_sync[0], epoch};
        previous_hs <= video_hs;
        if (!qualified_sync[1] || !visible_sync[1] || !allow_sync[1] || pixel_epoch_sync[1] != pixel_ack) begin
            video_active <= 0;
            if (!allow_sync[1] && !visible_line) pixel_ack <= pixel_epoch_sync[1];
            if (boundary) begin
                visible_line <= 0;
                if (!allow_sync[1]) pixel_ack <= pixel_epoch_sync[1];
            end
        end else if (boundary) begin visible_line <= 1; video_active <= 1; end
    end
    // Readiness closes within three pixel edges; clock_fault latches within
    // three system edges. Accepted reads keep their reservations and drain
    // after clocks resume. Pin data/DE blank immediately on qualification loss.
    // Transport epoch remains healthy while the
    // transmitter reconfigures. These are existing pixel-domain qualifications;
    // configuration visibility still uses the unchanged complete-line pin gate.
    assign stream_ready = qualified_sync[1] && allow_sync[1] &&
        pixel_epoch_sync[1] == pixel_ack;
    assign display_ready = hdmi_active;
    wire pin_allow = config_reset_n && visible_allow;
    assign HDMI_TX_CLK = pixel_clk;
    reg hdmi_active = 0;
    initial begin
        HDMI_TX_D = 24'd0;
        HDMI_TX_DE = 1'b0;
        HDMI_TX_HS = 1'b0;
        HDMI_TX_VS = 1'b0;
    end
    // Preserve the parent's aligned ADV7513 output stage. The retained token
    // handshake still owns rearming; qualification loss blanks data at once.
    always @(posedge pixel_clk or negedge pin_allow) begin
        if (!pin_allow) begin
            hdmi_active <= 0;
            HDMI_TX_D <= 24'd0;
            HDMI_TX_DE <= 1'b0;
        end else begin
            hdmi_active <= video_active && visible_line;
            HDMI_TX_D <= video_active && visible_line ? video_rgb : 24'd0;
            HDMI_TX_DE <= video_active && visible_line && video_de;
        end
    end
    always @(posedge pixel_clk) begin
        HDMI_TX_HS <= visible_line && video_hs;
        HDMI_TX_VS <= visible_line && video_vs;
    end
endmodule
