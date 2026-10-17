// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"

// One fabric register in each direction, matching the existing linkable-slot
// model. This portable boundary has no physical BEL/CRAM layout yet.
module pcxt_isa_boundary (
    input wire clk,
    input wire reset,
    input wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] request,
    output reg [`FES_PCXT_ISA_REQUEST_BITS-1:0] card_request,
    input wire [`FES_PCXT_ISA_RESPONSE_BITS-1:0] card_response,
    output reg [`FES_PCXT_ISA_RESPONSE_BITS-1:0] response
);
    always @(posedge clk) begin
        if (reset) begin
            card_request <= 0;
            card_request[`FES_PCXT_ISA_RESET_BIT] <= 1;
            response <= 0;
        end else begin
            card_request <= request;
            response <= card_response;
        end
    end
endmodule
