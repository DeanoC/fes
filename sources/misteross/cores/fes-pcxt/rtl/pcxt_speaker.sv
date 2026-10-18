// SPDX-License-Identifier: GPL-2.0-or-later
// Integrate the one-bit speaker waveform at 100 MHz, emit mono 48 kHz PCM.
// Window lengths alternate 2083/2084 clocks. A low-cost fixed gain gives at
// most four PCM counts of gain jitter; a ~30 Hz DC blocker removes static
// cone drive. This is a development filter, not a measured speaker model.
module pcxt_speaker (
    input wire clk, reset, speaker,
    output reg sample_valid,
    output reg signed [15:0] sample
);
    reg [12:0] phase;
    wire [13:0] next_phase = {1'b0, phase} + 14'd3;
    reg signed [15:0] area;
    wire signed [15:0] next_area = area + (speaker ? 16'sd4 : 16'sd0);
    reg signed [23:0] dc;
    wire signed [23:0] scaled = {next_area, 8'b0};
    wire signed [23:0] error = scaled - dc;
    always @(posedge clk) begin
        if (reset) begin
            phase <= 0; area <= 0; dc <= 0; sample_valid <= 0; sample <= 0;
        end else begin
            sample_valid <= 0;
            if (next_phase >= 6250) begin
                phase <= 13'(next_phase - 14'd6250);
                area <= 0;
                sample <= error[23:8]; sample_valid <= 1;
                dc <= dc + (error >>> 8);
            end else begin
                phase <= next_phase[12:0]; area <= next_area;
            end
        end
    end
endmodule
