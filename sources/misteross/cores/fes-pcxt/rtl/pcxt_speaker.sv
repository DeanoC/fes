// SPDX-License-Identifier: GPL-2.0-or-later
// Integrate the one-bit speaker waveform at 50 or 100 MHz, emit mono 48 kHz PCM.
// At 100 MHz windows alternate 2083/2084 clocks; at 50 MHz, 1041/1042.
// Doubling the per-clock gain at 50 MHz preserves amplitude. Fixed gain gives at
// most eight PCM counts of gain jitter; a ~30 Hz DC blocker removes static
// cone drive. This is a development filter, not a measured speaker model.
module pcxt_speaker #(parameter integer CLOCK_HZ = 100000000) (
    input wire clk, reset, speaker,
    output reg sample_valid,
    output reg signed [15:0] sample
);
    localparam integer PHASE_WRAP = CLOCK_HZ / 16000;
    localparam integer GAIN = CLOCK_HZ == 50000000 ? 8 : 4;
    initial if (CLOCK_HZ != 50000000 && CLOCK_HZ != 100000000)
        $fatal(1, "pcxt_speaker supports only 50 or 100 MHz");
    reg [12:0] phase;
    wire [13:0] next_phase = {1'b0, phase} + 14'd3;
    reg signed [15:0] area;
    wire signed [15:0] next_area = area + (speaker ? 16'(GAIN) : 16'sd0);
    reg signed [23:0] dc;
    wire signed [23:0] scaled = {next_area, 8'b0};
    wire signed [23:0] error = scaled - dc;
    always @(posedge clk) begin
        if (reset) begin
            phase <= 0; area <= 0; dc <= 0; sample_valid <= 0; sample <= 0;
        end else begin
            sample_valid <= 0;
            if (next_phase >= 14'(PHASE_WRAP)) begin
                phase <= 13'(next_phase - 14'(PHASE_WRAP));
                area <= 0;
                sample <= error[23:8]; sample_valid <= 1;
                dc <= dc + (error >>> 8);
            end else begin
                phase <= next_phase[12:0]; area <= next_area;
            end
        end
    end
endmodule
