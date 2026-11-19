// SPDX-License-Identifier: GPL-2.0-or-later
// Board audio for a machine with its own PLL: the machine PLL's second output
// is the I2S master clock, in the DE10 cores' exact 17:4 ratio to the machine
// clock (52.222222 and 12.287582 MHz, both -34 ppm). The standard coherent
// PCM crossing and I2S serializer follow. Guest HOLD only mutes samples; a
// scanout clock fault mutes them too. The fault is a level from another clock,
// so it is synchronized into the machine clock and joined to HOLD there: the
// audio clock then sees one registered machine-clock level.
module fes_de25_machine_audio (
    input wire source_clk, audio_clk, locked, hold, fault,
    input wire [15:0] left_sample, right_sample,
    output wire HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    assign HDMI_MCLK = audio_clk;
    (* async_reg = "true" *) reg [1:0] fault_sync = 2'b11;
    (* preserve, dont_merge *) reg mute = 1'b1;
    always @(posedge source_clk) begin
        fault_sync <= {fault_sync[0], fault};
        mute <= hold || fault_sync[1];
    end
    fes_audio_output audio (
        .source_clk(source_clk), .audio_clk(audio_clk), .locked(locked), .hold(mute),
        .left_sample(left_sample), .right_sample(right_sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
endmodule
