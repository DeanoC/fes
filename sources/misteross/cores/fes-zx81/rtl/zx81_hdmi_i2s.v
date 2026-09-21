// SPDX-License-Identifier: GPL-2.0-or-later
// ADV7513 I2S0 master for the socketed ZX81 HDMI shell.
//
// fes-gp-v1 Menu720p60 already programs I2S0, 16-bit, 48 kHz, N=6144,
// CTS=74250 against the 74.25 MHz pixel clock. Phase accumulators derive
// MCLK=256*fs and SCLK=64*fs from that pixel clock. Signed 16-bit PCM.
module zx81_hdmi_i2s (
    input  wire               pixel_clk,
    input  wire signed [15:0] sample,
    output wire               mclk,
    output wire               sclk,
    output reg                lrclk,
    output reg                i2s
);
    // round(Hz * 2^32 / 74_250_000)
    localparam [31:0] MCLK_INC = 32'd708669604;
    localparam [31:0] SCLK_INC = 32'd177167401;

    reg [31:0] mclk_phase;
    reg [31:0] sclk_phase;
    reg        sclk_d;
    reg [5:0]  bit_index;
    reg [15:0] shift;
    reg signed [15:0] sample_meta;
    reg signed [15:0] sample_pix;

    wire sclk_bit = sclk_phase[31];
    wire sclk_fall = sclk_d && !sclk_bit;

    assign mclk = mclk_phase[31];
    assign sclk = sclk_bit;

    initial begin
        mclk_phase = 32'd0;
        sclk_phase = 32'd0;
        sclk_d = 1'b0;
        bit_index = 6'd0;
        shift = 16'd0;
        sample_meta = 16'd0;
        sample_pix = 16'd0;
        lrclk = 1'b0;
        i2s = 1'b0;
    end

    always @(posedge pixel_clk) begin
        mclk_phase <= mclk_phase + MCLK_INC;
        sclk_phase <= sclk_phase + SCLK_INC;
        sclk_d <= sclk_bit;
        sample_meta <= sample;
        sample_pix <= sample_meta;
        if (sclk_fall) begin
            if (bit_index == 6'd0 || bit_index == 6'd32) begin
                shift <= sample_pix;
                lrclk <= (bit_index == 6'd32);
                i2s <= 1'b0;
            end else if (bit_index[4:0] <= 5'd16) begin
                i2s <= shift[15];
                shift <= {shift[14:0], 1'b0};
            end else begin
                i2s <= 1'b0;
            end
            bit_index <= bit_index + 1'b1;
        end
    end
endmodule
