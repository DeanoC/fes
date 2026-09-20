// SPDX-License-Identifier: GPL-2.0-or-later
// Sega SN76489-compatible PSG for the FES Master System slice.
//
// Writes to ports 0x7E and 0x7F share the same latch/data protocol. The chip
// enable is the SMS 3.58 MHz approximation (one pulse every 16 system clocks);
// tone and noise counters then divide by 16, matching F = clk / (32 * N).
// Period 0 reloads as 1024 (Sega PSG). Audio leaves this module as a signed
// 16-bit mix; HDMI I2S lives in the board top.

module sms_psg (
    input  wire               clk,
    input  wire               reset,
    input  wire               ce,
    input  wire               cpu_ce,
    input  wire               cpu_iorq_n,
    input  wire               cpu_wr_n,
    input  wire [7:0]         cpu_a,
    input  wire [7:0]         cpu_din,
    output wire [9:0]         tone0_period,
    output wire [3:0]         tone0_atten,
    output wire [3:0]         tone1_atten,
    output wire [3:0]         tone2_atten,
    output wire [3:0]         noise_atten,
    output wire               tone0,
    output wire signed [15:0] sample
);
    wire write = cpu_ce && !cpu_iorq_n && !cpu_wr_n &&
                 (cpu_a == 8'h7e || cpu_a == 8'h7f);

    reg [9:0] period0;
    reg [9:0] period1;
    reg [9:0] period2;
    reg [3:0] atten0;
    reg [3:0] atten1;
    reg [3:0] atten2;
    reg [3:0] atten3;
    reg [2:0] noise_ctrl;
    reg [2:0] latched;

    reg [3:0]  tone_div0;
    reg [3:0]  tone_div1;
    reg [3:0]  tone_div2;
    reg [3:0]  noise_div;
    reg [10:0] tone_cnt0;
    reg [10:0] tone_cnt1;
    reg [10:0] tone_cnt2;
    reg [10:0] noise_cnt;
    reg        tone_out0;
    reg        tone_out1;
    reg        tone_out2;
    reg [15:0] lfsr;

    assign tone0_period = period0;
    assign tone0_atten = atten0;
    assign tone1_atten = atten1;
    assign tone2_atten = atten2;
    assign noise_atten = atten3;
    assign tone0 = tone_out0;

    function automatic [10:0] reload;
        input [9:0] period;
        begin
            reload = (period == 10'd0) ? 11'd1024 : {1'b0, period};
        end
    endfunction

    function automatic [10:0] noise_reload;
        input [1:0] rate;
        input [9:0] tone2;
        begin
            case (rate)
                2'b00: noise_reload = 11'd16;
                2'b01: noise_reload = 11'd32;
                2'b10: noise_reload = 11'd64;
                default: noise_reload = reload(tone2);
            endcase
        end
    endfunction

    function automatic signed [15:0] channel_sample;
        input tone;
        input [3:0] atten;
        reg [12:0] mag;
        begin
            case (atten)
                4'd0:  mag = 13'd8191;
                4'd1:  mag = 13'd6507;
                4'd2:  mag = 13'd5168;
                4'd3:  mag = 13'd4105;
                4'd4:  mag = 13'd3261;
                4'd5:  mag = 13'd2590;
                4'd6:  mag = 13'd2057;
                4'd7:  mag = 13'd1634;
                4'd8:  mag = 13'd1298;
                4'd9:  mag = 13'd1031;
                4'd10: mag = 13'd819;
                4'd11: mag = 13'd650;
                4'd12: mag = 13'd516;
                4'd13: mag = 13'd410;
                4'd14: mag = 13'd326;
                default: mag = 13'd0;
            endcase
            channel_sample = tone ? $signed({3'b000, mag}) : -$signed({3'b000, mag});
        end
    endfunction

    wire signed [17:0] mix =
        channel_sample(tone_out0, atten0) +
        channel_sample(tone_out1, atten1) +
        channel_sample(tone_out2, atten2) +
        channel_sample(lfsr[0], atten3);
    // Register the mix in the system domain so HDMI I2S sees a flop, not a
    // combinational path into the pixel-clock CDC.
    reg signed [15:0] sample_q;
    assign sample = sample_q;

    initial begin
        period0 = 10'd0;
        period1 = 10'd0;
        period2 = 10'd0;
        atten0 = 4'hF;
        atten1 = 4'hF;
        atten2 = 4'hF;
        atten3 = 4'hF;
        noise_ctrl = 3'b000;
        latched = 3'b000;
        tone_div0 = 4'd0;
        tone_div1 = 4'd0;
        tone_div2 = 4'd0;
        noise_div = 4'd0;
        tone_cnt0 = 11'd0;
        tone_cnt1 = 11'd0;
        tone_cnt2 = 11'd0;
        noise_cnt = 11'd0;
        tone_out0 = 1'b0;
        tone_out1 = 1'b0;
        tone_out2 = 1'b0;
        lfsr = 16'h8000;
        sample_q = 16'sd0;
    end

    always @(posedge clk) begin
        if (reset) begin
            period0 <= 10'd0;
            period1 <= 10'd0;
            period2 <= 10'd0;
            atten0 <= 4'hF;
            atten1 <= 4'hF;
            atten2 <= 4'hF;
            atten3 <= 4'hF;
            noise_ctrl <= 3'b000;
            latched <= 3'b000;
            tone_div0 <= 4'd0;
            tone_div1 <= 4'd0;
            tone_div2 <= 4'd0;
            noise_div <= 4'd0;
            tone_cnt0 <= 11'd0;
            tone_cnt1 <= 11'd0;
            tone_cnt2 <= 11'd0;
            noise_cnt <= 11'd0;
            tone_out0 <= 1'b0;
            tone_out1 <= 1'b0;
            tone_out2 <= 1'b0;
            lfsr <= 16'h8000;
            sample_q <= 16'sd0;
        end else begin
            sample_q <= mix[15:0];
            if (write) begin
                if (cpu_din[7]) begin
                    latched <= cpu_din[6:4];
                    case (cpu_din[6:4])
                        3'b000: period0[3:0] <= cpu_din[3:0];
                        3'b001: atten0 <= cpu_din[3:0];
                        3'b010: period1[3:0] <= cpu_din[3:0];
                        3'b011: atten1 <= cpu_din[3:0];
                        3'b100: period2[3:0] <= cpu_din[3:0];
                        3'b101: atten2 <= cpu_din[3:0];
                        3'b110: begin
                            noise_ctrl <= cpu_din[2:0];
                            lfsr <= 16'h8000;
                        end
                        default: atten3 <= cpu_din[3:0];
                    endcase
                end else begin
                    case (latched)
                        3'b000: period0[9:4] <= cpu_din[5:0];
                        3'b001: atten0 <= cpu_din[3:0];
                        3'b010: period1[9:4] <= cpu_din[5:0];
                        3'b011: atten1 <= cpu_din[3:0];
                        3'b100: period2[9:4] <= cpu_din[5:0];
                        3'b101: atten2 <= cpu_din[3:0];
                        3'b110: begin
                            noise_ctrl <= cpu_din[2:0];
                            lfsr <= 16'h8000;
                        end
                        default: atten3 <= cpu_din[3:0];
                    endcase
                end
            end
            if (ce) begin
                if (tone_div0 == 4'd0) begin
                    tone_div0 <= 4'd15;
                    if (tone_cnt0 <= 11'd1) begin
                        tone_cnt0 <= reload(period0);
                        tone_out0 <= ~tone_out0;
                    end else begin
                        tone_cnt0 <= tone_cnt0 - 1'b1;
                    end
                end else begin
                    tone_div0 <= tone_div0 - 1'b1;
                end
                if (tone_div1 == 4'd0) begin
                    tone_div1 <= 4'd15;
                    if (tone_cnt1 <= 11'd1) begin
                        tone_cnt1 <= reload(period1);
                        tone_out1 <= ~tone_out1;
                    end else begin
                        tone_cnt1 <= tone_cnt1 - 1'b1;
                    end
                end else begin
                    tone_div1 <= tone_div1 - 1'b1;
                end
                if (tone_div2 == 4'd0) begin
                    tone_div2 <= 4'd15;
                    if (tone_cnt2 <= 11'd1) begin
                        tone_cnt2 <= reload(period2);
                        tone_out2 <= ~tone_out2;
                    end else begin
                        tone_cnt2 <= tone_cnt2 - 1'b1;
                    end
                end else begin
                    tone_div2 <= tone_div2 - 1'b1;
                end
                if (noise_div == 4'd0) begin
                    noise_div <= 4'd15;
                    if (noise_cnt <= 11'd1) begin
                        noise_cnt <= noise_reload(noise_ctrl[1:0], period2);
                        if (noise_ctrl[2])
                            lfsr <= {lfsr[0] ^ lfsr[3], lfsr[15:1]};
                        else
                            lfsr <= {lfsr[0], lfsr[15:1]};
                    end else begin
                        noise_cnt <= noise_cnt - 1'b1;
                    end
                end else begin
                    noise_div <= noise_div - 1'b1;
                end
            end
        end
    end
endmodule
