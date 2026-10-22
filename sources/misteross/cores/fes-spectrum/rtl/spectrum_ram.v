// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum RAM: 48 KiB (three 16 KiB banks) or 128 KiB (eight banks),
// addressed as {bank, 14-bit offset}. Port A is the CPU, port B the ULA screen
// fetch; both run on the system clock. Each read has two registered stages
// (block read, then bank select), so data follows its address by two clocks.
// The 48K map keeps the 128K bank numbers: bank 5 at $4000, 2 at $8000 and 0
// at $C000, stored as local banks 0, 1 and 2.
module spectrum_ram #(
    parameter integer BANKS = 8
) (
    input  wire        clk,
    input  wire        cpu_we,
    input  wire [2:0]  cpu_bank,
    input  wire [13:0] cpu_offset,
    input  wire [7:0]  cpu_wdata,
    output reg  [7:0]  cpu_rdata = 8'h00,
    input  wire [2:0]  ula_bank,
    input  wire [13:0] ula_offset,
    output reg  [7:0]  ula_rdata = 8'h00
);
    localparam integer DEPTH = BANKS * 16384;
    localparam integer AW = BANKS > 4 ? 17 : 16;

    function [AW-1:0] local_address(input [2:0] bank, input [13:0] offset);
        reg [1:0] local_bank;
        begin
            if (BANKS > 4) local_address = {bank, offset};
            else begin
                local_bank = bank == 3'd5 ? 2'd0 : bank == 3'd2 ? 2'd1 : 2'd2;
                local_address = {local_bank, offset};
            end
        end
    endfunction

    (* ramstyle = "M10K" *) reg [7:0] mem [0:DEPTH-1];
    reg [7:0] cpu_raw = 8'h00;
    reg [7:0] ula_raw = 8'h00;

`ifdef VERILATOR
    integer init_i;
    initial begin
        for (init_i = 0; init_i < DEPTH; init_i = init_i + 1)
            mem[init_i] = 8'h00;
    end
`endif

    wire [AW-1:0] cpu_address = local_address(cpu_bank, cpu_offset);
    wire [AW-1:0] ula_address = local_address(ula_bank, ula_offset);

    always @(posedge clk) begin
        if (cpu_we)
            mem[cpu_address] <= cpu_wdata;
        cpu_raw <= mem[cpu_address];
        cpu_rdata <= cpu_raw;
    end

    always @(posedge clk) begin
        ula_raw <= mem[ula_address];
        ula_rdata <= ula_raw;
    end
endmodule
