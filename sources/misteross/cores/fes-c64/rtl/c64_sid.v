// SPDX-License-Identifier: GPL-2.0-or-later
// MOS6581 digital oscillators and ADSR with optional staged analog filtering.
// Direct fallback retains linear master volume. OSC3 and ENV3 are readable. Single-wave/TEST timing
// follows reSID, including hard sync, triangle ring and pulse/triangle.
// Remaining combined-wave fidelity and analog hardware acceptance stay open.
// See c64_sid.COPYING and ../README.md for source provenance and scope.
module c64_sid #(parameter bit PREPARE_PHASE = 1'b0,
    parameter bit PREPARE_ENVELOPE = 1'b0,
    parameter bit FILTER_AUDIO = 1'b0) (
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
    wire [11:0] digital_wave [0:2], clocked_wave [0:2];
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
        c64_sid_oscillator #(.PREPARE_PHASE(PREPARE_PHASE)) oscillator (
            .clk(clk),.reset(reset),.phi(phi),
            .we(we && cs && oscillator_addr<=5'd4),
            .addr(oscillator_addr[2:0]),.din(din),
            .sync_clear(sync_clear[v]),.source_msb(ring_source_msb),
            .phase_now(phase_now[v]),.phase_advanced(phase_advanced[v]),
            .msb_rising(msb_rising[v]),.sync_enabled(sync_enabled[v]),
            .waveform(digital_wave[v]),.clocked_waveform(clocked_wave[v]),.osc(oscillator_read[v])
        );
        c64_sid_envelope #(.PREPARE_STATE(PREPARE_ENVELOPE)) envelope (
            .clk(clk), .reset(reset), .phi(phi),
            .we(we && cs && (control_write || ad_write || sr_write)),
            .addr(control_write ? 2'd0 : ad_write ? 2'd1 : 2'd2),
            .din(din), .level(env[v]), .env3(env_read[v])
        );
    end endgenerate
    assign dout=addr==5'h1c ? env_read[2] : addr==5'h1b ? oscillator_read[2] : 8'h00;

    generate if(FILTER_AUDIO) begin : filtered_audio
        reg pending=0;
        reg [11:0] captured_wave0,captured_wave1,captured_wave2;
        reg [10:0] cutoff=0, captured_cutoff;
        reg [7:0] routing=0,mode_volume=0,captured_routing,captured_mode;
        wire done,busy;
        wire signed [31:0] wide_sample;
        // Capture old controls and the post-Phi, pre-write waveform together.
        // Start one SYS edge later, when envelope levels include the Phi clock.
        always @(posedge clk) begin
            if(reset) begin
                pending<=0;cutoff<=0;routing<=0;mode_volume<=0;
                captured_wave0<=0;captured_wave1<=0;captured_wave2<=0;
                captured_cutoff<=0;captured_routing<=0;captured_mode<=0;
            end else begin
                pending<=phi;
                if(phi) begin
                    captured_wave0<=clocked_wave[0];captured_wave1<=clocked_wave[1];captured_wave2<=clocked_wave[2];
                    captured_cutoff<=cutoff;captured_routing<=routing;captured_mode<=mode_volume;
                end
                if(we && cs) case(addr)
                    5'h15:cutoff[2:0]<=din[2:0];
                    5'h16:cutoff[10:3]<=din;
                    5'h17:routing<=din;
                    5'h18:mode_volume<=din;
                    default:;
                endcase
            end
        end
        c64_sid_audio audio(.clk(clk),.reset(reset),.start(pending),
            .wave0(captured_wave0),.wave1(captured_wave1),.wave2(captured_wave2),
            .env0(env[0]),.env1(env[1]),.env2(env[2]),
            .fc(captured_cutoff),.res_filt(captured_routing),.mode_vol(captured_mode),
            .done(done),.busy(busy),.sample(wide_sample),.internal_sample(),
            .hp(),.bp(),.lp(),.bpx(),.bpc(),.lpx(),.lpc(),.external_low(),.external_high());
        // External-filter transients are wide; saturate at the PCM boundary.
        always @* begin
            if(wide_sample>32'sd32767)sample=16'sh7fff;
            else if(wide_sample< -32'sd32768)sample=16'sh8000;
            else sample=wide_sample[15:0];
        end
    end else begin : direct_audio
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
    end endgenerate
endmodule
