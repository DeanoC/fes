// SPDX-License-Identifier: GPL-2.0-or-later
// MOS6581 / MOS8580 digital oscillator adapted from VICE 3.10 reSID WaveformGenerator.
// Copyright (C) 2010 Dag Lem <resid@nimrod.no>
// GPL version 2 or later; complete license: c64_sid.COPYING.
// See ../README.md for pinned source, regeneration and hardware limits.
// Single waveforms/TEST, three-voice hard sync and triangle ring modulation.
// Combined waveforms, noise writeback, phase feedback and OSC3 follow the selected die.
module c64_sid_oscillator #(
    // The caller must guarantee a non-Phi edge between Phi edges.
    parameter bit PREPARE_PHASE = 1'b0,
    // Defer the waveform stage (wave, saw feedback, noise write-back, fade
    // and pulse) from the Phi edge to a two-edge lookup/finish sequence: the
    // caller's `stage` edge (or a CONTROL write selecting a waveform) issues
    // `lookup_request` with the combined-wave table index, and the second
    // edge after it finishes the stage from the caller's registered
    // `lookup_tables` (index registered, then tables registered). The
    // caller sequences the three voices three SYS edges apart in reSID output
    // order, so the ring/sync source MSB is simply the source's current
    // phase and no table lookup chains with another within one SYS period.
    // Phi still advances the accumulator, noise shift and hard sync. The
    // caller must keep writes and the next Phi out of the stage window and
    // leave one idle edge after every finish before the next Phi.
    parameter bit SEQUENCED = 1'b0,
    parameter bit SID_8580 = 1'b0
) (
    input wire clk, reset, phi, we, stage,
    output wire lookup_request,
    output wire [11:0] lookup_index,
    output wire lookup_ring_xor,
    // {triangle_saw, pulse_triangle, pulse_saw, pulse_triangle_saw}
    input wire [47:0] lookup_tables,
    input wire [2:0] addr,
    input wire [7:0] din,
    input wire sync_clear, source_msb, source_write_msb,
    output wire [23:0] phase_now, phase_advanced, phase_feedback,
    output wire msb_rising, sync_enabled,
    output wire [11:0] waveform,
    output reg [11:0] clocked_waveform,
    output wire [7:0] osc
);
    localparam integer RESET_BITS = SID_8580 ? 22 : 16;
    localparam integer FADE_BITS = SID_8580 ? 23 : 18;
    typedef struct packed {
        logic [23:0] phase;
        logic msb_rising;
        logic [15:0] freq;
        logic [11:0] pw;
        logic [7:0] control;
        logic [22:0] lfsr;
        logic [1:0] shift_pipe;
        logic [RESET_BITS-1:0] reset_count;
        logic [FADE_BITS-1:0] fade_count;
        logic [11:0] pulse;
        logic [11:0] wave;
        logic [11:0] tri_saw_pipeline, osc3;
    } state_t;
    state_t q,n;
    // reSID's deterministic power-up fixture; warm reset preserves phase.
    initial begin q='0; q.tri_saw_pipeline=12'h555; q.phase=24'h555555; q.lfsr=23'h7ffffe; q.pulse=12'hfff; end
    assign phase_now=q.phase;
    wire [23:0] next_phase;
    generate if (PREPARE_PHASE) begin : prepared_phase
        reg [23:0] value = 24'h555555;
        // The final idle state includes FREQ, TEST and combined-wave feedback.
        // Forward even a repeated CONTROL write on the last idle edge.
        // Reset preserves the old phase and clears frequency, even on Phi.
        // Sequenced voices only change phase or frequency on an idle edge
        // through a register write (TEST set clears the phase); feedback is
        // applied on finish edges, after which an idle edge recomputes the
        // sum from q. The explicit write mux keeps the full next-state logic
        // out of the adder path. Direct mode forwards the complete n.
        wire write_test_set=we && addr==3'd4 && !q.control[3] && din[3];
        wire [23:0] idle_phase=write_test_set ? 24'd0 : q.phase;
        wire [15:0] idle_freq=(we && addr==3'd0) ? {q.freq[15:8],din} :
                              (we && addr==3'd1) ? {din,q.freq[7:0]} : q.freq;
        always @(posedge clk)
            if(reset) value <= q.phase;
            else if(!phi && !(SEQUENCED && pending[1]))
                value <= SEQUENCED ? idle_phase+{8'd0,idle_freq} : n.phase+{8'd0,n.freq};
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
    assign osc=SID_8580 ? q.osc3[11:4] : q.wave[11:4];
    function automatic [11:0] noise_bits(input [22:0] sr);
        noise_bits={sr[20],sr[18],sr[14],sr[11],sr[9],sr[5],sr[2],sr[0],4'b0};
    endfunction
    `include "c64_sid_combined_waves.vh"
    `include "c64_sid_8580_waves.vh"
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
                        if(t.lfsr!=23'h7fffff) t.reset_count=RESET_BITS'(SID_8580 ? 315000 : 1000);
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
        if (SID_8580)
            pre_writeback = before_wave > 8 && (before_wave != 12 || after_wave == 9 || after_wave == 14);
        else
            pre_writeback = before_wave > 8 && before_wave != 12 &&
                !((before_wave[1:0]==1 && after_wave[1:0]==2) ||
                  (before_wave[1:0]==2 && after_wave[1:0]==1));
    endfunction
    // Waveform selection from the combined-wave table values for this
    // state's phase (pulse_triangle already indexed with the ring XOR).
    function automatic [11:0] table_wave(input state_t s, input ring_active,
        input [11:0] triangle_saw, pulse_triangle, pulse_saw, pulse_triangle_saw);
        begin
            case(s.control[6:4])
            1:table_wave={((s.phase[23] ^ ring_active) ? ~s.phase[22:12] : s.phase[22:12]),1'b0};
            2:table_wave=s.phase[23:12];
            3:table_wave=triangle_saw;
            4:table_wave=12'hfff;
            5:table_wave=pulse_triangle;
            6:table_wave=pulse_saw;
            7:table_wave=pulse_triangle_saw;
            default:table_wave=12'hfff;
            endcase
        end
    endfunction
    function automatic [11:0] gated_wave(input state_t s, input [11:0] base);
        gated_wave = base & (s.control[6] ? s.pulse : 12'hfff) &
                     (s.control[7] ? noise_bits(s.lfsr) : 12'hfff);
    endfunction
    function automatic [11:0] output_wave(input state_t s, input [11:0] base);
        begin
            output_wave = gated_wave(s,base);
            if(s.control[7:6]==2'b11) begin
                if (SID_8580)
                    output_wave = output_wave < 12'hfc0 ? output_wave & (output_wave << 1) : 12'hfc0;
                else
                    output_wave = output_wave < 12'hf00 ? 12'h000 :
                        output_wave & (output_wave << 1) & (output_wave << 2);
            end
        end
    endfunction
    // Single-edge selection with local table lookups (direct mode).
    function automatic [11:0] selected_wave(input state_t s, input source_high);
        reg ring_active;
        begin
            ring_active=s.control[2] && !source_high;
            selected_wave=table_wave(s,ring_active,
                model_combined_triangle_saw(s.phase[23:12]),
                model_combined_pulse_triangle(s.phase[23:12] ^ (ring_active ? 12'h800 : 12'h000)),
                model_combined_pulse_saw(s.phase[23:12]),
                model_combined_pulse_triangle_saw(s.phase[23:12]));
        end
    endfunction
    function automatic [23:0] feedback_phase(input [23:0] phase_value,
        input [3:0] selector, input [11:0] wave);
        feedback_phase=phase_value;
        if(!SID_8580 && selector[1] && (selector[3] || selector[2] || selector[0]))
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
        feedback_wave=output_wave(feedback_state,selected_wave(feedback_state,1'b0));
    end
    assign phase_feedback=feedback_phase(synced_phase,q.control[7:4],feedback_wave);
    // The waveform stage of one Phi: output wave, saw-selected accumulator
    // feedback, noise write-back, floating-output decay and pulse compare.
    function automatic state_t waveform_stage(input state_t s, input [11:0] base, input fade);
        state_t t;
        begin
            t=s;
            if(t.control[7:4]!=0) begin
                t.wave=output_wave(t,base);
                if (SID_8580 && t.control[5:4] != 0) begin
                    t.osc3 = gated_wave(t,t.tri_saw_pipeline);
                    t.tri_saw_pipeline = base;
                end else t.osc3 = t.wave;
                t.phase=feedback_phase(t.phase,t.control[7:4],t.wave);
                if(t.control[7:4]>8 && !t.control[3] && t.shift_pipe!=1)
                    t.lfsr=noise_writeback(t.lfsr,t.wave);
            end
            else if(fade && t.fade_count!=0) begin
                t.fade_count=t.fade_count-1'b1;
                if(t.fade_count==0) begin
                    t.wave=t.wave&(t.wave>>1);
                    t.osc3=t.wave;
                    if(t.wave!=0) t.fade_count=FADE_BITS'(SID_8580 ? 50000 : 1500);
                end
            end
            t.pulse=t.phase[23:12]>=t.pw ? 12'hfff : 12'h000;
            waveform_stage=t;
        end
    endfunction
    // Sequenced lookup/finish bookkeeping: `pending[1]` finishes the stage
    // two edges after a request; `pending_write` marks a CONTROL-write stage,
    // which skips the floating-output decay; `ring_latched` is the ring
    // modulation state the lookup index was formed with.
    reg [1:0] pending=2'b00; reg pending_write=0, ring_latched=0;
    reg write_request;
    wire finish_ring_active=ring_latched;
    wire [11:0] finish_wave=table_wave(q,finish_ring_active,
        lookup_tables[47:36],lookup_tables[35:24],lookup_tables[23:12],lookup_tables[11:0]);
    always @* begin
        n=q;
        write_request=0;
        if(phi) begin
            n=phi_base;
            // Synchronize only after every voice has advanced its phase.
            // Noise edge/shift behavior above observes the pre-sync phase.
            if(sync_clear)n.phase=0;
            if(!SEQUENCED) n=waveform_stage(n,selected_wave(n,source_msb),1'b1);
        end
        // Deferred finish: q holds the advanced, synchronized (or written)
        // state and the caller's tables hold this voice's requested lookup.
        if(SEQUENCED && pending[1]) n=waveform_stage(n,finish_wave,!pending_write);
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
                n.phase=0; n.shift_pipe=0; n.reset_count=RESET_BITS'(SID_8580 ? 2519864 : 35000); n.pulse=12'hfff;
            end else if(q.control[3] && !din[3]) begin
                if(pre_writeback(q.control[7:4],din[7:4]))
                    n.lfsr=noise_writeback(n.lfsr,n.wave);
                n.lfsr={n.lfsr[21:0],~n.lfsr[17]};
            end
            if(din[7:4]!=0) begin
                if(SEQUENCED) write_request=1;
                else n=waveform_stage(n,selected_wave(n,source_write_msb),1'b0);
            end else if(q.control[7:4]!=0) n.fade_count=FADE_BITS'(SID_8580 ? 4400000 : 182000);
        end
        default:;
        endcase
        if(reset) begin
            n='0; n.phase=q.phase; n.tri_saw_pipeline=q.tri_saw_pipeline; n.lfsr=23'h7ffffe; n.pulse=12'hfff;
        end
    end
    always @(posedge clk) q<=n;
    // Lookup request: the index is the state the finish edge will see.
    wire request_ring_active=n.control[2] && !(write_request ? source_write_msb : source_msb);
    assign lookup_request=SEQUENCED && !reset && (stage || write_request);
    assign lookup_index=n.phase[23:12];
    assign lookup_ring_xor=request_ring_active;
    always @(posedge clk) begin
        if(reset) begin pending<=2'b00; pending_write<=0; ring_latched<=0; end
        else begin
            pending<={pending[0],lookup_request};
            if(lookup_request) begin pending_write<=write_request; ring_latched<=request_ring_active; end
        end
    end
endmodule
