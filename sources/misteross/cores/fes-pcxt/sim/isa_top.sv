// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"
module isa_top (
    input wire clk, reset,
    input wire cycle_valid,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write, cycle_aen, cycle_lock,
    input wire [1:0] cycle_dma_channel,
    input wire cycle_terminal_count, guest_ce,
    output wire cycle_done,
    output wire [7:0] cycle_rdata,
    output wire [1:0] cycle_fault,
    output wire [7:0] irq,
    output wire [2:0] drq,
    input wire [2:0] inject,
    output wire [47:0] observed_request,
    output reg reset_side_effect
);
    wire [47:0] request;
    wire [255:0] responses;
    wire [47:0] card_request [0:7];
    wire [31:0] card_response [0:7];
    wire [31:0] boundary_response [0:7];
    wire [7:0] slot_commit;
    pcxt_isa_bridge bridge (.*);
    assign observed_request = request;
    // The boundary register still holds COMMIT on the edge reset rises.
    // Card endpoints must see that reset themselves; RESET in the register
    // arrives one cycle later.
    always @(posedge clk) begin
        if (!reset) reset_side_effect <= 0;
        else reset_side_effect <= reset_side_effect | (|slot_commit);
    end
    genvar n;
    generate for (n=0; n<8; n=n+1) begin: slots
        pcxt_isa_boundary boundary (
            .clk(clk), .reset(reset), .request(request),
            .card_request(card_request[n]), .card_response(card_response[n]),
            .response(boundary_response[n])
        );
        if (n == 0 || n == 7) begin: cards
            pcxt_isa_probe #(
                .IO_BASE(n == 0 ? 16'h0300 : 16'h0308),
                .IO_MASK(n == 0 ? 16'h03f8 : 16'hfff8),
                .MEMORY_BASE(n == 0 ? 20'hc8000 : 20'hc9000),
                .IRQ_LINE(n == 0 ? 5 : 6), .DMA_CHANNEL(n == 0 ? 2 : 3)
            ) probe (.clk(clk), .reset(reset), .request(card_request[n]),
                     .response(card_response[n]), .commit_pulse(slot_commit[n]));
        end else begin: vacant
            assign card_response[n] = 0;
            assign slot_commit[n] = 1'b0;
        end
        // Invalid cards are simulation fault injection only. Production vacant
        // sockets return zero and cannot contribute IRQ, DMA, claim or READY.
        if (n == 3) begin: fault
            assign responses[32*n +: 32] = inject == 6 ? 0 : inject == 1 ? boundary_response[0] :
                inject == 2 ? (32'h00002000) :
                inject == 3 ? (boundary_response[0] ^ 32'h00000200) :
                inject == 4 ? (32'h0000e000) : boundary_response[n];
        end else begin: normal_response
            assign responses[32*n +: 32] = (inject == 6 || (n == 0 && inject == 5)) ? 0 : boundary_response[n];
        end
    end endgenerate
endmodule
