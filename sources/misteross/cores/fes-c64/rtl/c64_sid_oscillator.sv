// SPDX-License-Identifier: GPL-2.0-or-later
// MOS6581 digital oscillator adapted from VICE 3.10 reSID WaveformGenerator.
// Copyright (C) 2010 Dag Lem <resid@nimrod.no>
// GPL version 2 or later; complete license: c64_sid.COPYING.
// See ../README.md for pinned source, regeneration and hardware limits.
// Single waveforms/TEST, three-voice hard sync and triangle ring modulation.
// Pulse/triangle uses pinned MOS6581 samples; other combinations remain open.
module c64_sid_oscillator #(
    // The caller must guarantee a non-Phi edge between Phi edges.
    parameter bit PREPARE_PHASE = 1'b0
) (
    input wire clk, reset, phi, we,
    input wire [2:0] addr,
    input wire [7:0] din,
    input wire sync_clear, source_msb,
    output wire [23:0] phase_now, phase_advanced,
    output wire msb_rising, sync_enabled,
    output wire [11:0] waveform,
    output reg [11:0] clocked_waveform,
    output wire [7:0] osc
);
    typedef struct packed {
        logic [23:0] phase;
        logic msb_rising;
        logic [15:0] freq;
        logic [11:0] pw;
        logic [7:0] control;
        logic [22:0] lfsr;
        logic [1:0] shift_pipe;
        logic [15:0] reset_count;
        logic [17:0] fade_count;
        logic [11:0] pulse;
        logic [11:0] wave;
    } state_t;
    state_t q,n;
    // reSID's deterministic power-up fixture; warm reset preserves phase.
    initial begin q='0; q.phase=24'h555555; q.lfsr=23'h7ffffe; q.pulse=12'hfff; end
    assign phase_now=q.phase;
    wire [23:0] next_phase;
    generate if (PREPARE_PHASE) begin : prepared_phase
        reg [23:0] value = 24'h555555;
        wire [15:0] frequency = we && addr==0 ? {q.freq[15:8],din} :
                               we && addr==1 ? {din,q.freq[7:0]} : q.freq;
        wire test_rise = we && addr==4 && !q.control[3] && din[3];
        wire [23:0] phase_base = test_rise ? 24'd0 : q.phase;
        // Forward writes on the last idle edge, including TEST's phase clear.
        // Reset preserves the old phase and clears frequency, even on Phi.
        always @(posedge clk)
            if(reset) value <= q.phase;
            else if(!phi) value <= phase_base+{8'd0,frequency};
        assign next_phase=value;
    end else begin : direct_phase
        assign next_phase=q.phase+{8'd0,q.freq};
    end endgenerate
    // Only Phi consumes an advanced phase. The prepared sum is refreshed
    // after Phi; idle-edge source-MSB write behavior uses current q instead.
    assign phase_advanced=q.control[3] ? q.phase : next_phase;
    // reSID retains this edge latch while TEST holds the accumulator.
    assign msb_rising=q.control[3] ? q.msb_rising : (!q.phase[23] && phase_advanced[23]);
    assign sync_enabled=q.control[1];
    assign waveform=q.wave;
    assign osc=q.wave[11:4];
    function automatic [11:0] noise_bits(input [22:0] sr);
        noise_bits={sr[20],sr[18],sr[14],sr[11],sr[9],sr[5],sr[2],sr[0],4'b0};
    endfunction
    `include "c64_sid_combined_waves.vh"
    function automatic [11:0] selected_wave(input state_t s, input source_high);
        reg [7:0] legacy;
        begin
            case(s.control[7:4])
            4'h1:selected_wave={((s.phase[23] ^ (s.control[2] && !source_high)) ? ~s.phase[22:12] : s.phase[22:12]),1'b0};
            4'h2:selected_wave=s.phase[23:12];
            4'h4:selected_wave=s.pulse;
            4'h8:selected_wave=noise_bits(s.lfsr);
            4'h5:selected_wave=combined_pulse_triangle(s.phase[23:12] ^ ((s.control[2] && !source_high) ? 12'h800 : 12'h000)) & s.pulse;
            default:begin
                // Retain the old OR formula for the remaining unsupported combinations.
                // Update timing and coupled-wave fidelity remain unqualified.
                legacy=0;
                if(s.control[6] && s.phase[23:12]>=s.pw) legacy=legacy|8'hff;
                if(s.control[5]) legacy=legacy|s.phase[23:16];
                if(s.control[4]) legacy=legacy|(s.phase[23] ? ~s.phase[22:15] : s.phase[22:15]);
                if(s.control[7]) legacy=legacy|s.phase[7:0];
                selected_wave={legacy,4'b0};
            end
            endcase
        end
    endfunction
    always @* begin
        n=q;
        if(phi) begin
            if(q.control[3]) begin
                if(n.reset_count!=0) begin
                    n.reset_count=n.reset_count-1'b1;
                    if(n.reset_count==0) begin
                        n.lfsr=n.lfsr|23'd1;
                        n.lfsr=n.lfsr|(n.lfsr<<1);
                        if(n.lfsr!=23'h7fffff) n.reset_count=16'd1000;
                    end
                end
                n.pulse=12'hfff;
            end else begin
                n.phase=phase_advanced;
                n.msb_rising=msb_rising;
                if(!q.phase[19] && n.phase[19]) n.shift_pipe=2;
                else if(n.shift_pipe!=0) begin
                    n.shift_pipe=n.shift_pipe-1'b1;
                    if(n.shift_pipe==0) n.lfsr={n.lfsr[21:0],n.lfsr[22]^n.lfsr[17]};
                end
            end
            // Synchronize only after every voice has advanced its phase.
            // Noise edge/shift behavior above observes the pre-sync phase.
            if(sync_clear)n.phase=0;
            if(n.control[7:4]!=0) n.wave=selected_wave(n,source_msb);
            else if(n.fade_count!=0) begin
                n.fade_count=n.fade_count-1'b1;
                if(n.fade_count==0) begin
                    n.wave=n.wave&(n.wave>>1);
                    if(n.wave!=0) n.fade_count=18'd1500;
                end
            end
            n.pulse=n.phase[23:12]>=n.pw ? 12'hfff : 12'h000;
        end
        clocked_waveform=n.wave;
        // CPU writes follow the old-register oscillator clock on this SYS edge.
        if(we) case(addr)
        0:n.freq[7:0]=din;
        1:n.freq[15:8]=din;
        2:begin n.pw[7:0]=din; n.pulse=n.phase[23:12]>=n.pw ? 12'hfff : 12'h000; end
        3:begin n.pw[11:8]=din[3:0]; n.pulse=n.phase[23:12]>=n.pw ? 12'hfff : 12'h000; end
        4:begin
            n.control=din;
            if(!q.control[3] && din[3]) begin
                n.phase=0; n.shift_pipe=0; n.reset_count=16'd35000; n.pulse=12'hfff;
            end else if(q.control[3] && !din[3]) begin
                // Coupled-noise pre-writeback is outside this prototype.
                n.lfsr={n.lfsr[21:0],~n.lfsr[17]};
            end
            if(din[7:4]!=0) begin
                n.wave=selected_wave(n,source_msb);
                n.pulse=n.phase[23:12]>=n.pw ? 12'hfff : 12'h000;
            end else if(q.control[7:4]!=0) n.fade_count=18'd182000;
        end
        default:;
        endcase
        if(reset) begin
            n='0; n.phase=q.phase; n.lfsr=23'h7ffffe; n.pulse=12'hfff;
        end
    end
    always @(posedge clk) q<=n;
endmodule
