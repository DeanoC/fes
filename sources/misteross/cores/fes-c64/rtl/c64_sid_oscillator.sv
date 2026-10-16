// SPDX-License-Identifier: GPL-2.0-or-later
// MOS6581 digital oscillator adapted from VICE 3.10 reSID WaveformGenerator.
// Copyright (C) 2010 Dag Lem <resid@nimrod.no>
// GPL version 2 or later; complete license: c64_sid.COPYING.
// See ../README.md for pinned source, regeneration and hardware limits.
// Single waveforms/TEST, three-voice hard sync and triangle ring modulation.
// Combinations use pinned MOS6581 samples, noise writeback and phase feedback.
module c64_sid_oscillator #(
    // The caller must guarantee a non-Phi edge between Phi edges.
    parameter bit PREPARE_PHASE = 1'b0
) (
    input wire clk, reset, phi, we,
    input wire [2:0] addr,
    input wire [7:0] din,
    input wire sync_clear, source_msb, source_write_msb,
    output wire [23:0] phase_now, phase_advanced, phase_feedback,
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
        // The final idle state includes FREQ, TEST and combined-wave feedback.
        // Forward even a repeated CONTROL write on the last idle edge.
        // Reset preserves the old phase and clears frequency, even on Phi.
        always @(posedge clk)
            if(reset) value <= q.phase;
            else if(!phi) value <= n.phase+{8'd0,n.freq};
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
    // One Phi of oscillator/noise preparation, before sync and waveform output.
    function automatic state_t advance_clock(input state_t s,
        input [23:0] advanced, input rising);
        state_t t;
        begin
            t=s;
            if(s.control[3]) begin
                if(t.reset_count!=0) begin
                    t.reset_count=t.reset_count-1'b1;
                    if(t.reset_count==0) begin
                        t.lfsr=t.lfsr|23'd1;
                        t.lfsr=t.lfsr|(t.lfsr<<1);
                        if(t.lfsr!=23'h7fffff) t.reset_count=16'd1000;
                    end
                end
                t.pulse=12'hfff;
            end else begin
                t.phase=advanced; t.msb_rising=rising;
                if(!s.phase[19] && t.phase[19]) t.shift_pipe=2;
                else if(t.shift_pipe!=0) begin
                    t.shift_pipe=t.shift_pipe-1'b1;
                    if(t.shift_pipe==0) t.lfsr={t.lfsr[21:0],t.lfsr[22]^t.lfsr[17]};
                end
            end
            advance_clock=t;
        end
    endfunction
    wire state_t phi_base=advance_clock(q,phase_advanced,msb_rising);
    function automatic [22:0] noise_writeback(input [22:0] shift, input [11:0] wave);
        reg [22:0] result;
        begin
            result=shift;
            result[20]=shift[20]&wave[11]; result[18]=shift[18]&wave[10];
            result[14]=shift[14]&wave[9]; result[11]=shift[11]&wave[8];
            result[9]=shift[9]&wave[7]; result[5]=shift[5]&wave[6];
            result[2]=shift[2]&wave[5]; result[0]=shift[0]&wave[4];
            noise_writeback=result;
        end
    endfunction
    function automatic pre_writeback(input [3:0] before_wave, after_wave);
        pre_writeback=before_wave>8 && before_wave!=12 &&
            !((before_wave[1:0]==1 && after_wave[1:0]==2) ||
              (before_wave[1:0]==2 && after_wave[1:0]==1));
    endfunction
    function automatic [11:0] selected_wave(input state_t s, input source_high);
        begin
            case(s.control[6:4])
            1:selected_wave={((s.phase[23] ^ (s.control[2] && !source_high)) ? ~s.phase[22:12] : s.phase[22:12]),1'b0};
            2:selected_wave=s.phase[23:12];
            3:selected_wave=combined_triangle_saw(s.phase[23:12]);
            4:selected_wave=s.pulse;
            5:selected_wave=combined_pulse_triangle(s.phase[23:12] ^ ((s.control[2] && !source_high) ? 12'h800 : 12'h000)) & s.pulse;
            6:selected_wave=combined_pulse_saw(s.phase[23:12]) & s.pulse;
            7:selected_wave=combined_pulse_triangle_saw(s.phase[23:12]) & s.pulse;
            default:selected_wave=12'hfff;
            endcase
            if(s.control[7]) selected_wave=selected_wave & noise_bits(s.lfsr);
            if(s.control[7:6]==2'b11)
                selected_wave=selected_wave<12'hf00 ? 12'h000 :
                    selected_wave & (selected_wave<<1) & (selected_wave<<2);
        end
    endfunction
    function automatic [23:0] feedback_phase(input [23:0] phase_value,
        input [3:0] selector, input [11:0] wave);
        feedback_phase=phase_value;
        if(selector[1] && (selector[3] || selector[2] || selector[0]))
            feedback_phase[23]=phase_value[23] & wave[11];
    endfunction
    // Saw-selected feedback is independent of ring modulation. Expose this
    // local stage, not n, to preserve a finite cross-voice feed-forward path.
    wire [23:0] synced_phase=sync_clear ? 24'd0 : phase_advanced;
    reg [11:0] feedback_wave;
    state_t feedback_state;
    always @* begin
        feedback_state=phi_base;
        feedback_state.phase=synced_phase;
        // Saw-selected feedback ignores ring modulation.
        feedback_wave=selected_wave(feedback_state,1'b0);
    end
    assign phase_feedback=feedback_phase(synced_phase,q.control[7:4],feedback_wave);
    always @* begin
        n=q;
        if(phi) begin
            n=phi_base;
            // Synchronize only after every voice has advanced its phase.
            // Noise edge/shift behavior above observes the pre-sync phase.
            if(sync_clear)n.phase=0;
            if(n.control[7:4]!=0) begin
                n.wave=selected_wave(n,source_msb);
                n.phase=feedback_phase(n.phase,n.control[7:4],n.wave);
                if(n.control[7:4]>8 && !n.control[3] && n.shift_pipe!=1)
                    n.lfsr=noise_writeback(n.lfsr,n.wave);
            end
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
                if(pre_writeback(q.control[7:4],din[7:4]))
                    n.lfsr=noise_writeback(n.lfsr,n.wave);
                n.lfsr={n.lfsr[21:0],~n.lfsr[17]};
            end
            if(din[7:4]!=0) begin
                n.wave=selected_wave(n,source_write_msb);
                n.phase=feedback_phase(n.phase,n.control[7:4],n.wave);
                if(n.control[7:4]>8 && !n.control[3] && n.shift_pipe!=1)
                    n.lfsr=noise_writeback(n.lfsr,n.wave);
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
