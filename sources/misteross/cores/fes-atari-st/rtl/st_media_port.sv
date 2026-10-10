// SPDX-License-Identifier: GPL-3.0-or-later
// Two clients share a held-request SDRAM port. Latch ownership and payload
// until the physical completion, then supply a low-request rearm clock.
// Withdrawn owners drain without delivering a completion to a later request.
module st_media_port #(parameter integer ADDR_BITS = 20) (
    input wire clk, cold_reset,
    input wire [1:0] source_req,
    input wire [ADDR_BITS-1:0] source_addr0, source_addr1,
    input wire [15:0] source_data0, source_data1,
    input wire [1:0] source_enable0, source_enable1,
    output wire [1:0] source_ready,
    output wire memory_req,
    output reg [ADDR_BITS-1:0] memory_addr,
    output reg [15:0] memory_data,
    output reg [1:0] memory_enable,
    input wire memory_ready
);
    localparam [1:0] IDLE = 0, WAIT = 1, GAP = 2;
    reg [1:0] state, seen;
    reg owner, priority_owner, discarded;
    wire [1:0] eligible = source_req & ~seen;
    wire selected = eligible[priority_owner] ? priority_owner : ~priority_owner;
    assign memory_req = state == WAIT && !cold_reset;
    assign source_ready = (state == WAIT && memory_ready && !discarded &&
                           source_req[owner] && !cold_reset) ? (2'b01 << owner) : 2'd0;
    always @(posedge clk) begin
        if (cold_reset) begin
            state <= IDLE; seen <= 0; owner <= 0; priority_owner <= 0;
            discarded <= 0; memory_addr <= 0; memory_data <= 0; memory_enable <= 0;
        end else begin
            seen <= seen & source_req;
            case (state)
                IDLE: if (|eligible) begin
                    owner <= selected; priority_owner <= ~selected;
                    seen[selected] <= 1;
                    memory_addr <= selected ? source_addr1 : source_addr0;
                    memory_data <= selected ? source_data1 : source_data0;
                    memory_enable <= selected ? source_enable1 : source_enable0;
                    discarded <= 0; state <= WAIT;
                end
                WAIT: begin
                    if (!source_req[owner]) discarded <= 1;
                    if (memory_ready) state <= GAP;
                end
                GAP: state <= IDLE;
                default: state <= IDLE;
            endcase
        end
    end
endmodule
