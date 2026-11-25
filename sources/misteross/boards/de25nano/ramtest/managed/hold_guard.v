// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
// Delay a valid Execution Hold at the mailbox boundary until the tester has
// stopped and drained. The transport holds GPO stable until the matching ACK.
// Invalid commands remain the normal mailbox's responsibility. No new opcode
// or host-visible interface is introduced.
module fes_de25_ramtest_hold_guard (
    input wire clk,
    input wire [31:0] gpo, mailbox_gpi,
    input wire drained,
    output reg [31:0] mailbox_gpo = 32'd0,
    output wire hold_requested
);
    wire request = ((gpo & `FES_COMPUTER_REQUEST_MASK) != 0) !=
                   ((mailbox_gpi & `FES_COMPUTER_ACK_MASK) != 0);
    wire valid_hold = gpo[30:24] == `FES_COMPUTER_OPCODE_EXECUTION &&
                      gpo[23:0] == 24'd0;
    assign hold_requested = request && valid_hold;
    always @(posedge clk)
        if (!hold_requested || drained) mailbox_gpo <= gpo;
endmodule
