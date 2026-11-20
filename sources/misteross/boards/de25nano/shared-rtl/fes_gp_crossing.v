// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
// fes.computer GP words between a board transport's clock and a machine clock
// that has no phase relation to it. Each direction is the mailbox's own
// handshake: the whole word changes on one launch clock with its toggle (GPO
// request, GPI acknowledge), and stays put until the other side answers. The
// toggle alone crosses a two-stage synchronizer; the receiver copies the held
// word one receiving clock after it sees the toggle move, so the payload has
// had at least a full receiving period to arrive. The received word takes its
// toggle bit from that synchronizer, never from the launch register, so the
// toggle has exactly one path into the other clock. Board constraints cut only
// the two toggle ingress paths and bound both held payloads.
// Like the transport's GP word, nothing here is reset: a transport fault
// holds the machine instead, and only reconfiguration starts a new epoch.
module fes_gp_crossing (
    input  wire        transport_clk,
    input  wire        machine_clk,
    // Transport side.
    input  wire [31:0] gpo,
    output reg  [31:0] gpi = `FES_COMPUTER_SIGNATURE,
    // Machine side.
    output reg  [31:0] machine_gpo = 32'd0,
    input  wire [31:0] machine_gpi
);
    (* preserve, dont_merge *) reg [31:0] request_payload = 32'd0;
    (* preserve, dont_merge *) reg [31:0] reply_payload = `FES_COMPUTER_SIGNATURE;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] request_sync = 2'b00;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] reply_sync = 2'b00;
    wire request_toggle = (request_payload & `FES_COMPUTER_REQUEST_MASK) != 32'd0;
    wire reply_toggle = (reply_payload & `FES_COMPUTER_ACK_MASK) != 32'd0;
    wire machine_toggle = (machine_gpo & `FES_COMPUTER_REQUEST_MASK) != 32'd0;
    wire transport_toggle = (gpi & `FES_COMPUTER_ACK_MASK) != 32'd0;
    // Launch registers: nothing but the crossing reads them.
    always @(posedge transport_clk) request_payload <= gpo;
    always @(posedge machine_clk) reply_payload <= machine_gpi;
    always @(posedge machine_clk) begin
        request_sync <= {request_sync[0], request_toggle};
        if (request_sync[1] != machine_toggle)
            machine_gpo <= (request_payload & ~`FES_COMPUTER_REQUEST_MASK) |
                           (request_sync[1] ? `FES_COMPUTER_REQUEST_MASK : 32'd0);
    end
    always @(posedge transport_clk) begin
        reply_sync <= {reply_sync[0], reply_toggle};
        if (reply_sync[1] != transport_toggle)
            gpi <= (reply_payload & ~`FES_COMPUTER_ACK_MASK) |
                   (reply_sync[1] ? `FES_COMPUTER_ACK_MASK : 32'd0);
    end
endmodule
