// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"

// One fabric register in each direction, matching the existing linkable-slot
// model. This portable boundary has no physical BEL/CRAM layout yet.
module pcxt_isa_boundary (
    input wire clk,
    input wire reset,
    input wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] request,
    output wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] card_request,
    input wire [`FES_PCXT_ISA_RESPONSE_BITS-1:0] card_response,
    output reg [`FES_PCXT_ISA_RESPONSE_BITS-1:0] response
);
    reg [`FES_PCXT_ISA_REQUEST_BITS-2:0] request_payload;
    reg request_reset;
    // Keep only this one-bit process: each occupied slot needs a local reset
    // flop rather than a merged global request-reset fanout. Payload/response
    // registers retain normal optimization and the same one-cycle boundary.
    (* keep *) always @(posedge clk)
        request_reset <= reset || request[`FES_PCXT_ISA_RESET_BIT];
    assign card_request = {
        request_payload[`FES_PCXT_ISA_REQUEST_BITS-2:`FES_PCXT_ISA_RESET_BIT],
        request_reset, request_payload[`FES_PCXT_ISA_RESET_BIT-1:0]};
    always @(posedge clk) begin
        if (reset) begin
            request_payload <= 0;
            response <= 0;
        end else begin
            request_payload <= {
                request[`FES_PCXT_ISA_REQUEST_BITS-1:`FES_PCXT_ISA_RESET_BIT+1],
                request[`FES_PCXT_ISA_RESET_BIT-1:0]};
            response <= card_response;
        end
    end
endmodule
