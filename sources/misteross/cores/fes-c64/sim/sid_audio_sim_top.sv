// SPDX-License-Identifier: GPL-2.0-or-later
// Observation-only wrapper for the complete digital/DAC/filter SID path.
module sid_audio_sim_top #(parameter bit PREPARED=1'b0) (
    input wire clk, reset, phi, cs, we,
    input wire [4:0] addr,
    input wire [7:0] din,
    output wire [7:0] dout, env3, osc3,
    output wire signed [15:0] sample,
    output wire signed [31:0] wide_sample,
    output wire done, busy
);
    c64_sid #(.PREPARE_PHASE(PREPARED), .PREPARE_ENVELOPE(PREPARED),
              .FILTER_AUDIO(1'b1)) dut (
        .clk(clk), .reset(reset), .phi(phi), .cs(cs), .we(we),
        .addr(addr), .din(din), .dout(dout), .sample(sample)
    );
    assign env3=dut.env_read[2];
    assign osc3=dut.oscillator_read[2];
    assign wide_sample=dut.filtered_audio.wide_sample;
    assign done=dut.filtered_audio.done;
    assign busy=dut.filtered_audio.busy;
endmodule
