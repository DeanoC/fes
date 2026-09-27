// SPDX-License-Identifier: GPL-2.0-or-later
// Menu scanout diagnostic. Never selects an appliance boot/idle image.
module top #(
    parameter [0:0] TEST_PATTERN = 1'b1,
    parameter [0:0] DIAGNOSTIC_ENABLE = 1'b1
) (
    input wire FPGA_CLK1_50,
    output wire HDMI_TX_CLK, HDMI_TX_DE, HDMI_TX_HS, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    inout wire HDMI_I2C_SCL, HDMI_I2C_SDA
);
    wire scl_in, sda_in, scl_low, sda_low, pixel_clk, locked;
    MISTRAL_IO hdmi_scl_pad (.I(1'b0), .OE(scl_low), .O(scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO hdmi_sda_pad (.I(1'b0), .OE(sda_low), .O(sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(scl_in), .sda(sda_in), .out_clk(scl_low), .out_data(sda_low)
    );
    pixel_pll video_clock (.refclk(FPGA_CLK1_50), .rst(1'b0), .outclk_0(pixel_clk), .locked(locked));
    wire de, hs, vs;
    wire [23:0] rgb;
    wire [31:0] displayed_sequence, underflows;
    reg last_vs = 1'b0;
    reg [5:0] frames = 6'd0;
    reg [31:0] desired_sequence = 32'd0;
    always @(posedge pixel_clk) begin
        last_vs <= vs;
        if (!locked) begin
            frames <= 6'd0;
            desired_sequence <= 32'd0;
            last_vs <= 1'b0;
        end else if (vs && !last_vs) begin
            if (frames == 6'd59) begin
                frames <= 6'd0;
                desired_sequence <= desired_sequence + 32'd1;
            end else frames <= frames + 6'd1;
        end
    end
    generate if (TEST_PATTERN) begin : pattern
    fes_menu_video #(.TEST_PATTERN(1'b1)) scanout (
        .clk(pixel_clk), .rst(!locked), .enable(locked && DIAGNOSTIC_ENABLE), .quiesce(1'b0),
        .submit_valid(desired_sequence != displayed_sequence),
        .submit_slot(desired_sequence[0]), .submit_sequence(desired_sequence),
        .submit_ready(), .displayed_sequence(displayed_sequence),
        .underflows(underflows), .quiesced(), .rgb(rgb), .de(de), .hs(hs), .vs(vs),
        .address(), .burstcount(), .read(), .waitrequest(1'b0),
        .readdata(128'd0), .readdatavalid(1'b0)
    );
    end else begin : ddr
        // This diagnostic reads slot 0 only. There is no host submission ABI.
        fes_menu_ddr scanout (
            .clk(pixel_clk), .reset_hold(!locked), .enable(locked && DIAGNOSTIC_ENABLE),
            .quiesce(1'b0), .submit_valid(1'b0), .submit_slot(1'b0), .submit_sequence(32'd0),
            .submit_ready(), .displayed_sequence(displayed_sequence), .underflows(underflows),
            .quiesced(), .faulted(), .rgb(rgb), .de(de), .hs(hs), .vs(vs)
        );
    end endgenerate
    assign HDMI_TX_CLK = pixel_clk;
    assign HDMI_TX_DE = locked && de;
    assign HDMI_TX_HS = locked && hs;
    assign HDMI_TX_VS = locked && vs;
    // White becomes red if any underflow occurred; normal pattern stays exact.
    assign HDMI_TX_D = !locked ? 24'd0 :
        (underflows != 32'd0 && rgb == 24'hffffff ? 24'hff0000 : rgb);
endmodule
