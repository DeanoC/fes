// SPDX-License-Identifier: GPL-2.0-or-later
// One byte lane of a word-addressed M10K memory. Port A reads and writes
// (the CPU); port B only reads (the raster). Both reads are synchronous. A
// port-A write returns the new data on port A, as the registered M10K
// true-dual-port mode does; the CPU never depends on that value.
module fes_riscv_lane_ram #(
    parameter integer DEPTH = 8192,
    parameter integer ADDR_WIDTH = 13,
    parameter INIT_FILE = ""
) (
    input  wire                  clk,
    input  wire [ADDR_WIDTH-1:0] a_addr,
    input  wire [7:0]            a_wdata,
    input  wire                  a_we,
    output reg  [7:0]            a_q,
    input  wire [ADDR_WIDTH-1:0] b_addr,
    output reg  [7:0]            b_q
);
    (* ram_style = "m10k_tdp" *) reg [7:0] mem [0:DEPTH-1];

    initial begin
        if (INIT_FILE != "")
            $readmemh(INIT_FILE, mem);
    end

    always @(posedge clk) begin
        if (a_we) begin
            mem[a_addr] <= a_wdata;
            a_q <= a_wdata;
        end else begin
            a_q <= mem[a_addr];
        end
    end

    always @(posedge clk)
        b_q <= mem[b_addr];
endmodule
