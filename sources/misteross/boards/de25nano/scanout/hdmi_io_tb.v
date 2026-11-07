module fes_de25_hdmi_io_tb(input wire clk, reset, irq_n, slave_low,
        output wire scl, sda, ready, irq_filtered);
    tri1 bus_sda;
    assign bus_sda = slave_low ? 1'b0 : 1'bz;
    assign sda = bus_sda;
    wire unused_pixel, unused_reset, unused_fault, unused_display;
    wire [23:0] unused_rgb;
    fes_de25_scanout_board board (
        .CLOCK0_50(clk), .system_clk(clk), .top_reset_n(!reset), .ninit_done(reset),
        .HDMI_TX_INT(irq_n), .video_rgb(24'd0), .video_de(1'b0), .video_hs(1'b0), .video_vs(1'b0),
        .pixel_clk(unused_pixel), .scanout_reset_n(unused_reset), .clock_fault(unused_fault),
        .display_ready(unused_display), .HDMI_TX_CLK(), .HDMI_TX_DE(), .HDMI_TX_HS(),
        .HDMI_TX_VS(), .HDMI_TX_D(unused_rgb), .HDMI_I2C_SCL(scl), .HDMI_I2C_SDA(bus_sda),
        .HDMI_MCLK(), .HDMI_SCLK(), .HDMI_LRCLK(), .HDMI_I2S());
    assign ready = board.hdmi_ready;
    assign irq_filtered = board.hdmi_int_sync[1];
endmodule
