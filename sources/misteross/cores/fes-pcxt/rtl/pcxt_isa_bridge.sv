// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"

// Shell-side byte-cycle controller. CPU arbitration and the 8237 DMA master
// sit upstream. DMA here means a peripheral DACK byte, not a RAM transfer.
module pcxt_isa_bridge #(
    parameter integer SLOTS = 8
) (
    input wire clk,
    input wire reset,
    input wire cycle_valid,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write,
    input wire cycle_aen,
    input wire cycle_lock,
    input wire [1:0] cycle_dma_channel,
    input wire cycle_terminal_count,
    input wire guest_ce,
    output reg cycle_done,
    output reg [7:0] cycle_rdata,
    output reg [1:0] cycle_fault,
    output reg [`FES_PCXT_ISA_REQUEST_BITS-1:0] request,
    input wire [SLOTS*`FES_PCXT_ISA_RESPONSE_BITS-1:0] responses,
    output reg [7:0] irq,
    output reg [2:0] drq
);
    localparam [2:0] IDLE=0, SETTLE=1, PREPARE=2, COMMIT=3, DRAIN=4;
    reg [2:0] state;
    reg [3:0] delay_count;
    reg tag;
    reg [SLOTS-1:0] owner;
    reg [SLOTS-1:0] claims, decoded;
    reg all_seen, all_ready, all_done, owner_valid;
    reg [7:0] selected_data;
    reg [31:0] rsp;
    integer i;
    wire conflict = (claims & (claims - {{(SLOTS-1){1'b0}},1'b1})) != 0;
    wire malformed = (cycle_kind == 2'(`FES_PCXT_ISA_KIND_IO) && cycle_address[19:16] != 0) ||
        (cycle_kind == 2'(`FES_PCXT_ISA_KIND_DMA) && (!cycle_aen || cycle_dma_channel == 0)) ||
        (cycle_kind != 2'(`FES_PCXT_ISA_KIND_DMA) && cycle_dma_channel != 0);
    assign cycle_ready = !reset && state == IDLE;

    always @* begin
        irq = 0;
        drq = 0;
        claims = 0;
        all_seen = 1;
        all_ready = 1;
        all_done = 1;
        owner_valid = 1;
        selected_data = 8'hff;
        rsp = 0;
        for (i=0; i<SLOTS; i=i+1) begin
            rsp = responses[i*`FES_PCXT_ISA_RESPONSE_BITS +: `FES_PCXT_ISA_RESPONSE_BITS];
            if (rsp[`FES_PCXT_ISA_PRESENT_BIT]) begin
                if (!reset) begin
                    irq = irq | (rsp[`FES_PCXT_ISA_IRQ_LOW +: 8] & 8'hfc);
                    drq = drq | rsp[`FES_PCXT_ISA_DRQ_LOW +: 3];
                end
                if (rsp[31:25] != 0 || !rsp[`FES_PCXT_ISA_SEEN_BIT] || rsp[`FES_PCXT_ISA_RESPONSE_TAG_BIT] != tag)
                    all_seen = 0;
                else if (rsp[`FES_PCXT_ISA_CLAIM_BIT]) begin
                    claims[i] = 1;
                    selected_data = rsp[`FES_PCXT_ISA_READ_DATA_LOW +: 8];
                    if (!rsp[`FES_PCXT_ISA_READY_BIT]) all_ready = 0;
                end
            end
            if (owner[i]) begin
                // PRESENT, SEEN and the tag must still describe this transaction.
                // DONE may lag by the response-register delay, so it does not
                // by itself make the owner invalid.
                if (!rsp[`FES_PCXT_ISA_PRESENT_BIT] || !rsp[`FES_PCXT_ISA_SEEN_BIT] ||
                    rsp[`FES_PCXT_ISA_RESPONSE_TAG_BIT] != tag || rsp[31:25] != 0)
                    owner_valid = 0;
                if (!rsp[`FES_PCXT_ISA_PRESENT_BIT] || !rsp[`FES_PCXT_ISA_SEEN_BIT] ||
                    rsp[`FES_PCXT_ISA_RESPONSE_TAG_BIT] != tag ||
                    !rsp[`FES_PCXT_ISA_DONE_BIT]) all_done = 0;
            end
        end
    end

    always @(posedge clk) begin
        cycle_done <= 0;
        if (reset) begin
            state <= DRAIN;
            delay_count <= 0;
            request <= 0;
            tag <= 0;
            owner <= 0;
            decoded <= 0;
            cycle_rdata <= 8'hff;
            cycle_fault <= 0;
        end else begin
            // CE is a live phase marker, never a gate on fabric handshakes.
            request[`FES_PCXT_ISA_GUEST_CE_BIT] <= guest_ce;
            case (state)
                IDLE: if (cycle_valid) begin
                    cycle_rdata <= 8'hff;
                    cycle_fault <= 0;
                    owner <= 0;
                    delay_count <= 0;
                    if (malformed) begin
                        cycle_fault <= 2'(`FES_PCXT_ISA_FAULT_REQUEST);
                        state <= DRAIN;
                    end else begin
                        tag <= !tag;
                        request <= 0;
                        request[`FES_PCXT_ISA_ADDRESS_LOW +: 20] <= cycle_address;
                        request[`FES_PCXT_ISA_WRITE_DATA_LOW +: 8] <= cycle_wdata;
                        request[`FES_PCXT_ISA_KIND_LOW +: 2] <= cycle_kind;
                        request[`FES_PCXT_ISA_WRITE_BIT] <= cycle_write;
                        request[`FES_PCXT_ISA_VALID_BIT] <= 1;
                        request[`FES_PCXT_ISA_TAG_BIT] <= !tag;
                        request[`FES_PCXT_ISA_AEN_BIT] <= cycle_aen;
                        request[`FES_PCXT_ISA_LOCK_BIT] <= cycle_lock;
                        request[`FES_PCXT_ISA_DMA_CHANNEL_LOW +: 2] <= cycle_dma_channel;
                        request[`FES_PCXT_ISA_TERMINAL_COUNT_BIT] <= cycle_terminal_count;
                        request[`FES_PCXT_ISA_GUEST_CE_BIT] <= guest_ce;
                        state <= SETTLE;
                    end
                end
                SETTLE: begin
                    if (delay_count == 4'(`FES_PCXT_ISA_DECODE_SETTLE_CYCLES-1)) begin
                        decoded <= claims;
                        state <= PREPARE;
                    end
                    else delay_count <= delay_count + 1'b1;
                end
                PREPARE: begin
                    // Decode must be prompt; device READY may wait indefinitely.
                    if (!all_seen || claims != decoded || conflict) begin
                        cycle_fault <= conflict ? 2'(`FES_PCXT_ISA_FAULT_CONFLICT) : 2'(`FES_PCXT_ISA_FAULT_PROTOCOL);
                        request[`FES_PCXT_ISA_VALID_BIT] <= 0;
                        delay_count <= 0;
                        state <= DRAIN;
                    end else if (claims == 0) begin
                        request[`FES_PCXT_ISA_VALID_BIT] <= 0;
                        delay_count <= 0;
                        state <= DRAIN;
                    end else if (all_ready) begin
                        owner <= claims;
                        cycle_rdata <= selected_data;
                        request[`FES_PCXT_ISA_COMMIT_BIT] <= 1;
                        state <= COMMIT;
                    end
                end
                COMMIT: begin
                    // COMMIT is already on the fabric. A lost owner faults and
                    // drains; clearing COMMIT here does not recall a pulse the
                    // card has already accepted, and it is not reissued.
                    if (!owner_valid) begin
                        cycle_fault <= 2'(`FES_PCXT_ISA_FAULT_PROTOCOL);
                        request[`FES_PCXT_ISA_VALID_BIT] <= 0;
                        request[`FES_PCXT_ISA_COMMIT_BIT] <= 0;
                        delay_count <= 0;
                        state <= DRAIN;
                    end else if (all_done) begin
                        request[`FES_PCXT_ISA_VALID_BIT] <= 0;
                        request[`FES_PCXT_ISA_COMMIT_BIT] <= 0;
                        delay_count <= 0;
                        state <= DRAIN;
                    end
                end
                DRAIN: begin
                    if (delay_count == 4'(`FES_PCXT_ISA_DRAIN_CYCLES-1)) begin
                        // Reset drain does not represent a completed bus cycle.
                        cycle_done <= pending;
                        state <= IDLE;
                    end else delay_count <= delay_count + 1'b1;
                end
                default: state <= IDLE;
            endcase
        end
    end

    reg pending;
    always @(posedge clk) begin
        if (reset) pending <= 0;
        else if (cycle_valid && cycle_ready) pending <= 1;
        else if (state == DRAIN && delay_count == 4'(`FES_PCXT_ISA_DRAIN_CYCLES-1)) pending <= 0;
    end
endmodule
