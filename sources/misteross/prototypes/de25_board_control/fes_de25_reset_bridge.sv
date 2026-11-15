// SPDX-License-Identifier: GPL-3.0-or-later
// One device reset owner follows the memory sequencer. Its early request
// asserts without a clock; memory reset keeps devices held until release.
// Both input deassertions must already be qualified on clk by the sequencer.
module fes_de25_reset_bridge (
    input wire clk,
    input wire in_reset_n, in_reset_req,
    output wire out_reset_n
);
    assign out_reset_n = in_reset_n && !in_reset_req;
endmodule
