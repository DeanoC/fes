// SPDX-License-Identifier: GPL-2.0-or-later
// DE25 pc386: DMA controller 2 as its register file only, without the
// fes_dma8237 transfer engine (derive.py retarget_at_core puts it in
// pc386_at_core's dma2). The DE10 keeps the full controller.
//
// Nothing in the machine raises a controller-2 DREQ, so with no software
// request this reads back exactly as fes_dma8237 does: base addresses and
// counts through the byte flip-flop, the status register's request bits, 0
// from the temporary register and FFh elsewhere.
//
// The limit: it never runs a cycle. A software request on channels 4-7 (or
// command bit 6 inverting the DREQ sense, which on the full controller turns
// every unmasked channel into a request) starts no service, so that
// channel's current address and count do not count down, no TC status bit is
// set, and no terminal count clears the request bit. Mode, mask and page
// writes are accepted and change no read. No title or test depends on
// controller-2 transfers; devices_tb checks readback only.
module pc386_dma2_registers (
    input wire clk, reset,
    input wire reg_read, reg_write,
    input wire [3:0] reg_address,
    input wire [7:0] reg_wdata,
    output reg [7:0] reg_rdata
);
    reg [15:0] address [0:3], count [0:3];
    reg [3:0] software_request;
    reg dreq_low, byte_high;
    integer n;
    always @* begin
        reg_rdata = 8'hff;
        if (!reg_address[3]) begin
            if (reg_address[0])
                reg_rdata = byte_high ? count[reg_address[2:1]][15:8] : count[reg_address[2:1]][7:0];
            else
                reg_rdata = byte_high ? address[reg_address[2:1]][15:8] : address[reg_address[2:1]][7:0];
        end else if (reg_address == 8) reg_rdata = {software_request | {4{dreq_low}}, 4'b0000};
        else if (reg_address == 13) reg_rdata = 0;
    end
    always @(posedge clk) begin
        if (reset) begin
            software_request <= 0; dreq_low <= 0; byte_high <= 0;
            for (n = 0; n < 4; n = n + 1) begin address[n] <= 0; count[n] <= 0; end
        end else begin
            if (reg_read && !reg_address[3]) byte_high <= !byte_high;
            if (reg_write) begin
                if (!reg_address[3]) begin
                    if (reg_address[0]) begin
                        if (byte_high) count[reg_address[2:1]][15:8] <= reg_wdata;
                        else count[reg_address[2:1]][7:0] <= reg_wdata;
                    end else begin
                        if (byte_high) address[reg_address[2:1]][15:8] <= reg_wdata;
                        else address[reg_address[2:1]][7:0] <= reg_wdata;
                    end
                    byte_high <= !byte_high;
                end else case (reg_address)
                    8: dreq_low <= reg_wdata[6];
                    9: software_request[reg_wdata[1:0]] <= reg_wdata[2];
                    12: byte_high <= 0;
                    13: begin dreq_low <= 0; software_request <= 0; byte_high <= 0; end
                    default: begin end
                endcase
            end
        end
    end
endmodule
