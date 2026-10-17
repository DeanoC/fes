// SPDX-License-Identifier: MIT
// Motherboard Phi/write edges for short periods, plus TOD at adjacent rates.
// The sequenced and direct SIDs see the machine's real cpu_ce and bus strobe.
module cadence_sim_top (
    input wire clk,
    input wire reset_sid,
    input wire reset_tod,
    input wire [4:0] sid_we,
    input wire [4:0] sid_addr,
    input wire [7:0] sid_din,
    output wire [4:0] phi,
    output wire [4:0] strobe,
    output wire [4:0] sequenced,
    output wire [23:0] seq_phase0 [0:4],
    output wire [23:0] dir_phase0 [0:4],
    output wire [23:0] seq_phase2 [0:4],
    output wire [23:0] dir_phase2 [0:4],
    output wire [11:0] seq_wave0 [0:4],
    output wire [11:0] dir_wave0 [0:4],
    output wire [11:0] seq_wave2 [0:4],
    output wire [11:0] dir_wave2 [0:4],
    output wire [7:0] seq_ctl0 [0:4],
    output wire [7:0] dir_ctl0 [0:4],
    output wire [7:0] seq_ctl2 [0:4],
    output wire [7:0] dir_ctl2 [0:4],
    output wire [15:0] seq_freq0 [0:4],
    output wire [15:0] dir_freq0 [0:4],
    output wire [3:0] tod_phi,
    output wire [3:0] tod_tick,
    output wire [3:0] tod_sys_reset
);
    for (genvar i = 0; i < 5; i = i + 1) begin : period
        localparam integer PERIOD = 27 + i;
        c64_machine #(.SYSTEM_CLOCK_HZ(PERIOD), .CPU_CLOCK_HZ(1)) board (
            .clk_sys(clk), .video_clk(clk), .reset(1'b1), .keyboard_rows(144'd0),
            .controller_buttons(16'd0), .media_write_addr(18'd0), .media_write_data(16'd0),
            .media_write_enable(2'd0), .disk_ready(1'b0), .slot1_response(0), .slot2_response(0),
            .slot1_request(), .slot2_request(), .red(), .green(), .blue(), .de(), .hsync(), .vsync(),
            .audio_sample(), .debug_stage(), .debug_error(), .debug_iec(), .debug_pc()
        );
        wire we = sid_we[i];
        c64_sid #(
            .PREPARE_PHASE(1'b1), .PREPARE_ENVELOPE(1'b1), .SEQUENCE_VOICES(1'b1),
            .FILTER_AUDIO(1'b0), .RESID_FILTER(1'b0)
        ) seq_sid (
            .clk(clk), .phi(board.cpu_ce), .reset(reset_sid), .cs(we), .we(we),
            .addr(sid_addr), .din(sid_din), .dout(), .sample()
        );
        c64_sid #(
            .PREPARE_PHASE(1'b1), .PREPARE_ENVELOPE(1'b1), .SEQUENCE_VOICES(1'b0),
            .FILTER_AUDIO(1'b0), .RESID_FILTER(1'b0)
        ) dir_sid (
            .clk(clk), .phi(board.cpu_ce), .reset(reset_sid), .cs(we), .we(we),
            .addr(sid_addr), .din(sid_din), .dout(), .sample()
        );
        assign phi[i] = board.cpu_ce;
        assign strobe[i] = board.bus_strobe;
        assign sequenced[i] = board.sid.SEQUENCE_VOICES;
        assign seq_phase0[i] = seq_sid.envelopes[0].oscillator.q.phase;
        assign dir_phase0[i] = dir_sid.envelopes[0].oscillator.q.phase;
        assign seq_phase2[i] = seq_sid.envelopes[2].oscillator.q.phase;
        assign dir_phase2[i] = dir_sid.envelopes[2].oscillator.q.phase;
        assign seq_wave0[i] = seq_sid.envelopes[0].oscillator.q.wave;
        assign dir_wave0[i] = dir_sid.envelopes[0].oscillator.q.wave;
        assign seq_wave2[i] = seq_sid.envelopes[2].oscillator.q.wave;
        assign dir_wave2[i] = dir_sid.envelopes[2].oscillator.q.wave;
        assign seq_ctl0[i] = seq_sid.envelopes[0].oscillator.q.control;
        assign dir_ctl0[i] = dir_sid.envelopes[0].oscillator.q.control;
        assign seq_ctl2[i] = seq_sid.envelopes[2].oscillator.q.control;
        assign dir_ctl2[i] = dir_sid.envelopes[2].oscillator.q.control;
        assign seq_freq0[i] = seq_sid.envelopes[0].oscillator.q.freq;
        assign dir_freq0[i] = dir_sid.envelopes[0].oscillator.q.freq;
    end

    for (genvar t = 0; t < 4; t = t + 1) begin : tod
        localparam integer SYS = t == 0 ? 200 : t == 1 ? 300 : t == 2 ? 10000 : 52224000;
        localparam integer CPU = t == 3 ? 985248 : 200;
        c64_machine #(.SYSTEM_CLOCK_HZ(SYS), .CPU_CLOCK_HZ(CPU)) board (
            .clk_sys(clk), .video_clk(clk), .reset(reset_tod), .keyboard_rows(144'd0),
            .controller_buttons(16'd0), .media_write_addr(18'd0), .media_write_data(16'd0),
            .media_write_enable(2'd0), .disk_ready(1'b0), .slot1_response(0), .slot2_response(0),
            .slot1_request(), .slot2_request(), .red(), .green(), .blue(), .de(), .hsync(), .vsync(),
            .audio_sample(), .debug_stage(), .debug_error(), .debug_iec(), .debug_pc()
        );
        assign tod_phi[t] = board.cpu_ce;
        assign tod_tick[t] = board.tod_tick;
        assign tod_sys_reset[t] = board.system_reset;
    end
endmodule
