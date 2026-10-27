// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum RAM: 48 KiB (three 16 KiB banks) or 128 KiB (eight banks),
// addressed as {bank, 14-bit offset}. Port A is the CPU, port B the ULA screen
// fetch; both run on the system clock. Each bank is its own memory, so the
// ULA port reads the screen banks (5, and 7 when there are eight) at one
// offset in parallel. Each read has two registered stages, so data follows
// its address by two clocks. The 48K map keeps the 128K bank
// numbers: bank 5 at $4000, 2 at $8000 and 0 at $C000, stored as banks 0, 1
// and 2.
module spectrum_ram #(
    parameter integer BANKS = 8
) (
    input  wire        clk,
    input  wire        cpu_we,
    input  wire [2:0]  cpu_bank,
    input  wire [13:0] cpu_offset,
    input  wire [7:0]  cpu_wdata,
    output reg  [7:0]  cpu_rdata = 8'h00,
    input  wire [13:0] ula_offset,
    output reg  [7:0]  ula5_rdata = 8'h00,
    output reg  [7:0]  ula7_rdata = 8'h00
);
    // Stored bank for a Spectrum bank number.
    function [2:0] stored(input [2:0] number);
        stored = BANKS > 4 ? number : number == 3'd5 ? 3'd0 : number == 3'd2 ? 3'd1 : 3'd2;
    endfunction

    wire [7:0] cpu_raw [0:BANKS-1];
    reg [7:0] ula5_q = 8'h00, ula7_q = 8'h00;
    genvar b;
    generate for (b = 0; b < BANKS; b = b + 1) begin : bank
        localparam [2:0] NUMBER = BANKS > 4 ? b : b == 0 ? 3'd5 : b == 1 ? 3'd2 : 3'd0;
        (* ramstyle = "M10K" *) reg [7:0] mem [0:16383];
        reg [7:0] cpu_q = 8'h00;
`ifdef VERILATOR
        integer init_i;
        initial for (init_i = 0; init_i < 16384; init_i = init_i + 1) mem[init_i] = 8'h00;
`endif
        always @(posedge clk) begin
            if (cpu_we && cpu_bank == NUMBER)
                mem[cpu_offset] <= cpu_wdata;
            cpu_q <= mem[cpu_offset];
        end
        assign cpu_raw[b] = cpu_q;
        if (NUMBER == 3'd5) begin : screen5
            always @(posedge clk) ula5_q <= mem[ula_offset];
        end else if (NUMBER == 3'd7) begin : screen7
            always @(posedge clk) ula7_q <= mem[ula_offset];
        end
    end endgenerate

    reg [2:0] cpu_stored_q = 3'd0;
    always @(posedge clk) begin
        cpu_stored_q <= stored(cpu_bank);
        cpu_rdata <= cpu_raw[cpu_stored_q];
        ula5_rdata <= ula5_q;
        ula7_rdata <= ula7_q;
    end
endmodule
