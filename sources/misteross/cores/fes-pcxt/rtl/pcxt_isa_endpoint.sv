// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"

// Card-side prepare/commit adapter. Decode and prepare have no side effects.
// The device performs read/write side effects only on device_commit.
module pcxt_isa_endpoint (
    input wire clk,
    input wire reset,
    input wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] request,
    output reg [`FES_PCXT_ISA_RESPONSE_BITS-1:0] response,
    output reg device_valid,
    output reg [`FES_PCXT_ISA_REQUEST_BITS-1:0] device_request,
    input wire device_claim,
    input wire device_ready,
    input wire [7:0] device_rdata,
    output wire device_commit,
    input wire [7:0] irq,
    input wire [2:0] drq
);
    reg seen, tag, claimed, prepared, committed;
    // Updated on the same edges as prepared/committed; no extra bus cycle.
    // Keep the preparation state decode off the device write-enable path.
    reg commit_ready;
    reg [7:0] data;
    wire valid = request[`FES_PCXT_ISA_VALID_BIT];
    wire same_tag = request[`FES_PCXT_ISA_TAG_BIT] == tag;
    wire clear = reset || request[`FES_PCXT_ISA_RESET_BIT] || !valid;
    assign device_commit = !clear && same_tag && commit_ready &&
                           request[`FES_PCXT_ISA_COMMIT_BIT];

    always @* begin
        response = 0;
        response[`FES_PCXT_ISA_PRESENT_BIT] = 1'b1;
        if (!reset && !request[`FES_PCXT_ISA_RESET_BIT]) begin
            response[`FES_PCXT_ISA_IRQ_LOW +: 8] = irq;
            response[`FES_PCXT_ISA_DRQ_LOW +: 3] = drq;
            response[`FES_PCXT_ISA_READ_DATA_LOW +: 8] = data;
            response[`FES_PCXT_ISA_SEEN_BIT] = seen;
            response[`FES_PCXT_ISA_RESPONSE_TAG_BIT] = tag;
            response[`FES_PCXT_ISA_CLAIM_BIT] = claimed;
            response[`FES_PCXT_ISA_READY_BIT] = prepared;
            response[`FES_PCXT_ISA_DONE_BIT] = committed;
        end
    end

    always @(posedge clk) begin
        if (clear) begin
            device_valid <= 0;
            device_request <= 0;
            seen <= 0;
            tag <= 0;
            claimed <= 0;
            prepared <= 0;
            committed <= 0;
            commit_ready <= 0;
            data <= 0;
        end else if (!device_valid) begin
            device_valid <= 1;
            device_request <= request;
            tag <= request[`FES_PCXT_ISA_TAG_BIT];
        end else if (!seen) begin
            // The device decodes the latched request in one fabric cycle.
            seen <= 1;
            claimed <= device_claim;
        end else if (claimed && !prepared && device_ready) begin
            prepared <= 1;
            commit_ready <= 1;
            data <= device_rdata;
        end else if (device_commit) begin
            committed <= 1;
            commit_ready <= 0;
        end
    end
endmodule
