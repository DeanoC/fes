// SPDX-License-Identifier: GPL-2.0-or-later
// A reduced 6581: three digital oscillators, pipelined ADSR envelopes and
// linear master volume. OSC3 and ENV3 are readable. Single-wave/TEST timing
// follows reSID, including hard sync and triangle ring. Filter and
// combined-wave fidelity remain open.
// See c64_sid.COPYING and ../README.md for source provenance and scope.
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
    wire [23:0] phase_now [0:2], phase_advanced [0:2];
    wire [2:0] msb_rising, sync_enabled, sync_clear;
    wire [11:0] digital_wave [0:2];
    wire [7:0] oscillator_read [0:2];
    wire [7:0] env [0:2];
    wire [7:0] env_read [0:2];
    reg [3:0] volume;
    reg [7:0] wave0, wave1, wave2;
    reg signed [18:0] mixed;
    reg signed [23:0] volume_mix;
    genvar v;
    generate for(v=0;v<3;v=v+1) begin: envelopes
        localparam [4:0] BASE=5'(v*7);
        localparam integer SOURCE=(v+2)%3, SOURCE_SOURCE=(v+1)%3;
        // Suppress sync when its source is itself synced on the same edge.
        assign sync_clear[v]=sync_enabled[v] && msb_rising[SOURCE] &&
            !(sync_enabled[SOURCE] && msb_rising[SOURCE_SOURCE]);
        wire ring_source_msb=phi ? (sync_clear[SOURCE] ? 1'b0 : phase_advanced[SOURCE][23]) : phase_now[SOURCE][23];
        wire [4:0] oscillator_addr=addr-BASE;
        wire control_write=addr==BASE+5'd4;
        wire ad_write=addr==BASE+5'd5;
        wire sr_write=addr==BASE+5'd6;
        c64_sid_oscillator oscillator (
            .clk(clk),.reset(reset),.phi(phi),
            .we(we && cs && oscillator_addr<=5'd4),
            .addr(oscillator_addr[2:0]),.din(din),
            .sync_clear(sync_clear[v]),.source_msb(ring_source_msb),
            .phase_now(phase_now[v]),.phase_advanced(phase_advanced[v]),
            .msb_rising(msb_rising[v]),.sync_enabled(sync_enabled[v]),
            .waveform(digital_wave[v]),.osc(oscillator_read[v])
        );
        c64_sid_envelope envelope (
            .clk(clk), .reset(reset), .phi(phi),
            .we(we && cs && (control_write || ad_write || sr_write)),
            .addr(control_write ? 2'd0 : ad_write ? 2'd1 : 2'd2),
            .din(din), .level(env[v]), .env3(env_read[v])
        );
    end endgenerate
    assign dout=addr==5'h1c ? env_read[2] : addr==5'h1b ? oscillator_read[2] : 8'h00;

    always @* begin
        wave0 = digital_wave[0][11:4];
        wave1 = digital_wave[1][11:4];
        wave2 = digital_wave[2][11:4];
        mixed = ($signed({1'b0,wave0})-19'sd128) * $signed({1'b0,env[0]}) +
                ($signed({1'b0,wave1})-19'sd128) * $signed({1'b0,env[1]}) +
                ($signed({1'b0,wave2})-19'sd128) * $signed({1'b0,env[2]});
        volume_mix = mixed * $signed({1'b0,volume});
    end

    always @(posedge clk) begin
        if (reset) begin
            volume <= 4'd0;
            sample <= 16'sd0;
        end else begin
            if(we && cs && addr==5'h18) volume<=din[3:0];
            sample <= 16'(volume_mix >>> 6);
        end
    end
endmodule
