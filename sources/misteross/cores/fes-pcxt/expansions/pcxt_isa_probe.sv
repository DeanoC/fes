// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"

// Diagnostic expansion card: eight I/O registers, 16-byte scratch RAM and a
// DMA loopback byte. This exercises the card interface without a CPU or BIOS.
module pcxt_isa_probe #(
    parameter [15:0] IO_BASE = 16'h0300,
    parameter [15:0] IO_MASK = 16'h03f8,
    parameter [19:0] MEMORY_BASE = 20'hc8000,
    parameter integer IRQ_LINE = 5,
    parameter integer DMA_CHANNEL = 2
) (
    input wire clk,
    input wire reset,
    input wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] request,
    output wire [`FES_PCXT_ISA_RESPONSE_BITS-1:0] response
);
    wire active, commit;
    wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] access_request;
    wire unused_request = &{1'b0, access_request[47:40], access_request[36], access_request[34:31]};
    wire [19:0] address = access_request[`FES_PCXT_ISA_ADDRESS_LOW +: 20];
    wire [1:0] kind = access_request[`FES_PCXT_ISA_KIND_LOW +: 2];
    wire write_cycle = access_request[`FES_PCXT_ISA_WRITE_BIT];
    wire [7:0] wdata = access_request[`FES_PCXT_ISA_WRITE_DATA_LOW +: 8];
    wire io_selected = active && kind == 2'(`FES_PCXT_ISA_KIND_IO) &&
                       !access_request[`FES_PCXT_ISA_AEN_BIT] &&
                       (address[15:0] & IO_MASK) == IO_BASE;
    wire memory_selected = active && kind == 2'(`FES_PCXT_ISA_KIND_MEMORY) &&
                           address[19:4] == MEMORY_BASE[19:4];
    wire dma_selected = active && kind == 2'(`FES_PCXT_ISA_KIND_DMA) &&
                        access_request[`FES_PCXT_ISA_AEN_BIT] &&
                        access_request[`FES_PCXT_ISA_DMA_CHANNEL_LOW +: 2] == 2'(DMA_CHANNEL);
    reg [7:0] data, flags, read_count, write_count, wait_cycles;
    reg [7:0] dma_data, dma_count, terminal_count;
    reg [7:0] memory [0:15];
    reg [7:0] waited;
    reg [7:0] rdata;
    wire [7:0] irq = flags[0] ? (8'h01 << IRQ_LINE) : 8'h00;
    wire [2:0] drq = flags[1] ? (3'b001 << (DMA_CHANNEL-1)) : 3'b000;
    integer i;

    pcxt_isa_endpoint endpoint (
        .clk(clk), .reset(reset), .request(request), .response(response),
        .device_valid(active), .device_request(access_request),
        .device_claim(io_selected || memory_selected || dma_selected),
        .device_ready(waited >= wait_cycles), .device_rdata(rdata),
        .device_commit(commit), .irq(irq), .drq(drq)
    );

    always @* begin
        rdata = 8'hff;
        if (memory_selected) rdata = memory[address[3:0]];
        else if (dma_selected) rdata = dma_data;
        else if (io_selected) case (address[2:0])
            0: rdata = data;
            1: rdata = flags;
            2: rdata = read_count;
            3: rdata = write_count;
            4: rdata = wait_cycles;
            5: rdata = dma_data;
            6: rdata = dma_count;
            7: rdata = terminal_count;
        endcase
    end

    always @(posedge clk) begin
        if (reset || request[`FES_PCXT_ISA_RESET_BIT]) begin
            data <= 0;
            flags <= 0;
            read_count <= 0;
            write_count <= 0;
            wait_cycles <= 0;
            dma_data <= 0;
            dma_count <= 0;
            terminal_count <= 0;
            waited <= 0;
            for (i=0; i<16; i=i+1) memory[i] <= 0;
        end else begin
            if (!active) waited <= 0;
            else if (waited != 8'hff) waited <= waited + 1'b1;
            if (commit) begin
                if (dma_selected) begin
                    dma_count <= dma_count + 1'b1;
                    if (write_cycle) dma_data <= wdata;
                    if (access_request[`FES_PCXT_ISA_TERMINAL_COUNT_BIT])
                        terminal_count <= terminal_count + 1'b1;
                end else if (memory_selected) begin
                    if (write_cycle) memory[address[3:0]] <= wdata;
                end else if (io_selected) begin
                    if (!write_cycle) read_count <= read_count + 1'b1;
                    else begin
                        write_count <= write_count + 1'b1;
                        case (address[2:0])
                            0: data <= wdata;
                            1: flags <= wdata;
                            4: wait_cycles <= wdata;
                            5: dma_data <= wdata;
                            default: begin end
                        endcase
                    end
                end
            end
        end
    end
endmodule
