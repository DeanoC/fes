// SPDX-License-Identifier: GPL-2.0-or-later
// A reduced 6581: three voices, pulse/saw/triangle/noise, a pipelined ADSR envelope
// and linear master volume. Digital ADSR follows reSID timing; filter, sync,
// ring modulation and accurate waveform generation remain open. ENV3 is readable.
module c64_sid (
    input  wire               clk,
    input  wire               phi,
    input  wire               reset,
    input  wire               cs,
    input  wire               we,
    input  wire [4:0]         addr,
    input  wire [7:0]         din,
    output wire [7:0]         dout,
    output reg  signed [15:0] sample
);
    reg [15:0] freq [0:2];
    reg [11:0] pw [0:2];
    reg [7:0] ctrl [0:2];
    wire [7:0] env [0:2];
    wire [7:0] env_read [0:2];
    reg [23:0] phase [0:2];
    reg [3:0] volume;
    integer voice;
    reg [7:0] wave0, wave1, wave2;
    reg signed [18:0] mixed;
    reg signed [23:0] volume_mix;
    genvar v;
    generate for(v=0;v<3;v=v+1) begin: envelopes
        localparam [4:0] BASE=5'(v*7);
        wire control_write=addr==BASE+5'd4;
        wire ad_write=addr==BASE+5'd5;
        wire sr_write=addr==BASE+5'd6;
        c64_sid_envelope envelope (
            .clk(clk), .reset(reset), .phi(phi),
            .we(we && cs && (control_write || ad_write || sr_write)),
            .addr(control_write ? 2'd0 : ad_write ? 2'd1 : 2'd2),
            .din(din), .level(env[v]), .env3(env_read[v])
        );
    end endgenerate
    assign dout=addr==5'h1c ? env_read[2] : 8'h00;

    function automatic [7:0] voice_wave;
        input [7:0] control;
        input [23:0] phase_now;
        input [11:0] width;
        reg [7:0] wave;
        begin
            wave = 8'd0;
            if (control[6] && phase_now[23:12] >= width) wave = wave | 8'hFF;
            if (control[5]) wave = wave | phase_now[23:16];
            if (control[4]) wave = wave | (phase_now[23] ? ~phase_now[22:15] : phase_now[22:15]);
            if (control[7]) wave = wave | phase_now[7:0];
            voice_wave = wave;
        end
    endfunction

    always @* begin
        wave0 = voice_wave(ctrl[0], phase[0], pw[0]);
        wave1 = voice_wave(ctrl[1], phase[1], pw[1]);
        wave2 = voice_wave(ctrl[2], phase[2], pw[2]);
        mixed = ($signed({1'b0,wave0})-19'sd128) * $signed({1'b0,env[0]}) +
                ($signed({1'b0,wave1})-19'sd128) * $signed({1'b0,env[1]}) +
                ($signed({1'b0,wave2})-19'sd128) * $signed({1'b0,env[2]});
        volume_mix = mixed * $signed({1'b0,volume});
    end

    always @(posedge clk) begin
        if (reset) begin
            volume <= 4'd0;
            sample <= 16'sd0;
            for (voice = 0; voice < 3; voice = voice + 1) begin
                freq[voice] <= 16'd0;
                pw[voice] <= 12'd0;
                ctrl[voice] <= 8'd0;
                phase[voice] <= 24'd0;
            end
        end else begin
            if (we && cs) begin
                case (addr)
                    5'h00: freq[0][7:0] <= din;
                    5'h01: freq[0][15:8] <= din;
                    5'h02: pw[0][7:0] <= din;
                    5'h03: pw[0][11:8] <= din[3:0];
                    5'h04: ctrl[0] <= din;
                    5'h07: freq[1][7:0] <= din;
                    5'h08: freq[1][15:8] <= din;
                    5'h09: pw[1][7:0] <= din;
                    5'h0A: pw[1][11:8] <= din[3:0];
                    5'h0B: ctrl[1] <= din;
                    5'h0E: freq[2][7:0] <= din;
                    5'h0F: freq[2][15:8] <= din;
                    5'h10: pw[2][7:0] <= din;
                    5'h11: pw[2][11:8] <= din[3:0];
                    5'h12: ctrl[2] <= din;
                    5'h18: volume <= din[3:0];
                    default: ;
                endcase
            end
            if (phi) begin
                for (voice = 0; voice < 3; voice = voice + 1) begin
                    phase[voice] <= phase[voice] + {8'd0, freq[voice]};

                end
            end
            sample <= 16'(volume_mix >>> 6);
        end
    end
endmodule
