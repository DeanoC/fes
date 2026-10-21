// SPDX-License-Identifier: MIT
module c64_sid_sim_top #(
 parameter bit PREPARE_PHASE=1'b0,
 parameter bit PREPARE_ENVELOPE=1'b0,
 parameter bit SEQUENCE_VOICES=1'b0
) (
 input wire clk, reset, phi, cs, we,
 input wire [4:0] addr,
 input wire [7:0] din,
 output wire [7:0] dout, e0, e1, e2,
 output wire signed [15:0] sample
);
 c64_sid #(.PREPARE_PHASE(PREPARE_PHASE),.PREPARE_ENVELOPE(PREPARE_ENVELOPE),.SEQUENCE_VOICES(SEQUENCE_VOICES)) sid(.*);
 assign e0=sid.env[0];
 assign e1=sid.env[1];
 assign e2=sid.env[2];
endmodule
